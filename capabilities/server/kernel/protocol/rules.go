package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func EvaluateRule(expr platform.Expr, reads platform.RuleReadSpec, caller platform.Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (platform.EntityInfo, bool)) (result platform.ExprResult, refusal *platform.Error) {
	var observed *platform.ExprContext
	if caller.TraceEnabled() {
		defer func() {
			event := platform.DecisionTrace{Kind: "rule", Type: reads.Object, Expression: &expr, Reads: observed, Result: &result}
			if refusal != nil {
				event.Refusal = refusal.Message
			}
			caller.Trace(event)
		}()
	}
	if caller.HasRuntime() && !caller.Replaying && reads.Object != "" {
		var subject struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(platform.Raw(record), &subject)
		var check func(platform.Expr) *platform.Error
		check = func(value platform.Expr) *platform.Error {
			if value.Ref != "" && !strings.Contains(value.Ref, ".") && subject.ID != "" {
				fields := []string{value.Ref}
				if value.Ref == "id" {
					fields = nil
				}
				decision := caller.Authorize(platform.AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: reads.Object, Record: subject.ID, Fields: fields}, at)
				if !decision.Allow || len(decision.Withheld) > 0 {
					return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule field is outside the caller's permissions")
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
			return platform.ExprResult{}, err
		}
	}
	schema, err := RuleSchema(expr, reads, lookup, caller.Rules())
	if err != nil {
		return platform.ExprResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Rule: {reason} / 规则：{reason}", err.Error(), err.Error())
	}
	context, err := RuleContext(caller, expr, reads, lookup, record, inputs, at)
	if err != nil {
		return platform.ExprResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Rule input: {reason} / 规则输入：{reason}", err.Error(), err.Error())
	}
	if caller.TraceEnabled() {
		filtered := ruleTraceContext(expr, context)
		observed = &filtered
	}
	program, err := caller.Rules().CompileExpr(expr, schema)
	if err != nil {
		return platform.ExprResult{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	value, err := program.Eval(context)
	value.Sources = slices.Clone(context.Sources)
	if err != nil {
		code := pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
		var issue *platform.ExprIssue
		if errors.As(err, &issue) && issue.Code == "withheld" {
			code = pb.ErrorCode_ERROR_CODE_POLICY_DENIED
		}
		return value, platform.Refuse(code, "Rule: {reason} / 规则：{reason}", err.Error(), err.Error())
	}
	return value, nil
}

func EvaluateCriteria(plan platform.RuleCriteria, c platform.Caller, record any, inputs map[string]any, at time.Time, lookup func(string) (platform.EntityInfo, bool)) (bool, string, *platform.Error) {
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

func DecideTable(plan platform.RuleTablePlan, facts platform.ExprContext, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) (json.RawMessage, error) {
	schemas := []platform.ExprSchema{}
	for _, row := range plan.Rows {
		schema, err := RuleSchema(row.Condition, plan.Reads, lookup, engines...)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return DecideTableSchemas(plan, facts, schemas, engines...)
}

func DecideTableSchemas(plan platform.RuleTablePlan, facts platform.ExprContext, schemas []platform.ExprSchema, engines ...platform.RuleEngine) (json.RawMessage, error) {
	var engine platform.RuleEngine = platform.NativeRules{}
	if len(engines) > 0 {
		engine = engines[0]
	}
	if len(schemas) != len(plan.Rows) {
		return nil, fmt.Errorf("decision table schemas are not bound")
	}
	for i, row := range plan.Rows {
		p, err := engine.CompileExpr(row.Condition, schemas[i])
		if err != nil {
			return nil, err
		}
		r, err := p.Eval(facts)
		if err != nil {
			return nil, err
		}
		if string(r.Value.Data) == "true" {
			return row.Result, nil
		}
	}
	if len(plan.Default) > 0 {
		return plan.Default, nil
	}
	return nil, fmt.Errorf("no decision table row matches the captured input")
}

func ruleTraceContext(expr platform.Expr, context platform.ExprContext) platform.ExprContext {
	out := platform.ExprContext{Refs: map[string]platform.ExprValue{}, Sets: map[string]platform.ExprCollection{}, Sources: context.Sources, Me: context.Me, Now: context.Now, Today: context.Today}
	var visit func(platform.Expr)
	visit = func(e platform.Expr) {
		if e.Ref != "" {
			if v, ok := context.Refs[e.Ref]; ok {
				out.Refs[e.Ref] = v
			}
		}
		if e.Window != nil {
			if out.Windows == nil {
				out.Windows = map[string]platform.ExprWindowSummary{}
			}
			out.Windows[e.Window.Source] = context.Windows[e.Window.Source]
		}
		if e.Agg != nil {
			out.Sets[e.Agg.Source] = context.Sets[e.Agg.Source]
		}
		for _, arg := range e.Args {
			visit(arg)
		}
		if e.In != nil {
			visit(*e.In)
		}
	}
	visit(expr)
	return out
}

func RuleAssignment(e platform.Expr, s platform.RuleReadSpec, c platform.Caller, record any, inputs map[string]any, now time.Time, lookup func(string) (platform.EntityInfo, bool), provenance ...*[]string) (any, *platform.Error) {
	r, err := EvaluateRule(e, s, c, record, inputs, now, lookup)
	for _, refs := range provenance {
		*refs = append(*refs, r.Sources...)
	}
	if err != nil {
		return nil, err
	}
	v, failure := ExprGoValue(r.Value)
	if failure != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
	}
	return v, nil
}
