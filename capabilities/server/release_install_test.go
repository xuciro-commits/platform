package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestSavedReleaseInstallsFrozenClosureAndRecovers(t *testing.T) {
	checkSavedReleaseInstallation(t, nil, "install-release", false)
}

func TestSavedV2ReleaseInstallsFrozenLayoutAndRecovers(t *testing.T) {
	checkSavedReleaseInstallation(t, nil, "install-v2-release", true)
}

func TestJournalAcceptedSavedReleaseInstallation(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	journal, err := OpenJournal(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("install-release-%d", time.Now().UnixNano())
	defer journal.pool.Exec(context.Background(), `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(context.Background(), id, 0); err != nil {
		t.Fatal(err)
	}
	checkSavedReleaseInstallation(t, journal, id, false)
}

func checkSavedReleaseInstallation(t *testing.T, journal *Journal, id string, v2 bool) {
	compose := func() *Tenant {
		tn, err := NewTenant(id, NewConsole(id, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}},
			Seat{Subjects: []string{"operator"}, Member: platform.Member{ID: "operator", Roles: map[string]string{build.ID: build.User}}}), build.New(id))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	live := compose()
	builder, _ := live.Member("builder")
	operator, _ := live.Member("operator")
	at := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	var entries []Entry
	committed := map[string]Entry{}
	fail, crash := false, false
	persist := func(entry Entry, key, hash string) ([]byte, error) {
		if fail {
			return nil, errors.New("append failed")
		}
		if old, ok := committed[entry.App+"/"+key]; ok {
			priorHash, _ := resultRequestHash(old.Body)
			if priorHash != hash {
				return nil, fmt.Errorf("idempotency conflict")
			}
			return old.Body, nil
		}
		if journal != nil {
			raw, err := journal.AppendAccepted(context.Background(), id, entry, key, hash)
			if err != nil {
				return nil, err
			}
			entry.Body = raw
		}
		committed[entry.App+"/"+key] = entry
		entries = append(entries, entry)
		if crash {
			panic("crash after activation append")
		}
		return entry.Body, nil
	}
	live.AcceptResult = persist
	keys := 0
	do := func(member platform.Member, typ, target, schema, payload string) {
		t.Helper()
		keys++
		if _, refusal := live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID,
			Authority: build.ID, IdempotencyKey: fmt.Sprint("step-", keys), Target: &pb.EntityRef{Type: typ, Id: target},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, at); refusal != nil {
			t.Fatal(refusal)
		}
	}
	save := func(kind platform.AssetKind, target string) string {
		t.Helper()
		preview, err := live.PreviewRelease(builder, kind, target)
		if err != nil || preview.Diagnostic != "" {
			t.Fatalf("preview: %+v, %v", preview, err)
		}
		if _, err := live.SaveReleaseCandidate(builder, kind, target, preview.CandidateID, fmt.Sprint("save-", keys), at); err != nil {
			t.Fatal(err)
		}
		return preview.CandidateID
	}
	do(builder, build.ObjectType, "O1", build.ObjectType+".create", `{"name":"visit","title":"Saved Visit","fields":[{"name":"note","title":"Note","type":"text"}],"states":[{"name":"open","title":"Open"},{"name":"done","title":"Done"}],"actions":[{"name":"close","title":"Close","from":["open"],"to":"done"}]}`)
	candidate := save(platform.AssetObject, "O1")
	do(builder, build.ObjectType, "O1", build.ObjectType+".edit", `{"title":"Later Draft"}`)
	if _, ok := live.entity("build.visit"); ok {
		t.Fatal("saving or editing installed the object")
	}
	review, err := live.ReviewSavedRelease(builder, candidate)
	if err != nil || !review.CanActivate || review.RunningMatches {
		t.Fatalf("saved new object cannot be installed: %+v, %v", review, err)
	}
	fail = true
	if _, err := live.ActivateRelease(builder, candidate, "activate-object", at); err == nil {
		t.Fatal("append failure activated the object")
	}
	if _, ok := live.entity("build.visit"); ok || live.ActiveRelease() != "" {
		t.Fatal("failed activation leaked a descriptor or pointer")
	}
	fail, crash = false, true
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("activation did not reach append boundary")
			}
		}()
		_, _ = live.ActivateRelease(builder, candidate, "activate-object", at)
	}()
	crash = false
	if _, ok := live.entity("build.visit"); ok || live.ActiveRelease() != "" {
		t.Fatal("activation installed definitions before applying the committed result")
	}
	if journal != nil {
		var err error
		entries, err = journal.Entries(context.Background(), id, 0)
		if err != nil {
			t.Fatal(err)
		}
	}
	live = compose()
	if err := live.Replay(entries); err != nil {
		t.Fatal(err)
	}
	live.AcceptResult = persist
	if _, err := live.ActivateRelease(builder, candidate, "activate-object", at); err != nil {
		t.Fatal(err)
	}
	info, ok := live.entity("build.visit")
	if !ok || info.Title != "Saved Visit" {
		t.Fatalf("activation installed today's draft instead of frozen bytes: %+v", info)
	}
	row, _ := platform.Get[build.Object](live.caller(builder, live.app(build.ID), false), "O1")
	if row.Title != "Later Draft" || row.Revision != 2 {
		t.Fatalf("activation overwrote the mutable draft: %+v", row)
	}
	do(operator, "build.visit", "V1", "build.visit.create", `{"note":"Operator's record"}`)
	pageDraft := `{"name":"visits","title":"Visit desk","object":"build.visit","list":["note"],"detail":["note"]}`
	if v2 {
		pageDraft = fmt.Sprintf(`{"name":"visits","title":"Visit desk","object":"build.visit","sections":[{"id":"list","configVersion":1,"widget":"table","fields":["note"]},{"id":"detail","configVersion":1,"widget":"detail","fields":["note"]}],"document":{"formatVersion":2,"uiProfile":%q,"root":"root","nodes":{"root":{"kind":"rows","children":["columns"]},"columns":{"kind":"tabs","activeVariable":"active","children":["list","detail"]},"list":{"kind":"widget","section":"list"},"detail":{"kind":"widget","section":"detail"}},"variables":{"active":{"scope":"page","type":"string","mode":"state","initial":"list"},"record":{"scope":"page","type":"record","mode":"resource","source":{"kind":"record","section":"list"}}}}}`, platform.PageUIProfile())
	}
	if v2 {
		var draft map[string]any
		_ = json.Unmarshal([]byte(pageDraft), &draft)
		raw, _ := json.Marshal(draft["document"])
		var document platform.PageDocument
		_ = json.Unmarshal(raw, &document)
		document.Nodes["root"] = platform.PageLayoutNode{Kind: "rows", Children: []string{"columns", "trigger", "loop"}}
		document.Nodes["trigger"] = platform.PageLayoutNode{Kind: "widget", Section: "trigger"}
		document.Nodes["overlay"] = platform.PageLayoutNode{Kind: "flow", Children: []string{"overlayDetail"}, Align: "start"}
		document.Nodes["overlayDetail"] = platform.PageLayoutNode{Kind: "widget", Section: "overlayDetail"}
		document.Variables["open"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)}
		document.Overlays = map[string]platform.PageOverlay{"panel": {Root: "overlay", Kind: "drawer", Title: "Frozen panel", OpenVariable: "open"}}
		document.Events = []platform.PageEventBinding{{Source: "trigger", Event: "click", Target: "open", Value: json.RawMessage(`true`)}}
		document.Nodes["loop"] = platform.PageLayoutNode{Kind: "loop", Children: []string{"loopDetail"}, Loop: &platform.PageLoop{Collection: "window", ItemVariable: "item", Limit: 20}}
		document.Nodes["loopDetail"] = platform.PageLayoutNode{Kind: "widget", Section: "loopDetail"}
		document.Variables["window"] = platform.PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "query", Section: "list"}}
		document.Variables["item"] = platform.PageVariable{Scope: "loop-item", Owner: "loop", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "item", Node: "loop"}}
		draft["document"] = document
		draft["sections"] = append(draft["sections"].([]any), map[string]any{"id": "trigger", "configVersion": 1, "widget": "button", "title": "Open panel"}, map[string]any{"id": "overlayDetail", "configVersion": 1, "widget": "detail", "fields": []string{"note"}}, map[string]any{"id": "loopDetail", "configVersion": 1, "widget": "detail", "fields": []string{"note"}, "recordVariable": "item"})
		bytes, _ := json.Marshal(draft)
		pageDraft = string(bytes)
	}

	do(builder, build.PageType, "P1", build.PageType+".create", pageDraft)
	page := save(platform.AssetPage, "P1")
	if v2 {
		// Alter the mutable layout after freezing. Activation must retain the
		// saved tree while preserving this later draft beside it.
		stored, _ := platform.Get[build.Page](live.caller(builder, live.app(build.ID), false), "P1")
		raw, _ := json.Marshal(stored.Document)
		var edited platform.PageDocument
		_ = json.Unmarshal(raw, &edited)
		edited.Nodes["root"] = platform.PageLayoutNode{Kind: "columns", Children: []string{"columns", "trigger", "loop"}}
		o := edited.Overlays["panel"]
		o.Title = "Later panel"
		edited.Overlays["panel"] = o
		payload, _ := json.Marshal(map[string]any{"title": "Later page draft", "document": edited})
		do(builder, build.PageType, "P1", build.PageType+".edit", string(payload))
	}
	if _, err := live.ActivateRelease(builder, page, "activate-page", at); err != nil {
		t.Fatal(err)
	}
	do(builder, build.AppType, "A1", build.AppType+".create", `{"name":"frontdesk","title":"Front desk","pages":["visits"]}`)
	app := save(platform.AssetApp, "A1")
	if _, err := live.ActivateRelease(builder, app, "activate-app", at); err != nil {
		t.Fatal(err)
	}
	if err := live.runningMatchesLocked(app, live.releaseCandidates[app]); err != nil {
		t.Fatal(err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, live, entries, compose)
	snapshot, _, err := live.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	fromSnapshot := compose()
	if err := fromSnapshot.Restore(snapshot); err != nil {
		t.Fatal(err)
	}
	for _, tn := range []*Tenant{recovered, fromSnapshot} {
		if err := tn.runningMatchesLocked(app, tn.releaseCandidates[app]); err != nil {
			t.Fatalf("recovered closure differs: %v", err)
		}
		if tn.ActiveRelease() != app {
			t.Fatal("recovery lost the active release")
		}
		stored, ok := platform.Get[build.Object](tn.caller(builder, tn.app(build.ID), false), "O1")
		if !ok || stored.Title != "Later Draft" {
			t.Fatal("recovery discarded the draft beside its publication")
		}
		if v2 {
			var installed *platform.Page
			for _, definition := range tn.Definitions(operator) {
				if definition.Ref.Kind == platform.AssetPage && definition.Ref.Name == "visits" {
					installed = definition.Page
				}
			}
			if installed == nil || installed.Document == nil || installed.Document.Nodes["root"].Kind != "rows" || installed.Document.Overlays["panel"].Title != "Frozen panel" || len(installed.Document.Events) != 1 || installed.Document.Events[0].Target != "open" || installed.Document.Nodes["overlay"].Kind != "flow" || installed.Document.Nodes["loop"].Loop == nil || installed.Document.Nodes["loop"].Loop.ItemVariable != "item" || installed.Sections[len(installed.Sections)-1].RecordVariable != "item" || installed.Sections[0].ID != "list" || installed.Sections[1].ConfigVersion != 1 || installed.Document.Nodes["columns"].Kind != "tabs" || string(installed.Document.Variables["active"].Initial) != `"list"` || installed.Document.Variables["record"].Source == nil || installed.Document.Variables["record"].Source.Section != "list" {
				t.Fatalf("recovery lost the frozen V2 layout: %+v", installed)
			}
			draft, _ := platform.Get[build.Page](tn.caller(builder, tn.app(build.ID), false), "P1")
			if draft.Title != "Later page draft" || draft.Document.Nodes["root"].Kind != "columns" {
				t.Fatal("activation or recovery discarded the later layout draft")
			}
		}
	}
	// A candidate can become incompatible between save and activation: do not
	// migrate new operator records or expose a partial closure in that case.
	do(builder, build.ObjectType, "O1", build.ObjectType+".edit", `{"fields":[{"name":"note","title":"Note","type":"text"},{"name":"priority","title":"Priority","type":"integer"}]}`)
	// The preview accepts additive fields; activation requires an explicit
	// record migration plan once real rows exist.
	changed := save(platform.AssetObject, "O1")
	before, _ := json.Marshal(live.definitions)
	if _, err := live.ActivateRelease(builder, changed, "unsupported-shape", at); err == nil {
		t.Fatal("activation silently changed stored record shape")
	}
	after, _ := json.Marshal(live.definitions)
	if string(before) != string(after) || live.ActiveRelease() != app {
		t.Fatal("rejected migration altered the running release")
	}
}
