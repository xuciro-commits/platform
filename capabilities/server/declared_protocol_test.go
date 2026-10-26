package platformserver

import (
	"bytes"
	"encoding/json"
	"platformserver/apps/build"
	"platformserver/internal/construction"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestDeclarativeProtocolPublicationAndRecovery(t *testing.T) {
	at := time.Date(2026, 10, 17, 13, 0, 0, 0, time.UTC)
	compose := func() *Tenant {
		return composeTenant(t, "declared-protocol", []Seat{seatOf("builder", "build:builder", "platform:admin"), seatOf("alice", "build:user"), seatOf("bob", "build:user")}, build.New("declared-protocol"))
	}
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	object := map[string]any{"name": "job", "title": "Job", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}, "access": []map[string]any{{"role": "user", "read": "own", "create": true, "edit": true}}, "states": []map[string]any{{"name": "open", "title": "Open"}}, "actions": []map[string]any{{"name": "first", "title": "First", "from": []string{"open"}, "inputs": []map[string]any{{"name": "batch", "title": "Batch", "type": "text", "required": true}}, "transaction": map[string]any{"subject": "build.job", "steps": []map[string]any{{"name": "update", "kind": "update", "target": map[string]any{"type": "build.job", "id": "$subject"}, "changes": []map[string]any{{"field": "title", "expr": platform.ExprReference("text", "input.batch")}}}}}}, {"name": "second", "title": "Second", "from": []string{"open"}, "inputs": []map[string]any{{"name": "batch", "title": "Batch", "type": "text", "required": true}}, "transaction": map[string]any{"subject": "build.job", "steps": []map[string]any{{"name": "update", "kind": "update", "target": map[string]any{"type": "build.job", "id": "$subject"}, "changes": []map[string]any{{"field": "title", "expr": platform.ExprLiteralValue("text", "second")}}}}}}}, "protocols": []platform.ProtocolBinding{{Name: "example.jobs", Version: 1, Actions: map[string]string{"confirm": "first"}, Reads: []string{"jobs"}}}}
	publishObject(t, tn, "builder", "job", object, at)
	decide(t, tn, "alice", construction.ID, "build.job.create", "build.job", "A", map[string]any{"title": "Alice"}, at)
	for i := 0; i < 201; i++ {
		decide(t, tn, "bob", construction.ID, "build.job.create", "build.job", "B"+time.Duration(i).String(), map[string]any{"title": "Bob"}, at)
	}
	result, refusal := tn.Query(memberOf(t, tn, "alice"), "example.jobs/1", "jobs")
	if refusal != nil {
		t.Fatal(refusal)
	}
	var rows []map[string]any
	if err := json.Unmarshal(platform.Raw(result), &rows); err != nil || len(rows) != 1 || rows[0]["id"] != "A" {
		t.Fatalf("protocol did not use visible records: %s %v", platform.Raw(result), err)
	}
	decide(t, tn, "builder", PlatformApp, "platform.protocol.bind", "platform.protocol", "example.jobs/1", map[string]any{"provider": construction.ID}, at)
	bindings := []platform.ProtocolBinding{{Name: "example.jobs", Version: 1, Actions: map[string]string{"confirm": "second"}, Reads: []string{"jobs"}}}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "job", map[string]any{"protocols": bindings}, at)
	decide(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "job", map[string]any{}, at)
	if _, _, refusal := tn.Invoke(memberOf(t, tn, "alice"), "example.jobs/1", "confirm", "A", []byte(`{"batch":"one"}`), "mapped-second", at); refusal != nil {
		t.Fatal(refusal)
	}
	view, refusal := tn.RecordOf(memberOf(t, tn, "alice"), "build.job", "A", at)
	if refusal != nil || !bytes.Contains(platform.Raw(view.Record), []byte(`"title":"second"`)) {
		t.Fatalf("bound protocol retained its old mapping: %s %v", platform.Raw(view.Record), refusal)
	}
	decide(t, tn, "builder", construction.ID, "build.object.edit", construction.ObjectType, "job", map[string]any{"protocols": []platform.ProtocolBinding{}}, at)
	if got := refuse(t, tn, "builder", construction.ID, "build.object.publish", construction.ObjectType, "job", map[string]any{}, at); got == "ok" {
		t.Fatal("removed a bound provider needed by control recovery")
	}
	CheckReplay(t, tn, entries, compose)

	// A declared protocol payload must match its mapped action exactly.
	contract := &platform.Protocol{Name: "example.checked", Version: 1, Actions: []platform.Action{{Schema: "confirm", Payload: []platform.Field{{Name: "token", Type: "text", Required: true}}}}}
	bad := builderTenant(t, "incompatible-protocol")
	decide(t, bad, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "bad", map[string]any{
		"name": "provider", "title": "Provider", "fields": []construction.Field{}, "states": []construction.State{{Name: "open", Title: "Open"}},
		"actions":   []construction.Action{{Name: "apply", Title: "Apply", From: []string{"open"}}},
		"protocols": []platform.ProtocolBinding{{Name: contract.Name, Version: contract.Version, Contract: contract, Actions: map[string]string{"confirm": "apply"}}},
	}, at)
	if got := refuse(t, bad, "builder", construction.ID, construction.ObjectType+".publish", construction.ObjectType, "bad", nil, at); !strings.Contains(got, "incompatible payload") {
		t.Fatalf("published an incompatible protocol payload: %s", got)
	}
}

