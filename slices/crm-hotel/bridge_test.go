package crmhotel

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"crm"
	"hotel"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

var (
	t0        = time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	sales     = Member{ID: "sales-1", Tenant: "hotel-a", Roles: map[string]string{"crm": "sales", "hotel": "front-desk"}}
	salesOnly = Member{ID: "sales-2", Tenant: "hotel-a", Roles: map[string]string{"crm": "sales"}}
	desk      = Member{ID: "desk-1", Tenant: "hotel-a", Roles: map[string]string{"hotel": "front-desk"}}
	manager   = Member{ID: "manager-1", Tenant: "hotel-a", Roles: map[string]string{"crm": "sales-manager", "hotel": "manager"}}
)

func sub(who Member, schema, targetType, id, key string, payload any) *pb.Submission {
	raw, _ := json.Marshal(payload)
	return &pb.Submission{TenantId: "hotel-a", PrincipalId: who.ID, Authority: "-", Target: &pb.EntityRef{Type: targetType, Id: id},
		Schema: &pb.SchemaRef{Name: schema, Version: 1}, IdempotencyKey: key, Payload: raw}
}

func code(_ *pb.ChangeRecord, err interface{ Error() string }) string {
	if err == nil || fmt.Sprint(err) == "<nil>" {
		return "ok"
	}
	return err.Error()
}

// setup: an account with one open opportunity, and a hotel with a single suite.
func setup(t *testing.T) *Tenant {
	tn := NewTenant("hotel-a", map[string]hotel.RoomType{"suite": {Rooms: 1}})
	for _, s := range []*pb.Submission{
		sub(sales, crm.SchemaAccount, crm.AccountType, "ACME", "a", map[string]string{"name": "Acme Corp", "kind": "company"}),
		sub(sales, crm.SchemaOpen, crm.OpportunityType, "OPP-1", "o", map[string]string{"account": "ACME", "title": "Board offsite"}),
	} {
		s.Authority = crm.Authority
		if _, err := tn.CRM.Submit(sales.CRM(), s, t0); err != nil {
			t.Fatal(err)
		}
	}
	return tn
}

func book(tn *Tenant, who Member, opp, key, checkIn string) string {
	s := sub(who, SchemaBook, LinkType, opp, key, map[string]string{"roomType": "suite", "checkIn": checkIn, "checkOut": "2026-10-03", "guest": "Acme board"})
	s.Authority = Authority
	r, err := tn.Bridge.Submit(who, s, t0)
	if err != nil {
		return err.Error()
	}
	return code(r, nil)
}

func stays(tn *Tenant) []hotel.Reservation { return tn.Bridge.Customers()[0].Opportunities[0].Stays }

func TestBookingCrossesPackagesThroughDeclaredActions(t *testing.T) {
	tn := setup(t)
	if got := book(tn, sales, "OPP-1", "b-1", "2026-10-01"); got != "ok" {
		t.Fatalf("book: %s", got)
	}
	if got := book(tn, sales, "OPP-1", "b-1", "2026-10-01"); got != "ok" || len(tn.Hotel.Reservations()) != 1 {
		t.Fatalf("a resend must not book twice: %s, %d reservations", got, len(tn.Hotel.Reservations()))
	}
	if s := stays(tn); len(s) != 1 || s[0].ID != "OPP-1-R1" || s[0].Guest != "Acme board" {
		t.Fatalf("customer view: %+v", s)
	}
	// The hotel's rules decide: the only suite is taken, so the bridge links nothing.
	if got := book(tn, sales, "OPP-1", "b-2", "2026-10-02"); got != "ERROR_CODE_CONFLICT" || len(stays(tn)) != 1 {
		t.Fatalf("sold out: %s, %d stays", got, len(stays(tn)))
	}
	// Each package checks its own role: no hotel role, or no CRM role, is refused.
	if got := book(tn, salesOnly, "OPP-1", "b-3", "2026-10-05"); got != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("without a hotel role: %s", got)
	}
	if got := book(tn, desk, "OPP-1", "b-4", "2026-10-05"); got != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("without a CRM role: %s", got)
	}
	// The hotel cancels on its own; the customer view reads it live.
	cancel := sub(manager, hotel.SchemaCancel, hotel.ReservationType, "OPP-1-R1", "c-1", struct{}{})
	cancel.Authority = hotel.Authority
	if _, err := tn.Hotel.Submit(manager.Hotel(), cancel, t0); err != nil {
		t.Fatal(err)
	}
	if !stays(tn)[0].Canceled {
		t.Fatal("the cancellation does not show on the customer")
	}
	// A lost opportunity takes no more bookings.
	close := sub(sales, crm.SchemaClose, crm.OpportunityType, "OPP-1", "l-1", map[string]string{"outcome": "lost"})
	close.Authority = crm.Authority
	if _, err := tn.CRM.Submit(sales.CRM(), close, t0); err != nil {
		t.Fatal(err)
	}
	if got := book(tn, sales, "OPP-1", "b-5", "2026-10-06"); got != "ERROR_CODE_CONFLICT" {
		t.Fatalf("lost opportunity: %s", got)
	}
}

func TestMemberCatalogIsTheUnionOfPackageGrants(t *testing.T) {
	tn := setup(t)
	var got []string
	for _, a := range append(append(tn.CRM.Catalog(sales.CRM()), tn.Bridge.Catalog(sales)...), tn.Hotel.Catalog(sales.Hotel())...) {
		got = append(got, a.Schema)
	}
	want := fmt.Sprint([]string{crm.SchemaAccount, crm.SchemaOpen, crm.SchemaClose, SchemaBook, hotel.SchemaCreate, hotel.SchemaModify})
	if fmt.Sprint(got) != want {
		t.Fatalf("catalog %v, want %v", got, want)
	}
	if len(tn.Bridge.Catalog(desk)) != 0 || len(tn.Bridge.Catalog(salesOnly)) != 0 {
		t.Fatal("booking is offered without both the CRM and the hotel grant")
	}
}
