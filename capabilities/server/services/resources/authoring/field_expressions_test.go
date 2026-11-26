package authoring

import (
	"testing"

	"platformserver/services/ontology"
)

func TestCheckFieldExpressionsPointsAtTheProblemAndListsWhatFits(t *testing.T) {
	fields := []ontology.Field{{Name: "employees", Title: "Employees", Type: "integer"}, {Name: "since", Title: "Since", Type: "date"}}
	wrong := CheckFieldExpressions(FieldExpressionCheck{Fields: fields, Field: ontology.Field{Name: "score", Title: "Score", Type: "decimal", Formula: "employes * 2"}})
	if wrong.Valid || len(wrong.Issues) != 1 || wrong.Issues[0].Slot != "formula" || wrong.Issues[0].From != 0 || wrong.Issues[0].To != 8 {
		t.Fatalf("misspelt operand: %+v", wrong)
	}
	if len(wrong.Operands) != 1 || wrong.Operands[0].Label != "employees" {
		t.Fatalf("operands: %+v", wrong.Operands)
	}
	if right := CheckFieldExpressions(FieldExpressionCheck{Fields: fields, Field: ontology.Field{Name: "score", Title: "Score", Type: "decimal", Formula: "employees * 2"}}); !right.Valid {
		t.Fatalf("valid formula refused: %+v", right)
	}
	late := CheckFieldExpressions(FieldExpressionCheck{Fields: fields, Field: ontology.Field{Name: "met", Title: "Met on", Type: "date", Default: "now"}})
	if late.Valid || len(late.Issues) != 1 || late.Issues[0].Slot != "default" || late.Issues[0].To != 3 || late.Defaults[0].Label != "today" {
		t.Fatalf("date default now: %+v", late)
	}
}
