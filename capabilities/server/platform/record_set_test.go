package platform

import (
	"slices"
	"testing"
)

func TestDeclaredSetRejectsUnboundedOrUnsupportedSources(t *testing.T) {
	root := EntityInfo{Type: "test.root", App: "test", Fields: []FieldInfo{{Name: "parent", Type: "reference", Ref: "test.root"}}}
	set := RecordSet{Name: "parent", From: "parent", Direction: "reference", Object: root.Type, Order: []RecordSetSort{{Expr: ExprReference("text", "id")}}, Limit: 10}
	lookup := func(typ string) (EntityInfo, bool) { return root, typ == root.Type }
	root.Sets = []RecordSet{set}
	if err := CheckRecordSets(root, lookup); err != nil {
		t.Fatal(err)
	}
	for _, alter := range []func(*RecordSet){
		func(s *RecordSet) { s.Limit = 0 },
		func(s *RecordSet) { s.Limit = RecordSetMaxRows + 1 },
		func(s *RecordSet) { s.Order = nil },
		func(s *RecordSet) { s.From = "undeclared" },
		func(s *RecordSet) { s.Path = slices.Repeat([]string{"parent"}, 17) },
	} {
		invalid := set
		alter(&invalid)
		root.Sets = []RecordSet{invalid}
		if err := CheckRecordSets(root, lookup); err == nil {
			t.Fatalf("invalid set was accepted: %+v", invalid)
		}
	}
}
