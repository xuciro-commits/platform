package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	"platformserver/services/work"
)

func TestInvariantK21SavedViewsShareDefinitionsWithoutAccessOrPreferenceChanges(t *testing.T) {
	const tenant, entity = "invariant-saved-views", "invariantproject.opportunity"
	live := newInvariantTenantWithApps(t, tenant, []platform.Service{work.New(tenant, actions.NewAuthority)},
		protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Roles: map[string]string{build.ID: ontology.UserRole}}},
	)
	live.accept(t, "build.object.create", build.ObjectType, "OPP", map[string]any{
		"name": "opportunity", "title": "Opportunity", "access": []ontology.Access{{Role: ontology.UserRole, Read: "own", Create: true}},
		"fields": []ontology.Field{{Name: "title", Type: "text", Title: "Title"}, {Name: "private", Type: "text", Title: "Private", Read: []string{ontology.BuilderRole}}},
	})
	live.accept(t, "build.object.publish", build.ObjectType, "OPP", nil)
	live.accept(t, entity+".create", entity, "HIDDEN", map[string]any{"title": "Hidden"})
	submit := func(principal, authority, typ, id, schema string, body any) *platform.Error {
		t.Helper()
		member, _ := live.Member(principal)
		req := live.submission(schema, typ, id, body)
		req.PrincipalId, req.Authority = principal, authority
		_, refusal := live.Submit(member, req, invariantClock)
		return refusal
	}
	accept := func(principal, authority, typ, id, schema string, body any) {
		t.Helper()
		if refusal := submit(principal, authority, typ, id, schema, body); refusal != nil {
			t.Fatalf("%s %s: %s %s", principal, schema, refusal.Code, refusal.Message)
		}
	}
	accept("other", build.ID, entity, "VISIBLE", entity+".create", map[string]any{"title": "Visible"})
	live.accept(t, entity+".edit", entity, "VISIBLE", map[string]any{"private": "protected field"})
	view := map[string]any{"title": "Opportunities", "entity": entity, "state": `{"version":1,"layout":"table"}`}
	accept("builder", work.ID, work.ViewType, "SHARED", work.SchemaViewSave, view)
	accept("other", work.ID, work.ViewType, "PERSONAL", work.SchemaViewSave, view)
	prefID := "other@" + entity
	accept("other", work.ID, work.ViewPreferenceType, prefID, work.SchemaViewPreference, map[string]any{"view": "PERSONAL", "pinned": true, "default": true})
	_, handler := observationHost(live.Tenant)
	get := func(role, path string) ([]byte, int) {
		t.Helper()
		response := observationRequest(t, handler, tenant, observationToken(tenant, role), path, nil)
		return response.Body.Bytes(), response.Code
	}
	for _, scope := range []string{"self", "project", "tenant", "self"} {
		body := map[string]any{"scope": scope}
		if scope == "project" {
			body["project"] = invariantProject
		}
		accept("builder", work.ID, work.ViewType, "SHARED", work.SchemaViewShare, body)
		record, status := get("other", "/v1/records/work.view/SHARED")
		list, listStatus := get("other", "/v1/records/work.view")
		visible := scope == "tenant"
		if (status == http.StatusOK) != visible || listStatus != http.StatusOK || bytes.Contains(list, []byte("SHARED")) != visible {
			t.Fatalf("%s single=%d %s list=%d %s", scope, status, record, listStatus, list)
		}
		if !visible {
			continue
		}
		if !bytes.Contains(record, []byte(`"share":"tenant"`)) || !bytes.Contains(record, []byte(`"state":`)) {
			t.Fatalf("shared definition: %s", record)
		}
		rows, status := get("other", "/v1/records/"+entity)
		if status != http.StatusOK || !bytes.Contains(rows, []byte("VISIBLE")) || bytes.Contains(rows, []byte("HIDDEN")) || bytes.Contains(rows, []byte("protected field")) {
			t.Fatalf("opening shared view widened data access: %d %s", status, rows)
		}
		if refusal := submit("other", work.ID, work.ViewType, "SHARED", work.SchemaViewRemove, map[string]any{}); refusal == nil {
			t.Fatal("shared viewer removed the owner's definition")
		}
		accept("other", work.ID, work.ViewPreferenceType, prefID, work.SchemaViewPreference, map[string]any{"view": "SHARED", "pinned": true})
		data, status := get("other", "/v1/records/work.viewpreference/"+prefID)
		var answer struct{ Record protocol.SavedViewPreference }
		if status != http.StatusOK || json.Unmarshal(data, &answer) != nil || answer.Record.Default != "PERSONAL" || len(answer.Record.Pinned) != 2 {
			t.Fatalf("opening or pinning a shared view changed the personal default: %d %s", status, data)
		}
		if _, status := get("builder", "/v1/records/work.viewpreference/"+prefID); status == http.StatusOK {
			t.Fatal("preference was shared with the view's owner")
		}
	}
}
