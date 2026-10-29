package organization

import (
	"bytes"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"testing"
	"time"
)

func TestOrgProjection(t *testing.T) {
	seed := platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}},
		Units:       []platform.Unit{{ID: "plant", Name: "Plant", Kind: "factory", From: "2020-01-01"}, {ID: "l1", Name: "Line 1", Kind: "line", From: "2020-01-01"}},
		Edges:       []platform.Edge{{Structure: "site", Unit: "l1", Parent: "plant", From: "2020-01-01"}},
		Memberships: []platform.Membership{{Party: "member:ann", Unit: "plant", Role: "manager", From: "2020-01-01"}, {Party: "member:bob", Unit: "l1", Role: "operator", From: "2020-01-01"}}}
	e := New("t-1", seed)
	if got := e.Units("member:ann", "site", "2026-01-01"); len(got) != 2 {
		t.Fatalf("ann's units %v", got)
	}
	if got := e.Units("member:bob", "", "2026-01-01"); len(got) != 1 || got[0] != "l1" {
		t.Fatalf("bob's units %v", got)
	}
	if got := e.Holders("site", "l1", "", "2026-01-01"); len(got) != 2 {
		t.Fatalf("holders %v", got)
	}
	back := e.Model().OrgSeed()
	if len(back.Units) != 2 || len(back.Edges) != 1 || len(back.Memberships) != 2 || back.Memberships[0].Party != "member:ann" {
		t.Fatalf("projection %+v", back)
	}
	// a decision: add a post and fill it
	c := platform.Caller{Tenant: "t-1", ID: "ann", App: ID, Roles: map[string]string{ID: Admin}}
	sub := func(schema, id string, raw []byte) *pb.Submission {
		typ := ElementType
		if schema == SchemaRelationshipAdd {
			typ = RelationshipType
		}
		return &pb.Submission{TenantId: "t-1", PrincipalId: "ann", Authority: ID, IdempotencyKey: schema + id, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: typ, Id: id}, Payload: raw}
	}
	submit := func(schema, id string, body any) *pb.ChangeRecord {
		raw, _ := json.Marshal(body)
		rec, err := e.Submit(c, sub(schema, id, raw), time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
		if err != nil {
			t.Fatalf("%s: %v", schema, err)
		}
		return rec
	}
	submit(SchemaElementAdd, "post-1", map[string]any{"stereotype": Post, "name": "Line lead"})
	submit(SchemaElementAdd, "person-1", map[string]any{"stereotype": Person, "name": "Ann"})
	submit(SchemaRelationshipAdd, "r-1", map[string]any{"stereotype": FillsPost, "source": "person-1", "target": "post-1"})
	raw, _ := json.Marshal(map[string]any{"stereotype": Capability, "name": "Bad"})
	if _, err := e.Submit(c, sub(SchemaElementAdd, "cap-2", raw), time.Now()); err == nil {
		t.Fatal("a retired standard type passed")
	}
	if got := e.Model().Of("person-1", FillsPost, true, "2026-02-01"); len(got) != 1 || got[0] != "post-1" {
		t.Fatalf("post of person-1 %v", got)
	}
	// what the host's Directory answers the apps
	if info, ok := e.Element("l1", "2026-02-01"); !ok || info.Stereotype != Organization || info.Kind != "line" {
		t.Fatalf("element l1 %+v %v", info, ok)
	}
	if _, ok := e.Element("l1", "2019-01-01"); ok {
		t.Fatal("l1 is live before it starts")
	}
	if got := e.Related("post-1", FillsPost, false, "2026-02-01"); len(got) != 1 || got[0] != "person-1" {
		t.Fatalf("holders of post-1 %v", got)
	}
}

func TestAcceptedModelOwnsNestedValuesAndAppliesWithoutDecisions(t *testing.T) {
	live := New("tenant", platform.OrgSeed{})
	live.model.Elements = []Element{{ID: "unit", Stereotype: Organization, Name: "Unit", Properties: map[string]any{"nested": map[string]any{"owner": "old"}}}}
	live.model.Views = []View{{ID: "view", Name: "View", Elements: []string{"unit"}, Layout: map[string][2]float64{"unit": {1, 2}}, Pins: []Pin{{Ref: "record:work.task/task", Label: "Old"}}}}
	before, _ := live.AcceptedState()
	app, err := live.ForkAcceptedState()
	if err != nil {
		t.Fatal(err)
	}
	private := app.(*Enterprise)
	private.model.Elements[0].Properties["nested"].(map[string]any)["owner"] = "new"
	private.model.Views[0].Elements[0] = "next"
	private.model.Views[0].Layout["unit"] = [2]float64{3, 4}
	private.model.Views[0].Pins[0].Label = "New"
	unchanged, _ := live.AcceptedState()
	if !bytes.Equal(before, unchanged) {
		t.Fatal("private enterprise model aliases live nested values")
	}
	image, _ := private.AcceptedState()
	if err := live.ApplyAcceptedState(image); err != nil {
		t.Fatal(err)
	}
	after, _ := live.AcceptedState()
	if !bytes.Equal(image, after) || live.AcceptedLedger() != private.AcceptedLedger() || len(live.ledger.RecordsFor("tenant")) != 0 {
		t.Fatal("pure model application changed its ledger or omitted nested values")
	}
	if err := live.ApplyAcceptedState([]byte(`{"elements":[{"id":"same"},{"id":"same"}]}`)); err == nil {
		t.Fatal("duplicate enterprise element accepted")
	}
	still, _ := live.AcceptedState()
	if !bytes.Equal(after, still) {
		t.Fatal("invalid model image changed live state")
	}
}

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

