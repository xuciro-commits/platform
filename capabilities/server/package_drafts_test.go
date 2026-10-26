package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/core"
	"platformserver/platform"
)

func TestPackageBlueprintCreatesOnlyDraftsAndRecovers(t *testing.T) {
	at := time.Date(2026, 10, 15, 15, 0, 0, 0, time.UTC)
	source := builderTenant(t, "blueprint-source")
	decide(t, source, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "object", map[string]any{"name": "item", "title": "Item", "fields": []map[string]any{{"name": "title", "title": "Title", "type": "text"}}}, at)
	decide(t, source, "builder", construction.ID, construction.TestPlanType+".create", construction.TestPlanType, "scenario", map[string]any{"name": "createitem", "title": "Create item", "roots": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: "build.item"}}, "at": at, "steps": []platform.TestStep{{Type: "build.item", ID: "sample", Action: "build.item.create", Payload: `{"title":"Example"}`, Expect: "accepted"}}}, at)
	decide(t, source, "builder", construction.ID, construction.AppType+".create", construction.AppType, "application", map[string]any{"name": "inventory", "title": "Inventory", "pages": []string{"item"}, "resources": []platform.AssetRef{{App: construction.ID, Kind: platform.AssetTestPlan, Name: "createitem"}}}, at)
	source.mu.Lock()
	preview, candidate, err := source.previewReleaseLocked([]construction.JointDraftRef{{Kind: platform.AssetObject, ID: "object"}, {Kind: platform.AssetApp, ID: "application"}, {Kind: platform.AssetTestPlan, ID: "scenario"}}, memberOf(t, source, "builder"))
	source.mu.Unlock()
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("owner candidate: %+v %v", preview, err)
	}
	descriptor := PackageDescriptor{ID: "example.inventory", Version: "1.0.0", Namespace: "example.inventory", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{{App: construction.ID, Kind: platform.AssetApp, Name: "inventory"}}, Assets: candidate.Assets}}
	directory := t.TempDir()
	raw, _ := json.Marshal(descriptor)
	original := append([]byte(nil), raw...)
	path := filepath.Join(directory, "inventory.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	compose := func() *Tenant {
		tn := composeTenant(t, "blueprint-target", []Seat{seatOf("admin", PlatformApp+":"+Admin), seatOf("builder", construction.ID+":"+construction.Builder), seatOf("user", construction.ID+":"+construction.User)}, build.New("blueprint-target"))
		testConsoleOf(tn).BindPackageIndex(index)
		return tn
	}
	target := compose()
	var entries []Entry
	fail := false
	target.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, target, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	builder := memberOf(t, target, "builder")
	if _, err = target.CreatePackageDrafts(memberOf(t, target, "admin"), descriptor.ID, descriptor.Version, "no-builder", at); err == nil {
		t.Fatal("package admin created Build drafts without Build permission")
	}
	// A directory edit cannot replace the installed, sealed blueprint.
	for i := range descriptor.Blueprint.Assets {
		if descriptor.Blueprint.Assets[i].Ref.Kind == platform.AssetApp {
			var body map[string]any
			_ = json.Unmarshal(descriptor.Blueprint.Assets[i].Body, &body)
			body["title"] = "Uninstalled change"
			descriptor.Blueprint.Assets[i].Body = platform.Raw(body)
		}
	}
	raw, _ = json.Marshal(descriptor)
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	fail = true
	if _, err = target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "create", at); err == nil {
		t.Fatal("failed append created an application")
	}
	if _, ok := platform.Get[build.Application](target.automation(construction.ID, false), "inventory"); ok {
		t.Fatal("append failure leaked an application draft")
	}
	fail = false
	limited := builder
	limited.Scopes = []string{construction.AppType + ".create"}
	if _, err = target.CreatePackageDrafts(limited, descriptor.ID, descriptor.Version, "limited", at); err == nil {
		t.Fatal("one allowed create bypassed another draft's scope")
	}
	answer, err := target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "create", at)
	if err != nil {
		t.Fatal(err)
	}
	if answer.ApplicationID != "inventory" || len(answer.Drafts) != 3 || len(entries) != 2 {
		t.Fatalf("created drafts: %+v entries=%d", answer, len(entries))
	}
	app, ok := platform.Get[build.Application](target.automation(construction.ID, false), answer.ApplicationID)
	if !ok || app.Title != "Inventory" || app.Published != "" || app.State != "draft" {
		t.Fatalf("created application is not the sealed draft: %+v", app)
	}
	object, ok := platform.Get[construction.Object](target.automation(construction.ID, false), "item")
	if !ok || object.Published != "" || object.State != "draft" {
		t.Fatalf("created object is not a draft: %+v", object)
	}
	if _, ok := target.entity("build.item"); ok || target.ActiveRelease() != "" {
		t.Fatal("package creation published definitions")
	}
	plan, ok := platform.Get[construction.TestPlan](target.automation(construction.ID, false), "createitem")
	if !ok || len(plan.Roots) != 1 || plan.Object != "" || plan.Application != "" || plan.Published != "" {
		t.Fatalf("portable scenario draft: %+v", plan)
	}
	if got, refusal := target.Read(builder, "packages"); refusal != nil || len(got.([]PackageView)) != 1 {
		t.Fatalf("builder cannot discover installed blueprint: %v", refusal)
	}
	if _, refusal := target.Read(memberOf(t, target, "user"), "packages"); refusal == nil {
		t.Fatal("business user discovered builder packages")
	}
	if roles := memberOf(t, target, "builder").Roles; len(roles) != 1 || roles[construction.ID] != construction.Builder {
		t.Fatalf("package granted roles: %v", roles)
	}
	retried, err := target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "create", at.Add(time.Hour))
	if err != nil || !reflect.DeepEqual(retried, answer) || len(entries) != 2 {
		t.Fatalf("same key did not reuse created IDs: %+v %v", retried, err)
	}
	if _, err = target.CreatePackageDrafts(builder, descriptor.ID, descriptor.Version, "another", at); err == nil || !strings.Contains(err.Error(), "already has a draft") {
		t.Fatalf("existing asset was not reported: %v", err)
	}
	decide(t, target, "admin", PlatformApp, SchemaPackageRetire, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID}, at)
	if got := refuse(t, target, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at); got == "ok" {
		t.Fatal("changed blueprint replaced its original version")
	}
	if err = os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	decide(t, target, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, at)
	if held := testConsoleOf(target).packagesOf()[descriptor.ID]; held.State != "active" || len(held.Retained) != 0 {
		t.Fatalf("blueprint was not reinstalled as the same version: %+v", held)
	}
	if current, ok := platform.Get[build.Application](target.automation(construction.ID, false), answer.ApplicationID); !ok || !reflect.DeepEqual(current, app) {
		t.Fatalf("uninstall and reinstall changed the application draft: %+v", current)
	}
	recovered := compose()
	recovered.Files = target.files()
	if err = recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	again, err := recovered.CreatePackageDrafts(memberOf(t, recovered, "builder"), descriptor.ID, descriptor.Version, "create", at.Add(2*time.Hour))
	if err != nil || !reflect.DeepEqual(again, answer) {
		t.Fatalf("recovered package retry changed IDs: %+v %v", again, err)
	}
	if _, ok := recovered.entity("build.item"); ok {
		t.Fatal("recovery published a package draft")
	}
	if _, problem := recovered.SimulateCandidate(memberOf(t, recovered, "builder"), CandidateSimulationRequest{Roots: plan.Roots, At: plan.At, Steps: []SimulationStep{{Type: "build.item", ID: "sample", Action: "build.item.create", Payload: json.RawMessage(`{"title":"Example"}`), Expect: "accepted"}}}); problem != nil {
		t.Fatalf("portable roots cannot use original simulation: %v", problem)
	}
}

