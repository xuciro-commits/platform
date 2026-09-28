package manufacturing

import (
	"testing"
	"time"

	"erp"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/platform"
)

func TestAcceptedResultManufacturingProbe(t *testing.T) {
	seat := platformserver.Seat{Subjects: []string{"controller"}, Member: platform.Member{
		ID: "controller", Roles: map[string]string{erp.ID: erp.Controller}}}
	build := func() *platformserver.Tenant {
		tn, err := NewTenant("result-factory", erp.New("result-factory"), seat)
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
	m, _ := tn.Member("controller")
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	s := &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: erp.ID, IdempotencyKey: "probe-account",
		Target:  &pb.EntityRef{Type: erp.AccountType, Id: "1010"},
		Schema:  &pb.SchemaRef{Name: erp.AccountType + ".create", Version: 1},
		Payload: []byte(`{"name":"Cash","kind":"asset"}`)}
	if _, err := tn.Submit(m, s, now); err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Kind != "accepted-result" {
		t.Fatalf("not a committed result: %+v", entries)
	}
	platformserver.CheckReplay(t, tn, entries, build)
}
