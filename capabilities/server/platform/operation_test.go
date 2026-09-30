package platform

import "testing"

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
