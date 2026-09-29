package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

// An approval opened under release A keeps it: activating a release that
// changes its held action is refused until the approval is decided (ADR-0039 D4).
func TestPendingApprovalHoldsItsRelease(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	seat := func(id, role string) Seat {
		return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
	}
	tn, err := NewTenant("bind", NewConsole("bind", seat("dana", build.Builder), seat("eli", build.User), seat("sam", "manager")),
		work.New("bind"), build.New("bind"))
	if err != nil {
		t.Fatal(err)
	}
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	keys := 0
	do := func(who, schema, typ, id string, payload any) {
		t.Helper()
		keys++
		raw, _ := json.Marshal(payload)
		authority := build.ID
		if typ == work.ApprovalType {
			authority = work.ID
		}
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "bind", PrincipalId: who, Authority: authority,
			IdempotencyKey: fmt.Sprint("k", keys), Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			t.Fatalf("%s: %s %s", schema, err.Code, err.Message)
		}
	}
	release := func() (string, error) {
		t.Helper()
		preview, err := tn.PreviewRelease(member("dana"), platform.AssetObject, "O1")
		if err != nil || preview.CandidateID == "" {
			t.Fatalf("preview: %+v, %v", preview, err)
		}
		keys++
		if _, err := tn.SaveReleaseCandidate(member("dana"), platform.AssetObject, "O1", preview.CandidateID, fmt.Sprint("s", keys), now); err != nil {
			t.Fatal(err)
		}
		return tn.ActivateRelease(member("dana"), preview.CandidateID, fmt.Sprint("a", keys), now)
	}
	action := map[string]any{"name": "handback", "title": "Hand it back", "from": []string{"found"}, "to": "returned",
		"roles": []string{"user"}, "approval": map[string]any{"pending": "pending", "levels": []map[string]any{{"title": "Manager", "role": "manager"}}}}
	object := map[string]any{"name": "lost", "title": "Lost item", "fields": []map[string]any{{"name": "item", "title": "Item", "type": "text"}},
		"states":  []map[string]any{{"name": "found", "title": "Found"}, {"name": "pending", "title": "Pending"}, {"name": "returned", "title": "Returned"}},
		"actions": []any{action}, "access": []map[string]any{{"role": "user", "read": "own", "create": true}, {"role": "manager", "read": "all"}}}
	do("dana", build.ObjectType+".create", build.ObjectType, "O1", object)
	do("dana", build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	first, err := release()
	if err != nil {
		t.Fatalf("activate first release: %v", err)
	}
	lost := build.TypeOf("lost")
	do("eli", lost+".create", lost, "L-1", map[string]any{"item": "Umbrella"})
	do("eli", lost+".handback", lost, "L-1", map[string]any{})
	out, _ := tn.Read(member("eli"), "requests")
	var pending work.ApprovalRequest
	for _, r := range out.([]work.ApprovalRequest) {
		if r.State == "pending" {
			pending = r
		}
	}
	if pending.ID == "" || pending.Release != first {
		t.Fatalf("approval did not record its release: %+v", pending)
	}
	// The builder changes the held action and installs it; activating that
	// release must wait for the approval opened under the first one.
	changed := map[string]any{}
	for k, v := range action {
		changed[k] = v
	}
	changed["title"] = "Return it"
	do("dana", build.ObjectType+".edit", build.ObjectType, "O1", map[string]any{"actions": []any{changed}})
	do("dana", build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	if _, err := release(); err == nil || !strings.Contains(err.Error(), pending.ID) || tn.ActiveRelease() != first {
		t.Fatalf("activation ignored a pending approval: %v, active %s", err, tn.ActiveRelease())
	}
	do("sam", work.ApprovalType+".approve", work.ApprovalType, pending.ID, map[string]any{})
	if second, err := release(); err != nil || second == first || tn.ActiveRelease() != second {
		t.Fatalf("activation after the approval was decided: %s, %v", second, err)
	}
}
