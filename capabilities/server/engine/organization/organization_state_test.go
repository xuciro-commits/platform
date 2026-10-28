package organization

import (
	"bytes"
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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
	submit(SchemaElementAdd, "cap-1", map[string]any{"stereotype": Capability, "name": "Assemble", "properties": map[string]any{"kind": "Operational"}})
	submit(SchemaRelationshipAdd, "r-1", map[string]any{"stereotype": Performs, "source": "l1", "target": "cap-1"})
	raw, _ := json.Marshal(map[string]any{"stereotype": Capability, "name": "Bad", "properties": map[string]any{"kind": "Nope"}})
	if _, err := e.Submit(c, sub(SchemaElementAdd, "cap-2", raw), time.Now()); err == nil {
		t.Fatal("a bad enumeration literal passed")
	}
	if got := e.Model().Of("l1", Performs, true, "2026-02-01"); len(got) != 1 || got[0] != "cap-1" {
		t.Fatalf("capabilities of l1 %v", got)
	}
	// what the host's Directory answers the apps
	if info, ok := e.Element("l1", "2026-02-01"); !ok || info.Stereotype != Organization || info.Kind != "line" {
		t.Fatalf("element l1 %+v %v", info, ok)
	}
	if _, ok := e.Element("l1", "2019-01-01"); ok {
		t.Fatal("l1 is live before it starts")
	}
	if got := e.Related("cap-1", Performs, false, "2026-02-01"); len(got) != 1 || got[0] != "l1" {
		t.Fatalf("performers of cap-1 %v", got)
	}
}

// Federation: a subsidiary publishes, the group imports read-only.
func TestFederation(t *testing.T) {
	sub := New("sub", platform.OrgSeed{})
	grp := New("grp", platform.OrgSeed{})
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	do := func(e *Enterprise, tenant, schema, typ, id string, body any) *kernel.Error {
		raw, _ := json.Marshal(body)
		c := platform.Caller{Tenant: tenant, ID: "ann", App: ID, Roles: map[string]string{ID: Admin}}
		_, err := e.Submit(c, &pb.Submission{TenantId: tenant, PrincipalId: "ann", Authority: ID, IdempotencyKey: schema + id, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Target: &pb.EntityRef{Type: typ, Id: id}, Payload: raw}, at)
		return err
	}
	must := func(err *kernel.Error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%v: %s", err, err.Message)
		}
	}
	must(do(sub, "sub", SchemaElementAdd, ElementType, "co", map[string]any{"stereotype": Organization, "name": "Acme Industrial", "legal": true}))
	must(do(sub, "sub", SchemaElementAdd, ElementType, "hr", map[string]any{"stereotype": Organization, "name": "HR"}))
	must(do(sub, "sub", SchemaElementAdd, ElementType, "cap", map[string]any{"stereotype": Capability, "name": "Machining"}))
	must(do(sub, "sub", SchemaRelationshipAdd, RelationshipType, "r1", map[string]any{"stereotype": Performs, "source": "co", "target": "cap"}))
	published := true
	for _, id := range []string{"co", "cap"} {
		must(do(sub, "sub", SchemaElementEdit, ElementType, id, map[string]any{"published": published}))
	}
	slice, _ := sub.Read(platform.Caller{}, ReadPublished)
	sl := slice.(Slice)
	if len(sl.Elements) != 2 || len(sl.Relationships) != 1 || sl.Elements[0].Owner != "tenant:sub" {
		t.Fatalf("slice %+v", sl)
	}
	must(do(grp, "grp", SchemaElementAdd, ElementType, "group", map[string]any{"stereotype": Organization, "name": "Acme Group", "legal": true}))
	must(do(grp, "grp", SchemaKindAdd, ModelType, "legal", map[string]any{"name": "Ownership", "kind": "legal"}))
	must(do(grp, "grp", SchemaSliceImport, ModelType, "sub", map[string]any{"slice": sl}))
	company := ImportedElementID("sub", "co")
	must(do(grp, "grp", SchemaRelationshipAdd, RelationshipType, "own", map[string]any{"stereotype": Placement, "kind": "legal", "source": company, "target": "group", "share": 1}))
	if err := do(grp, "grp", SchemaElementEdit, ElementType, company, map[string]any{"name": "Renamed"}); err == nil {
		t.Fatal("a mirrored element was edited in the group")
	}
	if got := grp.Units("", "legal", "2026-02-01"); len(got) != 0 {
		t.Fatalf("units %v", got)
	}
	if !grp.Model().BelowIn("legal", "group", company, "2026-02-01") {
		t.Fatal("the subsidiary is not below the group in the legal kind")
	}
	// a second import with the capability unpublished drops it, keeps the group's own ownership edge
	sl.Elements = sl.Elements[:1]
	sl.Relationships = nil
	must(do(grp, "grp", SchemaSliceImport, ModelType, "sub-2", map[string]any{"slice": sl}))
	if m := grp.Model(); len(m.Elements) != 3 || len(m.Relationships) != 2 || m.ElementOf(ImportedElementID("sub", "cap")).Until == "" || m.RelationshipOf("own") == nil {
		t.Fatalf("after re-import: %d elements %+v", len(m.Elements), m.Relationships)
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
	kindID := ImportedElementID("source", "management")
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
