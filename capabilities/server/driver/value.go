package driver

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
)

// TransformPointValue shares the platform's exact scalar arithmetic. It adds
// no scripting language and never changes a bad read into a zero value.
func TransformPointValue(point platform.DriverPoint, value any) (any, error) {
	kind := point.Type
	if kind == "enum" {
		kind = "choice"
	}
	if kind == "number" {
		kind = "decimal"
	}
	expr := platform.ExprLiteralValue(kind, value)
	if point.Scale != "" || point.Offset != "" {
		if kind != "integer" && kind != "decimal" {
			return nil, fmt.Errorf("only numeric points support scale and offset")
		}
		scale, offset := point.Scale, point.Offset
		if scale == "" {
			scale = "1"
		}
		if offset == "" {
			offset = "0"
		}
		expr = platform.Expr{Op: "+", Kind: "decimal", Args: []platform.Expr{{Op: "*", Kind: "decimal", Args: []platform.Expr{expr, platform.ExprLiteralValue("decimal", scale)}}, platform.ExprLiteralValue("decimal", offset)}}
	}
	program, err := platform.CompileExpr(expr, platform.ExprSchema{})
	if err != nil {
		return nil, err
	}
	result, err := program.Eval(platform.ExprContext{})
	if err != nil {
		return nil, err
	}
	converted, err := platform.ExprGoValue(result.Value)
	if err != nil {
		return nil, err
	}
	// An integer point must remain integral after explicit linear conversion.
	check, err := platform.CompileExpr(platform.ExprLiteralValue(kind, converted), platform.ExprSchema{})
	if err != nil {
		return nil, err
	}
	if _, err = check.Eval(platform.ExprContext{}); err != nil {
		return nil, err
	}
	if number, ok := converted.(json.Number); ok {
		return number.String(), nil
	}
	return converted, nil
}
