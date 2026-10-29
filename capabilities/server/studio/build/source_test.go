package build

import (
	"encoding/json"
	"strings"

	"testing"
	"time"

	"platformserver/platform"
)

func TestSourceResetEndsPendingInputWithoutMutatingItsHistory(t *testing.T) {
	prior := SourcePull{Cursor: "2", Execution: &IntegrationExecution{ID: "original", Member: "builder", Pending: true}}
	source := Source{URL: "https://source.example/rows", Cursor: "1", Last: &prior}
	if err := New("source-reset").resetCursor(platform.Caller{}, &source, nil, time.Time{}); err != nil {
		t.Fatal(err)
	}
	if source.Cursor != "" || !source.Requested || source.Last.Execution.Pending || source.Last.Error == "" {
		t.Fatalf("reset did not end the old input and request a fresh pull: %+v", source)
	}
	if !prior.Execution.Pending || prior.Error != "" || source.Last == &prior || source.Last.Execution == prior.Execution {
		t.Fatal("reset mutated the previous accepted input")
	}
}

func TestIntegrationPreparationCannotExpandOrReplaceFrozenTokenScopes(t *testing.T) {
	member := platform.Member{ID: "runner", Scopes: []string{"build.source.*", "build.dataset.*"}}
	prepared := IntegrationExecution{ID: "run", Member: member.ID, Scopes: append([]string{}, member.Scopes...), Snapshot: strings.Repeat("ab", 32), Pending: true}
	if problem := checkIntegrationExecution(nil, &prepared, member); problem != nil {
		t.Fatal("matching preparation refused", problem)
	}
	unscoped := prepared
	unscoped.Scopes = nil
	if problem := checkIntegrationExecution(nil, &unscoped, member); problem == nil {
		t.Fatal("narrow token forged an unbounded preparation")
	}
	finished := prepared
	finished.Pending = false
	if problem := checkIntegrationExecution(&prepared, &finished, member); problem != nil {
		t.Fatal("the original bounded execution could not finish", problem)
	}
	finished.Scopes = []string{"build.*"}
	if problem := checkIntegrationExecution(&prepared, &finished, member); problem == nil {
		t.Fatal("a result replaced its frozen execution scope")
	}
}

func TestSourceProfilesDecodeAndGuard(t *testing.T) {
	s := Source{Profile: "csv", Key: "sku", Since: "changed", Mapping: []SourceField{{From: "sku", To: "code"}, {From: "qty", To: "quantity", Convert: "number"}}}
	rows, err := s.Decode([]byte("sku,qty,changed\nA,1,2026-01-01\nB,2.5,2026-01-03\n"))
	if err != nil || len(rows) != 2 {
		t.Fatalf("rows=%d err=%v", len(rows), err)
	}
	mapped, err := s.MapRows(rows)
	if err != nil || mapped[1].ID != "B" || string(mapped[1].Payload) != `{"code":"B","quantity":2.5}` {
		t.Fatalf("mapped=%+v err=%v", mapped, err)
	}
	if s.Advance(rows) != "2026-01-03" {
		t.Fatal("cursor")
	}
	sequence := Source{Since: "seq", Cursor: "9"}
	if got := sequence.Advance([]map[string]any{{"seq": float64(10)}, {"seq": float64(2)}}); got != "10" {
		t.Fatalf("numeric sequence cursor = %s, want 10", got)
	}
	sequence.Cursor = "9007199254740992"
	if got := sequence.Advance([]map[string]any{{"seq": json.Number("9007199254740993")}}); got != "9007199254740993" {
		t.Fatalf("full-width sequence cursor = %s", got)
	}
	timestamps := Source{Since: "at", Cursor: "2026-10-10T00:00:00Z"}
	if got := timestamps.Advance([]map[string]any{{"at": "2026-10-10T00:00:00.1Z"}}); got != "2026-10-10T00:00:00.1Z" {
		t.Fatal("fractional event position did not advance chronologically")
	}
	for where, ok := range map[string]bool{"": true, "plant = '1000'": true, "plant = '1000' and active = true": true, "1=1; drop table x": false, "plant in ('1')": false} {
		if simpleWhere(where) != ok {
			t.Fatalf("simpleWhere(%q) != %v", where, ok)
		}
	}
	if !tableName("mes.confirmations") || tableName("a.b.c") || tableName("x;") {
		t.Fatal("tableName")
	}
	c := Connection{Address: "https://sap.example/odata/"}
	if c.Resolve("Products") != "https://sap.example/odata/Products" || c.Resolve("https://o.example/x") != "https://o.example/x" {
		t.Fatal("resolve")
	}
}
