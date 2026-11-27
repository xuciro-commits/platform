package host

import (
	"bytes"
	"encoding/json"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"strings"
	"testing"
)

func TestInvariantK49LiveDerivedReadsCurrentSources(t *testing.T) {
	const source, line, root = "invariantproject.source", "invariantproject.line", "invariantproject.root"
	const readerID = "reader"
	reader := platform.Member{ID: readerID, Roles: map[string]string{"invariantproject": "reader"}}
	seats := []protocol.Seat{{Subjects: []string{readerID}, Member: reader}}
	live := newInvariantTenant(t, "invariant-k49-live", seats...)
	declare := func(id, name string, fields []map[string]any, extra map[string]any) {
		t.Helper()
		draft := map[string]any{"name": name, "title": name, "fields": fields}
		for k, v := range extra {
			draft[k] = v
		}
		live.accept(t, "build.object.create", build.ObjectType, id, draft)
	}
	publish := func(id string) { t.Helper(); live.accept(t, "build.object.publish", build.ObjectType, id, nil) }
	path := map[string]any{"op": "path", "kind": "choice", "args": []any{map[string]any{"ref": "item.source", "kind": "reference"}}, "path": []string{"status"}}
	agg := map[string]any{"op": "sum", "kind": "integer", "agg": map[string]any{"source": "lines"}, "in": map[string]any{"op": "if", "kind": "integer", "args": []any{
		map[string]any{"op": "=", "kind": "boolean", "args": []any{path, map[string]any{"kind": "choice", "lit": map[string]any{"value": "active"}}}},
		map[string]any{"kind": "integer", "lit": map[string]any{"value": 0}}, map[string]any{"kind": "integer", "lit": map[string]any{"value": 1}},
	}}}
	sets := []platform.RecordSet{{Name: "lines", Object: line, Direction: "inverse", From: "lines", Limit: 200, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}}}
	reads := &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{"lines": {App: build.ID, Object: line, Set: "lines", Policy: "complete"}}}
	access := []ontology.Access{{Role: "reader", Read: "all"}}
	declare("SOURCE", "source", []map[string]any{{"name": "status", "title": "Status", "type": "choice", "choices": "active,inactive", "required": true}}, map[string]any{"access": []ontology.Access{{Role: "reader", Read: "none"}}})
	publish("SOURCE")
	declare("ROOT", "root", []map[string]any{{"name": "invalid", "title": "Invalid", "type": "integer", "live": agg}, {"name": "note", "title": "Note", "type": "text"}}, map[string]any{"sets": sets, "reads": reads, "access": access})
	declare("LINE", "line", []map[string]any{{"name": "root", "title": "Root", "type": "reference", "ref": root, "inverse": "lines", "required": true}, {"name": "source", "title": "Source", "type": "reference", "ref": source}}, map[string]any{"access": access})
	publish("LINE")
	zero := platform.Expr{Kind: "integer", Lit: &platform.ExprLiteral{Value: platform.Raw(0)}}
	guard := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("integer", "invalid"), zero}}
	approved := platform.Expr{Kind: "text", Lit: &platform.ExprLiteral{Value: platform.Raw("approved")}}
	action := ontology.Action{Name: "approve", Title: "Approve", Transaction: &platform.BusinessTxn{Subject: root, Guard: &guard, Message: "Source is inactive", Steps: []platform.TxnStep{{Name: "set", Kind: "update", Target: platform.TxnRef{Type: root, ID: "$subject"}, Changes: []platform.TxnSet{{Field: "note", Expr: &approved}}}}}}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "APPROVE", map[string]any{"name": root + ".approve", "object": root, "definition": action})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "APPROVE", nil)
	publish("ROOT")
	live.accept(t, source+".create", source, "S", map[string]any{"status": "active"})
	live.accept(t, root+".create", root, "R", map[string]any{})
	read := func(l *invariantTenant, m platform.Member) (map[string]any, []string) {
		t.Helper()
		v, e := l.RecordOf(t.Context(), m, root, "R", invariantClock)
		if e != nil {
			t.Fatal(e)
		}
		var data map[string]any
		if err := json.Unmarshal(platform.Raw(v.Record), &data); err != nil {
			t.Fatal(err)
		}
		return data, v.Restricted
	}
	if v, _ := read(live, live.member(t)); v["invalid"] != float64(0) {
		t.Fatalf("empty live aggregate: %v", v)
	}
	live.accept(t, line+".create", line, "L", map[string]any{"root": "R", "source": "S"})
	if v, _ := read(live, live.member(t)); v["invalid"] != float64(0) {
		t.Fatalf("active: %v", v)
	}
	live.accept(t, root+".approve", root, "R", map[string]any{})
	before := live.recordImage(t)[root]
	live.accept(t, source+".edit", source, "S", map[string]any{"status": "inactive"})
	if v, _ := read(live, live.member(t)); v["invalid"] != float64(1) {
		t.Fatalf("stale value: %v", v)
	}
	if !bytes.Equal(before, live.recordImage(t)[root]) {
		t.Fatal("live calculation changed stored image")
	}
	if _, refusal := live.Submit(live.member(t), live.submission(root+".approve", root, "R", map[string]any{}), invariantClock); refusal == nil || !strings.Contains(refusal.Message, "Source is inactive") {
		t.Fatalf("stale approval guard: %+v", refusal)
	}
	m, _ := live.Member(readerID)
	// A short-circuit cannot turn unreadable sources into a passing guard.
	always := platform.Expr{Kind: "boolean", Lit: &platform.ExprLiteral{Value: platform.Raw(true)}}
	bypass := platform.Expr{Kind: "boolean", Op: "or", Args: []platform.Expr{always, guard}}
	allowed, denied := protocol.GuardSatisfied(bypass, platform.RuleReadSpec{Object: root}, live.objectCaller(m, build.ID), map[string]any{"id": "R"}, nil, invariantClock, live.Entity)
	if allowed || denied == nil {
		t.Fatalf("withheld source passed guard: %v %+v", allowed, denied)
	}

	if v, restricted := read(live, m); v["invalid"] != nil || !strings.Contains(strings.Join(restricted, ","), "invalid") {
		t.Fatalf("hidden source: %v %v", v, restricted)
	}
	page, e := live.Records(t.Context(), live.member(t), root, platform.Query{}, invariantClock)
	if e != nil {
		t.Fatal(e)
	}
	if len(page.Records) != 1 || !strings.Contains(string(platform.Raw(page.Records[0])), `"invalid":1`) {
		t.Fatalf("live list: %s", platform.Raw(page))
	}
	fresh := live.restore(t, live.entries, seats...)
	if v, _ := read(fresh, fresh.member(t)); v["invalid"] != float64(1) {
		t.Fatalf("replay live: %v", v)
	}
	for _, q := range []platform.Query{{Sort: []string{"invalid"}}, {Domain: platform.Raw([][]any{{"invalid", "=", 1}})}} {
		if _, refusal := live.Records(t.Context(), live.member(t), root, q, invariantClock); refusal == nil {
			t.Fatalf("indexed live field: %+v", q)
		}
	}
	if !bytes.Equal(live.recordImage(t)[root], fresh.recordImage(t)[root]) {
		t.Fatal("replay changed stored image")
	}
}

