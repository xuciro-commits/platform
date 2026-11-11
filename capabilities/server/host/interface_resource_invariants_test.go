package host

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func interfaceRecord(t *testing.T, live *invariantTenant, id string) build.InterfaceRecord {
	t.Helper()
	view, refusal := live.RecordOf(t.Context(), live.member(t), build.InterfaceType, id, invariantClock)
	if refusal != nil {
		t.Fatalf("read interface %s: %v", id, refusal)
	}
	var record build.InterfaceRecord
	if err := json.Unmarshal(platform.Raw(view.Record), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func resourceRecord(t *testing.T, live *invariantTenant, ref platform.AssetRef) build.Resource {
	t.Helper()
	rid := platform.ResourceID(live.ID, ref)
	view, refusal := live.RecordOf(t.Context(), live.member(t), build.ResourceType, rid, invariantClock)
	if refusal != nil {
		t.Fatalf("read resource %s: %v", ref, refusal)
	}
	var record build.Resource
	if err := json.Unmarshal(platform.Raw(view.Record), &record); err != nil {
		t.Fatal(err)
	}
	return record
}

func TestInvariantC14InterfaceResourceLifecycle(t *testing.T) {
	live := newInvariantTenant(t, "invariant-c14")
	shape := map[string]any{
		"name": "build.coded", "title": "Coded", "description": "Anything found by a code and a name.",
		"fields": []platform.InterfaceField{{Name: "code", Type: "text", Title: "Code"}, {Name: "name", Type: "text", Title: "Name"}},
	}
	live.accept(t, build.InterfaceType+".create", build.InterfaceType, "IFACE1", shape)
	live.accept(t, build.InterfaceType+".publish", build.InterfaceType, "IFACE1", map[string]any{})

	installed := interfaceRecord(t, live, "IFACE1")
	if installed.Version != 1 || installed.Published == "" || len(installed.Versions) != 1 {
		t.Fatalf("published interface has no retained version: %+v", installed)
	}
	resource := resourceRecord(t, live, platform.AssetRef{App: build.ID, Kind: platform.AssetInterface, Name: shape["name"].(string)})
	if resource.Project != live.project || resource.Ref.Kind != platform.AssetInterface {
		t.Fatalf("interface resource identity or project is wrong: %+v", resource)
	}

	const otherProject = "other-project"
	live.accept(t, build.AppType+".create", build.AppType, otherProject, map[string]any{"name": "otherproject", "title": "Other project"})
	object := map[string]any{
		"name": "codedorder", "title": "Coded order", "project": otherProject,
		"fields":     []build.Field{{Name: "code", Title: "Code", Type: "text"}, {Name: "name", Title: "Name", Type: "text"}, {Name: "note", Title: "Note", Type: "text"}},
		"implements": []string{"build.coded"},
	}
	live.accept(t, build.ObjectType+".create", build.ObjectType, "OBJ1", object)
	live.accept(t, build.ObjectType+".publish", build.ObjectType, "OBJ1", map[string]any{})
	if got := resourceRecord(t, live, platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.codedorder"}).Project; got != otherProject {
		t.Fatalf("cross-project object resource belongs to %q, want %q", got, otherProject)
	}

	invalid := map[string]any{
		"name": "missingcoded", "title": "Missing coded", "project": otherProject,
		"fields":     []build.Field{{Name: "name", Title: "Name", Type: "text"}},
		"implements": []string{"build.coded"},
	}
	invalidSubmission := live.submission(build.ObjectType+".create", build.ObjectType, "OBJ-BAD", invalid)
	if _, refusal := live.Submit(live.member(t), invalidSubmission, invariantClock); refusal == nil {
		invalidSubmission = live.submission(build.ObjectType+".publish", build.ObjectType, "OBJ-BAD", map[string]any{})
		if _, refusal := live.Submit(live.member(t), invalidSubmission, invariantClock); refusal == nil || !strings.Contains(refusal.Message, "no field code") {
			t.Fatalf("object missing an interface field was accepted: %v", refusal)
		}
	} else if !strings.Contains(refusal.Message, "no field code") {
		t.Fatalf("object missing an interface field was refused for the wrong reason: %v", refusal)
	}

	query := map[string]any{
		"name": "codedorders", "title": "Coded orders", "description": "Read every implementation of the coded interface.",
		"interface": "build.coded", "sort": []string{"id"}, "limit": 20,
	}
	live.accept(t, build.QueryType+".create", build.QueryType, "QUERY1", query)
	live.accept(t, build.QueryType+".publish", build.QueryType, "QUERY1", map[string]any{})
	queryRecord, refusal := live.RecordOf(t.Context(), live.member(t), build.QueryType, "QUERY1", invariantClock)
	if refusal != nil {
		t.Fatal(refusal)
	}
	var savedQuery build.Query
	if err := json.Unmarshal(platform.Raw(queryRecord.Record), &savedQuery); err != nil {
		t.Fatal(err)
	}
	if savedQuery.InterfaceShape == nil || !containsString(savedQuery.Implementations, "build.codedorder") {
		t.Fatalf("interface query did not bind the unified registry: %+v", savedQuery)
	}

	shape["title"] = "Coded and named"
	delete(object, "project")
	object["title"] = "Coded order, revised"
	live.accept(t, build.InterfaceType+".edit", build.InterfaceType, "IFACE1", shape)
	live.accept(t, build.ObjectType+".edit", build.ObjectType, "OBJ1", object)
	drafts := []build.JointDraftRef{{Kind: platform.AssetInterface, ID: "IFACE1"}, {Kind: platform.AssetObject, ID: "OBJ1"}}
	preview, err := live.PreviewReleaseDrafts(live.member(t), drafts, "", invariantClock)
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("interface candidate preview: %+v: %v", preview, err)
	}
	for _, ref := range []platform.AssetRef{
		{App: build.ID, Kind: platform.AssetInterface, Name: "build.coded"},
		{App: build.ID, Kind: platform.AssetObject, Name: "build.codedorder"},
	} {
		if !containsRef(preview.Included, ref) {
			t.Fatalf("interface candidate omits %s", ref)
		}
	}
	candidateID, err := live.SaveReleaseCandidates(live.member(t), drafts, preview.CandidateID, "c14-interface-candidate", invariantClock)
	if err != nil || candidateID != preview.CandidateID {
		t.Fatalf("save interface candidate: %s: %v", candidateID, err)
	}
	if active, err := live.ActivateRelease(live.member(t), candidateID, "c14-interface-release", invariantClock); err != nil || active != candidateID {
		t.Fatalf("activate interface release: %s: %v", active, err)
	}
	current := interfaceRecord(t, live, "IFACE1")
	if current.Version != 2 || len(current.Versions) != 2 || !containsString(current.Versions, installed.Published) {
		t.Fatalf("interface upgrade lost its retained history: %+v", current)
	}

	archivedDraft := map[string]any{
		"name": "build.archived", "title": "Archived draft", "description": "A draft that is archived before publication.",
		"fields": []platform.InterfaceField{{Name: "code", Type: "text", Title: "Code"}},
	}
	live.accept(t, build.InterfaceType+".create", build.InterfaceType, "IFACE2", archivedDraft)
	live.accept(t, build.InterfaceType+".archive", build.InterfaceType, "IFACE2", map[string]any{})
	if !interfaceRecord(t, live, "IFACE2").Archived {
		t.Fatal("an unpublished interface draft was not archived")
	}
	archivePublished := live.submission(build.InterfaceType+".archive", build.InterfaceType, "IFACE1", map[string]any{})
	if _, refusal := live.Submit(live.member(t), archivePublished, invariantClock); refusal == nil || !strings.Contains(refusal.Message, "published definition cannot be archived") {
		t.Fatalf("a published interface was archived: %v", refusal)
	}

	want := live.recordImage(t)
	for replay := 1; replay <= 2; replay++ {
		restored := live.restore(t, live.entries)
		if differing := differingTypes(want, restored.recordImage(t)); len(differing) != 0 {
			t.Fatalf("replay %d differs in %v", replay, differing)
		}
		registry := restored.interfaces()
		index := -1
		for i, s := range registry {
			if s.Name == "build.coded" {
				index = i
			}
		}
		if index < 0 || registry[index].Title != "Coded and named" {
			t.Fatalf("replay %d lost the installed interface: %+v", replay, registry)
		}
		var info platform.EntityInfo
		for _, entity := range restored.Entities(restored.member(t)) {
			if entity.Type == "build.codedorder" {
				info = entity
			}
		}
		if !containsString(info.Implements, "build.coded") {
			t.Fatalf("replay %d lost the object implementation: %+v", replay, info.Implements)
		}
	}
}

func TestInvariantC14ManifestInterfacesJoinRegistry(t *testing.T) {
	live := newInvariantTenantWithApps(t, "invariant-c14-manifest", []platform.App{&manifestInterfaceApp{ledger: platform.NewLedger("invariant-c14-manifest", "fixture", platform.NewCatalog())}})
	shape := map[string]any{
		"name": "build.coded", "title": "Tenant coded", "description": "A tenant interface.",
		"fields": []platform.InterfaceField{{Name: "code", Type: "text", Title: "Code"}},
	}
	live.accept(t, build.InterfaceType+".create", build.InterfaceType, "IFACE1", shape)
	live.accept(t, build.InterfaceType+".publish", build.InterfaceType, "IFACE1", map[string]any{})
	registry := (hostView{t: live.Tenant}).Interfaces()
	count := map[string]int{}
	for _, interfaceShape := range registry {
		count[interfaceShape.Name]++
	}
	if count["fixture.coded"] != 1 || count["build.coded"] != 1 {
		t.Fatalf("manifest and tenant interfaces did not join one registry: %+v", count)
	}
	tenant := map[string]any{
		"name": "coreimplementation", "title": "Core implementation",
		"fields":     []build.Field{{Name: "code", Title: "Code", Type: "text"}, {Name: "name", Title: "Name", Type: "text"}},
		"implements": []string{"fixture.coded"},
	}
	live.accept(t, build.ObjectType+".create", build.ObjectType, "OBJ1", tenant)
	live.accept(t, build.ObjectType+".publish", build.ObjectType, "OBJ1", map[string]any{})
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsRef(values []platform.AssetRef, want platform.AssetRef) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

type manifestInterfaceApp struct{ ledger *platform.Ledger }

func (a *manifestInterfaceApp) Manifest() platform.Manifest {
	return platform.Manifest{ID: "fixture", Title: "Fixture", Version: "1", Actions: a.ledger.Catalog, Interfaces: []platform.Interface{{Name: "fixture.coded", Title: "Coded", Description: "Anything found by a short unique code and a name.",
		Fields: []platform.InterfaceField{{Name: "code", Type: "text", Title: "Code"}, {Name: "name", Type: "text", Title: "Name"}}}}}
}
func (a *manifestInterfaceApp) AcceptedLedger() *platform.Ledger { return a.ledger }
func (a *manifestInterfaceApp) Fork(ledger *platform.Ledger) (platform.App, error) {
	return &manifestInterfaceApp{ledger: ledger}, nil
}
func (a *manifestInterfaceApp) Declarations() []*pb.AuthorityDeclaration {
	return a.ledger.Declarations()
}
func (a *manifestInterfaceApp) SnapshotFormat() string             { return "platform.ledger.v1" }
func (a *manifestInterfaceApp) Snapshot() (json.RawMessage, error) { return a.ledger.Snapshot() }
func (a *manifestInterfaceApp) Restore(raw json.RawMessage) error  { return a.ledger.Restore(raw) }
func (a *manifestInterfaceApp) Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
