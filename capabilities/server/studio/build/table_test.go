package build

import (
	"encoding/json"

	"platformserver/platform"
	"testing"
)

func decideTable(table Table, values map[string]any) (map[string]any, error) {
	plan, err := table.RulePlan()
	if err != nil {
		return nil, err
	}
	legacy := len(table.Rows) > 0 && len(table.Rows[0].Expressions) == 0
	raw, err := plan.Decide(platform.TableInput(plan.Reads, legacy, values), func(string) (platform.EntityInfo, bool) { return platform.EntityInfo{}, false })
	if err != nil {
		return nil, err
	}
	var out map[string]any
	err = json.Unmarshal(raw, &out)
	return out, err
}

func TestTableDecide(t *testing.T) {
	tb := Table{Name: "discount", Title: "Discount",
		Inputs:  []TableColumn{{Name: "tier", Type: "text"}, {Name: "amount", Type: "number"}, {Name: "rush", Type: "boolean"}},
		Outputs: []TableColumn{{Name: "percent", Type: "number"}, {Name: "approver", Type: "text"}},
		Rows: []TableRow{
			{When: []string{"gold|platinum", ">= 1000", ""}, Then: []string{"12", "none"}},
			{When: []string{"gold", "100..999.99", "false"}, Then: []string{"5", "none"}},
			{When: []string{"*", "> 5000", "true"}, Then: []string{"0", "manager"}},
		},
		Default: []string{"0", "none"}}
	if err := (&Build{}).checkTableShape(tb); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		in   map[string]any
		want map[string]any
	}{
		{map[string]any{"tier": "platinum", "amount": 2500.0, "rush": false}, map[string]any{"percent": 12.0, "approver": "none"}},
		{map[string]any{"tier": "gold", "amount": "250", "rush": "no"}, map[string]any{"percent": 5.0, "approver": "none"}},
		{map[string]any{"tier": "silver", "amount": 9000.0, "rush": true}, map[string]any{"percent": 0.0, "approver": "manager"}},
		{map[string]any{"tier": "silver", "amount": 10.0, "rush": false}, map[string]any{"percent": 0.0, "approver": "none"}},
	}
	for i, c := range cases {
		got, err := decideTable(tb, c.in)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		for k, v := range c.want {
			if got[k] != v {
				t.Fatalf("case %d: %s = %v, want %v", i, k, got[k], v)
			}
		}
	}
	tb.Default = nil
	if _, err := decideTable(tb, map[string]any{"tier": "silver", "amount": 10.0, "rush": false}); err == nil {
		t.Fatal("no default: expected a refusal")
	}
	if _, err := ValueParseCondition("> 3", "text"); err == nil {
		t.Fatal("comparison on text must be refused")
	}
}
