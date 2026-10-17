package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// A builder tries an action as a user: the dry run shows what it would write
// or why it is refused, and nothing of it is kept (ADR-0040 21d).
func TestSimulateIsDiscarded(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	seat := func(id, role string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
	}
	tn, err := NewTenant("sim", NewConsole("sim", seat("dana", build.Builder), seat("eli", build.User)), build.New("sim"))
	if err != nil {
		t.Fatal(err)
	}
	var journal []Entry
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	keys := 0
	sub := func(who, schema, typ, id string, payload any) *pb.Submission {
		keys++
		raw, _ := json.Marshal(payload)
		return &pb.Submission{TenantId: "sim", PrincipalId: who, Authority: build.ID, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}
	}
	must := func(who string, s *pb.Submission) {
		t.Helper()
		if _, err := tn.Submit(member(who), s, now); err != nil {
			t.Fatalf("%s: %s %s", s.GetSchema().GetName(), err.Code, err.Message)
		}
	}
	must("dana", sub("dana", build.ObjectType+".create", build.ObjectType, "O1", map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}},
		"states": []map[string]any{{"name": "open", "title": "Open"}, {"name": "done", "title": "Done"}},
		"actions": []map[string]any{{"name": "close", "title": "Close", "from": []string{"open"}, "to": "done",
			"conditions": []map[string]any{{"field": "guest", "operator": "not empty", "message": "A visit needs its guest"}}}}}))
	must("dana", sub("dana", build.SchemaPublish, build.ObjectType, "O1", map[string]any{}))
	visit := build.TypeOf("visit")
	must("eli", sub("eli", visit+".create", visit, "V1", map[string]any{"guest": "Ada"}))
	must("eli", sub("eli", visit+".create", visit, "V2", map[string]any{}))
	before, entries := snapshot(tn), len(journal)

	out, kerr := tn.Simulate(member("dana"), "eli", sub("eli", visit+".close", visit, "V1", map[string]any{}), now)
	if kerr != nil {
		t.Fatalf("dry run error: %v", kerr)
	}
	if !out.Accepted || len(out.Changes) != 1 || !strings.Contains(string(out.Changes[0].Record), `"state":"done"`) {
		t.Fatalf("dry run of an allowed action: %+v", out)
	}
	out, kerr = tn.Simulate(member("dana"), "eli", sub("eli", visit+".close", visit, "V2", map[string]any{}), now)
	if kerr != nil || out.Accepted || !strings.Contains(out.Refusal, "A visit needs its guest") {
		t.Fatalf("dry run of a refused action: %+v, %v", out, err)
	}
	if _, err := tn.Simulate(member("eli"), "", sub("eli", visit+".close", visit, "V1", map[string]any{}), now); err == nil {
		t.Fatal("a non-builder ran a dry run")
	}
	if snapshot(tn) != before || len(journal) != entries {
		t.Fatal("a dry run left records, notices or journal entries behind")
	}
}

func TestSimulationResultsUseBuilderReadPermissions(t *testing.T) {
	tn := stockTenant(t, build.New("t-1"))
	console := tn.app(PlatformApp).(*Console)
	console.members["op"].Roles[build.ID] = build.Builder
	builder, _ := tn.Member("op") // reads only line L1, without the lead-only secret
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	before := snapshot(tn)
	for _, line := range []string{"L1", "L2"} {
		payload, _ := json.Marshal(map[string]string{"name": "Private part", "line": line, "secret": "lead-only value"})
		sub := &pb.Submission{TenantId: "original", PrincipalId: "original", Authority: "original",
			Target: &pb.EntityRef{Type: "stock.item", Id: "TEST"}, Schema: &pb.SchemaRef{Name: "stock.item.create", Version: 1}, Payload: payload}
		original := proto.Clone(sub)
		out, err := tn.Simulate(builder, "lead", sub, now)
		if err != nil || !out.Accepted {
			t.Fatalf("simulate %s: %+v, %v", line, out, err)
		}
		if !proto.Equal(original, sub) {
			t.Fatal("simulation altered the supplied submission")
		}
		if line == "L1" {
			if len(out.Changes) != 1 || strings.Contains(string(out.Changes[0].Record), "lead-only value") {
				t.Fatalf("field read mask not applied: %+v", out)
			}
		} else if len(out.Changes) != 0 {
			t.Fatalf("out-of-scope test record leaked: %+v", out)
		}
	}
	foreign := builder
	foreign.Tenant = "foreign"
	if _, err := tn.Simulate(foreign, "lead", nil, now); err == nil {
		t.Fatal("foreign builder simulated this tenant")
	}
	if snapshot(tn) != before {
		t.Fatal("simulation changed production records or audit state")
	}
}
