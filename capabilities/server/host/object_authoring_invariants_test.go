package host

import (
	"bytes"
	"encoding/json"
	"net/http"
	"slices"
	"testing"

	"platformserver/kernel/protocol"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
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
	delegated := protocol.Seat{Subjects: []string{"other"}, Member: platform.Member{ID: "other", Grants: []platform.Grant{{App: build.ID, Role: build.Builder, By: "project:p1"}}}}
	live := newInvariantTenant(t, "invariant-k23", delegated)
	const id, typ = "CONTACT", "invariantproject.contact"
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
	if outsider := postActionCheck(t, handler, live.ID, "other", "build.object.publish", build.ObjectType, id); outsider.Allowed || outsider.Impact != nil || outsider.Message == "" {
		t.Fatalf("a builder delegated by another project saw the publish impact: %+v", outsider)
	}
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

func TestInvariantK25ProjectsOwnTheirTypeNames(t *testing.T) {
	live := newInvariantTenant(t, "invariant-k25")
	live.accept(t, protocol.AppType+".create", protocol.AppType, "SECOND", map[string]any{"name": "secondproject", "title": "Second project"})
	second := invariantObject("lead", "Lead")
	second["project"] = "SECOND"
	live.accept(t, "build.object.create", build.ObjectType, "LEAD1", invariantObject("lead", "Lead"))
	live.accept(t, "build.object.create", build.ObjectType, "LEAD2", second)
	squatter := invariantObject("lead", "Lead")
	squatter["namespace"] = "secondproject"
	if _, refusal := live.Submit(live.member(t), live.submission("build.object.create", build.ObjectType, "SQUAT", squatter), invariantClock); refusal == nil {
		t.Fatal("a project created a type in another project's namespace")
	}
	moved := invariantObject("lead", "Lead")
	moved["namespace"] = "secondproject"
	if _, refusal := live.Submit(live.member(t), live.submission("build.object.edit", build.ObjectType, "LEAD1", moved), invariantClock); refusal == nil {
		t.Fatal("an object type moved to another project's namespace")
	}
	for _, id := range []string{"LEAD1", "LEAD2"} {
		live.accept(t, "build.object.publish", build.ObjectType, id, map[string]any{})
	}
	live.accept(t, "build.object.create", build.ObjectType, "LEAD3", invariantObject("lead", "Lead again"))
	if _, refusal := live.Submit(live.member(t), live.submission("build.object.publish", build.ObjectType, "LEAD3", map[string]any{}), invariantClock); refusal == nil {
		t.Fatal("a project published a type name it already has")
	}
	live.accept(t, "invariantproject.lead.create", "invariantproject.lead", "L1", map[string]any{"note": "first"})
	live.accept(t, "secondproject.lead.create", "secondproject.lead", "L1", map[string]any{"note": "second"})
	want := live.recordImage(t)
	for _, typ := range []string{"invariantproject.lead", "secondproject.lead"} {
		if len(want[typ]) == 0 {
			t.Fatalf("%s has no rows", typ)
		}
	}
	if bytes.Equal(want["invariantproject.lead"], want["secondproject.lead"]) {
		t.Fatal("two projects' lead records share one store")
	}
	if differing := differingTypes(want, live.restore(t, live.entries).recordImage(t)); len(differing) != 0 {
		t.Fatalf("replay differs in %v", differing)
	}
}

func TestInvariantK26ProjectRolesActOnlyInTheirProject(t *testing.T) {
	seats := []protocol.Seat{
		{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: build.Builder}}},
		{Subjects: []string{"rep"}, Member: platform.Member{ID: "rep", Roles: map[string]string{"invariantproject": "rep"}}},
	}
	live := newInvariantTenant(t, "invariant-k26", seats...)
	live.accept(t, protocol.AppType+".create", protocol.AppType, "SECOND", map[string]any{"name": "secondproject", "title": "Second project"})
	for _, project := range []string{"", "SECOND"} {
		lead := invariantObject("lead", "Lead")
		lead["access"] = []ontology.Access{{Role: "rep", Read: "own", Create: true, Edit: true}}
		if project != "" {
			lead["project"] = project
		}
		live.accept(t, "build.object.create", build.ObjectType, "LEAD"+project, lead)
		live.accept(t, "build.object.publish", build.ObjectType, "LEAD"+project, map[string]any{})
	}
	as := func(actor, schema, typ, id string, payload any) *platform.Error {
		t.Helper()
		member, _ := live.Member(actor)
		request := live.submission(schema, typ, id, payload)
		request.PrincipalId = actor
		if typ == controlowner.MemberType {
			request.Authority = controlowner.PlatformApp
		}
		_, refusal := live.Submit(member, request, invariantClock)
		return refusal
	}
	if refusal := as("rep", "invariantproject.lead.create", "invariantproject.lead", "MINE", map[string]any{"note": "mine"}); refusal != nil {
		t.Fatalf("a project role could not create in its project: %v", refusal)
	}
	live.accept(t, "invariantproject.lead.create", "invariantproject.lead", "THEIRS", map[string]any{"note": "theirs"})
	rep, _ := live.Member("rep")
	if _, refusal := live.RecordOf(t.Context(), rep, "invariantproject.lead", "MINE", invariantClock); refusal != nil {
		t.Fatalf("the creator could not read an own-scoped row: %v", refusal)
	}
	if _, refusal := live.RecordOf(t.Context(), rep, "invariantproject.lead", "THEIRS", invariantClock); refusal == nil {
		t.Fatal("an own-scoped role read another member's row")
	}
	if as("rep", "secondproject.lead.create", "secondproject.lead", "ELSEWHERE", map[string]any{"note": "elsewhere"}) == nil {
		t.Fatal("a role held in one project acted in another project with the same role name")
	}
	if as("admin", controlowner.SchemaGrant, controlowner.MemberType, "rep", map[string]any{"app": "secondproject", "role": "nobody"}) == nil {
		t.Fatal("a grant named a role the project does not declare")
	}
	if refusal := as("admin", controlowner.SchemaGrant, controlowner.MemberType, "rep", map[string]any{"app": "secondproject", "role": "rep"}); refusal != nil {
		t.Fatalf("a project role could not be granted: %v", refusal)
	}
	if refusal := as("rep", "secondproject.lead.create", "secondproject.lead", "ELSEWHERE", map[string]any{"note": "elsewhere"}); refusal != nil {
		t.Fatalf("a granted project role could not create: %v", refusal)
	}
	if differing := differingTypes(live.recordImage(t), live.restore(t, live.entries, seats...).recordImage(t)); len(differing) != 0 {
		t.Fatalf("replay differs in %v", differing)
	}
}
