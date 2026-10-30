package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func ruleInputs(expr platform.Expr, into map[string]bool) {
	if name, ok := strings.CutPrefix(expr.Ref, "input."); ok {
		root, _, _ := strings.Cut(name, ".")
		into[root] = true
	}
	for _, arg := range expr.Args {
		ruleInputs(arg, into)
	}
	if expr.In != nil {
		ruleInputs(*expr.In, into)
	}
}

func (t *Tenant) actionCheck(m platform.Member, schema string, request ActionCheckRequest, at time.Time, lang string) (ActionCheckResult, *kernel.Error) {
	appID, action, ok := hostView{t: t}.Action(schema)
	if !ok || action.Target != request.Target.Object {
		return ActionCheckResult{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	transition, lifecycle, hasTransition := t.modelTransition(platform.AssetRef{App: appID, Kind: platform.AssetAction, Name: schema})
	caller := platform.NewCaller(runtime{t}, m, appID, false, false)
	decision := t.authorizeFrom(t.records, caller, platform.AuthzRequest{Principal: m.ID, Tenant: t.ID, Kind: "action", Action: schema, Type: request.Target.Object, Record: request.Target.ID}, at, false)
	result := ActionCheckResult{Allowed: decision.Allow, Conditions: []ActionConditionCheck{}, Authorization: &decision}
	say := func(message string) string { return t.i18n.SayInApp(appID, lang, message) }
	if !decision.Allow {
		result.Message = say(decision.Reason)
		return result, nil
	}
	var record map[string]any
	if !action.New {
		raw, refusal := t.readRecordFrom(t.records, caller, request.Target.Object, request.Target.ID, at)
		if refusal != nil {
			return ActionCheckResult{}, refusal
		}
		value, err := platform.DecodeValue(raw, 1<<20)
		if err != nil {
			return ActionCheckResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Action target is unavailable")
		}
		record, _ = value.(map[string]any)
		if record == nil {
			return ActionCheckResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Action target is unavailable")
		}
	}
	if hasTransition {
		state, _ := record[lifecycle].(string)
		if !slices.Contains(transition.From, state) && (transition.Approval == nil || transition.Approval.Pending != state) {
			result.Allowed = false
			result.Message = "The action is unavailable while this record is " + state + "."
		}
	}
	input := request.Inputs
	supplied := len(input) > 0 && string(input) != "null"
	if !supplied {
		input = json.RawMessage("{}")
	}
	given, err := platform.DecodeValue(input, 64<<10)
	inputs, valid := given.(map[string]any)
	if err != nil || !valid {
		return ActionCheckResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Action inputs must be one JSON object")
	}
	supplied = supplied && len(inputs) > 0
	result.Issues = platform.InputIssues(action.Payload, inputs)
	if len(result.Issues) > 0 && supplied {
		result.Allowed = false
		if result.Message == "" {
			result.Message = "Correct the highlighted fields."
		}
	}
	if hasTransition && transition.Criteria != nil {
		for _, condition := range transition.Criteria.Conditions {
			check := ActionConditionCheck{Message: condition.Message}
			read := map[string]bool{}
			ruleInputs(condition.Expr, read)
			for name := range read {
				if value, present := inputs[name]; !present || value == nil {
					check.NeedsInputs = true
				}
			}
			if check.NeedsInputs {
				check.Allowed, check.Message = true, ""
				result.Conditions = append(result.Conditions, check)
				continue
			}
			outcome, refusal := platform.EvaluateRule(condition.Expr, transition.Criteria.Reads, caller, record, inputs, at, t.entity)
			if refusal != nil {
				check.Message = refusal.Message
			} else {
				check.Allowed = string(outcome.Value.Data) == "true"
				if !check.Allowed {
					for _, issue := range outcome.Reasons {
						check.Message += " · " + issue.At + ": " + issue.Message
					}
				}
			}
			result.Conditions = append(result.Conditions, check)
			if !check.Allowed {
				result.Allowed = false
				if result.Message == "" {
					result.Message = check.Message
				}
			}
		}
	}
	result.Issues = t.localizedFieldIssues(m, result.Issues, lang)
	result.Message = say(result.Message)
	for i := range result.Conditions {
		result.Conditions[i].Message = say(result.Conditions[i].Message)
	}
	return result, nil
}
