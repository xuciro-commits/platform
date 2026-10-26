package platform

import (
	"strings"
	"testing"
)

func TestQuerySetBudgetsBoundAllBranches(t *testing.T) {
	var tree func(int) *RecordSetPredicate
	tree = func(depth int) *RecordSetPredicate {
		if depth == 0 {
			return &RecordSetPredicate{}
		}
		return &RecordSetPredicate{Set: &QuerySet{Op: "union", Inputs: []*RecordSetPredicate{tree(depth - 1), tree(depth - 1)}}}
	}
	if err := (Query{Set: tree(4).Set}).CheckSet(); err != nil {
		t.Fatal("maximum legal tree rejected", err)
	}
	if (Query{Set: tree(5).Set}).CheckSet() == nil {
		t.Fatal("oversized/deep tree accepted")
	}
	large := tree(4)
	var fill func(*RecordSetPredicate)
	fill = func(p *RecordSetPredicate) {
		if p.Set == nil {
			p.Search = strings.Repeat("x", 4096)
			return
		}
		for _, c := range p.Set.Inputs {
			fill(c)
		}
	}
	fill(large)
	if (Query{Set: large.Set}).CheckSet() == nil {
		t.Fatal("whole encoding budget ignored")
	}
	for _, q := range []Query{{Set: &QuerySet{Op: "subtract", Inputs: []*RecordSetPredicate{nil, {}}}}, {Set: &QuerySet{Op: "union", Inputs: []*RecordSetPredicate{{Search: strings.Repeat("x", 4097)}, {}}}}, {Set: &QuerySet{Op: "intersect", Inputs: []*RecordSetPredicate{{Domain: Raw(make([]string, 33))}, {}}}}, {Set: tree(1).Set, Offset: -1}} {
		if q.CheckSet() == nil {
			t.Fatal("invalid query set accepted", q)
		}
	}
}
