package host

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	workapp "platformserver/services/work"
)

func TestInvariantWorkViewStateVersions(t *testing.T) {
	id := "invariant-work-view-state"
	live := newInvariantTenantWithApps(t, id, []platform.App{workapp.New(id)})
	member := live.member(t)
	if _, ok := live.app(workapp.ID).(*workapp.Work); !ok {
		t.Fatal("the work owner is not attached")
	}
	save := func(viewID, title, state string) *kernel.Error {
		t.Helper()
		_, refusal := live.Submit(member, &pb.Submission{
			TenantId: live.ID, PrincipalId: member.ID, Authority: workapp.ID,
			IdempotencyKey: fmt.Sprintf("work-view-%s-%d", viewID, invariantKeys.Add(1)),
			Target:         &pb.EntityRef{Type: workapp.ViewType, Id: viewID},
			Schema:         &pb.SchemaRef{Name: workapp.SchemaViewSave, Version: 1},
			Payload:        platform.Raw(map[string]string{"title": title, "entity": "build.alpha", "state": state}),
		}, invariantClock)
		return refusal
	}
	views := func() ([]workapp.SavedView, *kernel.Error) {
		t.Helper()
		value, refusal := live.Read(t.Context(), member, "views")
		if refusal != nil {
			return nil, refusal
		}
		return value.([]workapp.SavedView), nil
	}
	find := func(items []workapp.SavedView, viewID string) workapp.SavedView {
		t.Helper()
		for _, item := range items {
			if item.ID == viewID {
				return item
			}
		}
		t.Fatalf("saved view %s is absent", viewID)
		return workapp.SavedView{}
	}

	before := live.liveVersion("/v1/views")
	if refusal := save("VIEW0", "Unversioned exploration", `{"kind":"exploration","entity":"build.alpha","set":{"object":"build.alpha"}}`); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatalf("unversioned save: refusal=%v; want INVALID_ARGUMENT", refusal)
	}
	if refusal := save("VIEW1", "Versioned list", `{"version":1,"kind":"list","search":"kept"}`); refusal != nil {
		t.Fatalf("version 1 state was refused: %s: %s", refusal.Code, refusal.Message)
	}
	if refusal := save("VIEW2", "Future list", `{"version":2,"kind":"list"}`); refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatalf("unknown version save: refusal=%v; want INVALID_ARGUMENT", refusal)
	}

	if after := live.liveVersion("/v1/views"); after == before {
		t.Fatal("a saved view did not invalidate its live read")
	}
	items, refusal := views()
	if refusal != nil {
		t.Fatalf("read the saved view: %s: %s", refusal.Code, refusal.Message)
	}
	if got := find(items, "VIEW1").State; got != `{"version":1,"kind":"list","search":"kept"}` {
		t.Fatalf("the saved state was rewritten: %q", got)
	}
}

func TestInvariantBuildQueryPublishesSharedObjectSet(t *testing.T) {
	live := newInvariantTenant(t, "invariant-build-query-contract")
	member := live.member(t)
	objectType := "build.orderedset"
	live.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("orderedset", "Ordered set"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJ1", map[string]any{})
	live.accept(t, objectType+".create", objectType, "SET1", map[string]any{"note": "keep"})
	live.accept(t, objectType+".create", objectType, "SET2", map[string]any{"note": "drop"})

	filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{
		platform.ExprReference("text", "note"), platform.ExprLiteralValue("text", "keep"),
	}}
	domain := []any{[]any{"note", "in", []any{"keep", "drop"}}}
	live.accept(t, "build.query.create", build.QueryType, "QUERY1", map[string]any{
		"name": "kept", "title": "Kept records", "description": "A shared object set with its full query contract",
		"object": objectType, "domain": domain, "filter": filter, "sort": []string{"note"}, "limit": 50,
	})
	live.accept(t, "build.query.publish", build.QueryType, "QUERY1", map[string]any{})

	var definition platform.Definition
	for _, item := range live.Definitions(member) {
		if item.Ref == (platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "kept"}) {
			definition = item
		}
	}
	if definition.Query == nil || definition.ContractVersion != platform.ObjectSetContractVersion {
		t.Fatalf("published query contract: %+v", definition)
	}
	published, exists := definition.QueryVersions[definition.Version]
	if !exists {
		t.Fatalf("published version %s is not retained", definition.Version)
	}
	if published.Object != objectType || published.Filter == nil || published.Sort == nil || published.Limit != 50 || published.ContractVersion() != platform.ObjectSetContractVersion {
		t.Fatalf("published query lost its object set contract: %+v", published)
	}
	var publishedDomain []any
	if err := json.Unmarshal(published.Domain, &publishedDomain); err != nil || !reflect.DeepEqual(publishedDomain, domain) {
		t.Fatalf("published query domain: %s", published.Domain)
	}

	page, refusal := live.queryWindowRead(member, build.ID, "kept", "", definition.Version, platform.Query{Limit: 50}, invariantClock)
	if refusal != nil {
		t.Fatalf("run the published query: %s: %s", refusal.Code, refusal.Message)
	}
	records, _ := json.Marshal(page.Records)
	if page.Total != 1 || !strings.Contains(string(records), `"id":"SET1"`) {
		t.Fatalf("published query read %d records: %s", page.Total, records)
	}
}