func TestInvariantK49StoredFormulaRejectsForeignReference(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k49-foreign")
	source := "invariantproject.source"
	live.accept(t, "build.object.create", build.ObjectType, "SOURCE", map[string]any{"name": "source", "title": "Source", "fields": []map[string]any{{"name": "amount", "title": "Amount", "type": "integer"}}})
	live.accept(t, "build.object.publish", build.ObjectType, "SOURCE", nil)
	expr := platform.Expr{Op: "path", Kind: "integer", Args: []platform.Expr{platform.ExprReference("reference", "source")}, Path: []string{"amount"}}
	live.accept(t, "build.object.create", build.ObjectType, "DRAFT", map[string]any{"name": "derived", "title": "Derived", "fields": []map[string]any{{"name": "source", "title": "Source", "type": "reference", "ref": source}, {"name": "amount", "title": "Amount", "type": "integer", "formulaExpr": expr}}})
	_, refusal := live.Submit(live.member(t), live.submission("build.object.publish", build.ObjectType, "DRAFT", nil), invariantClock)
	if refusal == nil || !strings.Contains(refusal.Message, "invariantproject.derived") || !strings.Contains(refusal.Message, "source.amount") || !strings.Contains(refusal.Message, "live, from, or a transaction guard") {
		t.Fatalf("foreign formula diagnostic: %+v", refusal)
	}
}
