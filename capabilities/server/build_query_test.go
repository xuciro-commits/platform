package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestReusableQueryVersionsFreezePagesAndRecover(t *testing.T) {
	t.Run("memory", func(t *testing.T) { reusableQueryVersions(t, nil) })
	t.Run("postgres", func(t *testing.T) {
		url := os.Getenv("PLATFORM_TEST_DATABASE")
		if url == "" {
			t.Skip("PLATFORM_TEST_DATABASE not set")
		}
		journal, err := OpenJournal(context.Background(), url)
		if err != nil {
			t.Fatal(err)
		}
		defer journal.Close()
		reusableQueryVersions(t, journal)
	})
}
func reusableQueryVersions(t *testing.T, journal *Journal) {
	tenant := fmt.Sprintf("reusable-queries-%d", time.Now().UnixNano())
	if journal != nil {
		defer journal.pool.Exec(context.Background(), `delete from journal where tenant=$1`, tenant)
	}
	compose := func() *Tenant {
		tn, err := NewTenant(tenant, NewConsole(tenant,
			Seat{Subjects: []string{"builder"}, Member: platform.Member{ID: "builder", Roles: map[string]string{build.ID: build.Builder}}},
			Seat{Subjects: []string{"reader"}, Member: platform.Member{ID: "reader", Roles: map[string]string{build.ID: build.User}}}), build.New(tenant))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := compose()
	builder, _ := tn.Member("builder")
	reader, _ := tn.Member("reader")
	at := time.Now().UTC()
	key := 0
	var entries []Entry
	fail := false
	tn.AcceptResult = func(e Entry, appendKey, hash string) ([]byte, error) {
		if fail {
			return nil, errors.New("append failed")
		}
		if journal != nil {
			raw, err := journal.AppendAccepted(context.Background(), tenant, e, appendKey, hash)
			if err != nil {
				return nil, err
			}
			entries = append(entries, e)
			return raw, nil
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(member platform.Member, typ, id, verb string, payload any) *kernel.Error {
		key++
		_, err := tn.Submit(member, &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: typ + "." + verb, Version: 1}, Payload: platform.Raw(payload)}, at)
		return err
	}
	must := func(typ, id, verb string, payload any) {
		t.Helper()
		if err := submit(builder, typ, id, verb, payload); err != nil {
			t.Fatal(typ, verb, err)
		}
	}
	must(build.ObjectType, "object", "create", map[string]any{"name": "note", "title": "Note", "access": []build.Access{{Role: build.User, Read: "all", Create: true}}, "fields": []build.Field{{Name: "bucket", Title: "Bucket", Type: "text"}, {Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}})
	must(build.ObjectType, "object", "publish", map[string]any{})
	for _, bucket := range []string{"A", "B"} {
		must("build.note", bucket, "create", map[string]string{"bucket": bucket, "secret": "private"})
	}
	q := platform.NamedQuery{Name: "shared", Title: "Shared query", Description: "Reusable read", Object: "build.note", Domain: json.RawMessage(`[["bucket","=","A"]]`), Sort: []string{"id"}, Limit: 20}
	must(build.QueryType, "query", "create", q)
	if _, ok := tn.namedQuery(build.ID, "shared"); ok {
		t.Fatal("draft installed")
	}
	if err := submit(reader, build.QueryType, "query", "publish", map[string]any{}); err == nil {
		t.Fatal("reader published query")
	}
	fail = true
	if err := submit(builder, build.QueryType, "query", "publish", map[string]any{}); err == nil {
		t.Fatal("failed append accepted")
	}
	fail = false
	if _, ok := tn.namedQuery(build.ID, "shared"); ok {
		t.Fatal("failed append installed")
	}
	// Freeze a draft candidate, then edit its row. Activation must install the saved bytes.
	preview, err := tn.PreviewRelease(builder, platform.AssetQuery, "query")
	if err != nil || preview.Diagnostic != "" {
		t.Fatalf("preview %+v %v", preview, err)
	}
	if _, err = tn.SaveReleaseCandidate(builder, platform.AssetQuery, "query", preview.CandidateID, "save-query", at); err != nil {
		t.Fatal(err)
	}
	must(build.QueryType, "query", "edit", map[string]any{"domain": [][]any{{"bucket", "=", "B"}}})
	if _, err = tn.ActivateRelease(builder, preview.CandidateID, "activate-query", at); err != nil {
		t.Fatal(err)
	}
	if installed, _ := tn.namedQuery(build.ID, "shared"); string(installed.Domain) != string(q.Domain) {
		t.Fatal("mutable draft changed saved query")
	}
	ref := platform.AssetRef{App: build.ID, Kind: platform.AssetQuery, Name: "shared"}
	binding := platform.AssetBinding{Ref: ref, SourceVersion: "1.query-1"}
	for i, name := range []string{"first", "second"} {
		doc := &platform.PageDocument{FormatVersion: 2, UIProfile: platform.PageUIProfile(), Root: "root", Nodes: map[string]platform.PageLayoutNode{"root": {Kind: "rows", Children: []string{"text"}}, "text": {Kind: "widget", Section: "text"}}, Variables: map[string]platform.PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &platform.PageResourceSource{Kind: "plan", Query: "read"}}}, Queries: map[string]platform.PageQuery{"read": {Object: platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: q.Object}, Query: &binding, Limit: 20}}}
		id := fmt.Sprint("page-", i)
		must(build.PageType, id, "create", map[string]any{"name": name, "title": name, "object": q.Object, "sections": []build.Section{{ID: "text", Widget: "text", ConfigVersion: 1, Text: name}}, "document": doc})
		must(build.PageType, id, "publish", map[string]any{})
	}
	// The later publication becomes latest, while both page plans remain at v1.
	must(build.QueryType, "query", "publish", map[string]any{})
	row, problem := tn.RunQuery(reader, build.ID, "shared", "", at)
	if problem != nil || len(row.Records) != 1 || !queryRecordID(row.Records[0], "B") {
		t.Fatalf("latest query %+v %v", row, problem)
	}
	for _, name := range []string{"first", "second"} {
		candidate, err := tn.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: name}})
		if err != nil {
			t.Fatal(err)
		}
		i := slices.IndexFunc(candidate.Assets, func(a platform.ReleaseAsset) bool { return a.Ref == ref })
		if i < 0 || candidate.Assets[i].SourceVersion != "1.query-1" || string(candidate.Assets[i].Body) == "" {
			t.Fatal("page candidate upgraded query")
		}
	}
	owner := tn.app(build.ID).(*build.Build)
	available, err := owner.ReleaseAssets()
	if err != nil {
		t.Fatal(err)
	}
	var third platform.Page
	for _, d := range tn.definitions {
		if d.Page != nil && d.Ref.Name == "first" {
			third = *d.Page
		}
	}
	third.Name = "third"
	doc := *third.Document
	doc.Queries = map[string]platform.PageQuery{}
	for id, q := range third.Document.Queries {
		copy := *q.Query
		copy.SourceVersion = "1.query-2"
		q.Query = &copy
		doc.Queries[id] = q
	}
	third.Document = &doc
	extra, err := platform.PageReleaseAsset(build.ID, "1", third)
	if err != nil {
		t.Fatal(err)
	}
	available = append(available, extra)
	if _, err := tn.candidateWithBindings([]platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "first"}, extra.Ref}, available, nil); err == nil {
		t.Fatal("mixed query versions in one candidate accepted")
	}

	retained, err := owner.QueryReleaseAsset("shared", "1.query-1")
	if err != nil {
		t.Fatal(err)
	}
	var old platform.NamedQuery
	json.Unmarshal(retained.Body, &old)
	if string(old.Domain) != string(q.Domain) {
		t.Fatal("retained query drifted")
	}
	must(build.QueryType, "query", "edit", map[string]any{"domain": [][]any{{"secret", "=", "private"}}})
	must(build.QueryType, "query", "publish", map[string]any{})
	// A member may discover old readable versions without seeing the private latest.
	var visible *platform.Definition
	for _, d := range tn.Definitions(reader) {
		if d.Ref == ref {
			copy := d
			visible = &copy
		}
	}
	if visible == nil || visible.Query != nil || len(visible.QueryVersions) != 2 || visible.QueryVersion("1.query-1") == nil {
		t.Fatal("member version projection leaked or lost readable history")
	}
	if _, problem = tn.RunQuery(reader, build.ID, "shared", "", at); problem == nil {
		t.Fatal("hidden condition bypassed field permission")
	}
	must(build.QueryType, "query", "edit", map[string]any{"title": "Unpublished title"})
	if err := submit(builder, build.QueryType, "query", "archive", map[string]any{}); err == nil {
		t.Fatal("published query archived")
	}
	if journal != nil {
		saved, err := journal.Entries(context.Background(), tenant, 0)
		if err != nil {
			t.Fatal(err)
		}
		CheckReplay(t, tn, saved, compose)
	} else {
		CheckReplay(t, tn, entries, compose)
	}
	raw, _, err := tn.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	restored := compose()
	if err := restored.Restore(raw); err != nil {
		t.Fatal(err)
	}
	for _, current := range []*Tenant{tn, restored} {
		for _, d := range current.definitions {
			if d.Ref == ref {
				if d.Query.Title != "Shared query" || len(d.QueryVersions) != 3 {
					t.Fatal("draft or history changed on restore")
				}
			}
			if d.Page != nil && (d.Ref.Name == "first" || d.Ref.Name == "second") {
				if err := current.checkPageQueries(*d.Page); err != nil {
					t.Fatal("restored page cannot resolve old query", err)
				}
			}
		}
	}
	// A retained workflow cannot silently pick a mutable latest tenant query.
	flow := build.Process{Name: "reuse", Title: "Reuse", Manual: true, Steps: []build.ProcessStep{{Name: "read", Kind: "query", App: build.ID, Query: "shared", Next: "done"}, {Name: "done", Kind: "end"}}}
	if problem := owner.CheckProcess(flow); problem == nil || !strings.Contains(problem.Message, "exact retained tenant query version") {
		t.Fatal("tenant query was offered without a frozen flow binding", problem)
	}

	// New object revisions cannot remove a field read by any retained query.
	if err := submit(builder, build.ObjectType, "object", "edit", map[string]any{"fields": []build.Field{{Name: "secret", Title: "Secret", Type: "text", Read: []string{build.Builder}}}}); err != nil {
		t.Fatal(err)
	}
	if err := submit(builder, build.ObjectType, "object", "publish", map[string]any{}); err == nil {
		t.Fatal("object update broke retained query")
	}
}

func queryRecordID(record any, id string) bool {
	raw, _ := json.Marshal(record)
	var value map[string]any
	_ = json.Unmarshal(raw, &value)
	return value["id"] == id
}