func TestDeclarativeProtocolReleaseMatchesInstalledAndRestored(t *testing.T) {
	at := time.Date(2026, 10, 17, 14, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return builderTenant(t, "protocol-release") }
	tenant := compose()
	var entries []Entry
	tenant.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	bindings := []platform.ProtocolBinding{{Name: "example.release", Version: 2, Actions: map[string]string{"release": "complete"}, Reads: []string{"records"}, Events: map[string]string{"released": "complete"}}}
	decide(t, tenant, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "provider", map[string]any{
		"name": "provider", "title": "Protocol provider", "fields": []construction.Field{{Name: "note", Title: "Order note", Type: "text"}},
		"states":  []construction.State{{Name: "open", Title: "Open"}, {Name: "done", Title: "Done"}},
		"actions": []construction.Action{{Name: "complete", Title: "Complete order", From: []string{"open"}, To: "done"}}, "protocols": bindings,
	}, at)
	assertReview := func(target *Tenant, candidate string) {
		t.Helper()
		review, err := target.ReviewSavedRelease(memberOf(t, target, "builder"), candidate)
		if err != nil || !review.Active || !review.RunningMatches || review.RunningDiagnostic != "" || review.Preview.CurrentID != candidate || len(review.Preview.Changed) != 0 {
			t.Fatalf("activated protocol release differs from installed definitions: %+v %v", review, err)
		}
	}
	for _, pinned := range []bool{false, true} {
		if pinned {
			bindings[0].Contract = &tenant.app(construction.ID).Manifest().Provides[0].Protocol
			decide(t, tenant, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "provider", map[string]any{"protocols": bindings}, at)
		}
		builder := memberOf(t, tenant, "builder")
		preview, err := tenant.PreviewRelease(builder, platform.AssetObject, "provider")
		if err != nil {
			t.Fatal(err)
		}
		suffix := "derived"
		if pinned {
			suffix = "contract"
		}
		candidate, err := tenant.SaveReleaseCandidate(builder, platform.AssetObject, "provider", preview.CandidateID, "save-"+suffix, at)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tenant.ActivateRelease(builder, candidate, "activate-"+suffix, at); err != nil {
			t.Fatal(err)
		}
		assertReview(tenant, candidate)
		restored := compose()
		saved, _, err := tenant.Snapshot(func() int64 { return int64(len(entries)) })
		if err != nil {
			t.Fatal(err)
		}
		if err = restored.Restore(saved); err != nil {
			t.Fatal(err)
		}
		assertReview(restored, candidate)
	}
	CheckReplay(t, tenant, entries, compose)
}
