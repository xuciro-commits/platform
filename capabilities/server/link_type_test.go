package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/internal/records"
	"platformserver/platform"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJointDraftsDeliverNewObjectsAndRelationship(t *testing.T) {
	tn := builderTenant(t, "joint-relationship")
	member := memberOf(t, tn, "builder")
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "PARENT", map[string]any{
		"name": "jointparent", "title": "Joint parent", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}},
	}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".create", construction.ObjectType, "CHILD", map[string]any{
		"name": "jointchild", "title": "Joint child", "fields": []construction.Field{
			{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.jointparent", Required: true},
			{Name: "note", Title: "Note", Type: "text"},
		},
	}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.LinkTypeType+".create", construction.LinkTypeType, "LINK", map[string]any{
		"name": "jointchildren", "title": "Joint children", "description": "Jointly delivered child references",
		"parent": "build.jointparent", "child": "build.jointchild", "via": "parent", "forward": "children", "reverse": "parent",
		"cardinality": "one-to-one", "deletePolicy": "restrict-active",
	}, txnAt)
	selection := []construction.JointDraftRef{
		{Kind: platform.AssetObject, ID: "PARENT"},
		{Kind: platform.AssetObject, ID: "CHILD"},
		{Kind: platform.AssetLinkType, ID: "LINK"},
	}
	preview, err := tn.PreviewReleaseDrafts(member, selection)
	if err != nil || preview.Diagnostic != "" || preview.CandidateID == "" {
		t.Fatalf("new objects and relationship could not be reviewed together: %+v %v", preview, err)
	}
	for _, definition := range tn.Definitions(member) {
		if definition.Ref.Name == "build.jointparent" || definition.Ref.Name == "build.jointchild" || definition.Ref.Name == "jointchildren" {
			t.Fatal("preview installed a draft")
		}
	}
	if alone, err := tn.PreviewRelease(member, platform.AssetLinkType, "LINK"); err == nil && alone.Diagnostic == "" {
		t.Fatal("relationship could be delivered without its unpublished objects")
	}
	decide(t, tn, "builder", construction.ID, construction.LinkTypeType+".edit", construction.LinkTypeType, "LINK", map[string]any{"via": "note"}, txnAt)
	if invalid, err := tn.PreviewReleaseDrafts(member, selection); err == nil && invalid.Diagnostic == "" {
		t.Fatal("joint candidate accepted a text field as its relationship reference")
	}
	decide(t, tn, "builder", construction.ID, construction.LinkTypeType+".edit", construction.LinkTypeType, "LINK", map[string]any{"via": "parent"}, txnAt)
	saved, err := tn.SaveReleaseCandidates(member, selection, preview.CandidateID, "save-joint-relationship", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tn.ActivateRelease(member, saved, "activate-joint-relationship", txnAt); err != nil {
		t.Fatal(err)
	}
	decide(t, tn, "user", construction.ID, "build.jointparent.create", "build.jointparent", "P", map[string]any{"note": "Parent"}, txnAt)
	decide(t, tn, "user", construction.ID, "build.jointchild.create", "build.jointchild", "C", map[string]any{"parent": "P"}, txnAt)
	if got := refuse(t, tn, "user", construction.ID, "build.jointchild.create", "build.jointchild", "DUPLICATE", map[string]any{"parent": "P"}, txnAt); got == "ok" {
		t.Fatal("joint publication lost the relationship's one-to-one constraint")
	}
	if got := refuse(t, tn, "user", construction.ID, "build.jointparent.archive", "build.jointparent", "P", map[string]any{}, txnAt); got == "ok" {
		t.Fatal("joint publication lost the relationship's active-child protection")
	}
}

