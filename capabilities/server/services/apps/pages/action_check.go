package pages

import (
	"encoding/json"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ActionCheckRequest struct {
	Target PageModelRecord `json:"target"`
	Inputs json.RawMessage `json:"inputs,omitempty"`
}

type ActionConditionCheck struct {
	Allowed     bool   `json:"allowed"`
	NeedsInputs bool   `json:"needsInputs,omitempty"`
	Message     string `json:"message"`
}

type ActionCheckResult struct {
	Allowed       bool                    `json:"allowed"`
	Message       string                  `json:"message,omitempty"`
	Conditions    []ActionConditionCheck  `json:"conditions"`
	Issues        []platform.FieldIssue   `json:"issues,omitempty"`
	Authorization *platform.AuthzDecision `json:"authorization,omitempty"`
}

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

type ActionChecks struct {
	Caller     platform.Caller
	Transition platform.Transition
	Lifecycle  string
	Moves      bool
	Read       func() (json.RawMessage, *kernel.Error)
	Entity     func(string) (platform.EntityInfo, bool)
	Say        func(string) string
	Localize   func([]platform.FieldIssue) []platform.FieldIssue
}

func (a ActionChecks) Check(action platform.Action, request ActionCheckRequest, decision platform.AuthzDecision, at time.Time) (ActionCheckResult, *kernel.Error) {
	caller, transition, lifecycle, hasTransition, read, entity, say, localize := a.Caller, a.Transition, a.Lifecycle, a.Moves, a.Read, a.Entity, a.Say, a.Localize
	result := ActionCheckResult{Allowed: decision.Allow, Conditions: []ActionConditionCheck{}, Authorization: &decision}
	if !decision.Allow {
		result.Message = say(decision.Reason)
		return result, nil
	}
	var record map[string]any
	if !action.New {
		raw, refusal := read()
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
			outcome, refusal := platform.EvaluateRule(condition.Expr, transition.Criteria.Reads, caller, record, inputs, at, entity)
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
	result.Issues = localize(result.Issues)
	result.Message = say(result.Message)
	for i := range result.Conditions {
		result.Conditions[i].Message = say(result.Conditions[i].Message)
	}
	return result, nil
}
