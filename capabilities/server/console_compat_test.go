package platformserver

import (
	"encoding/json"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

func TestAcceptedConsoleEnterpriseRenameReplay(t *testing.T) {
	compose := func(key string) *Tenant {
		s := Seat{Subjects: []string{"user:admin@example.test"}, Member: platform.Member{ID: "admin", Roles: map[string]string{PlatformApp: Admin, key: "admin"}}}
		tn, err := NewTenant("rename", NewConsole("rename", s), enterprise.New("rename", platform.OrgSeed{}))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	var entries []Entry
	attach := func(tn *Tenant) {
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			entries = append(entries, e)
			return e.Body, nil
		}
	}
	decide := func(tn *Tenant, schema, key, payload string) {
		t.Helper()
		admin, _ := tn.Member("admin")
		sub := &pb.Submission{TenantId: tn.ID, PrincipalId: admin.ID, Authority: PlatformApp, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: MemberType, Id: "admin"}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}
		if _, err := tn.Submit(admin, sub, now); err != nil {
			t.Fatal(err)
		}
	}
	live := compose("org")
	attach(live)
	decide(live, SchemaGrant, "old-grant", `{"app":"org","role":"admin"}`)
	restored := compose("enterprise")
	prior, _ := restored.app(PlatformApp).(*Console).AcceptedState()
	batch, _, err := decodeAcceptedBatch(entries[0].Body)
	if err != nil || !restored.legacyConsolePredecessor(batch.States[0], prior) {
		t.Fatalf("historical predecessor not recognized: %v", err)
	}
	var drift consoleState
	json.Unmarshal(prior, &drift)
	drift.Members["admin"].Language = "en"
	damaged, _ := json.Marshal(drift)
	if restored.legacyConsolePredecessor(batch.States[0], damaged) {
		t.Fatal("unrelated directory drift bypassed the predecessor hash")
	}
	drift.Members["admin"].Language = ""
	drift.Members["admin"].Roles["org"] = "admin"
	mixed, _ := json.Marshal(drift)
	if restored.legacyConsolePredecessor(batch.States[0], mixed) {
		t.Fatal("mixed-role bootstrap bypassed the predecessor hash")
	}
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	d := restored.app(PlatformApp).(*Console)
	raw, _ := d.AcceptedState()
	savedHash, _ := canonicalDigest(batch.States[0].Image)
	if digest, _ := canonicalDigest(raw); digest != savedHash {
		t.Fatal("recovery rewrote the historical directory image")
	}
	byID, _ := restored.Member("admin")
	bySubject, _ := d.Member("user:admin@example.test")
	for _, member := range []platform.Member{byID, bySubject} {
		if member.Roles["enterprise"] != "admin" || member.Roles["org"] != "" {
			t.Fatal("legacy role not projected to current owner")
		}
	}
	if got := d.holding("enterprise", "admin"); len(got) != 1 || d.Identities()[0].Roles["enterprise"] != "admin" {
		t.Fatal("identity/role-holder paths missed the projection")
	}
	attach(restored)
	decide(restored, SchemaRevoke, "revoke-enterprise", `{"app":"enterprise"}`)
	member, _ := restored.Member("admin")
	if member.Roles["enterprise"] != "" || d.members["admin"].Roles["org"] != "" {
		t.Fatal("revocation resurrected the legacy permission")
	}
	decide(restored, SchemaGrant, "grant-enterprise", `{"app":"enterprise","role":"admin"}`)
	CheckReplay(t, restored, entries, func() *Tenant { return compose("enterprise") })
	state, _, snapErr := restored.Snapshot(func() int64 { return int64(len(entries)) })
	fresh := compose("enterprise")
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	if err := fresh.Restore(state); err != nil {
		t.Fatal(err)
	}
	member, _ = fresh.Member("admin")
	if member.Roles["enterprise"] != "admin" || member.Roles["org"] != "" {
		t.Fatal("snapshot lost the canonical grant")
	}
}
