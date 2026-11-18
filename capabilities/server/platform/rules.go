package platform

import (
	"encoding/json"
	ruleexpr "platformserver/expr"
)

type RuleCriterion struct {
	Expr    Expr   `json:"expr"`
	Message string `json:"message"`
}

type RuleCriteria struct {
	Reads      RuleReadSpec    `json:"reads"`
	Conditions []RuleCriterion `json:"conditions"`
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

type Expr = ruleexpr.Expr

type ExprLiteral = ruleexpr.Literal

type ExprWindowType = ruleexpr.WindowType

type ExprWindowSummary = ruleexpr.WindowSummary

type ExprType = ruleexpr.Type

type ExprSetType = ruleexpr.SetType

type ExprSchema = ruleexpr.Schema

type ExprValue = ruleexpr.Value

type ExprContext = ruleexpr.Context

type ExprSource = ruleexpr.RefSource

type ExprItem = ruleexpr.Item

type ExprCollection = ruleexpr.Collection

type ExprResult = ruleexpr.Result

type ExprIssue = ruleexpr.Issue

type ExprProgram interface {
	Eval(ExprContext) (ExprResult, error)
	EvalFrozenFormula(ExprContext) (ExprResult, error)
}

type RowProgram interface {
	Match(ExprSource) bool
}

type PredicateProgram interface {
	ExprProgram
	EvalSource(ExprSource) (ExprResult, error)
}

type ExprNumericAggregate interface {
	Add(ExprValue) error
	Result() (ExprValue, error)
}

type RuleEngine interface {
	CompileExpr(Expr, ExprSchema) (ExprProgram, error)
	CompileFrozenFormula(Expr, ExprSchema) (ExprProgram, error)
	CompileRow(Expr, ExprSchema) (RowProgram, error)
	CompilePredicate(Expr, ExprSchema) (PredicateProgram, error)
	CompareExprValues(ExprValue, ExprValue) (int, error)
	NewExprNumericAggregate(string, string, string) (ExprNumericAggregate, error)
}

type NativeRules struct{}

func (c Caller) Rules() RuleEngine {
	if c.rt != nil {
		return c.rt.Rules()
	}
	return NativeRules{}
}

func (NativeRules) CompileExpr(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return ruleexpr.Compile(expr, schema)
}

func (NativeRules) CompileFrozenFormula(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return ruleexpr.CompileFrozenFormula(expr, schema)
}

func (NativeRules) CompileRow(expr Expr, schema ExprSchema) (RowProgram, error) {
	return ruleexpr.CompileRow(expr, schema)
}

func (NativeRules) CompilePredicate(expr Expr, schema ExprSchema) (PredicateProgram, error) {
	return ruleexpr.CompilePredicate(expr, schema)
}

func (NativeRules) CompareExprValues(a, b ExprValue) (int, error) {
	return ruleexpr.CompareValues(a, b)
}

func (NativeRules) NewExprNumericAggregate(op, kind, currency string) (ExprNumericAggregate, error) {
	return ruleexpr.NewNumericAggregate(op, kind, currency)
}

const ExprVersion = ruleexpr.Version

const ExprMaxDepth = ruleexpr.MaxDepth

const ExprMaxSet = ruleexpr.MaxSet

const ExprMaxContextBytes = ruleexpr.MaxContextBytes

func LowerFormula(text string, fields map[string]string) (Expr, error) {
	kinds := make(map[string]string, len(fields))
	for name, kind := range fields {
		kinds[name] = RuleFieldKind(kind)
	}
	return ruleexpr.LowerFormula(text, kinds)
}

func FormulaFields(text string) (map[string]bool, error) { return ruleexpr.FormulaFields(text) }

func ExprLiteralValue(kind string, value any) Expr { return ruleexpr.LiteralOf(kind, value) }

func ExprReference(kind, name string) Expr { return ruleexpr.RefOf(kind, name) }

func ExprPresent(kind string, value any) ExprValue { return ruleexpr.Present(kind, value) }

func CheckObservationDecimal(value string) error { return ruleexpr.CheckWindowDecimal(value) }

func RuleEngineOf(engines []RuleEngine) RuleEngine {
	if len(engines) > 0 {
		return engines[0]
	}
	return NativeRules{}
}
