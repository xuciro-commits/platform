package actions

import (
	"encoding/json"
	"log/slog"
	"maps"
	"reflect"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/objects"
)

const unavailableAction = "The action could not be checked"

func transitionRefusal(c platform.Caller, action platform.ActionType, typ, id, state string, archived bool, now time.Time) *platform.Error {
	if !c.Replaying && !c.Automation {
		read := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: []string{action.Criteria.State.Field}}, now)
		if !read.Allow || len(read.Withheld) != 0 {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
	}
	if archived {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is archived", id)
	}
	pending := action.Approval != nil && action.Approval.Pending != "" && state == action.Approval.Pending && !c.Automation && c.Staging() && !c.Probing()
	if !pending && !action.Criteria.State.Holds(state, c.Rules()) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{record} is {state}; {transition} takes it only from {states}", id, state, action.Name, strings.Join(action.Criteria.State.In, ", "))
	}
	return nil
}

func transactionSubject(c platform.Caller, typ, id string, at time.Time) (map[string]any, *platform.Error) {
	raw, refusal := c.ReadRecord(typ, id, at)
	if refusal != nil {
		return nil, refusal
	}
	subject, err := decodeTxnValues(raw)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction subject")
	}
	return subject, nil
}

func transactionReadSpec(txn *platform.BusinessTxn, inputs []platform.Field) platform.RuleReadSpec {
	spec := platform.RuleReadSpec{Object: txn.Subject, Inputs: map[string]platform.ExprType{"$subject": {Kind: "reference", Object: txn.Subject}}}
	if txn.Reads != nil {
		spec.Collections = txn.Reads.Collections
	}
	for _, in := range inputs {
		kind := platform.RuleFieldKind(in.Type)
		if in.Ref != "" {
			kind = "reference"
		} else if len(in.Choices) > 0 {
			kind = "choice"
		}
		if kind == "string" {
			kind = "text"
		}
		spec.Inputs[in.Name] = platform.ExprType{Kind: kind, Object: in.Ref}
	}
	return spec
}

func transactionGuard(c platform.Caller, txn *platform.BusinessTxn, spec platform.RuleReadSpec, subject, values map[string]any, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool)) (string, *platform.Error) {
	if txn.Guard == nil {
		return "", nil
	}
	if info, found := lookup(spec.Object); found {
		subject = maps.Clone(subject)
		for _, field := range info.Fields {
			if _, present := subject[field.Name]; present {
				continue
			}
			switch field.Type {
			case "text", "longtext", "choice", "reference":
				subject[field.Name] = ""
			}
		}
	}
	allowed, refusal := protocol.GuardSatisfied(*txn.Guard, spec, c, subject, values, now, lookup)
	if refusal != nil && refusal.Code != pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
		slog.Warn(unavailableAction, "object", txn.Subject, "code", refusal.Code.String())
		return "", refusal
	}
	if refusal != nil || allowed {
		return "", refusal
	}
	return txn.Message, nil
}

type ActionConditionCheck struct {
	Allowed     bool   `json:"allowed"`
	NeedsInputs bool   `json:"needsInputs,omitempty"`
	Message     string `json:"message"`
}

type ActionCheckResult struct {
	Impact        *objects.ObjectPublishImpact `json:"impact,omitempty"`
	Allowed       bool                         `json:"allowed"`
	Message       string                       `json:"message,omitempty"`
	Conditions    []ActionConditionCheck       `json:"conditions"`
	Issues        []platform.FieldIssue        `json:"issues,omitempty"`
	Authorization *platform.AuthzDecision      `json:"authorization,omitempty"`
}

func guardNeedsInputs(e platform.Expr, spec platform.RuleReadSpec, values map[string]any) bool {
	if name, input := strings.CutPrefix(e.Ref, "input."); input {
		root, _, _ := strings.Cut(name, ".")
		return values[root] == nil
	}
	if e.Agg != nil {
		of := spec.Collections[e.Agg.Source].Of
		if _, known := spec.Inputs[of]; known && values[of] == nil {
			return true
		}
	}
	for _, arg := range e.Args {
		if guardNeedsInputs(arg, spec, values) {
			return true
		}
	}
	return e.In != nil && guardNeedsInputs(*e.In, spec, values)
}

