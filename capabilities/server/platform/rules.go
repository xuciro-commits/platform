package platform

import (
	"encoding/json"
	"platformserver/internal/rules"
)

// Expr is the one versioned, side-effect-free business rule tree (ADR-0101).
// Application owners reach the compiler only through the platform API.
type Expr = rules.Expr
type ExprLiteral = rules.Literal
type ExprAggregate = rules.Aggregate
type ExprType = rules.Type
type ExprSetType = rules.SetType
type ExprSchema = rules.Schema
type ExprValue = rules.Value
type ExprContext = rules.Context
type ExprItem = rules.Item
type ExprCollection = rules.Collection
type ExprResult = rules.Result
type ExprIssue = rules.Issue
type ExprProgram = rules.Program

const ExprVersion = rules.Version

func CompileExpr(expr Expr, schema ExprSchema) (*ExprProgram, error) {
	return rules.Compile(expr, schema)
}
func ExprLiteralValue(kind string, value any) Expr { return rules.LiteralOf(kind, value) }
func ExprReference(kind, name string) Expr         { return rules.RefOf(kind, name) }
func ExprPresent(kind string, value any) ExprValue { return rules.Present(kind, value) }

func LowerPageExpr(raw json.RawMessage, kind string, schema ExprSchema) (Expr, error) {
	return rules.LowerPage(raw, kind, schema)
}

func CompileFrozenFormula(expr Expr, schema ExprSchema) (*ExprProgram, error) {
	return rules.CompileFrozenFormula(expr, schema)
}
