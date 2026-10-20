package build

import (
	"platformserver/internal/construction"
	"platformserver/platform"
	"testing"
	"time"
)

func TestFormula(t *testing.T) {
	o := construction.Object{Name: "line", Fields: []construction.Field{
		{Name: "price", Title: "Price", Type: "decimal"},
		{Name: "qty", Title: "Qty", Type: "integer", Required: true},
		{Name: "total", Title: "Total", Type: "decimal", Formula: "price * qty - (1 / 0)"},
		{Name: "half", Title: "Half", Type: "integer", Formula: "-qty / 2"},
	}}
	if err := checkFormulas(o.Fields); err != nil {
		t.Fatal(err)
	}
	if err := checkFormulas([]construction.Field{{Name: "a", Type: "text", Formula: "1"}}); err == nil {
		t.Fatal("a text field took a formula")
	}
	if err := checkFormulas([]construction.Field{{Name: "a", Type: "integer", Formula: "b + 1"}}); err == nil {
		t.Fatal("an unknown operand passed")
	}
	if _, err := construction.ValueParseFormula("1 +"); err == nil {
		t.Fatal("a broken formula parsed")
	}
	type rec struct {
		Price *float64 `json:"price"`
		Qty   int64    `json:"qty"`
		Total *float64
		Half  *int64
	}
	price := 2.5
	r := &rec{Price: &price, Qty: 3}
	if err := computeRulesOf(o, nil)(platform.Caller{}, r, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if r.Total == nil || *r.Total != 7.5 || r.Half == nil || *r.Half != -2 {
		t.Fatalf("computed %v %v", r.Total, r.Half)
	}
}