func checkAction(c platform.Caller, manifest platform.Manifest, action platform.Action, sub *pb.Submission, read func() (map[string]any, *platform.Error), now time.Time, lookup func(string) (platform.RecordTypeInfo, bool)) ActionCheckResult {
	result := ActionCheckResult{Conditions: []ActionConditionCheck{}}
	decision := manifest.Actions.DecideSubmission(c, sub, now)
	if !decision.Allow {
		return result
	}
	result.Authorization = &decision
	if len(sub.GetPayload()) != 0 {
		var supplied map[string]any
		json.Unmarshal(sub.GetPayload(), &supplied)
		fields, ok := platform.ResolveValueTypes(action.Payload, c.ResolveValueType)
		if !ok {
			result.Message = unavailableAction
			return result
		}
		result.Issues = protocol.InputIssues(fields, supplied)
		if len(result.Issues) != 0 {
			result.Message = "Correct the highlighted fields."
			return result
		}
	}
	if action.New {
		result.Allowed = true
		return result
	}
	subject, refusal := read()
	if refusal != nil {
		if refusal.Code == pb.ErrorCode_ERROR_CODE_NOT_FOUND || refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
			result.Authorization = nil
			return result
		}
		slog.Warn(unavailableAction, "action", action.Schema, "code", refusal.Code.String())
		result.Message = unavailableAction
		return result
	}
	if action.Schema == action.Target+".restore" && action.Reverse == action.Target+".archive" {
		archived, _ := subject["archived"].(bool)
		if refusal := restoreRefusal(archived); refusal != nil {
			result.Message = refusal.Message
			return result
		}
	}
	for _, entity := range manifest.Entities {
		if entity.Type != action.Target || entity.Lifecycle == nil {
			continue
		}
		transition, found := recordAction(entity, action.Schema)
		if !found || transition.Criteria.State == nil {
			break
		}
		state, _ := subject[transition.Criteria.State.Field].(string)
		archived, _ := subject["archived"].(bool)
		if refusal := transitionRefusal(c, transition, action.Target, sub.GetTarget().GetId(), state, archived, now); refusal != nil {
			if refusal.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				result.Authorization = nil
			} else {
				result.Message = refusal.Message
			}
			return result
		}
		for _, rule := range transition.Rules {
			if rule.Transaction == nil || rule.Transaction.Spec == nil || rule.Transaction.Spec.Guard == nil {
				continue
			}
			txn := rule.Transaction.Spec
			spec := transactionReadSpec(txn, transition.Parameters)
			inputs, _ := decodeTxnValues(sub.GetPayload())
			inputs["$subject"] = sub.GetTarget().GetId()
			if guardNeedsInputs(*txn.Guard, spec, inputs) {
				result.Conditions = append(result.Conditions, ActionConditionCheck{Allowed: true, NeedsInputs: true})
				continue
			}
			reason, failure := transactionGuard(c, txn, spec, subject, inputs, now, lookup)
			if failure != nil {
				if failure.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
					result.Authorization = nil
				} else {
					result.Message = failure.Message
				}
				return result
			}
			result.Conditions = append(result.Conditions, ActionConditionCheck{Allowed: reason == "", Message: reason})
			if reason != "" {
				result.Message = reason
				return result
			}
		}
		break
	}
	result.Allowed = true
	return result
}

func restoreRefusal(archived bool) *platform.Error {
	if !archived {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Only archived records can be restored")
	}
	return nil
}

func readActionSubject(c platform.Caller, typ, id string, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool)) (map[string]any, *platform.Error) {
	subject, refusal := transactionSubject(c, typ, id, now)
	if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_NOT_FOUND {
		return subject, refusal
	}
	if info, ok := lookup(typ); ok {
		if decision := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id}, now); decision.Allow {
			if record, exists := c.RecordValue(info.Go, id); exists && reflect.ValueOf(record).Field(0).Interface().(platform.Record).Archived {
				return map[string]any{"id": id, "archived": true}, nil
			}
		}
	}
	return subject, refusal
}

func CheckAction(owner platform.Service, c platform.Caller, action platform.Action, id string, inputs json.RawMessage, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool), say func(string) string) (ActionCheckResult, *platform.Error) {
	if len(inputs) != 0 && string(inputs) != "null" {
		value, err := platform.DecodeValue(inputs, 64<<10)
		if _, ok := value.(map[string]any); err != nil || !ok {
			return ActionCheckResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Action inputs must be one JSON object")
		}
	} else {
		inputs = nil
	}
	sub := &pb.Submission{Target: &pb.EntityRef{Type: action.Target, Id: id}, Schema: &pb.SchemaRef{Name: action.Schema}, Payload: inputs}
	result := checkAction(c, owner.Manifest(), action, sub, func() (map[string]any, *platform.Error) {
		return readActionSubject(c, action.Target, id, now, lookup)
	}, now, lookup)
	result.Message = say(result.Message)
	for i := range result.Conditions {
		result.Conditions[i].Message = say(result.Conditions[i].Message)
	}
	return result, nil
}

func RecordActions(owner platform.Service, c platform.Caller, typ, id string, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool), say func(string) string) []objects.RecordAction {
	if owner == nil {
		return nil
	}
	return recordActions(owner.Manifest(), c, typ, id, now, lookup, say)
}

func RecordPageActions(owner platform.Service, c platform.Caller, typ string, rows []any, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool), say func(string) string) map[string][]objects.RecordAction {
	if owner == nil {
		return nil
	}
	manifest := owner.Manifest()
	out := make(map[string][]objects.RecordAction, len(rows))
	for _, row := range rows {
		id := reflect.ValueOf(row).Field(0).Interface().(platform.Record).ID
		out[id] = recordActions(manifest, c, typ, id, now, lookup, say)
		if out[id] == nil {
			out[id] = []objects.RecordAction{}
		}
	}
	return out
}

func recordActions(manifest platform.Manifest, c platform.Caller, typ, id string, now time.Time, lookup func(string) (platform.RecordTypeInfo, bool), say func(string) string) []objects.RecordAction {
	var read bool
	var subject map[string]any
	var refusal *platform.Error
	var out []objects.RecordAction
	for _, entry := range manifest.Actions.All() {
		if entry.Target != typ || entry.New || entry.ConfirmationOnly {
			continue
		}
		sub := &pb.Submission{Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: entry.Schema}}
		result := checkAction(c, manifest, entry, sub, func() (map[string]any, *platform.Error) {
			if !read {
				subject, refusal = readActionSubject(c, typ, id, now, lookup)
				read = true
			}
			return subject, refusal
		}, now, lookup)
		if result.Authorization != nil {
			out = append(out, objects.RecordAction{Schema: entry.Schema, Allowed: result.Allowed, Reason: say(result.Message)})
		}
	}
	return out
}
