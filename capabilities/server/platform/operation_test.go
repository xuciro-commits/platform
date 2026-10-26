package platform

import (
	"strings"
	"testing"
)

func TestRecordJSONAllowsDeclaredRuleStructureWithinItsOriginalLimits(t *testing.T) {
	expr := ExprLiteralValue("boolean", true)
	for range ExprMaxDepth {
		expr = Expr{Op: "not", Kind: "boolean", Args: []Expr{expr}}
	}
	raw := Raw([]Constraint{{Name: "rule", Kind: "expression", Fields: []string{"value"}, Expr: &expr, Message: "Rule"}})
	if _, err := DecodeValue(raw, 64<<10); err == nil {
		t.Fatal("ordinary value decoding lost its smaller syntax budget")
	}
	if err := ValidateRecordJSON(raw); err != nil {
		t.Fatalf("declared rule structure was rejected: %v", err)
	}
	if _, err := CompileExpr(expr, ExprSchema{}); err != nil {
		t.Fatalf("rule at its original depth limit was rejected: %v", err)
	}
	expr = Expr{Op: "not", Kind: "boolean", Args: []Expr{expr}}
	if _, err := CompileExpr(expr, ExprSchema{}); err == nil {
		t.Fatal("record syntax budget expanded the rule compiler's depth limit")
	}
	for _, invalid := range []string{
		`{"same":1,"same":2}`, `{} {}`, `"` + strings.Repeat("a", 64<<10) + `"`,
		strings.Repeat("[", 2*ExprMaxDepth+17) + "0" + strings.Repeat("]", 2*ExprMaxDepth+17),
	} {
		if ValidateRecordJSON([]byte(invalid)) == nil {
			t.Fatal("record JSON accepted a duplicate, trailing value, oversized or overdeep input")
		}
	}
}

func TestVariantSchemaUsesItsDiscriminatorAndNullableType(t *testing.T) {
	schema := ValueSchema{Type: "variant", Nullable: true, Discriminator: "kind", Variants: map[string]ValueSchema{
		"amount":  {Type: "object", Properties: map[string]ValueSchema{"kind": {Type: "string", Enum: []string{"amount"}}, "quantity": {Type: "integer", Nullable: true}}, Required: []string{"kind", "quantity"}},
		"missing": {Type: "object", Properties: map[string]ValueSchema{"kind": {Type: "string", Enum: []string{"missing"}}, "reason": {Type: "string"}}, Required: []string{"kind", "reason"}},
	}}
	for _, raw := range []string{`null`, `{"kind":"amount","quantity":3}`, `{"kind":"amount","quantity":null}`, `{"kind":"missing","reason":"not supplied"}`} {
		if err := schema.Validate([]byte(raw), 4096); err != nil {
			t.Fatalf("valid variant %s: %v", raw, err)
		}
	}
	for _, raw := range []string{`{"kind":"amount","reason":"wrong branch"}`, `{"kind":"amount"}`, `{"kind":"unknown"}`, `{"kind":"amount","quantity":1,"quantity":2}`, `{"kind":"amount","quantity":1e999999999}`} {
		if schema.Validate([]byte(raw), 4096) == nil {
			t.Fatalf("invalid variant accepted: %s", raw)
		}
	}
	schema.Nullable = false
	if schema.Validate([]byte(`null`), 4096) == nil {
		t.Fatal("nonnullable variant accepted null")
	}
	wrong := schema.Variants["amount"]
	wrong.Required = []string{"quantity"}
	schema.Variants["amount"] = wrong
	if schema.Check() == nil {
		t.Fatal("variant omitted its required discriminator")
	}
}