func TestPackageBlueprintExtendsInstalledCodeWithoutNativeDrafts(t *testing.T) {
	base := platform.AssetRef{App: core.ID, Kind: platform.AssetApp, Name: core.ID}
	page, err := platform.PageReleaseAsset(construction.ID, "1", platform.Page{Name: "materialdesk", Title: "Materials", Object: platform.AssetRef{App: core.ID, Kind: platform.AssetObject, Name: core.MaterialType}, Sections: []platform.Section{{ID: "table", Widget: "table", Fields: []string{"name"}}}})
	if err != nil {
		t.Fatal(err)
	}
	application, err := platform.ApplicationReleaseAsset(construction.ID, "1", platform.Application{Name: "materialworkspace", Title: "Material workspace", Pages: []string{"materialdesk"}, Extends: &base, Entry: core.ID,
		Replacements: []platform.ApplicationPageReplacement{{View: "materials", Page: "materialdesk"}}})
	if err != nil {
		t.Fatal(err)
	}
	descriptor := PackageDescriptor{ID: "example.material-workspace", Version: "1.0.0", Namespace: "example.material-workspace", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{application.Ref}, Assets: []platform.ReleaseAsset{page, application}}}
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "materials.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	tn := composeTenant(t, "native-blueprint", []Seat{seatOf("admin", "platform:admin"), seatOf("builder", "build:builder", "core:steward")}, build.New("native-blueprint"), core.New("native-blueprint"))
	testConsoleOf(tn).BindPackageIndex(index)
	decide(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, txnAt)
	original, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	answer, err := tn.CreatePackageDrafts(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, "native-drafts", txnAt)
	if err != nil || len(answer.Drafts) != 2 || answer.Application != "materialworkspace" {
		t.Fatalf("native dependency prevented ordinary Build drafts: %+v %v", answer, err)
	}
	for _, draft := range answer.Drafts {
		if draft.Ref.App != construction.ID || draft.Type != construction.AppType && draft.Type != construction.PageType {
			t.Fatalf("native prerequisite became a draft: %+v", draft)
		}
	}
	current, err := tn.app(core.ID).(*core.Core).Snapshot()
	if err != nil || string(original) != string(current) || tn.ActiveRelease() != "" {
		t.Fatal("draft creation changed the native owner or activated the application")
	}
	preview, err := tn.PreviewReleaseDrafts(memberOf(t, tn, "builder"), []construction.JointDraftRef{{Kind: platform.AssetPage, ID: "materialdesk"}, {Kind: platform.AssetApp, ID: "materialworkspace"}})
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("native extension drafts do not form an ordinary candidate: %+v %v", preview, err)
	}
}

