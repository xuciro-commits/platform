package platform

import (
	"encoding/json"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

type RuleCriterion struct {
	Expr    Expr   `json:"expr"`
	Message string `json:"message"`
}

// RuleCriteria is the original action's pure precondition plan. It is exposed
// to model reads by the owner and runs before mutation through the same helper.
type RuleCriteria struct {
	Reads      RuleReadSpec    `json:"reads"`
	Conditions []RuleCriterion `json:"conditions"`
}

func EvaluateRule(expr Expr, reads RuleReadSpec, caller Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (EntityInfo, bool)) (result ExprResult, refusal *kernel.Error) {
	var observed *ExprContext
	if caller.TraceEnabled() {
		defer func() {
			event := DecisionTrace{Kind: "rule", Type: reads.Object, Expression: &expr, Reads: observed, Result: &result}
			if refusal != nil {
				event.Refusal = refusal.Message
			}
			caller.Trace(event)
		}()
	}
	if _, supported := caller.rt.(AuthorizationRuntime); supported && !caller.Replaying && reads.Object != "" {
		var subject struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(Raw(record), &subject)
		var check func(Expr) *kernel.Error
		check = func(value Expr) *kernel.Error {
			if value.Ref != "" && !strings.Contains(value.Ref, ".") && subject.ID != "" {
				decision := caller.Authorize(AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: reads.Object, Record: subject.ID, Fields: []string{value.Ref}}, at)
				if !decision.Allow || len(decision.Withheld) > 0 {
					return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule field is outside the caller's permissions")
				}
			}
			for _, arg := range value.Args {
				if err := check(arg); err != nil {
					return err
				}
			}
			if value.In != nil {
				return check(*value.In)
			}
			return nil
		}
		if err := check(expr); err != nil {
			return ExprResult{}, err
		}
	}
	schema, err := RuleSchema(expr, reads, lookup)
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule: {reason} / 规则：{reason}", err.Error(), err.Error())
	}
	context, err := caller.RuleContext(expr, reads, lookup, record, inputs, at)
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule input: {reason} / 规则输入：{reason}", err.Error(), err.Error())
	}
	if caller.TraceEnabled() {
		filtered := ruleTraceContext(expr, context)
		observed = &filtered
	}
	program, err := CompileExpr(expr, schema)
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	value, err := program.Eval(context)
	if err != nil {
		code := pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
		if issue, ok := err.(*ExprIssue); ok && issue.Code == "withheld" {
			code = pb.ErrorCode_ERROR_CODE_POLICY_DENIED
		}
		return value, Refuse(code, "Rule: {reason} / 规则：{reason}", err.Error(), err.Error())
	}
	return value, nil
}

func (plan RuleCriteria) Check(c Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (EntityInfo, bool)) *kernel.Error {
	matched, reason, err := plan.Evaluate(c, record, inputs, at, lookup)
	if err != nil {
		return err
	}
	if !matched {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{message}", reason)
	}
	return nil
}

func (plan RuleCriteria) Evaluate(c Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (EntityInfo, bool)) (bool, string, *kernel.Error) {
	for _, condition := range plan.Conditions {
		value, err := EvaluateRule(condition.Expr, plan.Reads, c, record, inputs, at, lookup)
		if err != nil {
			return false, "", err
		}
		if string(value.Value.Data) != "true" {
			reason := condition.Message
			for _, issue := range value.Reasons {
				reason += " · " + issue.At + ": " + issue.Message
			}
			return false, strings.TrimSpace(reason), nil
		}
	}
	return true, "", nil
}
