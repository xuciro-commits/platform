package csm

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func build(t *testing.T) *platformserver.Tenant {
	seat := func(id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
	}
	tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t",
		seat("dee", map[string]string{ID: Desk}), seat("lee", map[string]string{ID: Lead}),
		seat("mail", map[string]string{ID: Desk})), // an outside mail gateway's service account
		platformserver.NewAI("t"), work.New("t"), flow.New("t"), platformserver.NewAgents("t"), New("t"))
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

// ADR-0025 D3: customer service works alone — no CRM, no model: the desk
// opens, triages and answers tickets, numbered without gaps; a mail gateway
// outside opens one through the same action; a ticket unanswered when due is
// escalated to the leads by the service-level flow.
func TestTicketsStandalone(t *testing.T) {
	var journal []platformserver.Entry
	tn := build(t)
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, schema, id string, payload any) string {
		keys++
		m, _ := tn.Member(who)
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(m, &pb.Submission{TenantId: "t", PrincipalId: who, Authority: ID, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: TicketType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	expect := func(what string, got any, want string) {
		t.Helper()
		if fmt.Sprint(got) != want {
			t.Fatalf("%s: got %v, want %s", what, got, want)
		}
	}
	ticket := func(id string) Ticket {
		lee, _ := tn.Member("lee")
		v, err := tn.RecordOf(lee, TicketType, id, now)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return v.Record.(Ticket)
	}
	tick := func(d time.Duration) {
		now = now.Add(d)
		tn.Think(now)
		tn.Work(now)
	}

	expect("no subject", do("dee", SchemaOpen, "T-0", map[string]string{"customer": "a@x.test"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect("open", do("dee", SchemaOpen, "T-1", map[string]string{"subject": "Wifi keeps dropping", "customer": "anna@acme.test"}), "ok")
	expect("numbered without a gap", ticket("T-1").Number, "CS-2026-0001")
	tick(2 * time.Second) // no model: the flow asks the desk
	expect("triage", do("dee", SchemaTriage, "T-1", map[string]string{"category": "technical", "priority": "high"}), "ok")
	expect("due", ticket("T-1").Due.Sub(ticket("T-1").Created.At), "4h0m0s")
	expect("reply", do("dee", SchemaReply, "T-1", map[string]string{"reply": "The front desk resets it."}), "ok")
	expect("close", do("dee", SchemaClose, "T-1", map[string]string{}), "ok")
	expect("answered by the desk", ticket("T-1").Status+" "+ticket("T-1").Replied, "closed dee")

	// From outside: the mail gateway opens a ticket through the same action.
	expect("by mail", do("mail", SchemaOpen, "T-2", map[string]string{"subject": "Invoice", "customer": "bo@acme.test"}), "ok")
	expect("the same numbering", ticket("T-2").Number, "CS-2026-0002")
	expect("urgent", do("dee", SchemaTriage, "T-2", map[string]string{"category": "billing", "priority": "urgent"}), "ok")
	tick(2 * time.Second)
	tick(time.Hour) // unanswered when due
	tick(2 * time.Second)
	expect("escalated", ticket("T-2").Escalated, "true")
	lee, _ := tn.Member("lee")
	out, _ := tn.Read(lee, "inbox")
	var titles []string
	for _, task := range out.([]work.WorkTask) {
		titles = append(titles, task.Title)
	}
	expect("the lead's inbox", titles, "[Late ticket: Invoice]")
	platformserver.CheckReplay(t, tn, journal, func() *platformserver.Tenant { return build(t) })
}

func TestChinese(t *testing.T) {
	if missing := build(t).Untranslated(ID, "zh-CN"); len(missing) > 0 {
		t.Errorf("add to i18n/zh-CN.json: %q", missing)
	}
}
