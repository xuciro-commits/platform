package enterprise

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	enterprisemodel "platformserver/internal/enterprise"
	"platformserver/platform"
	"testing"
	"time"
)

// The original enterprise decision path, with a fresh key per attempted edit.
func lifecycleSubmit(e *Enterprise, c platform.Caller, schema, typ, id string, body any, at time.Time) *kernel.Error {
	raw, _ := json.Marshal(body)
	_, err := e.Submit(c, &pb.Submission{TenantId: c.Tenant, PrincipalId: c.ID, Authority: ID, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: typ, Id: id}, Payload: raw, IdempotencyKey: fmt.Sprintf("lifecycle-%s-%s-%s", schema, id, raw)}, at)
	return err
}

func TestElementLifecycleKeepsGapsAndDeletedIdentity(t *testing.T) {
	e := New("lifecycle", platform.OrgSeed{})
	c := platform.Caller{Tenant: "lifecycle", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	do := func(schema, id string, p map[string]any) {
		t.Helper()
		if err := lifecycleSubmit(e, c, schema, ElementType, id, p, at); err != nil {
			t.Fatalf("%s: %v %s", schema, err, err.Message)
		}
	}
	do(SchemaElementAdd, "unit", map[string]any{"name": "Unit", "stereotype": Organization, "shortName": "U", "kind": "team"})
	do(SchemaElementEdit, "unit", map[string]any{"name": "Renamed", "shortName": "", "kind": ""})
	model := e.Model()
	if model.ElementOf("unit").ShortName != "" || model.ElementOf("unit").Kind != "" {
		t.Fatal("optional text did not clear")
	}
	do(SchemaElementClose, "unit", map[string]any{"until": "2026-02-01", "reason": "Paused"})
	do(SchemaElementReopen, "unit", map[string]any{"from": "2026-03-01", "reason": "Operating again"})
	for day, want := range map[string]bool{"2026-01-15": true, "2026-02-15": false, "2026-03-15": true} {
		if _, ok := e.Element("unit", day); ok != want {
			t.Fatalf("effective on %s=%v want %v", day, ok, want)
		}
	}
	do(SchemaElementClose, "unit", map[string]any{"until": "2026-04-01", "reason": "Mistaken closure"})
	do(SchemaElementReinstate, "unit", map[string]any{"reason": "Correct mistaken closure"})
	if _, ok := e.Element("unit", "2026-04-15"); !ok {
		t.Fatal("reinstate did not remove last closure")
	}
	if _, ok := e.Element("unit", "2026-02-15"); ok {
		t.Fatal("reinstate erased an older gap")
	}
	do(SchemaElementAdd, "unused", map[string]any{"name": "Unused", "stereotype": Organization})
	do(SchemaElementDelete, "unused", map[string]any{"reason": "Created by mistake"})
	if err := lifecycleSubmit(e, c, SchemaElementAdd, ElementType, "unused", map[string]any{"name": "Reuse", "stereotype": Organization}, at); err == nil {
		t.Fatal("deleted identity was reused")
	}
	result, _ := e.Query(nil, "", "unused", "all", 50, at)
	if len(result.Elements) != 0 {
		t.Fatal("deleted element remains in picker")
	}
}

func TestRelationshipLifecycleChecksIntervalsSourcesAndUses(t *testing.T) {
	e := New("relationships", platform.OrgSeed{})
	c := platform.Caller{Tenant: "relationships", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	do := func(schema, typ, id string, p map[string]any) *kernel.Error {
		return lifecycleSubmit(e, c, schema, typ, id, p, at)
	}
	for _, id := range []string{"parent", "child", "other"} {
		if err := do(SchemaElementAdd, ElementType, id, map[string]any{"name": id, "stereotype": Organization}); err != nil {
			t.Fatal(err)
		}
	}
	if err := do(SchemaKindAdd, ModelType, "management", map[string]any{"name": "Management", "kind": "management"}); err != nil {
		t.Fatal(err)
	}
	if err := do(SchemaKindEdit, ModelType, "management", map[string]any{"name": "Reporting"}); err != nil {
		t.Fatal(err)
	}
	relation := func(source, target, from string) map[string]any {
		return map[string]any{"stereotype": Placement, "source": source, "target": target, "kind": "management", "from": from}
	}
	if err := do(SchemaRelationshipAdd, RelationshipType, "future", relation("child", "parent", "2026-03-01")); err != nil {
		t.Fatal(err)
	}
	if err := do(SchemaRelationshipAdd, RelationshipType, "overlap", relation("child", "other", "2026-02-01")); err == nil {
		t.Fatal("a scheduled second parent was accepted")
	}
	if err := do(SchemaRelationshipAdd, RelationshipType, "cycle", relation("parent", "child", "2026-04-01")); err == nil {
		t.Fatal("a future cycle was accepted")
	}
	if err := do(SchemaElementClose, ElementType, "parent", map[string]any{"until": "2026-02-01", "reason": "Close"}); err == nil {
		t.Fatal("element closed before its scheduled relationship")
	}
	if err := do(SchemaRelationshipEnd, RelationshipType, "future", map[string]any{"until": "2026-04-01"}); err != nil {
		t.Fatal(err)
	}
	if err := do(SchemaRelationshipEnd, RelationshipType, "future", map[string]any{"until": "2026-05-01"}); err == nil {
		t.Fatal("end silently reopened a relationship")
	}
	e.model.Relationships = append(e.model.Relationships, Relationship{ID: "sync:hr:membership", Stereotype: Membership, Source: "member:ann", Target: "parent", Role: "employee", From: "2026-01-01"})
	if err := do(SchemaRelationshipEnd, RelationshipType, "sync:hr:membership", map[string]any{"until": "2026-02-01"}); err == nil {
		t.Fatal("a source relationship was edited locally")
	}
	if err := do(SchemaElementDelete, ElementType, "child", map[string]any{"reason": "Delete"}); err == nil {
		t.Fatal("historically used element was deleted")
	}
}

func TestSubmitSourceSyncParsesElementObjectsAndKeepsSourceValidation(t *testing.T) {
	e := New("sync-submit", platform.OrgSeed{})
	c := platform.Caller{Tenant: "sync-submit", ID: "pipeline", App: ID, Roles: map[string]string{ID: Admin}, Automation: true}
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	elements := []Element{{ID: "team", Stereotype: Organization, Name: "Team"}, {ID: "cap", Stereotype: Capability, Name: "Capability"}}
	body := map[string]any{"source": "hr", "elements": elements, "relationships": []Relationship{{ID: "ability", Stereotype: Performs, Source: "team", Target: "cap"}}}
	if err := lifecycleSubmit(e, c, SchemaSliceSync, ModelType, "hr", body, at); err != nil {
		t.Fatalf("valid source sync refused: %v %s", err, err.Message)
	}
	model := e.Model()
	if model.ElementOf("team") == nil || model.ElementOf("team").Owner != "source:hr" || model.RelationshipOf("sync:hr:ability") == nil {
		t.Fatal("source objects or relationship not saved")
	}
	before, _ := e.AcceptedState()
	body["elements"] = []Element{{ID: "team", Stereotype: Organization, Name: "Team"}, {ID: "cap", Stereotype: Organization, Name: "Invalid changed type"}}
	if err := lifecycleSubmit(e, c, SchemaSliceSync, ModelType, "hr", body, at); err == nil || err.Message != "A source element keeps its identity and type" {
		t.Fatalf("source owner validation was bypassed: %v", err)
	}
	after, _ := e.AcceptedState()
	if string(before) != string(after) {
		t.Fatal("refused source sync changed the model")
	}
}

type lifecycleRuntime struct {
	platform.Runtime
	e    *Enterprise
	deny bool
}

func (r lifecycleRuntime) CheckEnterpriseUsage(platform.Caller, string, string, bool, time.Time) *kernel.Error {
	r.e.Model()
	if r.deny {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	return nil
}
func (r lifecycleRuntime) CheckEnterpriseTenantConnection(platform.Caller, string, time.Time) *kernel.Error {
	return nil
}
func (r lifecycleRuntime) CheckEnterpriseTenantLink(platform.Caller, string, uint32, time.Time) *kernel.Error {
	return nil
}

func TestElementUsageRefusalRunsOutsideModelLockAndKeepsState(t *testing.T) {
	e := New("usage", platform.OrgSeed{Units: []platform.Unit{{ID: "unit", Name: "Unit"}}})
	runtime := lifecycleRuntime{e: e, deny: true}
	member := platform.Member{Tenant: "usage", ID: "admin", Roles: map[string]string{ID: Admin}}
	c := platform.NewCaller(runtime, member, ID, false, false)
	before, _ := e.AcceptedState()
	done := make(chan *kernel.Error, 1)
	go func() {
		done <- lifecycleSubmit(e, c, SchemaElementClose, ElementType, "unit", map[string]any{"until": "2026-02-01", "reason": "Close"}, time.Now())
	}()
	select {
	case err := <-done:
		if err == nil || err.Code != pb.ErrorCode_ERROR_CODE_CONFLICT {
			t.Fatalf("usage refusal: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("usage check ran under the enterprise model lock")
	}
	after, _ := e.AcceptedState()
	if string(before) != string(after) {
		t.Fatal("usage refusal changed the model")
	}
}

func TestViewCanRemoveAnUnreadableOldPinAndSourceEndpointsCannotChange(t *testing.T) {
	e := New("view", platform.OrgSeed{Units: []platform.Unit{{ID: "unit", Name: "Unit"}}})
	e.model.Views = []View{{ID: "view", Name: "View", Viewpoint: "organization", Elements: []string{"unit"}, Pins: []Pin{{Ref: "record:work.task/hidden", Anchor: "unit"}}}}
	c := platform.Caller{Tenant: "view", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := lifecycleSubmit(e, c, SchemaViewSave, ViewType, "view", map[string]any{"name": "View", "viewpoint": "organization", "elements": []string{"unit"}, "pins": []Pin{}}, at); err != nil {
		t.Fatalf("removing unreadable pin: %v", err)
	}
	if len(e.Model().Views[0].Pins) != 0 {
		t.Fatal("old pin was retained")
	}
	e.model.Elements = append(e.model.Elements, Element{ID: "source", Stereotype: Organization, Name: "Source", Owner: "source:hr"}, Element{ID: "cap", Stereotype: Capability, Name: "Capability", Owner: "source:hr"})
	e.model.Relationships = append(e.model.Relationships, Relationship{ID: "sync:hr:ability", Stereotype: Performs, Source: "source", Target: "cap"})
	c.Automation = true
	if err := lifecycleSubmit(e, c, SchemaSliceSync, ModelType, "hr", map[string]any{"source": "hr", "elements": []Element{{ID: "source", Stereotype: Organization, Name: "Source"}, {ID: "cap", Stereotype: Capability, Name: "Capability"}}, "relationships": []Relationship{{ID: "ability", Stereotype: Performs, Source: "unit", Target: "cap"}}}, at); err == nil {
		t.Fatal("source relationship endpoints changed under the same ID")
	}
}

func TestSharedPlacementKindsUseSourceIdentityAndStayReadOnly(t *testing.T) {
	e := New("kind-import", platform.OrgSeed{Units: []platform.Unit{{ID: "local", Name: "Local"}}})
	e.model.TenantLinks = []TenantLink{{ID: "link", Organization: "local", Tenant: "source", Root: "root", Connection: "CONNECTION"}}
	slice := Slice{Tenant: "source", UAF: "1.3", Kinds: []Kind{{ID: "management", Name: "Management", Kind: "management"}}, Elements: []Element{{ID: "root", Stereotype: Organization, Name: "Root"}, {ID: "child", Stereotype: Organization, Name: "Child"}}, Relationships: []Relationship{{ID: "placement", Stereotype: Placement, Kind: "management", Source: "child", Target: "root"}}}
	c := platform.Caller{Tenant: "kind-import", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	at := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := lifecycleSubmit(e, c, SchemaSliceImport, ModelType, "link", map[string]any{"slice": slice, "link": "link", "base": e.Model().Fingerprint(), "content": slice.Fingerprint()}, at); err != nil {
		t.Fatalf("shared hierarchy import: %v %s", err, err.Message)
	}
	model := e.Model()
	kindID := enterprisemodel.ImportedElementID("source", "management")
	if model.KindOf(kindID) == nil || model.RelationshipOf("fed:source:placement").Kind != kindID {
		t.Fatal("shared hierarchy lost the source kind identity")
	}
	if err := lifecycleSubmit(e, c, SchemaKindEdit, ModelType, kindID, map[string]any{"name": "Local overwrite"}, at); err == nil {
		t.Fatal("source kind was renamed locally")
	}
}

func TestTaggedReferencesUseMetamodelTypeInsteadOfMatchingFreeText(t *testing.T) {
	e := New("tagged", platform.OrgSeed{})
	e.model.Elements = []Element{{ID: "milestone", Stereotype: "ActualProjectMilestone", Name: "Milestone"}, {ID: "note", Stereotype: Location, Name: "Location", Properties: map[string]any{"address": "milestone"}}, {ID: "project", Stereotype: Project, Name: "Project", From: "2026-01-01", Until: "2026-03-01", Properties: map[string]any{"ownedMilestone": []any{"milestone"}}}}
	if refs := e.model.ElementPropertyUses("milestone", ""); len(refs) != 1 || refs[0] != "project" {
		t.Fatalf("free text treated as a reference or typed list missed: %v", refs)
	}
	if refs := e.model.ElementPropertyUses("milestone", "2026-02-01"); len(refs) != 1 {
		t.Fatalf("active tagged reference lost: %v", refs)
	}
	if refs := e.model.ElementPropertyUses("milestone", "2026-04-01"); len(refs) != 0 {
		t.Fatalf("ended tagged reference still current: %v", refs)
	}
	if why := closingUsage(e.model, "milestone", "2026-02-01"); why != "project" {
		t.Fatal("tagged reference did not block close")
	}
	if why := e.model.InternalUsage("milestone"); why != "element project" {
		t.Fatal("historical tagged reference did not block delete")
	}
	e.model.Elements[2].Properties = nil
	if why := e.model.InternalUsage("milestone"); why != "" {
		t.Fatalf("arbitrary text blocked delete: %s", why)
	}
}

func TestTenantLinkActionsKeepOriginalOrganisationAndImportedHistory(t *testing.T) {
	e := New("tenant-links", platform.OrgSeed{Units: []platform.Unit{{ID: "local", Name: "Local"}}})
	c := platform.Caller{Tenant: "tenant-links", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	at := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	payload := map[string]any{"organization": "local", "tenant": "source", "root": "root", "connection": "CONNECTION"}
	if err := lifecycleSubmit(e, c, SchemaTenantLinkSave, TenantLinkType, "link", payload, at); err != nil {
		t.Fatal(err)
	}
	before := e.Model().Fingerprint()
	if err := lifecycleSubmit(e, c, SchemaElementClose, ElementType, "local", map[string]any{"until": "2026-10-20", "reason": "Retire"}, at); err == nil {
		t.Fatal("linked organisation was closed")
	}
	e.model.TenantLinks[0].Content = "retained-content"
	e.model.TenantLinks[0].UpdatedAt = "2026-10-19"
	e.model.Elements = append(e.model.Elements, Element{ID: "tenant:source:root", Stereotype: Organization, Name: "Remote root", Owner: "tenant:source"})
	payload["connection"] = "OTHER-CONNECTION"
	if err := lifecycleSubmit(e, c, SchemaTenantLinkSave, TenantLinkType, "link", payload, at); err != nil {
		t.Fatal(err)
	}
	model := e.Model()
	if model.TenantLinkOf("link").Content != "" || model.Fingerprint() == before {
		t.Fatal("changed source retained stale import metadata or fingerprint")
	}
	if err := lifecycleSubmit(e, c, SchemaTenantLinkRemove, TenantLinkType, "link", map[string]any{}, at); err != nil {
		t.Fatal(err)
	}
	model = e.Model()
	if len(model.TenantLinks) != 0 || model.ElementOf("tenant:source:root") == nil || model.ElementOf("local") == nil {
		t.Fatal("removing a link deleted model history")
	}
}

func TestOrganisationPeriodsSurviveProjectionAndDriveDirectoryApproversAndCalendar(t *testing.T) {
	periods := []platform.Period{{From: "2026-01-01", Until: "2026-02-01"}, {From: "2026-03-01"}}
	seed := platform.OrgSeed{Structures: []platform.Structure{{ID: "management", Name: "Management", Kind: "management"}}, Units: []platform.Unit{{ID: "root", Name: "Root", From: "2026-01-01", Periods: periods, Calendar: "special"}, {ID: "child", Name: "Child", From: "2026-01-01", Periods: periods}}, Edges: []platform.Edge{{Structure: "management", Unit: "child", Parent: "root", From: "2026-01-01", Periods: periods}}, Memberships: []platform.Membership{{Party: "member:ann", Unit: "root", Role: "approver", From: "2026-01-01", Periods: periods}}, Calendars: []platform.Calendar{{ID: "default", Name: "Default"}, {ID: "special", Name: "Special", Workdays: []int{1, 2, 3, 4}}}}
	e := New("periods", seed)
	for _, day := range []string{"2026-01-15", "2026-03-15"} {
		if units := e.Units("member:ann", "management", day); len(units) != 2 {
			t.Fatalf("live directory %s: %v", day, units)
		}
		if holders := e.Holders("management", "child", "approver", day); len(holders) != 1 || holders[0] != "ann" {
			t.Fatalf("live approver %s: %v", day, holders)
		}
		if calendar := e.Calendar("member:ann", day); calendar.ID != "special" {
			t.Fatalf("live calendar %s: %+v", day, calendar)
		}
	}
	day := "2026-02-15"
	if units := e.Units("member:ann", "management", day); len(units) != 0 {
		t.Fatalf("directory crossed the closure gap: %v", units)
	}
	if holders := e.Holders("management", "child", "approver", day); len(holders) != 0 {
		t.Fatalf("approval lookup crossed the closure gap: %v", holders)
	}
	if calendar := e.Calendar("member:ann", day); calendar.ID != "default" {
		t.Fatalf("calendar crossed the closure gap: %+v", calendar)
	}
	projected, err := e.Read(platform.Caller{}, ReadOrganization)
	if err != nil {
		t.Fatal(err)
	}
	out := projected.(platform.OrgSeed)
	if len(out.Units[0].Periods) != 2 || len(out.Edges[0].Periods) != 2 || len(out.Memberships[0].Periods) != 2 {
		t.Fatal("organization read collapsed the effective periods")
	}
	restored := New("restored", out)
	if units := restored.Units("member:ann", "management", day); len(units) != 0 {
		t.Fatalf("organization projection round trip crossed the gap: %v", units)
	}
	out.Units[0].Periods[0].Until = "2026-01-02"
	if _, ok := e.Element("root", "2026-01-15"); !ok {
		t.Fatal("organization projection shared mutable period values")
	}
}
