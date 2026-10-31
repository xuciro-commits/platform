package platform

import (
	"encoding/json"
	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strings"
	"time"
)

func (l *Ledger) ApplyTxnRecord(c Caller, r *pb.ChangeRecord, e Entity, verb, id string, payload json.RawMessage, now time.Time) (refusal *kernel.Error) {
	if c.TraceEnabled() {
		before, _ := c.ReadRecord(e.Type, id, now)
		defer func() {
			event := DecisionTrace{Kind: "transaction-change", Type: e.Type, ID: id, Operation: verb, Before: before}
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
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction step is outside its private authority / 事务步骤超出私有权威范围")
	}
	s := proto.Clone(r.GetSubmission()).(*pb.Submission)
	s.Schema = &pb.SchemaRef{Name: e.Type + "." + verb, Version: 1}
	s.Target = &pb.EntityRef{Type: e.Type, Id: id}
	s.Payload = payload
	catalog := l.catalogFor(c)
	a, ok := catalog.Action(s.Schema.Name)
	if !ok || a.Target != e.Type || verb == "create" && !e.Standard.Create || verb == "edit" && !e.Standard.Edit || verb == "archive" && !e.Standard.Archive {
		return Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Transaction step action is unavailable / 事务步骤动作不可用")
	}
	c.Automation = false
	authorization := s
	invocation := c.TransactionInput(r)
	if verb == "edit" && e.Type == invocation.GetTarget().GetType() && id == invocation.GetTarget().GetId() {
		original := invocation.GetSchema().GetName()
		declared := original == e.Type+".create" && e.CreateTxn != nil
		if e.Lifecycle != nil {
			declared = declared || slices.ContainsFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Txn != nil && original == e.Type+"."+t.Name })
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
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", layer, decision.Reason)
	}
	info, _ := Describe(c.App, e, func(_ reflect.Type) string { return "" })
	var given map[string]json.RawMessage
	if json.Unmarshal(payload, &given) != nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction fields / 事务字段无效")
	}
	for name, raw := range given {
		if f, found := info.Field(name); found && f.Type == "reference" && f.Ref != "" {
			var ref string
			if json.Unmarshal(raw, &ref) != nil {
				return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction reference / 事务引用无效")
			}
			if ref != "" && !c.rt.Readable(c, f.Ref+"/"+ref) {
				return notFound()
			}
		}
	}
	if raw, exists := given["state"]; exists {
		var state string
		if e.Type != invocation.GetTarget().GetType() || id != invocation.GetTarget().GetId() || e.Lifecycle == nil || e.Lifecycle.Field != "state" || json.Unmarshal(raw, &state) != nil || !slices.ContainsFunc(e.Lifecycle.States, func(s State) bool { return s.Name == state }) {
			return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Transaction state must be a declared state of its subject")
		}
		delete(given, "state")
		payload = Raw(given)
		s.Payload = payload
		compute := e.Compute
		e.Compute = func(value any) {
			if compute != nil {
				compute(value)
			}
			reflect.ValueOf(value).Elem().FieldByName("State").SetString(state)
		}
	}
	apply, err := l.checked(c, s, now, catalog, func() (func(*pb.ChangeRecord), *kernel.Error) { return standard(c, e, verb, s, now) })()
	if err != nil {
		return err
	}
	apply(r)
	return nil
}

func (l *Ledger) ApplyTxnRoute(c Caller, r *pb.ChangeRecord, e Entity, target TxnRef, now time.Time, run func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error) *kernel.Error {
	if !c.Staging() || c.Replaying || c.App != l.authority || target.Type != e.Type || target.Action == "" || target.ID == "" || e.Lifecycle == nil || run == nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Refusal route requires its original transaction owner / 拒绝改路须经原事务归属方")
	}
	index := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == target.Action })
	if index < 0 {
		return Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Refusal route transaction is unavailable / 拒绝改路目标事务不可用")
	}
	tr := e.Lifecycle.Transitions[index]
	if tr.Txn == nil || tr.Approval != nil || tr.Adoption != nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal routes require an ordinary declared transaction / 拒绝改路须指向已声明的普通事务")
	}
	c.Automation = false
	routed := proto.Clone(r).(*pb.ChangeRecord)
	routed.Submission.Schema = &pb.SchemaRef{Name: target.Type + "." + target.Action, Version: 1}
	routed.Submission.Target = &pb.EntityRef{Type: target.Type, Id: target.ID}
	catalog := l.catalogFor(c)
	decision := catalog.DecideSubmission(c, routed.Submission, now)
	if !decision.Allow {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Refusal route authorization denied: {reason} / 拒绝改路授权被拒：{reason}", decision.Reason, decision.Reason)
	}
	var failure *kernel.Error
	tr.ApplyTxn = func(c Caller, receipt *pb.ChangeRecord, at time.Time) *kernel.Error {
		failure = run(c.WithTransactionInput(routed.Submission), r, tr.Txn, at)
		return failure
	}
	apply, err := l.checked(c, routed.Submission, now, catalog, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return transition(c, e, tr, routed.Submission, now)
	})()
	if err != nil {
		return err
	}
	apply(r)
	return failure
}

type BooksChecks interface {
	CheckPosting(Caller, Journal) (Outcome, *kernel.Error)
	CheckReversal(Caller, string, json.RawMessage, time.Time) (Outcome, *kernel.Error)
	HistoricalPostingMessage(string) string
}

type TransactionHost interface {
	CheckTxnCall(TxnCall) error
}