func TestPackageBlueprintBatchesResumeFromAcceptedPrefix(t *testing.T) {
	const id = "blueprint-batches"
	var assets []platform.ReleaseAsset
	var pages []string
	for i := range 66 {
		name := fmt.Sprintf("materials%02d", i)
		page, err := platform.PageReleaseAsset(construction.ID, "1", platform.Page{Name: name, Title: name, Object: platform.AssetRef{App: core.ID, Kind: platform.AssetObject, Name: core.MaterialType}, Sections: []platform.Section{{ID: "table", Widget: "table", Fields: []string{"name"}}}})
		if err != nil {
			t.Fatal(err)
		}
		assets = append(assets, page)
		pages = append(pages, name)
	}
	application, err := platform.ApplicationReleaseAsset(construction.ID, "1", platform.Application{Name: "materials", Title: "Materials", Pages: pages})
	if err != nil {
		t.Fatal(err)
	}
	assets = append(assets, application)
	descriptor := PackageDescriptor{ID: "example.materials", Version: "1.0.0", Namespace: "example.materials", Blueprint: &PackageBlueprint{Roots: []platform.AssetRef{application.Ref}, Assets: assets}}
	alternative := descriptor
	alternative.ID, alternative.Namespace = "example.materials-alternative", "example.materials-alternative"
	alternative.Blueprint = &PackageBlueprint{Roots: descriptor.Blueprint.Roots, Assets: append([]platform.ReleaseAsset(nil), assets...)}
	var changedPage platform.Page
	if err = json.Unmarshal(assets[65].Body, &changedPage); err != nil {
		t.Fatal(err)
	}
	changedPage.Title = "A different final page"
	alternative.Blueprint.Assets[65], err = platform.PageReleaseAsset(construction.ID, "1", changedPage)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	if err = os.WriteFile(filepath.Join(directory, "materials.json"), platform.Raw(descriptor), 0600); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(directory, "alternative.json"), platform.Raw(alternative), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := LoadPackageIndex(directory)
	if err != nil {
		t.Fatal(err)
	}
	compose := func() *Tenant {
		tn := composeTenant(t, id, []Seat{seatOf("admin", "platform:admin"), seatOf("builder", "build:builder")}, build.New(id), core.New(id))
		testConsoleOf(tn).BindPackageIndex(index)
		return tn
	}
	tn := compose()
	var entries []Entry
	var prefix acceptedComposite
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		if saved, err := decodeAcceptedComposite(entry.Body); err == nil {
			if prefix.Key != "" {
				return nil, errors.New("second append unavailable")
			}
			prefix = saved
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	decide(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, descriptor.ID, map[string]any{"id": descriptor.ID, "version": descriptor.Version}, txnAt)
	decide(t, tn, "admin", PlatformApp, SchemaPackageInstall, PackageType, alternative.ID, map[string]any{"id": alternative.ID, "version": alternative.Version}, txnAt)
	if _, err = tn.CreatePackageDrafts(memberOf(t, tn, "builder"), descriptor.ID, descriptor.Version, "create", txnAt); err == nil || !strings.Contains(err.Error(), "second append unavailable") {
		t.Fatalf("second batch did not encounter the append failure: %v", err)
	}
	if prefix.Version != 3 || len(prefix.Edits) != 64 || len(prefix.Context) != 64 || len(entries) != 3 {
		t.Fatalf("unbounded or unsealed prefix: version=%d edits=%d entries=%d", prefix.Version, len(prefix.Edits), len(entries))
	}
	if tn.ActiveRelease() != "" {
		t.Fatal("partial draft creation published an application")
	}
	// Recovery applies accepted images; it must not attempt the unaccepted tail.
	recovered := compose()
	recovered.Files = tn.files()
	if err = recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, recovered, entries, compose)
	for _, edit := range prefix.Edits {
		if edit.Target.GetId() == changedPage.Name {
			t.Fatal("the changed page must be in the unaccepted tail for this scenario")
		}
		if _, problem := recovered.RecordOf(memberOf(t, recovered, "builder"), edit.Target.GetType(), edit.Target.GetId(), txnAt); problem != nil {
			t.Fatalf("accepted prefix was not restored: %v", problem)
		}
	}
	var tail acceptedComposite
	recovered.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		var err error
		tail, err = decodeAcceptedComposite(entry.Body)
		if err != nil {
			t.Fatal(err)
		}
		entries = append(entries, entry)
		return entry.Body, nil
	}
	if _, err = recovered.CreatePackageDrafts(memberOf(t, recovered, "builder"), alternative.ID, alternative.Version, "create", txnAt.Add(time.Hour)); err == nil || !strings.Contains(err.Error(), "key belongs to another request") || len(entries) != 3 {
		t.Fatalf("the same prefix allowed a different unaccepted tail: %v", err)
	}
	answer, err := recovered.CreatePackageDrafts(memberOf(t, recovered, "builder"), descriptor.ID, descriptor.Version, "create", txnAt.Add(time.Hour))
	if err != nil || len(answer.Drafts) != 67 || tail.Version != 3 || len(tail.Edits) != 3 || tail.Context != prefix.Context || len(entries) != 4 {
		t.Fatalf("retry did not append only the original tail: drafts=%d edits=%d entries=%d error=%v", len(answer.Drafts), len(tail.Edits), len(entries), err)
	}
	again, err := recovered.CreatePackageDrafts(memberOf(t, recovered, "builder"), descriptor.ID, descriptor.Version, "create", txnAt.Add(2*time.Hour))
	if err != nil || !reflect.DeepEqual(again, answer) || len(entries) != 4 || recovered.ActiveRelease() != "" {
		t.Fatalf("completed retry changed drafts or publication: %v", err)
	}
	for _, saved := range []acceptedComposite{prefix, tail} {
		for _, mutate := range []func(*acceptedComposite){
			func(value *acceptedComposite) { value.Context = "" },
			func(value *acceptedComposite) { value.Context = "invalid" },
			func(value *acceptedComposite) { value.Version = 2 },
			func(value *acceptedComposite) { value.Context = strings.Repeat("0", 64) },
		} {
			changed := saved
			mutate(&changed)
			if _, err := encodeAcceptedComposite(changed); err == nil {
				t.Fatal("changed batch context retained the original request identity")
			}
		}
	}
	final := compose()
	final.Files = recovered.files()
	if err = final.Replay(entries); err != nil {
		t.Fatal(err)
	}
	last, err := final.CreatePackageDrafts(memberOf(t, final, "builder"), descriptor.ID, descriptor.Version, "create", txnAt.Add(3*time.Hour))
	if err != nil || !reflect.DeepEqual(last, answer) || final.ActiveRelease() != "" {
		t.Fatalf("completed batches did not recover as unpublished drafts: %v", err)
	}
}
