package crm

import (
	"encoding/json"
	"platformserver"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

func submit(c *CRM, who platformserver.Caller, schema, targetType, id, key string, payload map[string]string) string {
	raw, _ := json.Marshal(payload)
	_, err := c.Submit(who, &pb.Submission{TenantId: "t", PrincipalId: who.ID, Authority: Authority,
		Target: &pb.EntityRef{Type: targetType, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1},
		IdempotencyKey: key, Payload: raw}, time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC))
	if err != nil {
		return err.Error()
	}
	return "ok"
}

func TestOpportunityOwnership(t *testing.T) {
	c := New("t")
	as := func(id string, role Role) platformserver.Caller {
		return platformserver.As("crm", platformserver.Member{ID: id, Tenant: "t", Roles: map[string]string{"crm": string(role)}})
	}
	ana, bo, lead := as("ana", Sales), as("bo", Sales), as("lead", Manager)
	for _, step := range []struct {
		got, want string
	}{
		{submit(c, ana, SchemaAccount, AccountType, "ACME", "1", map[string]string{"name": "Acme", "kind": "company"}), "ok"},
		{submit(c, ana, SchemaAccount, AccountType, "X", "2", map[string]string{"name": "X", "kind": "shop"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{submit(c, ana, SchemaOpen, OpportunityType, "O-1", "3", map[string]string{"account": "NOPE", "title": "t"}), "ERROR_CODE_INVALID_ARGUMENT"},
		{submit(c, ana, SchemaOpen, OpportunityType, "O-1", "4", map[string]string{"account": "ACME", "title": "Offsite"}), "ok"},
		{submit(c, bo, SchemaClose, OpportunityType, "O-1", "5", map[string]string{"outcome": "won"}), "ERROR_CODE_POLICY_DENIED"},
		{submit(c, lead, SchemaClose, OpportunityType, "O-1", "6", map[string]string{"outcome": "won"}), "ok"},
		{submit(c, ana, SchemaClose, OpportunityType, "O-1", "7", map[string]string{"outcome": "lost"}), "ERROR_CODE_CONFLICT"},
		{submit(c, as("desk", "front-desk"), SchemaAccount, AccountType, "Y", "8", map[string]string{"name": "Y", "kind": "person"}), "ERROR_CODE_POLICY_DENIED"},
	} {
		if step.got != step.want {
			t.Fatalf("got %s, want %s", step.got, step.want)
		}
	}
	if o := c.Opportunities()[0]; o.Stage != "won" || o.Owner != "ana" || o.Revision != 2 {
		t.Fatalf("opportunity %+v", o)
	}
}
