package platform

import (
	"encoding/json"
	"platformserver/internal/rules"
)

// Expr is the one versioned, side-effect-free business rule tree.
// Application owners reach the compiler only through the platform API.
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

// RuleEngine consumes captured facts, with no live state or external I/O.
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
	return NativeRules{} // standalone reference execution, without a host
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
const ExprMaxSet = rules.MaxSet
const ExprMaxContextBytes = rules.MaxContextBytes

func NewExprNumericAggregate(op, kind, currency string) (ExprNumericAggregate, error) {
	return rules.NewNumericAggregate(op, kind, currency)
}

func CompileExpr(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.Compile(expr, schema)
}
func ExprLiteralValue(kind string, value any) Expr { return rules.LiteralOf(kind, value) }
func ExprReference(kind, name string) Expr         { return rules.RefOf(kind, name) }
func ExprPresent(kind string, value any) ExprValue { return rules.Present(kind, value) }

func CheckObservationDecimal(value string) error { return rules.CheckWindowDecimal(value) }

func CompareExprValues(a, b ExprValue) (int, error) { return rules.CompareValues(a, b) }

func LowerPageExpr(raw json.RawMessage, kind string, schema ExprSchema) (Expr, error) {
	return rules.LowerPage(raw, kind, schema)
}

func CompileFrozenFormula(expr Expr, schema ExprSchema) (ExprProgram, error) {
	return rules.CompileFrozenFormula(expr, schema)
}

func ruleEngineOf(engines []RuleEngine) RuleEngine {
	if len(engines) > 0 {
		return engines[0]
	}
	return NativeRules{}
}
