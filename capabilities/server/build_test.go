package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/build"
	"platformserver/platform"
)

// ADR-0034: someone defines an object in the tenant, publishes it, and people
// work with its records on the machinery that is already there — generated
// actions, the record store's scope and search, aggregates, the journal. The
// object then gains a field and its records keep what they had.
func TestTenantDefinedObject(t *testing.T) {
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	var journal []Entry
	compose := func() *Tenant {
		seat := func(id, role string) Seat {
			return Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: map[string]string{build.ID: role}}}
		}
		tn, err := NewTenant("t-1", NewConsole("t-1", seat("dana", build.Builder), seat("eli", build.User)), build.New("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		journal = append(journal, e)
		return e.Body, nil
	}
	member := func(id string) platform.Member {
		m, _ := tn.app(PlatformApp).(*Console).Member(id)
		return m
	}
	keys := 0
	do := func(who, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: build.ID, IdempotencyKey: fmt.Sprint("b", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			if err.Message != "" {
				return err.Error() + ": " + err.Message
			}
			return err.Error()
		}
		return "ok"
	}
	fields := []map[string]any{
		{"name": "guest", "title": "Guest", "type": "text", "required": true, "search": true},
		{"name": "visited", "title": "Visited on", "type": "date"},
		{"name": "spend", "title": "Spend", "type": "money"},
		{"name": "kind", "title": "Kind", "type": "choice", "choices": "tour, tasting"},
	}
	// A definition the host could not install is refused while it is a draft, with the reason.
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1",
		map[string]any{"name": "Visit", "title": "Visit", "fields": fields}); got != "ERROR_CODE_INVALID_ARGUMENT: the name \"Visit\" is not lower-case letters and digits" {
		t.Fatalf("a name that is not a name: %s", got)
	}
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1", map[string]any{"name": "visit", "title": "Visit",
		"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "colour"}}}); got != "ERROR_CODE_INVALID_ARGUMENT: the field \"guest\" has no type \"colour\"" {
		t.Fatalf("a field type the platform has not: %s", got)
	}
	// The draft, then published.
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "O-1",
		map[string]any{"name": "visit", "title": "Visit", "plural": "Visits", "description": "A guest's visit.", "fields": fields}); got != "ok" {
		t.Fatalf("draft: %s", got)
	}
	if _, err := tn.Records(member("eli"), build.TypeOf("visit"), platform.Query{}, now); err == nil {
		t.Error("a draft was already installed")
	}
	if got := do("eli", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got == "ok" {
		t.Error("someone without the builder role published an object")
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish: %s", got)
	}

	// It is now an ordinary type: declared, in the member's entities and catalog.
	visit := build.TypeOf("visit")
	if !slices.ContainsFunc(tn.Entities(member("eli")), func(info platform.EntityInfo) bool { return info.Type == visit }) {
		t.Fatalf("the published object is not among eli's entities: %+v", tn.Entities(member("eli")))
	}
	if !slices.ContainsFunc(tn.Catalog(member("eli")), func(a platform.Action) bool { return a.Schema == visit+".create" }) {
		t.Error("eli cannot create records of the published object")
	}
	// A user keeps records of it, through the generated actions.
	if got := do("eli", visit+".create", visit, "V-1",
		map[string]any{"guest": "Ada Lovelace", "visited": "2026-10-02", "kind": "tour", "spend": map[string]any{"amount": 12000, "currency": "EUR"}}); got != "ok" {
		t.Fatalf("a record of the defined object: %s", got)
	}
	if got := do("eli", visit+".create", visit, "V-2", map[string]any{"guest": "Alan Turing", "kind": "coffee"}); got == "ok" {
		t.Error("a value outside the declared choices was stored")
	}
	if got := do("eli", visit+".create", visit, "V-2", map[string]any{"guest": "Alan Turing", "kind": "tasting"}); got != "ok" {
		t.Fatalf("second record: %s", got)
	}
	page, err := tn.Records(member("eli"), visit, platform.Query{Search: "Ada", Sort: []string{"id"}}, now)
	if err != nil || len(page.Records) != 1 {
		t.Fatalf("search over the defined object: %+v, %v", page, err)
	}
	reader := member("eli")
	if found := tn.Search(&reader, "Turing", now); len(found) != 1 || found[0].Type != visit {
		t.Errorf("the tenant's own object is not in search: %+v", found)
	}
	// Its page is an asset like a code page's (ADR-0032).
	defs := tn.Definitions(member("eli"))
	if !slices.ContainsFunc(defs, func(d platform.Definition) bool {
		return d.Ref.Kind == platform.AssetPage && d.Ref.Name == "visit" && d.Page != nil && d.Page.Object.Name == visit && d.Source == "tenant"
	}) {
		t.Errorf("the defined object has no page in the registry: %+v", defs)
	}

	// Published again with another field: the records keep what they had.
	grown := append(slices.Clone(fields), map[string]any{"name": "note", "title": "Note", "type": "longtext"})
	if got := do("dana", build.ObjectType+".edit", build.ObjectType, "O-1", map[string]any{"fields": grown}); got != "ok" {
		t.Fatalf("edit: %s", got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "O-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish again: %s", got)
	}
	if got := do("eli", visit+".edit", visit, "V-1", map[string]any{"note": "Asked about the tasting."}); got != "ok" {
		t.Fatalf("the new field: %s", got)
	}
	view, err := tn.RecordOf(member("eli"), visit, "V-1", now)
	if err != nil {
		t.Fatalf("record: %v", err)
	}
	kept, _ := json.Marshal(view.Record)
	for _, want := range []string{`"guest":"Ada Lovelace"`, `"visited":"2026-10-02"`, `"note":"Asked about the tasting."`, `"amount":12000`} {
		if !strings.Contains(string(kept), want) {
			t.Errorf("after the object grew, the record lost %s: %s", want, kept)
		}
	}
	// A page someone composes over an object: the same descriptor a code page
	// has, checked against what is installed, with the reason when it is wrong.
	bad := func(payload map[string]any, want string) {
		t.Helper()
		if got := do("dana", build.PageType+".create", build.PageType, "P-1", payload); got != want {
			t.Errorf("page refused as %q, want %q", got, want)
		}
	}
	bad(map[string]any{"name": "visits", "title": "Visits", "object": "stock.item", "list": []string{"guest"}, "detail": []string{"guest"}},
		"ERROR_CODE_INVALID_ARGUMENT: this tenant has no object \"stock.item\"")
	if got := do("dana", build.PageType+".create", build.PageType, "P-1",
		map[string]any{"name": "visits", "title": "Visits", "description": "Every visit.", "object": visit,
			"list": []string{"guest", "kind"}, "detail": []string{"guest", "visited", "spend", "kind", "note"}, "actions": []string{visit + ".edit"}}); got != "ok" {
		t.Fatalf("page draft: %s", got)
	}
	if slices.ContainsFunc(tn.Definitions(member("eli")), func(d platform.Definition) bool {
		return d.Ref.Kind == platform.AssetPage && d.Ref.Name == "visits"
	}) {
		t.Error("a page was offered before it was published")
	}
	if got := do("dana", build.SchemaRelease, build.PageType, "P-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish the page: %s", got)
	}
	composedPage := func(who, name string) *platform.Page {
		for _, d := range tn.Definitions(member(who)) {
			if d.Ref.Kind == platform.AssetPage && d.Ref.Name == name {
				return d.Page
			}
		}
		return nil
	}
	composed := composedPage("eli", "visits")
	if composed == nil || composed.Object.Name != visit || len(composed.ListFields) != 2 || len(composed.Actions) != 1 {
		t.Fatalf("the composed page: %+v", composed)
	}
	// Composed again with a field that is not there: refused, and the page people
	// open is still the one that worked.
	if got := do("dana", build.PageType+".edit", build.PageType, "P-1", map[string]any{"list": []string{"guest", "nothing"}}); got != "ok" {
		t.Fatalf("edit the page: %s", got)
	}
	if got := do("dana", build.SchemaRelease, build.PageType, "P-1", map[string]any{}); !strings.Contains(got, "has no field \"nothing\"") {
		t.Errorf("a page over a field that is not there: %s", got)
	}
	if again := composedPage("eli", "visits"); again == nil || len(again.ListFields) != 2 {
		t.Errorf("a refused composition changed the page people open: %+v", again)
	}
	// A page laid out from widgets (ADR-0035): what the host takes, and what it
	// refuses, with the reason.
	sections := []map[string]any{
		{"widget": "table", "width": "full", "title": "Visits", "fields": []string{"guest", "kind"}},
		{"widget": "detail", "width": "half", "fields": []string{"guest", "visited", "note"}},
		{"widget": "actions", "width": "half", "actions": []string{visit + ".edit"}},
		{"widget": "metric", "width": "half", "title": "How many", "measure": "count"},
		{"widget": "chart", "width": "half", "title": "By kind", "group": "kind", "measure": "count"},
		{"widget": "text", "width": "full", "text": "Ask the guest before you keep anything."},
		// 16b: a filter the table, chart and metric read; a form that makes a
		// visit; the selected record's history; what waits on it.
		{"widget": "filter", "width": "full", "fields": []string{"kind"}},
		{"widget": "form", "width": "half", "title": "New visit", "fields": []string{"guest", "kind"}},
		{"widget": "timeline", "width": "half"},
		{"widget": "tasks", "width": "half"},
	}
	if got := do("dana", build.PageType+".edit", build.PageType, "P-1", map[string]any{"sections": sections}); got != "ok" {
		t.Fatalf("lay out the page: %s", got)
	}
	if got := do("dana", build.SchemaRelease, build.PageType, "P-1", map[string]any{}); got != "ok" {
		t.Fatalf("publish the composed page: %s", got)
	}
	laid := composedPage("eli", "visits")
	if laid == nil || laid.Layout != "composed" || len(laid.Sections) != 10 || laid.Sections[0].Widget != "table" ||
		len(laid.Sections[2].Actions) != 1 || laid.Sections[2].Actions[0].Name != visit+".edit" {
		t.Fatalf("the composed page: %+v", laid)
	}
	for _, x := range []struct {
		why     string
		section map[string]any
		want    string
	}{
		{"a widget the platform has not", map[string]any{"widget": "map"}, `no widget "map"`},
		{"a field the object has not", map[string]any{"widget": "table", "fields": []string{"nothing"}}, `has no field nothing`},
		{"an action about something else", map[string]any{"widget": "actions", "actions": []string{"stock.item.edit"}}, "no action stock.item.edit"},
		{"a measure that is not one", map[string]any{"widget": "metric", "measure": "median:qty"}, "is not count"},
		{"a chart with nothing to group by", map[string]any{"widget": "chart", "measure": "count"}, "nothing to group by"},
		{"text with no words", map[string]any{"widget": "text"}, "no words to show"},
		{"a filter over nothing", map[string]any{"widget": "filter", "fields": []string{}}, "no fields to filter by"},
		{"a filter over words people type", map[string]any{"widget": "filter", "fields": []string{"guest"}}, "guest is a text field"},
		{"a form that leaves out what a visit needs", map[string]any{"widget": "form", "fields": []string{"kind"}}, "needs guest"},
	} {
		if got := do("dana", build.PageType+".edit", build.PageType, "P-1", map[string]any{"sections": []map[string]any{x.section}}); got != "ok" {
			t.Fatalf("%s: edit: %s", x.why, got)
		}
		if got := do("dana", build.SchemaRelease, build.PageType, "P-1", map[string]any{}); !strings.Contains(got, x.want) {
			t.Errorf("%s: %s, want %q", x.why, got, x.want)
		}
	}
	if still := composedPage("eli", "visits"); still == nil || len(still.Sections) != 10 {
		t.Errorf("a refused layout changed the page people open: %+v", still)
	}
	// Shared membership must not expose an unrelated private object.
	if got := do("dana", build.ObjectType+".create", build.ObjectType, "PRIVATE", map[string]any{"name": "privateasset", "title": "Private asset", "fields": []build.Field{{Name: "name", Title: "Name", Type: "text"}}, "access": []build.Access{{Role: build.User, Read: "none"}}}); got != "ok" {
		t.Fatal(got)
	}
	if got := do("dana", build.SchemaPublish, build.ObjectType, "PRIVATE", map[string]any{}); got != "ok" {
		t.Fatal(got)
	}
	shared := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: visit}
	private := platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "build.privateasset"}
	// An application handed to the people it was built for (ADR-0036): a name,
	// an icon and the pages it holds, offered to whoever may open one of them.
	if got := do("dana", build.AppType+".create", build.AppType, "A-1",
		map[string]any{"name": "frontdesk", "title": "Front desk", "icon": "clipboard", "pages": []string{"visits"}, "resources": []platform.AssetRef{shared, private}}); got != "ok" {
		t.Fatalf("an application: %s", got)
	}
	if got := do("dana", build.SchemaHandOver, build.AppType, "A-1", map[string]any{}); got != "ok" {
		t.Fatalf("hand it over: %s", got)
	}
	handed := func(who string) *platform.Application {
		for _, d := range tn.Definitions(member(who)) {
			if d.Ref.Kind == platform.AssetApp && d.Ref.Name == "frontdesk" {
				return d.Application
			}
		}
		return nil
	}
	if app := handed("eli"); app == nil || app.Title != "Front desk" || app.Icon != "clipboard" || len(app.Pages) != 1 {
		t.Fatalf("the application eli was handed: %+v", app)
	}
	if app := handed("eli"); !slices.Equal(app.Resources, []platform.AssetRef{shared}) {
		t.Fatalf("private membership leaked: %+v", app)
	}
	if app := handed("dana"); len(app.Resources) != 2 {
		t.Fatalf("builder membership lost: %+v", app)
	}
	if got := do("dana", build.AppType+".create", build.AppType, "A-2", map[string]any{"name": "otherdesk", "title": "Other desk", "pages": []string{"visits"}, "resources": []platform.AssetRef{shared}}); got != "ok" {
		t.Fatal(got)
	}
	if got := do("dana", build.SchemaHandOver, build.AppType, "A-2", map[string]any{}); got != "ok" {
		t.Fatalf("shared resource ownership changed: %s", got)
	}
	// It grants nothing: someone who may not open its pages is not handed it.
	if app := handed("boss"); app != nil {
		t.Errorf("an application was handed to someone who may open none of its pages: %+v", app)
	}
	// Its navigation has headings (17b): a page under one, as the builder chose.
	daily := []map[string]any{{"title": "Daily", "pages": []string{"visits"}}}
	if got := do("dana", build.AppType+".edit", build.AppType, "A-1", map[string]any{"groups": daily}); got != "ok" {
		t.Fatalf("groups: %s", got)
	}
	if got := do("dana", build.SchemaHandOver, build.AppType, "A-1", map[string]any{}); got != "ok" {
		t.Fatalf("hand it over with groups: %s", got)
	}
	if app := handed("eli"); app == nil || len(app.Groups) != 1 || app.Groups[0].Title != "Daily" || !slices.Equal(app.Groups[0].Pages, []string{"visits"}) {
		t.Fatalf("the groups eli was handed: %+v", app)
	}
	for _, x := range []struct {
		why    string
		fields map[string]any
		want   string
	}{
		{"a group over a page it does not hold", map[string]any{"groups": []map[string]any{{"title": "Other", "pages": []string{"elsewhere"}}}}, "does not hold"},
		{"a page under two headings", map[string]any{"groups": []map[string]any{{"title": "A", "pages": []string{"visits"}}, {"title": "B", "pages": []string{"visits"}}}}, "under \"A\" and \"B\""},
		{"a heading over nothing", map[string]any{"groups": []map[string]any{{"title": "Empty", "pages": []string{}}}}, "holds no page"},
		{"a page that is not there", map[string]any{"groups": []map[string]any{}, "pages": []string{"nothing"}}, "no page nothing"},
		{"no page at all", map[string]any{"pages": []string{}}, "holds at least one page"},
		{"missing resource", map[string]any{"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetCompute, Name: "missing"}}}, "no published resource"},
		{"duplicate resource", map[string]any{"resources": []platform.AssetRef{shared, shared}}, "declared twice"},
		{"page duplicated as a resource", map[string]any{"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "visits"}}}, "non-navigation"},
		{"malformed icon name", map[string]any{"icon": "../rocket"}, "not an icon name"},
	} {
		if got := do("dana", build.AppType+".edit", build.AppType, "A-1", merge(map[string]any{"pages": []string{"visits"}, "groups": daily, "resources": []platform.AssetRef{}}, x.fields)); got != "ok" {
			t.Fatalf("%s: edit: %s", x.why, got)
		}
		if got := do("dana", build.SchemaHandOver, build.AppType, "A-1", map[string]any{}); !strings.Contains(got, x.want) {
			t.Errorf("%s: %s, want %q", x.why, got, x.want)
		}
	}
	if still := handed("eli"); still == nil || len(still.Pages) != 1 {
		t.Errorf("a refused hand-over changed the application people have: %+v", still)
	}
	// The kit owns the growing vocabulary; optional icons and new shaped names
	// may publish, while malformed names are refused at publication above.
	for _, icon := range []string{"rocket", ""} {
		if got := do("dana", build.AppType+".edit", build.AppType, "A-1", map[string]any{"icon": icon, "pages": []string{"visits"}, "groups": daily, "resources": []platform.AssetRef{}}); got != "ok" {
			t.Fatalf("icon %q draft: %s", icon, got)
		}
		if got := do("dana", build.SchemaHandOver, build.AppType, "A-1", map[string]any{}); got != "ok" {
			t.Fatalf("icon %q publication: %s", icon, got)
		}
	}
	if !slices.ContainsFunc(journal, func(e Entry) bool {
		return e.Kind == "accepted-result" && e.App == build.ID
	}) {
		t.Fatal("tenant-defined records did not use the accepted-result boundary")
	}
	CheckReplay(t, tn, journal, compose)
}
