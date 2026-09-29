package hospitality

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"time"

	"crm"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/build"
	"platformserver/platform"
	"pms"
)

func TestAcceptedResultHospitalityProbe(t *testing.T) {
	seat := platformserver.Seat{Subjects: []string{"sales"}, Member: platform.Member{
		ID: "sales", Roles: map[string]string{crm.ID: string(crm.Sales), build.ID: build.Builder}}}
	compose := func() *platformserver.Tenant {
		tn, err := NewTenant("result-hotel", map[string]pms.RoomType{"standard": {Rooms: 2}}, seat)
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
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
	bad := &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: crm.ID,
		IdempotencyKey: "refused-account", Target: &pb.EntityRef{Type: crm.AccountType, Id: "INVALID"},
		Schema: &pb.SchemaRef{Name: crm.SchemaAccount, Version: 1}, Payload: []byte(`{"kind":"company"}`)}
	if _, refusal := tn.Submit(m, bad, now); refusal == nil || len(entries) != 2 {
		t.Fatalf("a refused CRM account had no durable answer: %v, entries=%d", refusal, len(entries))
	}
	if _, refusal := tn.Submit(m, bad, now.Add(time.Hour)); refusal == nil || len(entries) != 2 {
		t.Fatalf("the refused account was rerun: %v, entries=%d", refusal, len(entries))
	}
	for _, action := range []struct {
		key, schema, typ, target, payload string
	}{{"draft-visit", build.ObjectType + ".create", build.ObjectType, "O1",
		`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`},
		{"publish-visit", build.SchemaPublish, build.ObjectType, "O1", `{}`},
		{"visit-guest", build.TypeOf("visit") + ".create", build.TypeOf("visit"), "V1", `{"guest":"Ada"}`}} {
		request := &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: build.ID,
			IdempotencyKey: action.key, Target: &pb.EntityRef{Type: action.typ, Id: action.target},
			Schema: &pb.SchemaRef{Name: action.schema, Version: 1}, Payload: []byte(action.payload)}
		accepted, err := tn.Submit(m, request, now)
		if err != nil {
			t.Fatalf("%s: %v", action.schema, err)
		}
		before, _, _ := tn.Snapshot(func() int64 { return 0 })
		count := len(entries)
		replayed, err := tn.Submit(m, proto.Clone(request).(*pb.Submission), now.Add(time.Hour))
		if err != nil || !proto.Equal(replayed, accepted) || len(entries) != count {
			t.Fatalf("%s replay changed receipt or journal: %v", action.schema, err)
		}
		changed := proto.Clone(request).(*pb.Submission)
		changed.Payload = []byte(`{"different":true}`)
		if _, refusal := tn.Submit(m, changed, now); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT || len(entries) != count {
			t.Fatalf("%s conflicting retry changed journal: %v", action.schema, refusal)
		}
		after, _, _ := tn.Snapshot(func() int64 { return 0 })
		if string(before) != string(after) {
			t.Fatalf("%s replay/conflict changed state", action.schema)
		}
	}
	if len(entries) != 5 || entries[3].Kind != "accepted-result" {
		t.Fatalf("builder publication was not committed: %+v", entries)
	}
	platformserver.CheckReplay(t, tn, entries, compose)
}
