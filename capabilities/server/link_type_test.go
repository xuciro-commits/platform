package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestLinkTypeVersionsFreezeTraverseAndRecover(t *testing.T) {
	const tenant = "link-type-state"
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant, Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}}, Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}, Seat{Subjects: []string{"restricted"}, Member: platform.Member{ID: "restricted", Roles: map[string]string{build.ID: "guest"}}}), build.New(tenant))
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
	key := 0
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append failed")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(m platform.Member, typ, id, verb string, payload any) *kernel.Error {
		key++
		_, err := tn.Submit(m, &pb.Submission{TenantId: tenant, PrincipalId: m.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		return err
	}
	must := func(typ, id, verb string, payload any) {
		t.Helper()
		if err := submit(builder, typ, id, verb, payload); err != nil {
			t.Fatal(typ, verb, err.Message)
		}
	}
	for _, name := range []string{"parent", "child"} {
		fields := []build.Field{{Name: "note", Title: "Note", Type: "text"}}
		if name == "child" {
			fields = append(fields, build.Field{Name: "parent", Title: "Parent", Type: "reference", Ref: "build.parent", Required: true}, build.Field{Name: "secretparent", Title: "Secret parent", Type: "reference", Ref: "build.parent", Read: []string{build.Builder}})
		}
		must(build.ObjectType, name, "create", map[string]any{"name": name, "title": name, "fields": fields, "access": []build.Access{{Role: build.User, Read: "all", Create: true, Edit: true, Archive: true}, {Role: "guest", Read: map[bool]string{true: "own", false: "all"}[name == "parent"]}}})
		must(build.ObjectType, name, "publish", map[string]any{})
	}
	for _, id := range []string{"A", "B"} {
		must("build.parent", id, "create", map[string]any{"note": id})
		for n := 0; n < 2; n++ {
			must("build.child", id+fmt.Sprint(n), "create", map[string]any{"note": id, "parent": id, "secretparent": id})
		}
	}
	link := map[string]any{"name": "children", "title": "Children", "description": "Reference-backed child records", "parent": "build.parent", "child": "build.child", "via": "parent", "forward": "children", "reverse": "parent"}
	must(build.LinkTypeType, "link", "create", link)
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetLinkType, Name: "children"}
	binding := platform.AssetBinding{Ref: ref, SourceVersion: "1.link-1"}
	if _, err := tn.TraverseLink(reader, binding, "forward", "A", platform.Query{}, at); err == nil {
		t.Fatal("draft was traversable")
	}
	if err := submit(reader, build.LinkTypeType, "link", "publish", map[string]any{}); err == nil {
		t.Fatal("reader published link")
	}
	fail = true
	if err := submit(builder, build.LinkTypeType, "link", "publish", map[string]any{}); err == nil {
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
	must(build.LinkTypeType, "link", "edit", map[string]any{"title": "Later draft title", "forward": "newchildren"})
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
	must(build.LinkTypeType, "link", "publish", map[string]any{})
	for _, d := range tn.Definitions(reader) {
		if d.Ref == ref {
			if d.LinkType.Forward != "newchildren" || d.LinkVersion("1.link-1").LinkType.Forward != "children" {
				t.Fatal("later publication changed prior bytes")
			}
		}
	}
	must(build.LinkTypeType, "private", "create", map[string]any{"name": "privatechildren", "title": "Private children", "description": "Hidden reference", "parent": "build.parent", "child": "build.child", "via": "secretparent", "forward": "privatechildren", "reverse": "privateparent"})
	must(build.LinkTypeType, "private", "publish", map[string]any{})
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
	must(build.ObjectType, "child", "edit", map[string]any{"fields": []build.Field{{Name: "note", Title: "Note", Type: "text"}}})
	if problem := submit(builder, build.ObjectType, "child", "publish", map[string]any{}); problem == nil {
		t.Fatal("object publication removed retained link reference")
	}
	if problem := submit(builder, build.LinkTypeType, "link", "archive", map[string]any{}); problem == nil {
		t.Fatal("published link was archived")
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
