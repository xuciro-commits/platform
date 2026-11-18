package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
)

func TestInvariantK22ObjectDraftShowsOnlyUnpublishedAuthoringChanges(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k22")
	_, handler := observationHost(live.Tenant)
	get := func(path string, out any) {
		t.Helper()
		response := observationRequest(t, handler, live.ID, observationToken(live.ID, "builder"), path, nil)
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), out) != nil {
			t.Fatalf("%s: %d %s", path, response.Code, response.Body)
		}
	}
	var catalog []platform.Action
	get("/v1/actions", &catalog)
	index := slices.IndexFunc(catalog, func(action platform.Action) bool { return action.Schema == "build.object.edit" })
	if index < 0 {
		t.Fatal("object edit is absent")
	}
	params := map[string]platform.Field{}
	for _, p := range catalog[index].Payload {
		params[p.Name] = p
	}
	for _, p := range []string{"fields", "display", "states", "actionTypes"} {
		if params[p].Name == "" {
			t.Fatalf("missing object edit parameter %s", p)
		}
	}
	for _, child := range []string{"name", "title", "type", "required", "search", "choices", "ref", "group", "aside", "when"} {
		if !slices.ContainsFunc(params["fields"].Fields, func(f platform.Field) bool { return f.Name == child }) {
			t.Fatalf("missing child parameter %s", child)
		}
	}
	const id = "CONTACT"
	live.accept(t, "build.object.create", build.ObjectType, id, invariantObject("contact", "Contact"))
	read := func() []ontology.ObjectChange {
		var view objects.RecordView
		get("/v1/records/build.object/"+id, &view)
		return view.Changes
	}
	if initial := read(); len(initial) != 0 {
		t.Fatalf("new draft: %+v", initial)
	}
	live.accept(t, "build.object.publish", build.ObjectType, id, nil)
	live.accept(t, "build.object.edit", build.ObjectType, id, map[string]any{"title": "Customer contact"})
	changes := read()
	if len(changes) != 1 || changes[0].Path != "title" || changes[0].Change != "changed" || !bytes.Equal(changes[0].To, []byte(`"Customer contact"`)) {
		t.Fatalf("edited title: %+v", changes)
	}
	live.accept(t, "build.object.publish", build.ObjectType, id, nil)
	if published := read(); len(published) != 0 {
		t.Fatalf("published: %+v", published)
	}
	live.accept(t, "build.object.edit", build.ObjectType, id, map[string]any{"fields": []ontology.Field{
		{Name: "note", Title: "Note", Type: "text"}, {Name: "mobile", Title: "Mobile", Type: "text"},
	}})
	changes = read()
	if len(changes) != 1 || changes[0].Path != "fields.mobile" || changes[0].Change != "added" {
		t.Fatalf("added field: %+v", changes)
	}
}

func TestInvariantK23PublishCheckCountsDataAndMatchesTheOriginalDecision(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k23")
	const id, typ = "CONTACT", "build.contact"
	draft := invariantObject("contact", "Contact")
	draft["display"] = "note"
	live.accept(t, "build.object.create", build.ObjectType, id, draft)
	live.accept(t, "build.object.publish", build.ObjectType, id, nil)
	live.accept(t, typ+".create", typ, "CONTACT-1", map[string]any{"note": "One"})
	live.accept(t, typ+".create", typ, "CONTACT-2", map[string]any{})
	live.accept(t, "build.object.edit", build.ObjectType, id, map[string]any{"fields": []ontology.Field{}})
	_, handler := observationHost(live.Tenant)
	preview := func() actions.ActionCheckResult {
		t.Helper()
		before, length := live.recordImage(t), len(live.entries)
		_, result := checkRecordAction(t, handler, live.ID, "builder", "build.object.publish", build.ObjectType, id)
		if len(live.entries) != length || len(differingTypes(before, live.recordImage(t))) != 0 {
			t.Fatal("publish check changed accepted results or stored records")
		}
		if result.Impact == nil || result.Impact.Records != 2 || len(result.Impact.Fields) != 1 {
			t.Fatalf("publish impact: %+v", result.Impact)
		}
		if field := result.Impact.Fields[0]; field.Field != "note" || field.Change != "removed" || field.Records != 1 {
			t.Fatalf("affected field: %+v", field)
		}
		return result
	}
	result := preview()
	request := live.submission("build.object.publish", build.ObjectType, id, map[string]any{})
	request.ExpectedRevision = &result.Impact.Revision
	if _, refusal := live.Submit(live.member(t), request, invariantClock); result.Allowed || refusal == nil || result.Message != refusal.Message {
		t.Fatalf("preview=%+v publish=%+v", result, refusal)
	}
	live.accept(t, "build.object.edit", build.ObjectType, id, map[string]any{"display": ""})
	result = preview()
	request = live.submission("build.object.publish", build.ObjectType, id, map[string]any{})
	request.ExpectedRevision = &result.Impact.Revision
	_, refusal := live.Submit(live.member(t), request, invariantClock)
	if result.Allowed != (refusal == nil) || refusal != nil && result.Message != refusal.Message {
		t.Fatalf("preview=%+v publish=%+v", result, refusal)
	}
}
