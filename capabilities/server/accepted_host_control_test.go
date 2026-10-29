package platformserver

import (
	"encoding/json"
	"errors"

	"platformserver/platform"
	"testing"
	"time"
)

func TestHostControlAcceptsBeforeExposureAndRestoresHistory(t *testing.T) {
	const id = "host-control"
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	build := func() *Tenant { return composeTenant(t, id, []platform.Seat{seatOf("ana")}) }
	live := build()
	imageHash := func(tn *Tenant) string {
		t.Helper()
		image, err := tn.console.image()
		if err != nil {
			t.Fatal(err)
		}
		hash, err := platform.CanonicalDigest([]any{image, tn.Audit()})
		if err != nil {
			t.Fatal(err)
		}
		return hash
	}
	var entries []Entry
	fail := true
	before := imageHash(live)
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if imageHash(live) != before {
			t.Fatal("host authority or audit visible before append")
		}
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	if err := live.setHostLifecycle("suspend", "maintenance", "operator", at); err == nil || imageHash(live) != before {
		t.Fatal("lifecycle escaped failed append")
	}
	if _, err := live.openSupport("ana", "diagnose", "operator", 30, at); err == nil || imageHash(live) != before {
		t.Fatal("support grant escaped failed append")
	}
	fail = false
	grant, err := live.openSupport("ana", "diagnose", "operator", 30, at)
	if err != nil {
		t.Fatal(err)
	}
	before = imageHash(live)
	repeat, err := live.openSupport("ana", "diagnose", "operator", 30, at)
	if err != nil || repeat != grant || len(entries) != 1 || imageHash(live) != before {
		t.Fatal("support retry changed its grant or audited twice")
	}
	// Separate requests at one clock retain distinct support identities.
	other, err := live.openSupport("ana", "different incident", "operator", 30, at)
	if err != nil || other.ID == grant.ID {
		t.Fatalf("support identities collided: %v", err)
	}
	before = imageHash(live)
	fail = true
	if _, err := live.useSupport(grant.ID, "operator", at.Add(time.Minute)); err == nil || imageHash(live) != before {
		t.Fatal("support use escaped failed append")
	}
	fail = false
	if _, err := live.useSupport(grant.ID, "operator", at.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	before = imageHash(live)
	count := len(entries)
	if _, err := live.useSupport(grant.ID, "operator", at.Add(time.Minute)); err != nil || len(entries) != count || imageHash(live) != before {
		t.Fatal("support retry used the session twice")
	}
	if _, err := live.useSupport(grant.ID, "operator", at.Add(time.Hour)); err == nil || imageHash(live) != before {
		t.Fatal("expired support session changed state")
	}
	migration := MigrationResult{From: "source", To: id, Key: "migration:key", Types: []MigrationType{{Type: "example.record", Rows: 2, Written: 2}}}
	fail = true
	if err := live.rememberMigration(migration, "ana", at.Add(2*time.Minute)); err == nil || imageHash(live) != before {
		t.Fatal("migration manifest escaped failed append")
	}
	fail = false
	if err := live.rememberMigration(migration, "ana", at.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	before = imageHash(live)
	if err := live.setHostLifecycle("suspend", "maintenance", "operator", at.Add(3*time.Minute)); err != nil || !live.console.suspended() {
		t.Fatalf("suspension did not apply: %v", err)
	}
	before = imageHash(live)
	if err := live.setHostLifecycle("open", "finished", "operator", at.Add(4*time.Minute)); err != nil || live.console.suspended() {
		t.Fatalf("opening did not apply: %v", err)
	}
	for _, e := range entries {
		r, err := decodeAcceptedHostControl(e.Body)
		if err != nil || e.Kind != "accepted-result" || r.Tenant != id {
			t.Fatalf("host write used no complete result: %v", err)
		}
	}
	CheckReplay(t, live, entries, build)
	recovered := build()
	if err := recovered.Replay(entries); err != nil || imageHash(recovered) != imageHash(live) {
		t.Fatalf("host journal-only recovery differs: %v", err)
	}
	raw, _, err := live.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restarted := build()
	if err := restarted.Restore(raw); err != nil {
		t.Fatal(err)
	}
	restarted.AcceptResult = func(Entry, string, string) ([]byte, error) {
		t.Fatal("snapshot retry appended the original support grant again")
		return nil, nil
	}
	if got, err := restarted.openSupport("ana", "diagnose", "operator", 30, at); err != nil || got != grant {
		t.Fatalf("snapshot lost original receipt: %+v %v", got, err)
	}
	// Even a rehashed image cannot modify an unrelated support grant.
	first, err := decodeAcceptedHostControl(entries[0].Body)
	if err != nil {
		t.Fatal(err)
	}
	first.Image.Support[0].Member = "other"
	first.Digest, _ = hostResultDigest(first)
	corrupt, _ := json.Marshal(first)
	empty := build()
	original := imageHash(empty)
	if _, err := empty.applyAcceptedHostControl(corrupt); err == nil || imageHash(empty) != original {
		t.Fatal("host recovery accepted unrelated authority changes")
	}
}

func TestHostControlRecordAndMemoryUseSameDecision(t *testing.T) {
	at := time.Date(2026, 10, 11, 13, 0, 0, 0, time.UTC)
	for _, record := range []bool{false, true} {
		t.Run(map[bool]string{false: "memory", true: "record"}[record], func(t *testing.T) {
			tn := composeTenant(t, "host-modes", []platform.Seat{seatOf("ana")})
			count := 0
			if record {
				tn.Record = func(e Entry) {
					count++
					if len(tn.supportGrants(at)) != 0 || len(tn.Audit()) != 0 {
						t.Fatal("Record observed visible host changes before append")
					}
					if _, err := decodeAcceptedHostControl(e.Body); err != nil {
						t.Fatal(err)
					}
				}
			}
			grant, err := tn.openSupport("ana", "incident", "operator", 15, at)
			if err != nil || len(tn.supportGrants(at)) != 1 || len(tn.Audit()) != 1 || record && count != 1 {
				t.Fatalf("storage changed decision semantics: %v", err)
			}
			if repeated, err := tn.openSupport("ana", "incident", "operator", 15, at); err != nil || repeated != grant || len(tn.supportGrants(at)) != 1 || record && count != 1 {
				t.Fatal("storage retry bypassed the original receipt")
			}
		})
	}
}
