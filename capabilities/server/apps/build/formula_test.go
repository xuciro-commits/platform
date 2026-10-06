package build

import "testing"

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
	if _, err := parseFormula("1 +"); err == nil {
		t.Fatal("a broken formula parsed")
	}
	type rec struct {
		Price *float64
		Qty   int64
		Total *float64
		Half  *int64
	}
	price := 2.5
	r := &rec{Price: &price, Qty: 3}
	computeOf(o)(r)
	if r.Total == nil || *r.Total != 7.5 || r.Half == nil || *r.Half != -2 {
		t.Fatalf("computed %v %v", r.Total, r.Half)
	}
}
