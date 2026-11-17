package actions

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

func (l *authority) ApplyTxnRecord(c platform.Caller, r *pb.ChangeRecord, e platform.Entity, verb, id string, payload json.RawMessage, now time.Time) (refusal *platform.Error) {
	if c.TraceEnabled() {
		before, _ := c.ReadRecord(e.Type, id, now)
		defer func() {
			event := platform.DecisionTrace{Kind: "transaction-change", Type: e.Type, ID: id, Operation: verb, Before: before}
			if refusal != nil {
				event.Refusal = refusal.Message
			} else {
				event.After, _ = c.ReadRecord(e.Type, id, now)
			}
			c.Trace(event)
		}()
	}
	if !c.Staging() || c.Replaying || c.App != l.authority || r.GetSubmission().GetAuthority() != c.App ||
		r.GetSubmission().GetTenantId() != l.tenant || r.GetSubmission().GetPrincipalId() != c.ID || r.GetChangeId() == "" || id == "" || !strings.HasPrefix(e.Type, c.App+".") ||
		(verb != "create" && verb != "edit" && verb != "archive") {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction step is outside its private authority / 事务步骤超出私有权威范围")
	}
	s := proto.Clone(r.GetSubmission()).(*pb.Submission)
	s.Schema = &pb.SchemaRef{Name: e.Type + "." + verb, Version: 1}
	s.Target = &pb.EntityRef{Type: e.Type, Id: id}
	s.Payload = payload
	catalog := l.catalogFor(c)
	a, ok := catalog.Action(s.Schema.Name)
	action, enabled := builtinAction(e, verb)
	if !ok || a.Target != e.Type || !enabled {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Transaction step action is unavailable / 事务步骤动作不可用")
	}
	authorization := s
	invocation := c.TransactionInput(r)
	if verb == "edit" && e.Type == invocation.GetTarget().GetType() && id == invocation.GetTarget().GetId() {
		original := invocation.GetSchema().GetName()
		declared := original == e.Type+".create" && e.CreateTxn != nil
		if e.Lifecycle != nil {
			declared = declared || slices.ContainsFunc(e.Lifecycle.Actions, func(t platform.ActionType) bool {
				return original == e.Type+"."+t.Name && slices.ContainsFunc(t.Rules, func(rule platform.ActionRule) bool { return rule.Transaction != nil && rule.Transaction.Spec != nil })
			})
		}
		if declared {
			authorization = invocation
		}
	}
	decision := catalog.DecideSubmission(c, authorization, now)
	if !decision.Allow {
		layer := "verb"
		if len(decision.Layers) > 0 {
			layer = decision.Layers[len(decision.Layers)-1].Name
		}
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", layer, decision.Reason)
	}
	info, _ := records.Describe(c.App, e, func(_ reflect.Type) string { return "" })
	var given map[string]json.RawMessage
	if json.Unmarshal(payload, &given) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction fields / 事务字段无效")
	}
	for name, raw := range given {
		if f, found := info.Field(name); found && f.Type == "reference" && f.Ref != "" {
			var ref string
			if json.Unmarshal(raw, &ref) != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction reference / 事务引用无效")
			}
			if ref != "" && !c.Readable(f.Ref+"/"+ref) {
				return notFound()
			}
		}
	}
	if raw, exists := given["state"]; exists {
		var state string
		if verb != "create" && (e.Type != invocation.GetTarget().GetType() || id != invocation.GetTarget().GetId()) || e.Lifecycle == nil || e.Lifecycle.Field != "state" || json.Unmarshal(raw, &state) != nil || !slices.ContainsFunc(e.Lifecycle.States, func(s platform.State) bool { return s.Name == state }) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Transaction state must be a declared state of its subject")
		}
		delete(given, "state")
		payload = platform.Raw(given)
		s.Payload = payload
		compute := e.Compute
		e.Compute = func(value any) {
			if compute != nil {
				compute(value)
			}
			reflect.ValueOf(value).Elem().FieldByName("State").SetString(state)
		}
	}
	apply, err := l.checked(c, s, now, catalog, func() (func(*pb.ChangeRecord), *platform.Error) { return executeAction(c, e, action, s, now) })()
	if err != nil {
		return err
	}
	apply(r)
	return nil
}

func (l *authority) ApplyTxnRoute(c platform.Caller, r *pb.ChangeRecord, e platform.Entity, target platform.TxnRef, now time.Time, run func(platform.Caller, *pb.ChangeRecord, *platform.BusinessTxn, time.Time) *platform.Error) *platform.Error {
	if !c.Staging() || c.Replaying || c.App != l.authority || target.Type != e.Type || target.Action == "" || target.ID == "" || e.Lifecycle == nil || run == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Refusal route requires its original transaction owner / 拒绝改路须经原事务归属方")
	}
	index := slices.IndexFunc(e.Lifecycle.Actions, func(t platform.ActionType) bool { return t.Name == target.Action })
	if index < 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Refusal route transaction is unavailable / 拒绝改路目标事务不可用")
	}
	tr := e.Lifecycle.Actions[index]
	ruleIndex := slices.IndexFunc(tr.Rules, func(rule platform.ActionRule) bool { return rule.Transaction != nil && rule.Transaction.Spec != nil })
	if ruleIndex < 0 || tr.Approval != nil || tr.Adoption != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal routes require an ordinary declared transaction / 拒绝改路须指向已声明的普通事务")
	}
	c.Automation = false
	routed := proto.Clone(r).(*pb.ChangeRecord)
	routed.Submission.Schema = &pb.SchemaRef{Name: target.Type + "." + target.Action, Version: 1}
	routed.Submission.Target = &pb.EntityRef{Type: target.Type, Id: target.ID}
	catalog := l.catalogFor(c)
	decision := catalog.DecideSubmission(c, routed.Submission, now)
	if !decision.Allow {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Refusal route authorization denied: {reason} / 拒绝改路授权被拒：{reason}", decision.Reason, decision.Reason)
	}
	var failure *platform.Error
	tr.Rules = slices.Clone(tr.Rules)
	txn := *tr.Rules[ruleIndex].Transaction
	txn.Run = func(c platform.Caller, receipt *pb.ChangeRecord, at time.Time) *platform.Error {
		failure = run(c.WithTransactionInput(routed.Submission), r, txn.Spec, at)
		return failure
	}
	tr.Rules[ruleIndex].Transaction = &txn
	apply, err := l.checked(c, routed.Submission, now, catalog, func() (func(*pb.ChangeRecord), *platform.Error) {
		return executeAction(c, e, tr, routed.Submission, now)
	})()
	if err != nil {
		return err
	}
	apply(r)
	return failure
}
