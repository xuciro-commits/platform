package hospitality

import (
	"testing"
	"time"

	"crm"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
	"pms"
)

func TestAcceptedResultHospitalityProbe(t *testing.T) {
	seat := platformserver.Seat{Subjects: []string{"sales"}, Member: platform.Member{
		ID: "sales", Roles: map[string]string{crm.ID: string(crm.Sales)}}}
	build := func() *platformserver.Tenant {
		tn, err := NewTenant("result-hotel", map[string]pms.RoomType{"standard": {Rooms: 2}}, seat)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	var entries []platformserver.Entry
	tn.AcceptResult = func(e platformserver.Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	m, _ := tn.Member("sales")
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s := &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: crm.ID, IdempotencyKey: "probe-account",
		Target: &pb.EntityRef{Type: crm.AccountType, Id: "ACME"},
		Schema: &pb.SchemaRef{Name: crm.SchemaAccount, Version: 1}, Payload: []byte(`{"name":"Acme","kind":"company"}`)}
	if _, err := tn.Submit(m, s, now); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "accepted-result" {
		t.Fatalf("not a committed result: %+v", entries)
	}
	platformserver.CheckReplay(t, tn, entries, build)
}
