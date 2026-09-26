package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/work"
	"platformserver/platform"
)

// Meaning (ADR-0023 D1): what declarations say about themselves reaches
// agents' prompts and tools, search and the entity descriptions; the tenant's
// glossary layers its own words on top and never changes a declaration.
func TestMeaning(t *testing.T) {
	seat := func(id string, roles map[string]string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
	}
	tn, err := NewTenant("t-1", NewConsole("t-1", seat("ana", map[string]string{"desk": "clerk", KnowledgeApp: KnowledgeEditor, PlatformApp: Admin, AgentApp: AgentAdmin})),
		NewAI("t-1"), work.New("t-1"), NewFlows("t-1"), NewAgents("t-1"), NewKnowledge("t-1"), newDesk("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	ana, _ := tn.app(PlatformApp).(*Console).Member("ana")
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(ana, &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	expect := func(what string, ok bool, got any) {
		t.Helper()
		if !ok {
			t.Errorf("%s: %v", what, got)
		}
	}
	// Declared once, read by everyone: the entity description, the generated
	// field description that tools and forms show, and the agents' prompt.
	var desk platform.EntityInfo
	for _, e := range tn.Entities(ana) {
		if e.Type == "desk.ticket" {
			desk = e
		}
	}
	subject, _ := desk.Field("subject")
	expect("entity", desk.Description == "A customer's request the desk answers." && desk.Synonyms == "case,issue" && subject.Help != "", desk)
	terms := platform.Field{}
	for _, a := range tn.app(KnowledgeApp).Manifest().Actions.All() {
		if a.Schema == TermType+".create" {
			for _, f := range a.Payload {
				if f.Name == "term" {
					terms = f
				}
			}
		}
	}
	expect("generated field description", terms.Description == "Term: The word people here use, e.g. PO", terms.Description)
	expect("meaning", strings.Contains(tn.meaning("desk"), "- desk.ticket (Ticket): A customer's request the desk answers.; also called case,issue\n  - subject (Subject): What the customer asks, in their words; also called topic"), tn.meaning("desk"))

	// Search reads names: a word naming a type narrows the search to it.
	do("desk", "desk.ticket.open", "desk.ticket", "T1", map[string]string{"subject": "Wifi down"})
	do("desk", "desk.ticket.open", "desk.ticket", "T2", map[string]string{"subject": "Breakfast times"})
	hits := func(q string) string {
		var out []string
		for _, h := range tn.Search(&ana, q, now) {
			out = append(out, h.Type+"/"+h.ID)
		}
		return strings.Join(out, " ")
	}
	expect("by synonym", hits("cases wifi") == "desk.ticket/T1", hits("cases wifi"))
	expect("a type's name alone lists it", hits("issue") == "desk.ticket/T1 desk.ticket/T2" || hits("issue") == "desk.ticket/T2 desk.ticket/T1", hits("issue"))

	// The glossary: a term refers to what exists, never makes or renames it.
	expect("a term for nothing", do(KnowledgeApp, TermType+".create", TermType, "X", map[string]string{"term": "Gizmo", "meaning": "?", "refersTo": "desk.gizmo"}) == "ERROR_CODE_INVALID_ARGUMENT", nil)
	expect("a term", do(KnowledgeApp, TermType+".create", TermType, "SR", map[string]string{"term": "SR", "meaning": "A service request: what the desk calls a ticket", "synonyms": "Anfrage", "refersTo": "desk.ticket"}) == "ok", nil)
	expect("search by the glossary", strings.Contains(hits("SR breakfast"), "desk.ticket/T2") && !strings.Contains(hits("SR breakfast"), "T1"), hits("SR breakfast"))
	for _, e := range tn.Entities(ana) {
		if e.Type == "desk.ticket" {
			expect("the declaration is unchanged", e.Title == "Ticket" && e.Synonyms == "case,issue", e)
		}
	}
	a := tn.agents
	req := a.prompt(tn.automation(AgentApp, false), a.defs["desk.triage"], AgentRunRecord{Goal: "x"}, "m", now)
	system := req.Messages[0].Content
	expect("the prompt", strings.Contains(system, "What the records of your app mean:\n- desk.ticket (Ticket)") &&
		strings.Contains(system, "This organisation's own words (its glossary; the records above keep their meaning):\n- SR: A service request: what the desk calls a ticket (also: Anfrage) [desk.ticket]"), system)
}
