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

// One application, end to end, across two environments (ADR-0080 §3.1): a
// definition is drafted and frozen in dev, activated, used by an operator,
// promoted as sealed bytes into prod, its records migrated, then upgraded to a
// v2 that adds one optional scalar — on both environments, through the
// reviewed plan — and finally recovered from a snapshot plus the journal tail.
// Nothing here is a new engine: the test strings the existing candidate,
// release, environment, transfer and recovery owners together and asserts the
// seams between them hold.
func TestApplicationLifecycleAcrossEnvironments(t *testing.T) {
	at := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	seats := append(upgradeSeats(), Seat{Subjects: []string{"user:ops@example.test"}, Member: platform.Member{ID: "ops", Roles: map[string]string{build.ID: build.User}}})
	compose := func(id string) *Tenant {
		tn, err := NewTenant(id, NewConsole(id, seats...), work.New(id), build.New(id))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	type env struct {
		*Tenant
		entries []Entry
		keys    int
	}
	open := func(id string) *env {
		e := &env{Tenant: compose(id)}
		// The journal's contract: one key commits once; the same request
		// reads the committed bytes back, another request under it conflicts.
		committed := map[string]Entry{}
		hashes := map[string]string{}
		e.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
			if prior, ok := committed[entry.App+"/"+key]; ok {
				if hashes[entry.App+"/"+key] != hash {
					return nil, fmt.Errorf("idempotency conflict")
				}
				return prior.Body, nil
			}
			committed[entry.App+"/"+key], hashes[entry.App+"/"+key] = entry, hash
			e.entries = append(e.entries, entry)
			return entry.Body, nil
		}
		return e
	}
	member := func(e *env, who string) platform.Member {
		m, ok := e.Member(who)
		if !ok {
			t.Fatalf("%s: no member %s", e.ID, who)
		}
		return m
	}
	submit := func(e *env, who, typ, target, schema string, payload any) string {
		t.Helper()
		e.keys++
		raw, _ := json.Marshal(payload)
		if _, refusal := e.Submit(member(e, who), &pb.Submission{TenantId: e.ID, PrincipalId: who, Authority: build.ID,
			IdempotencyKey: fmt.Sprintf("%s-%d", e.ID, e.keys), Target: &pb.EntityRef{Type: typ, Id: target},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, at); refusal != nil {
			return refusal.Code.String()
		}
		return "ok"
	}
	freeze := func(e *env, drafts ...build.JointDraftRef) string {
		t.Helper()
		preview, err := e.PreviewReleaseDrafts(member(e, "dana"), drafts)
		if err != nil || preview.CandidateID == "" || preview.Diagnostic != "" {
			t.Fatalf("%s preview: %+v %v", e.ID, preview, err)
		}
		if _, err := e.SaveReleaseCandidates(member(e, "dana"), drafts, preview.CandidateID, "save-"+preview.CandidateID, at); err != nil {
			t.Fatalf("%s save: %v", e.ID, err)
		}
		if _, err := e.SealCandidate(preview.CandidateID, at); err != nil {
			t.Fatalf("%s seal: %v", e.ID, err)
		}
		return preview.CandidateID
	}
	visitV1 := map[string]any{"name": "visit", "title": "Visit",
		"fields":  []map[string]any{{"name": "note", "title": "Note", "type": "text"}},
		"states":  []map[string]any{{"name": "open", "title": "Open"}, {"name": "done", "title": "Done"}},
		"actions": []map[string]any{{"name": "close", "title": "Close", "from": []string{"open"}, "to": "done"}}}
	deskV1 := map[string]any{"name": "desk", "title": "Visit desk", "object": "build.visit", "list": []string{"note"}, "detail": []string{"note"}}
	appV1 := map[string]any{"name": "frontdesk", "title": "Front desk", "pages": []string{"desk"}}
	drafts := []build.JointDraftRef{{Kind: platform.AssetObject, ID: "O1"}, {Kind: platform.AssetPage, ID: "P1"}, {Kind: platform.AssetApp, ID: "A1"}}

	// 1. Definition and dependencies in dev; one joint candidate; activate.
	dev := open("dev")
	for _, step := range []struct {
		typ, target, schema string
		payload             any
	}{
		{build.ObjectType, "O1", build.ObjectType + ".create", visitV1},
		{build.PageType, "P1", build.PageType + ".create", deskV1},
		{build.AppType, "A1", build.AppType + ".create", appV1},
	} {
		if got := submit(dev, "dana", step.typ, step.target, step.schema, step.payload); got != "ok" {
			t.Fatalf("dev %s: %s", step.schema, got)
		}
	}
	v1 := freeze(dev, drafts...)
	if _, err := dev.ActivateRelease(member(dev, "dana"), v1, "activate-v1", at); err != nil {
		t.Fatalf("dev activate: %v", err)
	}
	if dev.ActiveRelease() != v1 {
		t.Fatal("dev did not activate v1")
	}
	// 2. Business operations in dev, by a plain user of the released object.
	for i := 1; i <= 3; i++ {
		if got := submit(dev, "ops", "build.visit", fmt.Sprintf("V%d", i), "build.visit.create", map[string]any{"note": fmt.Sprintf("visit %d", i)}); got != "ok" {
			t.Fatalf("dev visit %d: %s", i, got)
		}
	}
	if got := submit(dev, "ops", "build.visit", "V1", "build.visit.close", map[string]any{}); got != "ok" {
		t.Fatalf("dev close: %s", got)
	}

	// 3. Promotion into prod: prod holds the owner drafts the assets resolve
	// to (the same declarations, as a target-side install needs), receives
	// the sealed bytes as its own release result, and activates them.
	prod := open("prod")
	for _, step := range []struct {
		typ, target, schema string
		payload             any
	}{
		{build.ObjectType, "O1", build.ObjectType + ".create", visitV1},
		{build.PageType, "P1", build.PageType + ".create", deskV1},
		{build.AppType, "A1", build.AppType + ".create", appV1},
	} {
		if got := submit(prod, "dana", step.typ, step.target, step.schema, step.payload); got != "ok" {
			t.Fatalf("prod %s: %s", step.schema, got)
		}
	}
	if _, err := PromoteCandidate(dev.Tenant, prod.Tenant, v1, "promote-v1", true, member(prod, "mo"), at); err == nil || !strings.Contains(err.Error(), "builder or publisher") {
		t.Fatalf("a member without a release role promoted: %v", err)
	}
	promoted, err := PromoteCandidate(dev.Tenant, prod.Tenant, v1, "promote-v1", true, member(prod, "pat"), at)
	if err != nil {
		t.Fatalf("promote v1: %v", err)
	}
	if prod.ActiveRelease() != v1 || promoted.Assets < 3 {
		t.Fatalf("prod did not activate the promoted candidate: %+v", promoted)
	}
	if _, ok := prod.entity("build.visit"); !ok {
		t.Fatal("prod has no installed visit object")
	}
	if again, err := PromoteCandidate(dev.Tenant, prod.Tenant, v1, "promote-v1", true, member(prod, "pat"), at.Add(time.Minute)); err != nil || again.Digest != promoted.Digest {
		t.Fatalf("a repeated promotion is not idempotent: %+v %v", again, err)
	}

	// 4. Real data follows: export from dev as a reader, import into prod as
	// a member there; a second run moves nothing new.
	migration, err := MigrateRecords(dev.Tenant, prod.Tenant, member(dev, "ops"), member(prod, "ops"), []string{"build.visit"}, at)
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if len(migration.Types) != 1 || migration.Types[0].Rows != 3 || migration.Types[0].Written != 3 || len(migration.Types[0].Refused) != 0 {
		t.Fatalf("migration moved the wrong rows: %+v", migration.Types)
	}
	if visits := prod.records.types["build.visit"]; visits == nil || len(visits.rows) != 3 {
		t.Fatal("prod does not hold the migrated visits")
	}
	if image, _ := prod.records.types["build.visit"].rows["V1"].image(); !strings.Contains(string(image), `"visit 1"`) {
		t.Fatalf("migrated V1 lost its note: %s", image)
	}
	repeat, err := MigrateRecords(dev.Tenant, prod.Tenant, member(dev, "ops"), member(prod, "ops"), []string{"build.visit"}, at.Add(time.Minute))
	if err != nil || repeat.Key != migration.Key || repeat.Types[0].Written != 3 || len(prod.records.types["build.visit"].rows) != 3 {
		t.Fatalf("a repeated migration changed prod: %+v %v", repeat, err)
	}
	if _, err := MigrateRecords(dev.Tenant, prod.Tenant, member(dev, "ops"), member(prod, "ops"), []string{"build.nothing"}, at); err == nil || !strings.Contains(err.Error(), "does not host") {
		t.Fatalf("a type prod does not host migrated: %v", err)
	}

	// 5. v2: one optional scalar added to the existing object. Dev reviews the
	// plan against its own rows and upgrades; the same sealed candidate is
	// promoted without activation and prod activates it through its own plan.
	visitV2 := map[string]any{"fields": []map[string]any{{"name": "note", "title": "Note", "type": "text"}, {"name": "priority", "title": "Priority", "type": "integer"}}}
	if got := submit(dev, "dana", build.ObjectType, "O1", build.ObjectType+".edit", visitV2); got != "ok" {
		t.Fatalf("dev v2 draft: %s", got)
	}
	v2 := freeze(dev, drafts...)
	if _, err := dev.ActivateRelease(member(dev, "dana"), v2, "activate-v2-plain", at); err == nil {
		t.Fatal("dev activated a storage change without a reviewed plan")
	}
	review, err := dev.ReviewSavedRelease(member(dev, "dana"), v2)
	if err != nil || review.UpgradePlan == nil || len(review.UpgradePlan.Additions) != 1 || review.UpgradePlan.Additions[0].Records != 3 {
		t.Fatalf("dev v2 plan: %+v %v", review, err)
	}
	if _, err := dev.ActivateReleaseWithUpgrade(member(dev, "dana"), v2, "activate-v2", review.UpgradePlan.ID, at); err != nil {
		t.Fatalf("dev upgrade: %v", err)
	}
	if got := submit(dev, "ops", "build.visit", "V2", "build.visit.edit", map[string]any{"priority": 5}); got != "ok" {
		t.Fatalf("dev cannot write the new optional field: %s", got)
	}
	if got := submit(prod, "dana", build.ObjectType, "O1", build.ObjectType+".edit", visitV2); got != "ok" {
		t.Fatalf("prod v2 draft: %s", got)
	}
	if _, err := PromoteCandidate(dev.Tenant, prod.Tenant, v2, "promote-v2", true, member(prod, "pat"), at); err == nil {
		t.Fatal("prod activated a storage change during promotion without a plan")
	}
	if prod.ActiveRelease() != v1 {
		t.Fatal("the refused promotion moved prod's active release")
	}
	if _, err := PromoteCandidate(dev.Tenant, prod.Tenant, v2, "promote-v2-hold", false, member(prod, "pat"), at); err != nil {
		t.Fatalf("promote v2 without activation: %v", err)
	}
	prodReview, err := prod.ReviewSavedRelease(member(prod, "pat"), v2)
	if err != nil || prodReview.UpgradePlan == nil || prodReview.UpgradePlan.Additions[0].Records != 3 {
		t.Fatalf("prod v2 plan: %+v %v", prodReview, err)
	}
	// The plan is a receipt over content (candidate, active release, rows,
	// shape), not over an environment: two environments in the same state
	// derive the same plan, and each verifies it against its own rows.
	if prodReview.UpgradePlan.ID != review.UpgradePlan.ID {
		t.Fatalf("identical environments derived different plans: %s vs %s", prodReview.UpgradePlan.ID, review.UpgradePlan.ID)
	}
	if got := submit(prod, "ops", "build.visit", "V4", "build.visit.create", map[string]any{"note": "prod-only"}); got != "ok" {
		t.Fatalf("prod visit: %s", got)
	}
	if _, err := prod.ActivateReleaseWithUpgrade(member(prod, "pat"), v2, "activate-v2", prodReview.UpgradePlan.ID, at); err == nil {
		t.Fatal("prod accepted a plan whose row count drifted")
	}
	prodReview, err = prod.ReviewSavedRelease(member(prod, "pat"), v2)
	if err != nil || prodReview.UpgradePlan == nil || prodReview.UpgradePlan.Additions[0].Records != 4 || prodReview.UpgradePlan.ID == review.UpgradePlan.ID {
		t.Fatalf("prod did not re-derive its plan: %+v %v", prodReview, err)
	}
	before, _ := prod.records.types["build.visit"].rows["V1"].image()
	if _, err := prod.ActivateReleaseWithUpgrade(member(prod, "pat"), v2, "activate-v2", prodReview.UpgradePlan.ID, at); err != nil {
		t.Fatalf("prod upgrade: %v", err)
	}
	if after, _ := prod.records.types["build.visit"].rows["V1"].image(); string(after) != string(before) {
		t.Fatal("the prod upgrade rewrote an existing record")
	}
	if field, ok := prod.records.types["build.visit"].info.Field("priority"); !ok || field.Required {
		t.Fatal("prod lacks the optional field after the upgrade")
	}

	// 6. Failure handling and restart: a snapshot is taken after the upgrade,
	// work continues, and prod comes back from that snapshot plus the journal
	// tail written after it — with the v2 shape, both releases, the migrated
	// and later-edited rows, and the promotion and migration in its audit.
	cut := len(prod.entries)
	snapshot, _, err := prod.Snapshot(func() int64 { return int64(cut) })
	if err != nil {
		t.Fatal(err)
	}
	if got := submit(prod, "ops", "build.visit", "V3", "build.visit.edit", map[string]any{"priority": 9}); got != "ok" {
		t.Fatalf("prod cannot write the new optional field: %s", got)
	}
	if len(prod.entries) <= cut {
		t.Fatal("the edit after the snapshot wrote no journal entry")
	}
	restored := compose("prod")
	if err := restored.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	if err := restored.Replay(prod.entries[cut:]); err != nil {
		t.Fatalf("replay of the tail: %v", err)
	}
	if restored.ActiveRelease() != v2 || restored.releases.candidates[v1] == nil {
		t.Fatal("recovery lost a release")
	}
	if field, ok := restored.records.types["build.visit"].info.Field("priority"); !ok || field.Required {
		t.Fatal("recovery lost the upgraded shape")
	}
	if image, _ := restored.records.types["build.visit"].rows["V3"].image(); !strings.Contains(string(image), `"priority":9`) {
		t.Fatalf("recovery lost the write after the upgrade: %s", image)
	}
	if len(restored.records.types["build.visit"].rows) != 4 {
		t.Fatal("recovery changed the record count")
	}
	seen := map[string]bool{}
	for _, entry := range restored.Audit() {
		seen[entry.Action] = true
	}
	if !seen["host.promotion"] {
		t.Fatalf("recovery lost the promotion audit: %v", seen)
	}
	CheckReplay(t, prod.Tenant, prod.entries, func() *Tenant { return compose("prod") })
}
