package platformserver

import (
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

// A goods receipt defined in the builder gets a document number, posts a
// balanced entry into the books of its month, is refused into a closed month
// and lands in the next open one instead, and reverses by opposite entry.
func TestBooksFromBuilderActions(t *testing.T) {
	seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana", Roles: map[string]string{build.ID: build.Builder, core.ID: core.Accountant}}}
	tn, err := NewTenant("books", NewConsole("books", seat), core.New("books"), build.New("books"))
	if err != nil {
		t.Fatal(err)
	}
	member, _ := tn.Member("dana")
	at := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	submit := func(key, app, schema, typ, target, payload string, when time.Time) {
		t.Helper()
		if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: app, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, when); err != nil {
			t.Fatalf("%s: %s %s", key, err.Code, err.Message)
		}
	}
	submit("obj", build.ID, build.ObjectType+".create", build.ObjectType, "O1", `{"name":"goodsreceipt","title":"Goods receipt",
		"fields":[{"name":"number","title":"Number","type":"text"},{"name":"amount","title":"Amount","type":"decimal"},{"name":"receivedon","title":"Received on","type":"text"},{"name":"supplier","title":"Supplier","type":"text"}],
		"numbering":{"field":"number","prefix":"GR","yearly":true,"width":4},
		"states":[{"name":"open","title":"Open"},{"name":"posted","title":"Posted"},{"name":"cancelled","title":"Cancelled"}],
		"actions":[{"name":"post","title":"Post","from":["open"],"to":"posted","journal":{"date":"record.receivedon","text":"=Goods receipt","lines":[{"account":"=1403","debit":"record.amount","object":"record.number"},{"account":"=2290","credit":"record.amount","partner":"record.supplier"}]}},
		           {"name":"cancel","title":"Cancel","from":["posted"],"to":"cancelled","reverses":"post"}]}`, at)
	submit("obj-publish", build.ID, build.SchemaPublish, build.ObjectType, "O1", `{}`, at)
	submit("gr1", build.ID, "build.goodsreceipt.create", "build.goodsreceipt", "gr1", `{"amount":120.5,"receivedon":"2026-10-05","supplier":"ACME"}`, at)
	submit("gr2", build.ID, "build.goodsreceipt.create", "build.goodsreceipt", "gr2", `{"amount":80,"receivedon":"2026-09-28","supplier":"ACME"}`, at)
	for _, id := range []string{"gr1", "gr2"} {
		held, _ := tn.Held("build.goodsreceipt/" + id)
		if n := toMap(held)["number"]; n != "GR2026-000"+map[string]string{"gr1": "1", "gr2": "2"}[id] {
			t.Fatalf("%s numbered %v", id, n)
		}
	}
	// Close September, then post both: October's lands in October, September's shifts to October and says so.
	submit("close-sep", core.ID, core.PeriodType+".close", core.PeriodType, "per-2026-09", `{}`, at)
	submit("post1", build.ID, "build.goodsreceipt.post", "build.goodsreceipt", "gr1", `{}`, at)
	submit("post2", build.ID, "build.goodsreceipt.post", "build.goodsreceipt", "gr2", `{}`, at)
	tn.Dispatch(at) // the books lane sends one entry at a time, in order
	tn.Dispatch(at)
	c := tn.automation(core.ID, false)
	entries, _, _ := platform.Find[core.Journal](c, platform.Query{Limit: 10, Sort: []string{"number"}})
	if len(entries) != 2 {
		t.Fatalf("want two entries, got %d: %+v", len(entries), tn.Effects(at))
	}
	if e := entries[0]; e.Number != "2026-10-0001" || e.Period != "2026-10" || e.Shifted || e.Total != 120.5 || e.Lines[1].Partner != "ACME" {
		t.Fatalf("entry 1 %+v", e)
	}
	if e := entries[1]; e.Number != "2026-10-0002" || !e.Shifted || e.Date != "2026-09-28" {
		t.Fatalf("September's receipt should post to October, marked: %+v", e)
	}
	// Reverse through the object's cancel: an opposite entry, the balance nets to zero.
	submit("cancel1", build.ID, "build.goodsreceipt.cancel", "build.goodsreceipt", "gr1", `{}`, at.Add(time.Minute))
	tn.Dispatch(at.Add(time.Minute))
	tb, _ := tn.app(core.ID).Read(c, core.ReadTrialBalance+"/2026-10")
	balance := tb.(core.TrialBalance)
	if len(balance.Rows) != 2 || balance.Debit != balance.Credit || balance.Rows[0].Account != "1403" || balance.Rows[0].Balance != 80 {
		t.Fatalf("trial balance %+v", balance)
	}
	// An unbalanced hand entry and one into a month nobody opened are refused.
	if _, err := tn.Submit(member, &pb.Submission{TenantId: tn.ID, PrincipalId: member.ID, Authority: core.ID, IdempotencyKey: "bad",
		Target: &pb.EntityRef{Type: core.JournalType, Id: "bad"}, Schema: &pb.SchemaRef{Name: core.JournalType + ".create", Version: 1},
		Payload: []byte(`{"date":"2026-10-07","lines":[{"account":"1001","debit":10},{"account":"2202","credit":9}]}`)}, at); err == nil || !strings.Contains(err.Message, "balance") {
		t.Fatalf("unbalanced entry: %v", err)
	}
	// Close October too: a late receipt waits in the books queue until a period opens.
	submit("close-oct", core.ID, core.PeriodType+".close", core.PeriodType, "per-2026-10", `{}`, at)
	for _, p := range []string{"11", "12"} {
		submit("close-"+p, core.ID, core.PeriodType+".close", core.PeriodType, "per-2026-"+p, `{}`, at)
	}
	submit("gr3", build.ID, "build.goodsreceipt.create", "build.goodsreceipt", "gr3", `{"amount":5,"receivedon":"2026-10-06","supplier":"ACME"}`, at)
	submit("post3", build.ID, "build.goodsreceipt.post", "build.goodsreceipt", "gr3", `{}`, at)
	tn.Dispatch(at.Add(2 * time.Minute))
	if entries, _, _ = platform.Find[core.Journal](c, platform.Query{Limit: 10}); len(entries) != 3 {
		t.Fatalf("the late receipt must wait, got %d entries", len(entries))
	}
	submit("reopen-nov", core.ID, core.PeriodType+".reopen", core.PeriodType, "per-2026-11", `{}`, at)
	tn.Dispatch(at.Add(10 * time.Minute))
	if entries, _, _ = platform.Find[core.Journal](c, platform.Query{Limit: 10}); len(entries) != 4 {
		t.Fatalf("after November opens the receipt lands, got %d entries: %+v", len(entries), tn.Deliveries())
	}
}
