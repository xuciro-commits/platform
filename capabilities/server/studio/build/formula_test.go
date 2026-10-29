package build

import (
	"platformserver/platform"
	"testing"
	"time"
)

func TestFormula(t *testing.T) {
	o := Object{Name: "line", Fields: []Field{
		{Name: "price", Title: "Price", Type: "decimal"},
		{Name: "qty", Title: "Qty", Type: "integer", Required: true},
		{Name: "total", Title: "Total", Type: "decimal", Formula: "price * qty - (1 / 0)"},
		{Name: "half", Title: "Half", Type: "integer", Formula: "-qty / 2"},
	}}
	if err := checkFormulas(o.Fields); err != nil {
		t.Fatal(err)
	}
	if err := checkFormulas([]Field{{Name: "a", Type: "text", Formula: "1"}}); err == nil {
		t.Fatal("a text field took a formula")
	}
	if err := checkFormulas([]Field{{Name: "a", Type: "integer", Formula: "b + 1"}}); err == nil {
		t.Fatal("an unknown operand passed")
	}
	if _, err := platform.FormulaFields("1 +"); err == nil {
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

func TestPublishedFieldExprIsTheLocalPreviewDeclaration(t *testing.T) {
	expression := platform.Expr{Op: "*", Kind: "decimal", Args: []platform.Expr{{Kind: "decimal", Ref: "price"}, {Kind: "integer", Ref: "qty"}}}
	object := Object{Name: "entry", Fields: []Field{{Name: "price", Title: "Price", Type: "decimal"}, {Name: "qty", Title: "Quantity", Type: "integer"}, {Name: "total", Title: "Total", Type: "decimal", FormulaExpr: &expression}}}
	entity := CompileObjectShape(object)
	if string(platform.Raw(entity.LocalRules["total"])) != string(platform.Raw(expression)) {
		t.Fatal("local preview received a different formula")
	}
	info, err := platform.Describe(ID, entity, nil)
	if err != nil || string(platform.Raw(info.LocalRules["total"])) != string(platform.Raw(expression)) {
		t.Fatal("installed field metadata lost the original Expr", err)
	}
}
