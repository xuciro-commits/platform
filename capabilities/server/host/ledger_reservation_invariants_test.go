package host

import (
	"encoding/json"
	"platformserver/platform"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"testing"
)

func TestInvariantK47ReservationUsesLiveBalance(t *testing.T) {
	live := newInvariantTenant(t, "invariant-reservation")
	const root = "invariantproject.bucket"
	const incoming = "invariantproject.incoming"
	const claims = "invariantproject.claim"
	literal := func(v any, kind string) platform.Expr {
		return platform.Expr{Kind: kind, Lit: &platform.ExprLiteral{Value: platform.Raw(v)}}
	}
	sum := func(name string) platform.Expr {
		var e platform.Expr
		if err := json.Unmarshal(platform.Raw(map[string]any{"kind": "integer", "op": "sum", "agg": map[string]any{"source": name}, "in": map[string]any{"kind": "integer", "ref": "item.qty"}}), &e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	available := platform.Expr{Kind: "integer", Op: "-", Args: []platform.Expr{sum("incoming"), sum("claims")}}
	sets := []platform.RecordSet{}
	reads := &platform.RuleReadSpec{Collections: map[string]platform.RuleCollection{}}
	for _, b := range []struct{ name, typ string }{{"incoming", incoming}, {"claims", claims}} {
		sets = append(sets, platform.RecordSet{Name: b.name, Object: b.typ + ".balance", Direction: "inverse", From: b.name, Limit: 2, Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}}})
		reads.Collections[b.name] = platform.RuleCollection{App: build.ID, Object: b.typ + ".balance", Set: b.name, Policy: "complete"}
	}
	live.accept(t, "build.object.create", build.ObjectType, "ROOT", map[string]any{"name": "bucket", "title": "Bucket", "fields": []map[string]any{{"name": "available", "title": "Available", "type": "integer", "live": available}, {"name": "title", "title": "Title", "type": "text"}}, "sets": sets, "reads": reads})
	for _, b := range []struct{ id, name, inverse string }{{"INCOMING", "incoming", "incoming"}, {"CLAIM", "claim", "claims"}} {
		live.accept(t, "build.object.create", build.ObjectType, b.id, map[string]any{"name": b.name, "title": b.name, "ledger": true, "fields": []map[string]any{{"name": "bucket", "title": "Bucket", "type": "reference", "ref": root, "inverse": b.inverse, "required": true}, {"name": "qty", "title": "Quantity", "type": "integer", "required": true}}, "balances": []map[string]any{{"name": "balance", "title": "Balance", "keys": []string{"bucket"}, "measures": []string{"qty"}}}})
		live.accept(t, "build.object.publish", build.ObjectType, b.id, nil)
	}
	guard := platform.Expr{Kind: "boolean", Op: ">=", Args: []platform.Expr{platform.ExprReference("integer", "available"), literal(6, "integer")}}
	action := ontology.Action{Name: "claim", Title: "Claim", Transaction: &platform.BusinessTxn{Subject: root, Guard: &guard, Message: "Insufficient available quantity", Steps: []platform.TxnStep{{Name: "claim", Kind: "create", Target: platform.TxnRef{Type: claims}, Changes: []platform.TxnSet{{Field: "bucket", From: "$subject"}, {Field: "qty", Expr: func() *platform.Expr { e := literal(6, "integer"); return &e }()}}}}}}
	live.accept(t, "build.actiontype.create", build.ActionTypeType, "CLAIMACTION", map[string]any{"name": root + ".claim", "object": root, "definition": action})
	live.accept(t, "build.actiontype.publish", build.ActionTypeType, "CLAIMACTION", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "ROOT", nil)
	live.accept(t, root+".create", root, "B", map[string]any{"title": "B"})
	live.accept(t, incoming+".create", incoming, "A", map[string]any{"bucket": "B", "qty": 10})
	read := func(want float64) {
		t.Helper()
		v, e := live.RecordOf(t.Context(), live.member(t), root, "B", invariantClock)
		if e != nil {
			t.Fatal(e)
		}
		var data map[string]any
		_ = json.Unmarshal(platform.Raw(v.Record), &data)
		if data["available"] != want {
			t.Fatalf("available: %v want %v", data, want)
		}
	}
	read(10)
	live.accept(t, root+".claim", root, "B", nil)
	read(4)
	if _, err := live.Submit(live.member(t), live.submission(root+".claim", root, "B", nil), invariantClock); err == nil {
		t.Fatal("over reservation accepted")
	}
}
