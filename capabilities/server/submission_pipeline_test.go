package platformserver

import acceptedformat "platformserver/kernel/acceptedformat"

import "platformserver/studio/build"

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

func TestSubmissionUsesPrivateAcceptanceForEveryStorage(t *testing.T) {
	for _, mode := range []string{"memory", "record", "accepted-store"} {
		t.Run(mode, func(t *testing.T) {
			tn, owner, _ := setup(t, nil)
			m := memberOf(t, tn, "ana")
			at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
			var entries []Entry
			appendResult := func(e Entry) {
				if len(owner.texts) != 0 || len(owner.ledger.RecordsFor(tn.ID)) != 0 || len(tn.Audit()) != 0 {
					t.Fatal("custom action became visible before acceptance")
				}
				if e.Kind != "accepted-result" {
					t.Fatal("custom action wrote legacy submission")
				}
				entries = append(entries, e)
			}
			switch mode {
			case "record":
				tn.Record = appendResult
			case "accepted-store":
				tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { appendResult(e); return e.Body, nil }
			}
			s := submission(tn, m, "a", "a.note", "a.topic", "topic", "hello")
			s.Payload = []byte("hello")
			first, refusal := tn.Submit(m, s, at)
			if refusal != nil || first == nil || owner.texts["topic"] != "hello" {
				t.Fatalf("custom action: %v", refusal)
			}
			again, refusal := tn.Submit(m, s, at.Add(time.Hour))
			if refusal != nil || again.GetChangeId() != first.GetChangeId() || len(owner.ledger.RecordsFor(tn.ID)) != 1 {
				t.Fatal("custom retry redecided")
			}
			if mode != "memory" {
				if len(entries) != 1 {
					t.Fatal("retry appended another result")
				}
				fresh, freshOwner, _ := setup(t, nil)
				if err := fresh.Replay(entries); err != nil || freshOwner.texts["topic"] != "hello" {
					t.Fatalf("custom recovery: %v", err)
				}
			}
		})
	}
	tn, owner, _ := setup(t, nil)
	tn.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	m := memberOf(t, tn, "ana")
	if _, err := tn.Submit(m, submission(tn, m, "a", "a.note", "a.topic", "topic", "private"), time.Now()); err == nil || len(owner.texts) != 0 || len(owner.ledger.RecordsFor(tn.ID)) != 0 {
		t.Fatal("failed custom append exposed state")
	}
}

type blockedCompositeOwner struct{ *build.Build }

func (*blockedCompositeOwner) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	panic("composite recovery ran a business decision")
}

type brokenCompositePublisher struct{ *build.Build }

func (*brokenCompositePublisher) ApplyAcceptedPublication(string, []byte) error {
	panic("injected accepted publication failure")
}

func TestCompositeIsolatesAcceptedApplicationFailure(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "composite-application-failure")
	if result := submit("dana", build.ObjectType+".create", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}); result != "ok" {
		t.Fatal(result)
	}
	appended := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		appended++
		broken := &brokenCompositePublisher{tn.app(build.ID).(*build.Build)}
		for i, app := range tn.apps {
			if app.Manifest().ID == build.ID {
				tn.apps[i] = broken
			}
		}
		for key, app := range tn.owner {
			if app.Manifest().ID == build.ID {
				tn.owner[key] = broken
			}
		}
		return e.Body, nil
	}
	edits := []platform.CompositeEdit{{Schema: "build.object.publish", Target: &pb.EntityRef{Type: build.ObjectType, Id: "visit"}, Payload: json.RawMessage(`{}`)}}
	at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	if _, err := tn.Composite(member("dana"), "publish", edits, at); err == nil || !tn.quarantined() || appended != 1 {
		t.Fatalf("accepted application failure was not isolated: %v, quarantined=%v, appended=%d", err, tn.quarantined(), appended)
	} else if refusal := (compositeRefusal)(""); errors.As(err, &refusal) {
		t.Fatal("an application failure after acceptance was reported as a saved refusal")
	}
	if _, err := tn.Composite(member("dana"), "another", edits, at); err == nil || appended != 1 {
		t.Fatal("quarantined tenant accepted another command")
	}
}

