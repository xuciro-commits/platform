package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/rules"
	"slices"
	"strings"
	"time"
)

type RuleCriterion struct {
	Expr    Expr   `json:"expr"`
	Message string `json:"message"`
}

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
				fields := []string{value.Ref}
				if value.Ref == "id" {
					fields = nil
				}
				decision := caller.Authorize(AuthzRequest{Principal: caller.ID, Tenant: caller.Tenant, Kind: "read", Type: reads.Object, Record: subject.ID, Fields: fields}, at)
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
	schema, err := RuleSchema(expr, reads, lookup, caller.Rules())
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
	program, err := caller.Rules().CompileExpr(expr, schema)
	if err != nil {
		return ExprResult{}, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	value, err := program.Eval(context)
	value.Sources = slices.Clone(context.Sources)
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
		return c.RefuseBusinessCondition("CRITERIA_NOT_SATISFIED", reason, record, inputs, at)
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

type RuleTablePlan struct {
	Reads   RuleReadSpec    `json:"reads"`
	Rows    []RuleTableRow  `json:"rows"`
	Default json.RawMessage `json:"default,omitempty"`
}

type RuleTableRow struct {
	Condition Expr            `json:"condition"`
	Result    json.RawMessage `json:"result"`
}

func (plan RuleTablePlan) Decide(facts ExprContext, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) (json.RawMessage, error) {
	schemas := []ExprSchema{}
	for _, row := range plan.Rows {
		schema, err := RuleSchema(row.Condition, plan.Reads, lookup, engines...)
		if err != nil {
			return nil, err
		}
		schemas = append(schemas, schema)
	}
	return plan.DecideSchemas(facts, schemas, engines...)
}

func (plan RuleTablePlan) DecideSchemas(facts ExprContext, schemas []ExprSchema, engines ...RuleEngine) (json.RawMessage, error) {
	var engine RuleEngine = NativeRules{}
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

type Expr = rules.Expr

type ExprLiteral = rules.Literal

type ExprAggregate = rules.Aggregate

type ExprWindowReference = rules.WindowReference

type ExprWindowType = rules.WindowType

type ExprWindowSummary = rules.WindowSummary

type ExprType = rules.Type

type ExprSetType = rules.SetType

type ExprSchema = rules.Schema

type ExprValue = rules.Value

type ExprContext = rules.Context

type ExprItem = rules.Item

type ExprCollection = rules.Collection

type ExprResult = rules.Result

type ExprIssue = rules.Issue

type ExprProgram interface {
	Eval(ExprContext) (ExprResult, error)
	EvalFrozenFormula(ExprContext) (ExprResult, error)
}

type ExprNumericAggregate interface {
	Add(ExprValue) error
	Result() (ExprValue, error)
}

type RuleEngine interface {
	CompileExpr(Expr, ExprSchema) (ExprProgram, error)
	CompileFrozenFormula(Expr, ExprSchema) (ExprProgram, error)
	CompareExprValues(ExprValue, ExprValue) (int, error)
	NewExprNumericAggregate(string, string, string) (ExprNumericAggregate, error)
}

type NativeRules struct{}

type RuleRuntime interface{ Rules() RuleEngine }

func (c Caller) Rules() RuleEngine {
	if runtime, ok := c.rt.(RuleRuntime); ok {
		return runtime.Rules()
	}
	return NativeRules{}
}

func (NativeRules) CompileExpr(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.Compile(expr, schema)
}

func (NativeRules) CompileFrozenFormula(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.CompileFrozenFormula(expr, schema)
}

func (NativeRules) CompareExprValues(a, b ExprValue) (int, error) {
	return rules.CompareValues(a, b)
}

func (NativeRules) NewExprNumericAggregate(op, kind, currency string) (ExprNumericAggregate, error) {
	return rules.NewNumericAggregate(op, kind, currency)
}

const ExprVersion = rules.Version

const ExprMaxDepth = rules.MaxDepth

const ExprMaxSet = rules.MaxSet

const ExprMaxContextBytes = rules.MaxContextBytes

func CompileExpr(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.Compile(expr, schema)
}

func LowerFormula(text string, fields map[string]string) (Expr, error) {
	kinds := make(map[string]string, len(fields))
	for name, kind := range fields {
		kinds[name] = RuleFieldKind(kind)
	}
	return rules.LowerFormula(text, kinds)
}

func FormulaFields(text string) (map[string]bool, error) { return rules.FormulaFields(text) }

func ExprLiteralValue(kind string, value any) Expr { return rules.LiteralOf(kind, value) }

func ExprReference(kind, name string) Expr { return rules.RefOf(kind, name) }

func ExprPresent(kind string, value any) ExprValue { return rules.Present(kind, value) }

func CheckObservationDecimal(value string) error { return rules.CheckWindowDecimal(value) }

func LowerPageExpr(raw json.RawMessage, kind string, schema ExprSchema) (Expr, error) {
	return rules.LowerPage(raw, kind, schema)
}

func ruleEngineOf(engines []RuleEngine) RuleEngine {
	if len(engines) > 0 {
		return engines[0]
	}
	return NativeRules{}
}
