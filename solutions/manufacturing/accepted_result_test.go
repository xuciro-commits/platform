package manufacturing

import (
	"testing"
	"time"

	"erp"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestAcceptedResultManufacturingProbe(t *testing.T) {
	seat := platformserver.Seat{Subjects: []string{"controller"}, Member: platform.Member{
		ID: "controller", Roles: map[string]string{erp.ID: erp.Controller, build.ID: build.Builder}}}
	compose := func() *platformserver.Tenant {
		tn, err := NewTenant("result-factory", erp.New("result-factory"), seat)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	var entries []platformserver.Entry
	tn.Record = func(e platformserver.Entry) { entries = append(entries, e) }
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
	bad := &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: erp.ID,
		IdempotencyKey: "refused-account", Target: &pb.EntityRef{Type: erp.AccountType, Id: "9999"},
		Schema: &pb.SchemaRef{Name: erp.AccountType + ".create", Version: 1}, Payload: []byte(`{"kind":"asset"}`)}
	if _, refusal := tn.Submit(m, bad, now); refusal == nil || len(entries) != 2 {
		t.Fatalf("a refused ERP account had no durable answer: %v, entries=%d", refusal, len(entries))
	}
	if _, refusal := tn.Submit(m, bad, now.Add(time.Hour)); refusal == nil || len(entries) != 2 {
		t.Fatalf("the refused account was rerun: %v, entries=%d", refusal, len(entries))
	}
	for _, action := range []struct {
		key, schema string
	}{{"open-period", erp.SchemaPeriodOpen}, {"close-period", erp.PeriodType + ".close"},
		{"reopen-period", erp.PeriodType + ".reopen"}} {
		if _, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID,
			Authority: erp.ID, IdempotencyKey: action.key,
			Target: &pb.EntityRef{Type: erp.PeriodType, Id: "2026-10"},
			Schema: &pb.SchemaRef{Name: action.schema, Version: 1}, Payload: []byte(`{}`)}, now); err != nil {
			t.Fatalf("%s: %v", action.schema, err)
		}
	}
	if len(entries) != 5 || entries[3].Kind != "accepted-result" || entries[4].Kind != "accepted-result" {
		t.Fatalf("accounting period transitions escaped the result path: %+v", entries)
	}
	for _, action := range []struct {
		key, schema, typ, target, payload string
	}{{"draft-batch", build.ObjectType + ".create", build.ObjectType, "O1",
		`{"name":"batch","title":"Batch","fields":[{"name":"lot","title":"Lot","type":"text"}]}`},
		{"publish-batch", build.SchemaPublish, build.ObjectType, "O1", `{}`},
		{"batch-lot", build.TypeOf("batch") + ".create", build.TypeOf("batch"), "B1", `{"lot":"L1"}`}} {
		if _, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: action.key, Target: &pb.EntityRef{Type: action.typ, Id: action.target},
			Schema: &pb.SchemaRef{Name: action.schema, Version: 1}, Payload: []byte(action.payload)}, now); err != nil {
			t.Fatalf("%s: %v", action.schema, err)
		}
	}
	if len(entries) != 8 || entries[6].Kind != "accepted-result" {
		t.Fatalf("builder publication was not committed: %+v", entries)
	}
	platformserver.CheckReplay(t, tn, entries, compose)
}