func TestCompositeSealsOnceAndReplaysWithoutDeciding(t *testing.T) {
	tn, member, submit := upgradeTenant(t, "sealed-composite")
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	if result := submit("dana", build.ObjectType+".create", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}); result != "ok" {
		t.Fatal(result)
	}
	prior := append([]Entry(nil), entries...)
	edits := []platform.CompositeEdit{{Schema: "build.object.edit", Target: &pb.EntityRef{Type: build.ObjectType, Id: "visit"}, Payload: json.RawMessage(`{"name":"visit","title":"Visit today"}`)}}
	at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
	tn.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	if _, err := tn.Composite(member("dana"), "edit", edits, at); err == nil {
		t.Fatal("failed append accepted composite")
	}
	visit, _ := platform.Get[build.Object](tn.caller(member("dana"), tn.app(build.ID), false), "visit")
	if visit.Title != "Visit" {
		t.Fatal("composite changed live records before append")
	}
	tn.AcceptResult = nil
	answer, err := tn.Composite(member("dana"), "edit", edits, at)
	if err != nil || answer.Applied != 1 || len(entries) != len(prior)+1 {
		t.Fatalf("composite: %+v %v", answer, err)
	}
	again, err := tn.Composite(member("dana"), "edit", edits, at.Add(time.Hour))
	if err != nil || again.Digest != answer.Digest || len(entries) != len(prior)+1 {
		t.Fatal("composite retry ran again")
	}
	raw := entries[len(entries)-1].Body
	saved, err := acceptedformat.DecodeComposite(raw)
	if err != nil || saved.Version != 2 || len(saved.Changes) == 0 {
		t.Fatal("composite saved edits without results")
	}
	fresh, _, _ := upgradeTenant(t, tn.ID)
	if err := fresh.Replay(prior); err != nil {
		t.Fatal(err)
	}
	original := fresh.app(build.ID).(*build.Build)
	blocked := &blockedCompositeOwner{original}
	for i, app := range fresh.apps {
		if app.Manifest().ID == build.ID {
			fresh.apps[i] = blocked
		}
	}
	for key, app := range fresh.owner {
		if app.Manifest().ID == build.ID {
			fresh.owner[key] = blocked
		}
	}
	if _, err := fresh.applyAcceptedComposite(raw); err != nil {
		t.Fatal(err)
	}
	image, _, err := fresh.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _ := upgradeTenant(t, tn.ID)
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	if retry, err := restored.Composite(member("dana"), "edit", edits, at.Add(2*time.Hour)); err != nil || retry.Digest != answer.Digest {
		t.Fatalf("snapshot lost composite answer: %v", err)
	}
}

func TestReleaseStorageKeepsSnapshotRetriesAndPrivatePromotion(t *testing.T) {
	for _, mode := range []string{"memory", "record"} {
		t.Run(mode, func(t *testing.T) {
			live, member, create := upgradeTenant(t, "release-storage-"+mode)
			var entries []Entry
			if mode == "record" {
				live.Record = func(e Entry) { entries = append(entries, e) }
			}
			if err := create("dana", build.ObjectType+".create", build.ObjectType, "visit", map[string]any{"name": "visit", "title": "Visit", "fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}}}); err != "ok" {
				t.Fatal(err)
			}
			at := time.Date(2026, 10, 11, 15, 0, 0, 0, time.UTC)
			preview, err := live.PreviewRelease(member("dana"), platform.AssetObject, "visit")
			if err != nil {
				t.Fatal(err)
			}
			id, err := live.SaveReleaseCandidate(member("dana"), platform.AssetObject, "visit", preview.CandidateID, "save", at)
			if err != nil {
				t.Fatal(err)
			}
			image, _, err := live.Snapshot(func() int64 { return 0 })
			if err != nil {
				t.Fatal(err)
			}
			restored, as, _ := upgradeTenant(t, live.ID)
			if err := restored.Restore(image); err != nil {
				t.Fatal(err)
			}
			restored.Record = func(Entry) { t.Fatal("snapshot retry appended another release") }
			if again, err := restored.SaveReleaseCandidate(as("dana"), platform.AssetObject, "visit", id, "save", at.Add(time.Hour)); err != nil || again != id {
				t.Fatalf("snapshot release retry: %s %v", again, err)
			}
			if _, err := restored.SaveReleaseCandidate(as("pat"), platform.AssetObject, "visit", id, "save", at.Add(time.Hour)); err == nil {
				t.Fatal("another principal reused release key")
			}
			target, to, _ := upgradeTenant(t, "release-target-"+mode)
			target.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
			if _, err := PromoteCandidate(live, target, id, "promote", false, to("pat"), at); err == nil || target.releases.Raw(id) != nil || target.ActiveRelease() != "" {
				t.Fatal("failed promotion exposed candidate before acceptance")
			}
		})
	}
}
