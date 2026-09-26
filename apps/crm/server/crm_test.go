package crm

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestOpportunityOwnership(t *testing.T) {
	seat := func(id string, role Role) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{"crm": string(role)}}}
	}
	var journal []platformserver.Entry
	build := func() *platformserver.Tenant {
		tn, err := platformserver.NewTenant("t", platformserver.NewConsole("t", seat("ana", Sales), seat("bo", Sales), seat("lead", Manager), seat("desk", "front-desk"), seat("webform", Sales)), work.New("t"), platformserver.NewFlows("t"), platformserver.NewAgents("t"), New("t"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e platformserver.Entry) { journal = append(journal, e) }
	as := func(id string) platform.Member {
		m := platform.Member{ID: id, Tenant: "t", Roles: map[string]string{}}
		for _, r := range []struct{ id, role string }{{"ana", "sales"}, {"bo", "sales"}, {"lead", "sales-manager"}, {"desk", "front-desk"}, {"webform", "sales"}} {
			if r.id == id {
				m.Roles["crm"] = r.role
			}
		}
		return m
	}
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	submit := func(who, schema, targetType, id, key string, payload map[string]string) string {
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(as(who), &pb.Submission{TenantId: "t", PrincipalId: who, Authority: ID,
			Target: &pb.EntityRef{Type: targetType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
			IdempotencyKey: key, Payload: raw}, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	for _, step := range []struct {
		got, want string
	}{
		{submit("ana", SchemaAccount, AccountType, "ACME", "1", map[string]string{"name": "Acme", "kind": "company"}), "ok"},
		{submit("ana", SchemaAccount, AccountType, "X", "2", map[string]string{"name": "X", "kind": "shop"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{submit("ana", SchemaAccount, AccountType, "ACME", "2b", map[string]string{"name": "Again", "kind": "company"}), "ERROR_CODE_CONFLICT"},
		{submit("ana", SchemaOpen, OpportunityType, "O-1", "3", map[string]string{"account": "NOPE", "title": "t"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{submit("ana", SchemaOpen, OpportunityType, "O-1", "4", map[string]string{"account": "ACME", "title": "Offsite"}), "ok"},
		{submit("bo", SchemaOpen, OpportunityType, "O-2", "4b", map[string]string{"account": "ACME", "title": "Retreat"}), "ok"},
		{submit("bo", SchemaClose, OpportunityType, "O-1", "5", map[string]string{"outcome": "won"}), "ERROR_CODE_POLICY_DENIED"},
		{submit("lead", SchemaClose, OpportunityType, "O-1", "6", map[string]string{"outcome": "won"}), "ok"},
		{submit("ana", SchemaClose, OpportunityType, "O-1", "7", map[string]string{"outcome": "lost"}), "ERROR_CODE_CONFLICT"},
		{submit("desk", SchemaAccount, AccountType, "Y", "8", map[string]string{"name": "Y", "kind": "person"}), "ERROR_CODE_POLICY_DENIED"},
		// Generated edit and archive (ADR-0016 D5): only editable fields change.
		{submit("ana", "crm.account.edit", AccountType, "ACME", "9", map[string]string{"name": "Acme Corp"}), "ok"},
		{submit("ana", "crm.account.edit", AccountType, "ACME", "10", map[string]string{"id": "OTHER"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{submit("ana", "crm.account.archive", AccountType, "ACME", "11", map[string]string{}), "ok"},
		// From outside (ADR-0025 D3): a web form's service account captures an account through the same action.
		{submit("webform", SchemaAccount, AccountType, "WEB-1", "12", map[string]string{"name": "Harbour Travel", "kind": "company"}), "ok"},
		{submit("webform", SchemaAccount, AccountType, "WEB-1", "12", map[string]string{"name": "Harbour Travel", "kind": "company"}), "ok"}, // resent: the same decision
	} {
		if step.got != step.want {
			t.Fatalf("got %s, want %s", step.got, step.want)
		}
	}
	// Each member reads opportunities within their scope: sales their own, a manager all.
	titles := func(who string) []string {
		page, err := tn.Records(as(who), OpportunityType, platform.Query{Sort: []string{"id"}}, now)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, r := range page.Records {
			out = append(out, r.(Opportunity).Title)
		}
		return out
	}
	if got := titles("bo"); len(got) != 1 || got[0] != "Retreat" {
		t.Fatalf("bo sees %v", got)
	}
	if got := titles("lead"); len(got) != 2 {
		t.Fatalf("lead sees %v", got)
	}
	view, err := tn.RecordOf(as("lead"), OpportunityType, "O-1", now)
	if o := view.Record.(Opportunity); err != nil || o.Stage != "won" || o.Owner != "ana" || o.Revision != 2 || len(view.History) != 2 || view.History[0].Schema != SchemaClose {
		t.Fatalf("opportunity %+v %v", view, err)
	}
	accounts, _ := tn.Records(as("lead"), AccountType, platform.Query{Archived: true}, now)
	if a := accounts.Records[0].(Account); a.Name != "Acme Corp" || !a.Archived {
		t.Fatalf("account %+v", a)
	}
	if page, _ := tn.Records(as("lead"), AccountType, platform.Query{}, now); page.Total != 1 || page.Records[0].(Account).ID != "WEB-1" {
		t.Fatal("an archived account is listed, or the web form's is not")
	}
	platformserver.CheckReplay(t, tn, journal, build)
}
