package host

import (
	"encoding/json"
	"platformserver/platform"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	"strings"
	"testing"
)

func TestInvariantK55LedgerValuationAndDepletion(t *testing.T) {
	live := newInvariantTenant(t, "invariant-ledger-valuation")
	lit := func(kind string, v any) platform.Expr {
		return platform.Expr{Kind: kind, Lit: &platform.ExprLiteral{Value: platform.Raw(v)}}
	}
	priorExpr := func(kind, bal, meas string) platform.Expr {
		return platform.Expr{Op: "prior-balance", Kind: kind, Args: []platform.Expr{lit("text", bal), lit("text", meas)}}
	}

	rejectObject := func(id string, payload map[string]any) {
		t.Helper()
		live.accept(t, "build.object.create", build.ObjectType, id, payload)
		if _, err := live.Submit(live.member(t), live.submission("build.object.publish", build.ObjectType, id, nil), invariantClock); err == nil {
			t.Fatalf("expected %s publish to be refused", id)
		}
		live.accept(t, "build.object.archive", build.ObjectType, id, nil)
	}

	rejectObject("BADLIVE", map[string]any{
		"name": "badlive", "title": "Bad live", "ledger": true,
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "text", "required": true},
			{"name": "qty", "title": "Qty", "type": "decimal", "required": true},
			{"name": "peek", "title": "Peek", "type": "decimal", "live": priorExpr("decimal", "pool", "qty")},
		},
		"balances": []map[string]any{{"name": "pool", "title": "Pool", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
	})
	rejectObject("BADFORMULA", map[string]any{
		"name": "badformula", "title": "Bad formula", "ledger": true,
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "text", "required": true},
			{"name": "qty", "title": "Qty", "type": "decimal", "required": true},
			{"name": "calc", "title": "Calc", "type": "decimal", "formulaExpr": priorExpr("decimal", "pool", "qty")},
		},
		"balances": []map[string]any{{"name": "pool", "title": "Pool", "keys": []string{"bucket"}, "measures": []string{"qty"}}},
	})
	rejectObject("BADNONLEDGER", map[string]any{
		"name": "badnonledger", "title": "Bad nonledger",
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "text", "required": true},
			{"name": "qty", "title": "Qty", "type": "decimal", "from": priorExpr("decimal", "pool", "qty")},
		},
	})
	rejectObject("BADBALFIELD", map[string]any{
		"name": "badbalfield", "title": "Bad balance field", "ledger": true,
		"fields": []map[string]any{
			{"name": "bucket", "title": "Bucket", "type": "text", "required": true},
			{"name": "qty", "title": "Qty", "type": "decimal", "required": true},
		},
		"balances": []map[string]any{{
			"name": "pool", "title": "Pool", "keys": []string{"bucket"}, "measures": []string{"qty"},
			"fields": []map[string]any{{"name": "stored", "title": "Stored", "type": "decimal"}},
		}},
	})

	const tierType = "invariantproject.tier"
	const subjectType = "invariantproject.subject"
	const entryType = "invariantproject.entry"
	const poolType = entryType + ".pool"
	const sliceType = "invariantproject.slice"
	const remType = sliceType + ".remaining"

	live.accept(t, "build.object.create", build.ObjectType, "TIER", map[string]any{
		"name": "tier", "title": "Tier",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "method", "title": "Method", "type": "choice", "choices": "weighted,oldest", "required": true},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "TIER", nil)

	live.accept(t, "build.object.create", build.ObjectType, "SUBJECT", map[string]any{
		"name": "subject", "title": "Subject",
		"fields": []map[string]any{
			{"name": "title", "title": "Title", "type": "text", "required": true, "search": true},
			{"name": "tier", "title": "Tier", "type": "reference", "ref": tierType, "required": true},
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "SUBJECT", nil)

	live.accept(t, "build.object.create", build.ObjectType, "SLICE", map[string]any{
		"name": "slice", "title": "Slice", "ledger": true,
		"fields": []map[string]any{
			{"name": "subject", "title": "Subject", "type": "reference", "ref": subjectType, "required": true},
			{"name": "place", "title": "Place", "type": "text", "required": true},
			{"name": "origin", "title": "Origin", "type": "reference", "ref": entryType, "required": true},
			{"name": "consumer", "title": "Consumer", "type": "reference", "ref": entryType, "inverse": "consumptions", "required": true},
			{"name": "qty", "title": "Qty", "type": "decimal", "required": true},
			{"name": "amt", "title": "Amt", "type": "decimal", "required": true},
		},
		"balances": []map[string]any{
			{
				"name": "remaining", "title": "Remaining",
				"keys":     []string{"subject", "place", "origin"},
				"measures": []string{"qty", "amt"},
				"constraints": []platform.Constraint{{
					Name: "nonneg", Kind: "expression", Fields: []string{"qty"},
					Expr:    &platform.Expr{Op: ">=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "qty"), lit("decimal", 0)}},
					Message: "Slice cannot go negative",
				}},
			},
			{
				"name": "allocation", "title": "Allocation",
				"keys":     []string{"consumer", "origin"},
				"measures": []string{"qty", "amt"},
			},
		},
	})

	methodPath := platform.Expr{Op: "path", Kind: "choice", Args: []platform.Expr{platform.ExprReference("reference", "subject")}, Path: []string{"tier", "method"}}
	isWeighted := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{methodPath, lit("choice", "weighted")}}
	isOldest := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{methodPath, lit("choice", "oldest")}}
	hasPriorQty := platform.Expr{Op: ">", Kind: "boolean", Args: []platform.Expr{priorExpr("decimal", "pool", "qty"), lit("decimal", 0)}}
	weightedOut := platform.Expr{
		Op: "round", Kind: "decimal",
		Args: []platform.Expr{
			{Op: "/", Kind: "decimal", Args: []platform.Expr{
				{Op: "*", Kind: "decimal", Args: []platform.Expr{platform.ExprReference("decimal", "qty"), priorExpr("decimal", "pool", "amt")}},
				priorExpr("decimal", "pool", "qty"),
			}},
			lit("integer", 2),
		},
	}
	amtFrom := platform.Expr{
		Op: "if", Kind: "decimal",
		Args: []platform.Expr{
			{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "qty"), lit("decimal", 0)}},
			{Op: "coalesce", Kind: "decimal", Args: []platform.Expr{platform.ExprReference("decimal", "inputamt"), lit("decimal", 0)}},
			{Op: "if", Kind: "decimal", Args: []platform.Expr{
				{Op: "and", Kind: "boolean", Args: []platform.Expr{isWeighted, hasPriorQty}},
				weightedOut,
				lit("decimal", 0),
			}},
		},
	}
	rateLive := platform.Expr{
		Op: "if", Kind: "decimal",
		Args: []platform.Expr{
			{Op: ">", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "qty"), lit("decimal", 0)}},
			{Op: "round", Kind: "decimal", Args: []platform.Expr{
				{Op: "/", Kind: "decimal", Args: []platform.Expr{platform.ExprReference("decimal", "amt"), platform.ExprReference("decimal", "qty")}},
				lit("integer", 4),
			}},
			lit("decimal", 0),
		},
	}

	live.accept(t, "build.object.create", build.ObjectType, "ENTRY", map[string]any{
		"name": "entry", "title": "Entry", "ledger": true,
		"fields": []map[string]any{
			{"name": "subject", "title": "Subject", "type": "reference", "ref": subjectType, "required": true},
			{"name": "place", "title": "Place", "type": "text", "required": true},
			{"name": "qty", "title": "Qty", "type": "decimal", "required": true},
			{"name": "inputamt", "title": "Input amount", "type": "decimal"},
			{"name": "amt", "title": "Amount", "type": "decimal", "required": true, "from": amtFrom},
		},
		"sets": []platform.RecordSet{{
			Name: "consumptions", Object: sliceType + ".allocation", Direction: "inverse", From: "consumptions", Limit: 50,
			Order: []platform.RecordSetSort{{Expr: platform.ExprReference("text", "id")}},
		}},
		"reads": map[string]any{"collections": map[string]any{
			"consumptions": map[string]any{"app": "build", "object": sliceType + ".allocation", "set": "consumptions", "policy": "complete"},
		}},
		"balances": []map[string]any{{
			"name": "pool", "title": "Pool",
			"keys":     []string{"subject", "place"},
			"measures": []string{"qty", "amt"},
			"fields":   []map[string]any{{"name": "rate", "title": "Unit rate", "type": "decimal", "live": rateLive}},
			"constraints": []platform.Constraint{{
				Name: "available", Kind: "expression", Fields: []string{"qty"},
				Expr:    &platform.Expr{Op: ">=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("decimal", "qty"), lit("decimal", 0)}},
				Message: "Not enough pool quantity",
			}},
		}},
		"depletion": map[string]any{
			"ledger":   sliceType,
			"balance":  "remaining",
			"when":     isOldest,
			"origin":   "origin",
			"consumer": "consumer",
			"quantity": "qty",
			"amount":   "amt",
		},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "SLICE", nil)
	live.accept(t, "build.object.publish", build.ObjectType, "ENTRY", nil)

	live.accept(t, tierType+".create", tierType, "TW", map[string]any{"title": "Weighted", "method": "weighted"})
	live.accept(t, tierType+".create", tierType, "TO", map[string]any{"title": "Oldest", "method": "oldest"})
	live.accept(t, subjectType+".create", subjectType, "SW", map[string]any{"title": "Weighted Subject", "tier": "TW"})
	live.accept(t, subjectType+".create", subjectType, "SO", map[string]any{"title": "Oldest Subject", "tier": "TO"})

	live.accept(t, entryType+".create", entryType, "W1", map[string]any{"subject": "SW", "place": "P1", "qty": 10, "inputamt": 50, "on": "2026-10-01"})
	live.accept(t, entryType+".create", entryType, "W2", map[string]any{"subject": "SW", "place": "P1", "qty": 10, "inputamt": 70, "on": "2026-10-02"})
	live.accept(t, entryType+".create", entryType, "W3", map[string]any{"subject": "SW", "place": "P1", "qty": -15, "on": "2026-10-03"})

	readEntry := func(l *invariantTenant, id string) map[string]any {
		t.Helper()
		rv, err := l.RecordOf(t.Context(), l.member(t), entryType, id, invariantClock)
		if err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		var out map[string]any
		_ = json.Unmarshal(platform.Raw(rv.Record), &out)
		return out
	}
	readPool := func(l *invariantTenant, subj string) map[string]any {
		t.Helper()
		domain, _ := json.Marshal([][]any{{"subject", "=", subj}})
		page, err := l.Records(t.Context(), l.member(t), poolType, platform.Query{Domain: domain}, invariantClock)
		if err != nil || len(page.Records) != 1 {
			t.Fatalf("pool %s: %+v %v", subj, page, err)
		}
		var out map[string]any
		_ = json.Unmarshal(platform.Raw(page.Records[0]), &out)
		return out
	}

	if got := readEntry(live, "W3")["amt"]; got != float64(-90) {
		t.Fatalf("weighted outbound amt = %v, want -90", got)
	}
	wPool := readPool(live, "SW")
	if wPool["qty"] != float64(5) || wPool["amt"] != float64(30) || wPool["rate"] != float64(6) {
		t.Fatalf("weighted pool = %+v, want qty=5 amt=30 rate=6", wPool)
	}
	period, refusal := live.AggregateObjectSet(live.member(t), objects.ObjectSetRequest{
		Set:      platform.ObjectSet{Object: poolType, Domain: platform.Raw([][]any{{"subject", "=", "SW"}})},
		Groups:   []string{"subject"},
		Measures: []string{"sum:amt"},
		From:     "2026-10-01",
		To:       "2026-10-31",
	}, invariantClock)
	if refusal != nil || len(period.Rows) != 1 {
		t.Fatalf("weighted period: %+v %v", period, refusal)
	}
	for k, want := range map[string]string{"opening:amt": "0", "debit:amt": "120", "credit:amt": "90", "closing:amt": "30"} {
		if string(platform.Raw(period.Rows[0][k])) != want {
			t.Fatalf("weighted period %s = %s, want %s", k, platform.Raw(period.Rows[0][k]), want)
		}
	}

	live.accept(t, entryType+".create", entryType, "F1", map[string]any{"subject": "SO", "place": "P1", "qty": 10, "inputamt": 50, "on": "2026-10-01"})
	live.accept(t, entryType+".create", entryType, "F2", map[string]any{"subject": "SO", "place": "P1", "qty": 10, "inputamt": 70, "on": "2026-10-02"})
	live.accept(t, entryType+".create", entryType, "F3", map[string]any{"subject": "SO", "place": "P1", "qty": -15, "on": "2026-10-03"})

	if got := readEntry(live, "F3")["amt"]; got != float64(-85) {
		t.Fatalf("oldest outbound amt = %v, want -85", got)
	}
	fPool := readPool(live, "SO")
	if fPool["qty"] != float64(5) || fPool["amt"] != float64(35) || fPool["rate"] != float64(7) {
		t.Fatalf("oldest pool = %+v, want qty=5 amt=35 rate=7", fPool)
	}

	rt := runtime{t: live.Tenant, scoped: true}
	setRes, refusal := rt.ReadRecordSet(
		platform.NewCaller(rt, live.member(t), build.ID, false, false),
		entryType, "F3", "consumptions", invariantClock,
	)
	if refusal != nil || len(setRes.Records) != 2 {
		t.Fatalf("F3 consumptions set: %+v %v", setRes, refusal)
	}

	for _, originID := range []string{"F1", "F2"} {
		_, err := live.Submit(live.member(t), live.submission(entryType+".reverse", entryType, originID, map[string]any{"on": "2026-10-04"}), invariantClock)
		if err == nil || !strings.Contains(err.Message, "F3") {
			t.Fatalf("expected %s reversal refusal naming F3, got %+v", originID, err)
		}
	}

	live.accept(t, tierType+".edit", tierType, "TO", map[string]any{"method": "weighted"})
	if got := readEntry(live, "F3")["amt"]; got != float64(-85) {
		t.Fatalf("switching method mutated F3.amt to %v", got)
	}
	live.accept(t, entryType+".create", entryType, "F4", map[string]any{"subject": "SO", "place": "P1", "qty": 5, "inputamt": 45, "on": "2026-10-04"})
	live.accept(t, entryType+".create", entryType, "F5", map[string]any{"subject": "SO", "place": "P1", "qty": -4, "on": "2026-10-05"})
	if got := readEntry(live, "F5")["amt"]; got != float64(-32) {
		t.Fatalf("switched weighted outbound F5.amt = %v, want -32", got)
	}
	live.accept(t, entryType+".reverse", entryType, "F5", map[string]any{"on": "2026-10-06"})
	live.accept(t, entryType+".reverse", entryType, "F4", map[string]any{"on": "2026-10-06"})
	live.accept(t, tierType+".edit", tierType, "TO", map[string]any{"method": "oldest"})

	live.accept(t, entryType+".reverse", entryType, "F3", map[string]any{"on": "2026-10-07"})
	afterRevF3 := readPool(live, "SO")
	if afterRevF3["qty"] != float64(20) || afterRevF3["amt"] != float64(120) {
		t.Fatalf("pool after F3 reverse = %+v, want 20/120", afterRevF3)
	}
	live.accept(t, entryType+".reverse", entryType, "F1", map[string]any{"on": "2026-10-08"})
	afterRevF1 := readPool(live, "SO")
	if afterRevF1["qty"] != float64(10) || afterRevF1["amt"] != float64(70) || afterRevF1["rate"] != float64(7) {
		t.Fatalf("pool after F1 reverse = %+v, want 10/70/7", afterRevF1)
	}

	remPage, err := live.Records(t.Context(), live.member(t), remType, platform.Query{}, invariantClock)
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range remPage.Records {
		var r map[string]any
		_ = json.Unmarshal(platform.Raw(raw), &r)
		if r["origin"] == "F1" && (r["qty"] != nil && r["qty"] != float64(0) || r["amt"] != nil && r["amt"] != float64(0)) {
			t.Fatalf("F1 remaining slice not zeroed after reverse: %+v", r)
		}
		if r["origin"] == "F2" && (r["qty"] != float64(10) || r["amt"] != float64(70)) {
			t.Fatalf("F2 remaining slice mismatch: %+v", r)
		}
	}

	for _, ledger := range []string{entryType, sliceType} {
		if diff, err := live.Projection.ReconcileLedger(ledger); err != nil || len(diff) > 0 {
			t.Fatalf("reconcile %s: %v %v", ledger, diff, err)
		}
	}
	beforeImg := live.recordImage(t)
	fresh := live.restore(t, live.entries)
	afterImg := fresh.recordImage(t)
	for _, typ := range []string{entryType, sliceType} {
		if string(beforeImg[typ]) != string(afterImg[typ]) {
			t.Fatalf("replay byte mismatch for %s:\nbefore=%s\nafter=%s", typ, beforeImg[typ], afterImg[typ])
		}
	}
	if string(platform.Raw(readPool(fresh, "SW"))) != string(platform.Raw(readPool(live, "SW"))) ||
		string(platform.Raw(readPool(fresh, "SO"))) != string(platform.Raw(readPool(live, "SO"))) {
		t.Fatal("replay pool balance mismatch")
	}
}
