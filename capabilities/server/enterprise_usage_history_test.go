package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"platformserver/internal/records"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/enterprise"
	"platformserver/platform"
)

func TestEnterpriseHistorySurvivesRemovedBuildReferenceField(t *testing.T) {
	tn := composeTenant(t, "historical-enterprise-field", []Seat{seatOf("owner", "enterprise:admin", "build:builder")}, enterprise.New("historical-enterprise-field", platform.OrgSeed{}), build.New("historical-enterprise-field"))
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	decide(t, tn, "owner", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, "old-unit", map[string]any{"name": "Old unit", "stereotype": enterprise.Organization, "from": "2026-01-01"}, at)
	fields := []construction.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "unit", Title: "Unit", Type: "reference", Ref: "enterprise.element", Stereotype: "ActualOrganization"}}
	publishObject(t, tn, "owner", "definition", map[string]any{"name": "assignmenthistory", "title": "Assignment history", "fields": fields}, at)
	typ := "build.assignmenthistory"
	decide(t, tn, "owner", construction.ID, typ+".create", typ, "ROW", map[string]any{"name": "Past assignment", "unit": "old-unit"}, at)
	decide(t, tn, "owner", construction.ID, typ+".edit", typ, "ROW", map[string]any{"unit": ""}, at)
	decide(t, tn, "owner", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "definition", map[string]any{"fields": fields[:1]}, at)
	decide(t, tn, "owner", construction.ID, construction.SchemaPublish, construction.ObjectType, "definition", map[string]any{}, at)
	info, _ := tn.entity(typ)
	if _, present := info.Field("unit"); present {
		t.Fatal("test did not remove the original reference field")
	}
	paths := tn.enterpriseHistoricalReferencePaths(tn.records)
	if !slices.ContainsFunc(paths[typ], func(path []string) bool { return slices.Equal(path, []string{"unit"}) }) {
		t.Fatal("retained Build publication lost the removed reference declaration")
	}
	if got := refuse(t, tn, "owner", enterprise.ID, enterprise.SchemaElementDelete, enterprise.ElementType, "old-unit", map[string]any{"reason": "Remove unused unit"}, at); !strings.HasPrefix(got, "ERROR_CODE_CONFLICT") {
		t.Fatalf("removed definition field erased historical identity protection: %s", got)
	}
}

func TestEnterpriseHistoryUsesRetainedNestedPathsWithoutCurrentGoShape(t *testing.T) {
	tn := builderTenant(t, "historical-lines")
	object := platform.ReleaseAsset{Ref: platform.AssetRef{App: "native", Kind: platform.AssetObject, Name: "native.receipt"}, Body: platform.Raw(map[string]any{"type": "native.receipt", "entity": platform.EntityInfo{Type: "native.receipt", Fields: []platform.FieldInfo{{Name: "lines", Type: "lines", Fields: []platform.FieldInfo{{Name: "allocations", Type: "lines", Fields: []platform.FieldInfo{{Name: "unit", Type: "reference", Ref: "enterprise.element"}}}}}}}})}
	raw := platform.Raw(struct {
		Assets []platform.ReleaseAsset `json:"assets"`
	}{[]platform.ReleaseAsset{object}})
	// A retained release is read as original descriptors; this test deliberately
	// has no current native.receipt Go type to decode the old row into.
	tn.releases.candidates = map[string]json.RawMessage{"retained": raw}
	paths := tn.enterpriseHistoricalReferencePaths(tn.records)
	history := []records.RecordChange{{Fields: []records.FieldChange{{Field: "lines", Before: json.RawMessage(`[{"allocations":[{"unit":"old-unit"}]}]`), After: json.RawMessage(`[]`)}}}}
	if !historyNamesEnterprisePaths(paths["native.receipt"], history, "old-unit") {
		t.Fatal("nested historical enterprise reference was decoded using today's shape")
	}
	if historyNamesEnterprisePaths(paths["native.receipt"], history, "unrelated") {
		t.Fatal("history matched an undeclared or unrelated value")
	}
}

func TestEnterprisePublishedBuildEditPreservesClosedReferenceInRetainedCatalog(t *testing.T) {
	const tenant = "retained-enterprise-edit"
	tn := composeTenant(t, tenant, []Seat{seatOf("owner", "enterprise:admin", "build:builder")}, enterprise.New(tenant, platform.OrgSeed{}), build.New(tenant))
	at := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	for _, id := range []string{"old-unit", "closed-other"} {
		decide(t, tn, "owner", enterprise.ID, enterprise.SchemaElementAdd, enterprise.ElementType, id, map[string]any{"name": id, "stereotype": enterprise.Organization, "from": "2026-01-01"}, at)
	}
	fields := []construction.Field{{Name: "name", Title: "Name", Type: "text"}, {Name: "unit", Title: "Unit", Type: "reference", Ref: "enterprise.element", Stereotype: "ActualOrganization"}, {Name: "active", Title: "Active", Type: "boolean"}}
	publishObject(t, tn, "owner", "definition", map[string]any{"name": "retainedassignment", "title": "Retained assignment", "fields": fields, "enterpriseUse": platform.ExprReference("boolean", "active")}, at)
	member := memberOf(t, tn, "owner")
	preview, err := tn.PreviewRelease(member, platform.AssetObject, "definition")
	if err != nil {
		t.Fatal(err)
	}
	id, err := tn.SaveReleaseCandidate(member, platform.AssetObject, "definition", preview.CandidateID, "save", at)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member, id, "activate", at); err != nil {
		t.Fatal(err)
	}
	typ := "build.retainedassignment"
	decide(t, tn, "owner", construction.ID, typ+".create", typ, "ROW", map[string]any{"name": "Assignment", "unit": "old-unit", "active": true}, at)
	decide(t, tn, "owner", construction.ID, typ+".edit", typ, "ROW", map[string]any{"active": false}, at)
	for _, unit := range []string{"old-unit", "closed-other"} {
		decide(t, tn, "owner", enterprise.ID, enterprise.SchemaElementClose, enterprise.ElementType, unit, map[string]any{"until": "2026-10-20", "reason": "Retired"}, at)
	}
	full := map[string]any{"name": "Renamed assignment", "unit": "old-unit", "active": false}
	decide(t, tn, "owner", construction.ID, typ+".edit", typ, "ROW", full, at)
	candidate, err := tn.releases.candidate(id)
	if err != nil {
		t.Fatal(err)
	}
	draft := tn.newStagedDecision()
	parent := platform.NewCaller(draft, member, construction.ID, false, false)
	execution, err := (hostView{t: tn, decision: draft}).retainedExecution(parent, id, candidate.Assets)
	if err != nil {
		t.Fatal(err)
	}
	caller := execution.Caller(parent, member, construction.ID)
	full["name"] = "Edited through retained declaration"
	if _, refusal := execution.Submit(caller, submission(tn, member, construction.ID, typ+".edit", typ, "ROW", full), at); refusal != nil {
		t.Fatalf("retained generated edit lost its old-reference allowance: %v", refusal)
	}
	if got := refuse(t, tn, "owner", construction.ID, typ+".edit", typ, "ROW", map[string]any{"unit": "closed-other"}, at); !strings.HasPrefix(got, "ERROR_CODE_INVALID_REFERENCE") && !strings.HasPrefix(got, "ERROR_CODE_INVALID_ARGUMENT") {
		t.Fatalf("new closed reference accepted: %s", got)
	}
}
