package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestAcceptedConsoleCoreSeedReplay(t *testing.T) {
	compose := func(newSeed bool) *Tenant {
		roles := map[string]string{PlatformApp: Admin}
		if newSeed {
			roles["core"] = "steward"
		}
		seat := Seat{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: roles}}
		tn, err := NewTenant("core-seed", NewConsole("core-seed", seat), core.New("core-seed"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose(false)
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	live.Record = func(e Entry) {
		if e.Kind != "accepted-result" {
			entries = append(entries, e)
		}
	}
	admin, _ := live.Member("admin")
	// Old journals can contain platform settings before the first member image.
	setting := &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: "set-name",
		Target: &pb.EntityRef{Type: SettingType, Id: "platform/name"}, Schema: &pb.SchemaRef{Name: SchemaSettingSet, Version: 1}, Payload: []byte(`{"value":"Existing tenant"}`)}
	if _, err := live.Submit(admin, setting, time.Now()); err != nil {
		t.Fatal(err)
	}
	sub := &pb.Submission{TenantId: live.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: "add-member",
		Target: &pb.EntityRef{Type: MemberType, Id: "other"}, Schema: &pb.SchemaRef{Name: SchemaAdd, Version: 1}, Payload: []byte(`{"subject":"user:other@example.test"}`)}
	if _, err := live.Submit(admin, sub, time.Now()); err != nil || len(entries) != 2 {
		t.Fatalf("initial receipt: %v", err)
	}
	restored := compose(true)
	batch, _, err := decodeAcceptedBatch(entries[1].Body)
	if err != nil {
		t.Fatal(err)
	}
	prior, _ := restored.app(PlatformApp).(*Console).AcceptedState()
	if !restored.legacyCoreSeedPredecessor(batch.States[0], prior) {
		t.Fatal("new implicit seed role not recognized")
	}
	var drift consoleState
	_ = json.Unmarshal(prior, &drift)
	drift.Members["admin"].Language = "en"
	damaged, _ := json.Marshal(drift)
	if restored.legacyCoreSeedPredecessor(batch.States[0], damaged) {
		t.Fatal("unrelated directory drift bypassed the predecessor")
	}
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	after, _ := restored.app(PlatformApp).(*Console).AcceptedState()
	a, _ := canonicalDigest(after)
	b, _ := canonicalDigest(batch.States[0].Image)
	if a != b {
		t.Fatal("replay changed the authoritative historical image")
	}
	member, _ := restored.Member("admin")
	if member.Roles["core"] != "" {
		t.Fatal("a bootstrap change granted a role outside a decision")
	}
	if restored.legacyCoreSeedPredecessor(batch.States[0], after) {
		t.Fatal("seed projection accepted after the first console receipt")
	}
	CheckReplay(t, live, entries, func() *Tenant { return compose(true) })
}