func TestSourceSyncRetirementPreservesExistingSourceState(t *testing.T) {
	e := New("sync-submit", platform.OrgSeed{})
	e.model.Elements = []Element{{ID: "source:hr:cap", Stereotype: Capability, Name: "Capability", Owner: "source:hr"}}
	c := platform.Caller{Tenant: "sync-submit", ID: "pipeline", App: ID, Roles: map[string]string{ID: Admin}, Automation: true}
	before, _ := e.AcceptedState()
	if err := lifecycleSubmit(e, c, SchemaSliceSync, ModelType, "hr", map[string]any{"source": "hr", "elements": []Element{{ID: "unit", Stereotype: Organization, Name: "Unit"}}}, time.Now()); err == nil {
		t.Fatal("retired full source sync accepted")
	}
	after, _ := e.AcceptedState()
	if string(before) != string(after) {
		t.Fatal("retired sync changed source-owned history")
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

func TestRetiredViewWritesKeepOriginalPinsAndLayout(t *testing.T) {
	e := New("view", platform.OrgSeed{})
	e.model.Views = []View{{ID: "old", Name: "Original", Viewpoint: "organization", Elements: []string{}, Pins: []Pin{{Ref: "record:work.task/hidden", Anchor: "unit"}}}}
	c := platform.Caller{Tenant: "view", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	before, _ := e.AcceptedState()
	for _, schema := range []string{SchemaViewSave, SchemaViewDelete} {
		if err := lifecycleSubmit(e, c, schema, ViewType, "old", map[string]any{"name": "Replacement", "viewpoint": "organization", "elements": []string{}, "id": "old"}, time.Now()); err == nil {
			t.Fatal("retired view editor accepted", schema)
		}
	}
	after, _ := e.AcceptedState()
	if string(before) != string(after) {
		t.Fatal("view retirement rewrote existing history")
	}
	restarted := New("view", platform.OrgSeed{})
	if err := restarted.ApplyAcceptedState(before); err != nil {
		t.Fatal(err)
	}
	restored, _ := restarted.AcceptedState()
	if string(before) != string(restored) {
		t.Fatal("accepted historical view lost its original bytes")
	}
}

func TestRetainedSharedKindsAndElementsStaySourceOwned(t *testing.T) {
	e := New("kind-import", platform.OrgSeed{})
	kindID := ImportedElementID("source", "management")
	e.model.Kinds = []Kind{{ID: kindID, Name: "Management", Kind: "management", Owner: "tenant:source"}}
	e.model.Elements = []Element{{ID: "tenant:source:root", Stereotype: Organization, Name: "Root", Owner: "tenant:source"}}
	c := platform.Caller{Tenant: "kind-import", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	before, _ := e.AcceptedState()
	if err := lifecycleSubmit(e, c, SchemaKindEdit, ModelType, kindID, map[string]any{"name": "Overwrite"}, time.Now()); err == nil {
		t.Fatal("shared kind renamed locally")
	}
	if err := lifecycleSubmit(e, c, SchemaElementEdit, ElementType, "tenant:source:root", map[string]any{"name": "Overwrite"}, time.Now()); err == nil {
		t.Fatal("shared element renamed locally")
	}
	after, _ := e.AcceptedState()
	if string(before) != string(after) {
		t.Fatal("source-owned facts changed")
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

func TestDirectoryWritesRetireStandardTypesWithoutChangingHistory(t *testing.T) {
	e := New("directory-only", platform.OrgSeed{Units: []platform.Unit{{ID: "unit", Name: "Unit"}}})
	e.model.Elements = append(e.model.Elements, Element{ID: "legacy", Stereotype: Capability, Name: "Original capability", Properties: map[string]any{"kind": "Capability"}})
	e.model.Views = []View{{ID: "original", Name: "Original", Viewpoint: "function", Elements: []string{"legacy"}}}
	c := platform.Caller{Tenant: "directory-only", ID: "admin", App: ID, Roles: map[string]string{ID: Admin}}
	before, _ := e.AcceptedState()
	at := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	for _, v := range []struct {
		schema, typ, id string
		p               map[string]any
	}{
		{SchemaElementAdd, ElementType, "new-cap", map[string]any{"name": "Cap", "stereotype": Capability}},
		{SchemaElementEdit, ElementType, "legacy", map[string]any{"properties": map[string]any{}}},
		{SchemaRelationshipAdd, RelationshipType, "bad", map[string]any{"stereotype": FillsPost, "source": "unit", "target": "legacy"}},
		{SchemaSliceImport, ModelType, "model", map[string]any{"slice": Slice{Tenant: "other", UAF: "1.3"}}},
	} {
		if err := lifecycleSubmit(e, c, v.schema, v.typ, v.id, v.p, at); err == nil {
			t.Fatal("unsupported directory write accepted", v.schema)
		}
	}
	after, _ := e.AcceptedState()
	if string(before) != string(after) {
		t.Fatal("directory refusal changed historical model")
	}
	saved, err := e.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	recovered := New("directory-only", platform.OrgSeed{})
	if err := recovered.Restore(saved); err != nil {
		t.Fatal(err)
	}
	original, _ := recovered.AcceptedState()
	if string(before) != string(original) {
		t.Fatal("historical standard properties or view changed on recovery")
	}
	for _, schema := range []string{SchemaViewSave, SchemaViewDelete, SchemaSliceImport, SchemaSliceSync} {
		if slices.ContainsFunc(directoryActions(), func(action platform.Action) bool { return action.Schema == schema }) {
			t.Fatal("retired action remains discoverable", schema)
		}
	}
}
