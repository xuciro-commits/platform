package records

import (
	"fmt"
	"platformserver/platform"
	"slices"
	"testing"
)

type testRowSource struct {
	rows  map[string]*Row
	reads int
}

func (s *testRowSource) Get(id string) *Row { s.reads++; return s.rows[id] }
func (s *testRowSource) IDs(after string, limit int) []string {
	ids := make([]string, 0, len(s.rows))
	for id := range s.rows {
		if id > after {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	if len(ids) > limit {
		ids = ids[:limit]
	}
	return ids
}
func TestSourcedRowsKeepOnlyPrivateReplacements(t *testing.T) {
	source := &testRowSource{rows: map[string]*Row{}}
	for i := range 600 {
		source.rows[fmt.Sprintf("%04d", i)] = &Row{HistoryCount: i}
	}
	live := SourceRows(source)
	if live.Get("0599").HistoryCount != 599 || live.Stored() != 0 {
		t.Fatal("fetch retained a committed row")
	}
	if live.Len() != 600 || source.reads != 1 {
		t.Fatal("count loaded record values")
	}
	draft := live
	first := &Row{HistoryCount: 900}
	draft.Set("0001", first)
	draft.Remove("0002")
	draft.Set("new", &Row{HistoryCount: 901})
	child := draft
	child.Set("0001", &Row{HistoryCount: 902})
	child.Remove("new")
	if draft.Get("0001") != first || draft.Get("new") == nil || live.Get("0001") == first {
		t.Fatal("private edits changed a parent")
	}
	if child.Get("new") != nil || child.Get("0002") != nil || child.Len() != 599 {
		t.Fatal("nested deletion or count differs")
	}
	seen := map[string]bool{}
	last := ""
	for id, row := range child.All() {
		if seen[id] || row == nil {
			t.Fatal("iteration repeated an ID or exposed a tombstone")
		}
		if id <= last {
			t.Fatal("iteration changed record ID order")
		}
		last = id
		seen[id] = true
	}
	if len(seen) != 599 || seen["0002"] || seen["new"] || child.Stored() != 3 || live.Stored() != 0 {
		t.Fatal("iteration lost an overlay or retained base rows")
	}
	if live.Get("0002") == nil || live.Len() != 600 {
		t.Fatal("discarded draft mutated committed rows")
	}
}

func TestFieldProjectionKeepsOnlyReadableLocalRuleDependencies(t *testing.T) {
	source := platform.EntityInfo{App: "sample", Fields: []platform.FieldInfo{{Name: "qty", Type: "integer"}, {Name: "price", Type: "decimal", Read: []string{"admin"}}, {Name: "total", Type: "decimal", ReadOnly: true}}, LocalRules: map[string]platform.Expr{
		"total": {Op: "*", Kind: "decimal", Args: []platform.Expr{{Kind: "integer", Ref: "qty"}, {Kind: "decimal", Ref: "price"}}},
	}}
	reader := platform.Member{Roles: map[string]string{"sample": "user"}}
	projected, hidden := FieldView(reader, source)
	if len(hidden) != 1 || len(projected.LocalRules) != 0 || len(source.LocalRules) != 1 {
		t.Fatal("local preview exposed unreadable sources or mutated its owner")
	}
	reader.Roles["sample"] = "admin"
	full, hidden := FieldView(reader, source)
	if len(hidden) != 0 || len(full.LocalRules) != 1 {
		t.Fatal("authorized formula metadata was discarded")
	}
}