func TestLinkTypeVersionsFreezeTraverseAndRecover(t *testing.T) {
	t.Run("incremental constraints and isolated reverse indexes", func(t *testing.T) {
		tn := builderTenant(t, "local-reference-index")
		parent := publishObject(t, tn, "builder", "parent", map[string]any{"name": "parent", "title": "Parent", "fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}}, txnAt)
		child := publishObject(t, tn, "builder", "child", map[string]any{"name": "child", "title": "Child", "fields": []construction.Field{{Name: "parent", Title: "Parent", Type: "reference", Ref: parent}}}, txnAt)
		decide(t, tn, "builder", construction.ID, construction.LinkTypeType+".create", construction.LinkTypeType, "link", map[string]any{
			"name": "children", "title": "Children", "description": "Constrained references", "parent": parent, "child": child, "via": "parent", "forward": "children", "reverse": "parent", "cardinality": "one-to-one", "deletePolicy": "restrict-active",
		}, txnAt)
		decide(t, tn, "builder", construction.ID, construction.SchemaLinkType, construction.LinkTypeType, "link", map[string]any{}, txnAt)
		for _, id := range []string{"P1", "P2", "P3", "P4"} {
			decide(t, tn, "user", construction.ID, parent+".create", parent, id, map[string]any{"note": id}, txnAt)
		}
		for i, id := range []string{"C1", "C2"} {
			decide(t, tn, "user", construction.ID, child+".create", child, id, map[string]any{"parent": fmt.Sprintf("P%d", i+1)}, txnAt)
		}
		et := tn.records.types[child]
		field, _ := et.info.Field("parent")
		initial := et.referenceIndex(field)
		decide(t, tn, "user", construction.ID, parent+".edit", parent, "P4", map[string]any{"note": "Unrelated update"}, txnAt)
		et = tn.records.types[child]
		if et.referenceIndex(field) != initial {
			t.Fatal("unrelated write invalidated the reference index")
		}
		draft := tn.records.forkRecords()
		private := draft.types[child]
		changed := *private.rows.Get("C2")
		changed.value = copyOf(private.info.Go, changed.value.Interface())
		changed.value.FieldByIndex(field.Index).SetString("P1")
		private.setRow("C2", &changed)
		draft.writes[child+"/C2"] = true
		if private.referenceIndex(field).Get("P1").IDs.Len() != 2 || et.referenceIndex(field).Get("P1").IDs.Len() != 1 {
			t.Fatal("private reverse-index updates leaked into live records")
		}
		if err := tn.records.promoteRecords(draft); err == nil || et.referenceIndex(field) != initial {
			t.Fatal("final image admitted conflicting references or changed its predecessor")
		}
		bulk := tn.records.forkRecords()
		image, err := records.From([]records.Entry[*row]{{Key: "C1", Value: et.rows.Get("C1")}, {Key: "C2", Value: &changed}})
		if err != nil {
			t.Fatal(err)
		}
		bulk.types[child].setRows(image)
		if err := tn.records.promoteRecords(bulk); err == nil {
			t.Fatal("bulk schema image skipped complete relation admission")
		}
		if got := refuse(t, tn, "user", construction.ID, child+".edit", child, "C2", map[string]any{"parent": "P1"}, txnAt); got == "ok" {
			t.Fatal("incremental uniqueness accepted a duplicate")
		}
		if got := refuse(t, tn, "user", construction.ID, parent+".archive", parent, "P2", map[string]any{}, txnAt); got == "ok" {
			t.Fatal("reverse index missed an active child")
		}
		decide(t, tn, "user", construction.ID, child+".archive", child, "C2", map[string]any{}, txnAt)
		decide(t, tn, "user", construction.ID, parent+".archive", parent, "P2", map[string]any{}, txnAt)
		if got := refuse(t, tn, "user", construction.ID, child+".create", child, "C3", map[string]any{"parent": "P2"}, txnAt); got == "ok" {
			t.Fatal("archiving changed the original uniqueness semantics")
		}
		decide(t, tn, "user", construction.ID, parent+".archive", parent, "P3", map[string]any{}, txnAt)
		if got := refuse(t, tn, "user", construction.ID, child+".create", child, "C3", map[string]any{"parent": "P3"}, txnAt); got == "ok" {
			t.Fatal("an active child was attached to an archived parent")
		}
		decide(t, tn, "user", construction.ID, child+".edit", child, "C1", map[string]any{"parent": "P4"}, txnAt)
		et = tn.records.types[child]
		if et.referenceIndex(field).Get("P1").IDs.Len() != 0 || et.referenceIndex(field).Get("P4").IDs.Len() != 1 {
			t.Fatal("changing a reference left the old key behind")
		}
		decide(t, tn, "user", construction.ID, child+".edit", child, "C1", map[string]any{"parent": ""}, txnAt)
		page, refusal := tn.Records(memberOf(t, tn, "user"), child, platform.Query{Domain: platform.Raw([]any{[]any{"parent", "=", ""}})}, txnAt)
		if refusal != nil || page.Total != 1 {
			t.Fatalf("empty reference filtering: total=%d %v", page.Total, refusal)
		}
		// Bulk replacement invalidates only this type's derived indexes.
		before := tn.records.types[child]
		if err := tn.records.install(before.info); err != nil {
			t.Fatal(err)
		}
		if next := tn.records.types[child]; next == before || next.referenceIndexes != nil || !reflect.DeepEqual(next.rows.Get("C1").value.Interface(), before.rows.Get("C1").value.Interface()) {
			t.Fatal("schema replacement retained a stale index or changed record facts")
		}
	})
	const tenant = "link-type-state"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{construction.ID: construction.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{construction.ID: construction.User}}}, Seat{Subjects: []string{"restricted"}, Member: platform.Member{ID: "restricted", Roles: map[string]string{construction.ID: "guest"}}}), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Now().UTC()
	var entries []Entry
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(m platform.Member, typ, id, verb string, payload any) *kernel.Error {
		_, err := tn.Submit(m, submission(tn, m, construction.ID, typ+"."+verb, typ, id, payload), at)
		return err
	}
	must := func(typ, id, verb string, payload any) {
		t.Helper()
		if err := submit(builder, typ, id, verb, payload); err != nil {
			t.Fatal(typ, verb, err.Message)
		}
	}
	for _, name := range []string{"parent", "child"} {
		fields := []construction.Field{{Name: "note", Title: "Note", Type: "text"}}
		if name == "child" {
			fields = append(fields, construction.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.parent", Required: true}, construction.Field{Name: "secretparent", Title: "Secret parent", Type: "reference", Ref: "build.parent", Read: []string{construction.Builder}})
		}
		must(construction.ObjectType, name, "create", map[string]any{"name": name, "title": name, "fields": fields, "access": []construction.Access{{Role: construction.User, Read: "all", Create: true, Edit: true, Archive: true}, {Role: "guest", Read: map[bool]string{true: "own", false: "all"}[name == "parent"]}}})
		must(construction.ObjectType, name, "publish", map[string]any{})
	}
	for _, id := range []string{"A", "B"} {
		must("build.parent", id, "create", map[string]any{"note": id})
		for n := 0; n < 2; n++ {
			must("build.child", id+fmt.Sprint(n), "create", map[string]any{"note": id, "parent": id, "secretparent": id})
		}
	}
	link := map[string]any{"name": "children", "title": "Children", "description": "Reference-backed child records", "parent": "build.parent", "child": "build.child", "via": "parent", "forward": "children", "reverse": "parent"}
	must(construction.LinkTypeType, "link", "create", link)
	ref := platform.AssetRef{App: construction.ID, Kind: platform.AssetLinkType, Name: "children"}
	binding := platform.AssetBinding{Ref: ref, SourceVersion: "1.link-1"}
	if _, err := tn.TraverseLink(reader, binding, "forward", "A", platform.Query{}, at); err == nil {
		t.Fatal("draft was traversable")
	}
	if err := submit(reader, construction.LinkTypeType, "link", "publish", map[string]any{}); err == nil {
		t.Fatal("reader published link")
	}
	fail = true
	if err := submit(builder, construction.LinkTypeType, "link", "publish", map[string]any{}); err == nil {
		t.Fatal("failed append accepted")
	}
	fail = false
	if _, err := tn.TraverseLink(reader, binding, "forward", "A", platform.Query{}, at); err == nil {
		t.Fatal("failed append installed link")
	}
	preview, err := tn.PreviewRelease(builder, platform.AssetLinkType, "link")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetLinkType, "link", preview.CandidateID, "save", at); err != nil {
		t.Fatal(err)
	}
	must(construction.LinkTypeType, "link", "edit", map[string]any{"title": "Later draft title", "forward": "newchildren"})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate", at); err != nil {
		t.Fatal(err)
	}
	assert := func(current *Tenant) {
		t.Helper()
		defs := current.Definitions(reader)
		found := false
		for _, d := range defs {
			if d.Ref == ref {
				found = true
				if d.LinkType.Title != "Children" || d.LinkType.Forward != "children" || !d.LinkType.Required || d.LinkType.DeletePolicy != "owner" {
					t.Fatal("candidate did not retain original relationship")
				}
			}
		}
		if !found {
			t.Fatal("link was not discoverable")
		}
		out, problem := current.TraverseLink(reader, binding, "forward", "A", platform.Query{Limit: 1}, at)
		if problem != nil || out.Total != 2 || len(out.Records) != 1 {
			t.Fatalf("forward %+v %v", out, problem)
		}
		out, problem = current.TraverseLink(reader, binding, "reverse", "B0", platform.Query{}, at)
		if problem != nil || out.Total != 1 || !queryRecordID(out.Records[0], "B") {
			t.Fatalf("reverse %+v %v", out, problem)
		}
		for _, id := range []string{"", "missing"} {
			if _, problem := current.TraverseLink(reader, binding, "forward", id, platform.Query{}, at); problem == nil {
				t.Fatal("missing start broadened to all children")
			}
		}
		out, problem = current.TraverseLink(reader, binding, "forward", "A", platform.Query{Domain: platform.Raw([]any{"|", []any{"note", "=", "A"}, []any{"note", "=", "B"}})}, at)
		if problem != nil || out.Total != 2 {
			t.Fatal("caller domain escaped parent boundary")
		}
	}
	restricted, _ := tn.Member("restricted")
	for _, direction := range []string{"forward", "reverse"} {
		id := "A"
		if direction == "reverse" {
			id = "A0"
		}
		if _, err := tn.TraverseLink(restricted, binding, direction, id, platform.Query{}, at); err == nil {
			t.Fatal("hidden start or parent traversed")
		}
	}
	edge := &platform.LinkTraversal{Binding: binding, Direction: "forward", ID: "A"}
	rows, issue := tn.Records(reader, "build.child", platform.Query{Traversal: edge, Limit: 1}, at)
	if issue != nil || rows.Total != 2 || len(rows.Records) != 1 {
		t.Fatal("typed record traversal did not use its original window")
	}
	stats, issue := tn.Aggregate(reader, "build.child", AggregateQuery{Traversal: edge, Measures: []string{"count"}}, at)
	if issue != nil || len(stats.Rows) != 1 || stats.Rows[0]["count"] != 2 {
		t.Fatal("aggregate traversal lost complete membership")
	}
	if _, issue := tn.Aggregate(restricted, "build.child", AggregateQuery{Traversal: edge, Measures: []string{"count"}}, at); issue == nil {
		t.Fatal("aggregate traversed an unreadable start")
	}
	assert(tn)
	CheckReplay(t, tn, entries, compose)
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err = restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	assert(restored)
	must(construction.LinkTypeType, "link", "publish", map[string]any{})
	for _, d := range tn.Definitions(reader) {
		if d.Ref == ref {
			if d.LinkType.Forward != "newchildren" || d.LinkVersion("1.link-1").LinkType.Forward != "children" {
				t.Fatal("later publication changed prior bytes")
			}
		}
	}
	must(construction.LinkTypeType, "private", "create", map[string]any{"name": "privatechildren", "title": "Private children", "description": "Hidden reference", "parent": "build.parent", "child": "build.child", "via": "secretparent", "forward": "privatechildren", "reverse": "privateparent"})
	must(construction.LinkTypeType, "private", "publish", map[string]any{})
	for _, d := range tn.Definitions(reader) {
		if d.LinkType != nil && d.Ref.Name == "privatechildren" {
			t.Fatal("hidden field relation discovered")
		}
	}
	denied := binding
	denied.Ref.Name = "privatechildren"
	if _, problem := tn.TraverseLink(reader, denied, "forward", "A", platform.Query{}, at); problem == nil {
		t.Fatal("hidden field relation traversed")
	}
	must(construction.ObjectType, "child", "edit", map[string]any{"fields": []construction.Field{{Name: "note", Title: "Note", Type: "text"}}})
	if problem := submit(builder, construction.ObjectType, "child", "publish", map[string]any{}); problem == nil {
		t.Fatal("object publication removed retained link reference")
	}
	if problem := submit(builder, construction.LinkTypeType, "link", "archive", map[string]any{}); problem == nil {
		t.Fatal("published link was archived")
	}
	document := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"table", "text"}}, "table": {Kind: "widget", Section: "table"}, "text": {Kind: "widget", Section: "text"}}, Variables: map[string]platform.PageVariable{"parent": {Scope: "page", Type: "record", Mode: "resource", Source: &platform.PageResourceSource{Kind: "record", Section: "table"}}, "children": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "related"}}}, Queries: map[string]platform.PageQuery{"related": {Object: platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: "build.child"}, Query: &binding, Direction: "forward", For: &platform.PageValue{Variable: "parent"}, Limit: 1}}}
	must(construction.PageType, "linkpage", "create", map[string]any{"name": "linked", "title": "Linked page", "object": "build.parent", "sections": []build.Section{{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"note"}}, {ID: "text", Widget: "text", ConfigVersion: 1, Text: "Linked read"}}, "document": document})
	must(construction.PageType, "linkpage", "publish", map[string]any{})
	candidate, problem := tn.ReleaseCandidate([]platform.AssetRef{{App: construction.ID, Kind: platform.AssetPage, Name: "linked"}})
	if problem != nil {
		t.Fatal(problem)
	}
	foundBinding := false
	for _, asset := range candidate.Assets {
		if asset.Ref == ref {
			foundBinding = true
			if asset.SourceVersion != "1.link-1" {
				t.Fatal("page changed its retained relation")
			}
		}
	}
	if !foundBinding {
		t.Fatal("page omitted its relationship dependency")
	}
	document.Queries["private"] = platform.PageQuery{Object: document.Queries["related"].Object, Query: &denied, Direction: "forward", For: &platform.PageValue{Variable: "parent"}, Limit: 1}
	document.Variables["private"] = platform.PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "private"}}
	must(construction.PageType, "linkpage", "edit", map[string]any{"document": document})
	must(construction.PageType, "linkpage", "publish", map[string]any{})
	for _, d := range tn.Definitions(reader) {
		if d.Page != nil && d.Ref.Name == "linked" {
			if _, ok := d.Page.Document.Queries["private"]; ok {
				t.Fatal("page disclosed hidden relation source")
			}
			if d.Page.Document.Variables["private"].Mode != "" {
				t.Fatal("hidden relationship output survived")
			}
		}
	}
	CheckReplay(t, tn, entries, compose)
	h := NewHost(Tokens(map[string]string{"reader": "reader"}), tn)
	r := httptest.NewRequest("GET", "/v1/link-types/build/children/1.link-1/forward/A?limit=1", nil)
	r.Header.Set("Authorization", "Bearer reader")
	w := httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"total":2`) {
		t.Fatalf("traversal HTTP %d %s", w.Code, w.Body.String())
	}
	for _, body := range []string{`{"unknown":true}`, `null`, `{} {}`} {
		r := httptest.NewRequest("POST", "/v1/link-types/build/children/1.link-1/forward/A", strings.NewReader(body))
		r.Header.Set("Authorization", "Bearer reader")
		w := httptest.NewRecorder()
		h.Handler().ServeHTTP(w, r)
		if w.Code == 200 {
			t.Fatal("invalid traversal query accepted")
		}
	}
	r = httptest.NewRequest("POST", "/v1/link-types/build/children/1.link-1/forward/A", strings.NewReader(`{"limit":1}`))
	r.Header.Set("Authorization", "Bearer reader")
	w = httptest.NewRecorder()
	h.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("typed traversal HTTP %d", w.Code)
	}
	var page RecordPage
	if json.Unmarshal(w.Body.Bytes(), &page) != nil || len(page.Records) != 1 {
		t.Fatal("invalid typed traversal answer")
	}
}
