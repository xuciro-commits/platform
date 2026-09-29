package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/platform"
)

func TestJournalAcceptedBuilderPublicationRecovery(t *testing.T) {
	url := os.Getenv("PLATFORM_TEST_DATABASE")
	if url == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("publication-%d", time.Now().UnixNano())
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}}
		tenant, err := NewTenant(id, NewConsole(id, seat), build.New(id))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	live := compose()
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		return journal.AppendAccepted(ctx, id, e, key, hash)
	}
	member, _ := live.Member("dana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	for _, action := range []struct {
		key, schema, typ, target, payload string
	}{{"create", build.ObjectType + ".create", build.ObjectType, "O1",
		`{"name":"visit","title":"Visit","fields":[{"name":"guest","title":"Guest","type":"text"}]}`},
		{"publish", build.SchemaPublish, build.ObjectType, "O1", `{}`},
		{"page-create", build.PageType + ".create", build.PageType, "P1",
			`{"name":"visits","title":"Visits","object":"build.visit","list":["guest"],"detail":["guest"]}`},
		{"page-publish", build.SchemaRelease, build.PageType, "P1", `{}`},
		{"app-create", build.AppType + ".create", build.AppType, "A1",
			`{"name":"frontdesk","title":"Front Desk","pages":["visits"]}`},
		{"app-publish", build.SchemaHandOver, build.AppType, "A1", `{}`}} {
		if _, err := live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID,
			Authority: build.ID, IdempotencyKey: action.key, Target: &pb.EntityRef{Type: action.typ, Id: action.target},
			Schema: &pb.SchemaRef{Name: action.schema, Version: 1}, Payload: []byte(action.payload)}, at); err != nil {
			t.Fatal(err)
		}
	}
	function := platform.RecordAdviceFunction("build.visit", []string{"guest"}, []string{build.Builder, build.User})
	function.Name = "advice"
	for i, action := range []struct {
		schema  string
		payload any
	}{
		{build.FunctionType + ".create", function},
		{build.SchemaFunction, struct{}{}},
		{build.FunctionType + ".edit", map[string]string{"instructions": "Second published prompt"}},
		{build.SchemaFunction, struct{}{}},
	} {
		if _, refusal := live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprint("function-", i), Target: &pb.EntityRef{Type: build.FunctionType, Id: "F1"},
			Schema: &pb.SchemaRef{Name: action.schema, Version: 1}, Payload: platform.Raw(action.payload)}, at); refusal != nil {
			t.Fatal(refusal)
		}
	}
	if journal.Position(id) != 10 {
		t.Fatalf("ten generated decisions did not occupy ten journal positions: %d", journal.Position(id))
	}
	if object, ok := platform.Get[build.Object](live.caller(member, live.app(build.ID), false), "O1"); !ok ||
		object.State != "published" || object.Published == "" {
		t.Fatalf("committed PostgreSQL publication was not applied in memory: %+v, found=%t", object, ok)
	}
	reopened, err := OpenJournal(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	entries, err := reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 10 {
		t.Fatalf("publication was not recovered from PostgreSQL: %d entries, %v", len(entries), err)
	}
	recovered := compose()
	if err := recovered.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if _, installed := recovered.entity(build.TypeOf("visit")); !installed ||
		!slices.ContainsFunc(recovered.definitions, func(d platform.Definition) bool {
			return d.Ref.Kind == platform.AssetPage && d.Ref.Name == "visits"
		}) ||
		!slices.ContainsFunc(recovered.definitions, func(d platform.Definition) bool {
			return d.Ref.Kind == platform.AssetApp && d.Ref.Name == "frontdesk"
		}) {
		t.Fatal("persisted publications did not restore the generated object, page and application")
	}
	functions := recovered.app(build.ID).(*build.Build)
	if f, version, ok := functions.FunctionDefinition("advice", 1); !ok || version != 1 || f.Instructions != function.Instructions {
		t.Fatal("PostgreSQL recovery lost the first function version")
	}
	if f, version, ok := functions.FunctionDefinition("advice", 0); !ok || version != 2 || f.Instructions != "Second published prompt" {
		t.Fatal("PostgreSQL recovery did not install the latest function version")
	}
	CheckReplay(t, live, entries, compose)
	// A function publication uses the same append-before-install boundary as
	// objects/pages/apps. A committed third version survives a pre-apply crash.
	if _, refusal := live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: build.ID,
		IdempotencyKey: "function-third-draft", Target: &pb.EntityRef{Type: build.FunctionType, Id: "F1"},
		Schema: &pb.SchemaRef{Name: build.FunctionType + ".edit", Version: 1}, Payload: platform.Raw(map[string]string{"instructions": "Third published prompt"})}, at); refusal != nil {
		t.Fatal(refusal)
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, e, key, hash); err != nil {
			t.Fatal(err)
		}
		panic("crash after function publication append")
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("publication crash was not injected")
			}
		}()
		live.Submit(member, &pb.Submission{TenantId: id, PrincipalId: member.ID, Authority: build.ID,
			IdempotencyKey: "function-third-publish", Target: &pb.EntityRef{Type: build.FunctionType, Id: "F1"},
			Schema: &pb.SchemaRef{Name: build.SchemaFunction, Version: 1}, Payload: []byte(`{}`)}, at)
	}()
	if _, version, _ := live.app(build.ID).(*build.Build).FunctionDefinition("advice", 0); version != 2 {
		t.Fatal("publication became installed before committed application")
	}
	entries, err = reopened.Entries(ctx, id, 0)
	if err != nil || len(entries) != 12 {
		t.Fatalf("third publication was not durable: %d %v", len(entries), err)
	}
	restarted := compose()
	if err := restarted.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if f, version, ok := restarted.app(build.ID).(*build.Build).FunctionDefinition("advice", 0); !ok || version != 3 || f.Instructions != "Third published prompt" {
		t.Fatal("post-append publication did not recover")
	}
	CheckReplay(t, restarted, entries, compose)
}

func TestAcceptedBuilderPublicationRejectsBrokenDependentBeforeCommit(t *testing.T) {
	const tenantID = "published-dependent"
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}}
		tenant, err := NewTenant(tenantID, NewConsole(tenantID, seat), build.New(tenantID))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	live := compose()
	member, _ := live.Member("dana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	var entries []Entry
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(key, schema, typ, id string, payload any) *kernel.Error {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		_, refused := live.Submit(member, &pb.Submission{TenantId: tenantID, PrincipalId: member.ID,
			Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, at)
		return refused
	}
	for _, step := range []struct {
		key, schema, typ, id string
		payload              any
	}{
		{"object", build.ObjectType + ".create", build.ObjectType, "O1", map[string]any{
			"name": "visit", "title": "Visit", "fields": []map[string]any{
				{"name": "guest", "title": "Guest", "type": "text"},
				{"name": "room", "title": "Room", "type": "text"},
			}}},
		{"install", build.SchemaPublish, build.ObjectType, "O1", map[string]any{}},
		{"page", build.PageType + ".create", build.PageType, "P1", map[string]any{
			"name": "visitdesk", "title": "Visit desk", "object": "build.visit",
			"list": []string{"guest"}, "detail": []string{"room"}}},
		{"page-install", build.SchemaRelease, build.PageType, "P1", map[string]any{}},
		{"edit", build.ObjectType + ".edit", build.ObjectType, "O1", map[string]any{
			"fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}}},
	} {
		if refusal := submit(step.key, step.schema, step.typ, step.id, step.payload); refusal != nil {
			t.Fatalf("%s: %v", step.key, refusal)
		}
	}
	root := []platform.AssetRef{{App: build.ID, Kind: platform.AssetPage, Name: "visitdesk"}}
	prior, err := live.ReleaseCandidate(root)
	if err != nil {
		t.Fatal(err)
	}
	before := len(entries)
	if refusal := submit("bad-install", build.SchemaPublish, build.ObjectType, "O1", map[string]any{}); refusal == nil ||
		refusal.Code != pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
		t.Fatalf("accepted result did not refuse incompatible object installation: %v", refusal)
	}
	// A saved refusal may consume a journal row; it must never install the
	// invalid object or change the prior closed page.
	after, err := live.ReleaseCandidate(root)
	if err != nil || after.ID != prior.ID {
		t.Fatalf("refused result changed the published page: %v / %v", after, err)
	}
	if len(entries) < before || len(entries) > before+1 {
		t.Fatalf("unexpected journal growth after refusal: %d -> %d", before, len(entries))
	}
	CheckReplay(t, live, entries, compose)
}

func TestAcceptedBuilderPublicationIsInvisibleUntilCommitAndRestores(t *testing.T) {
	compose := func() *Tenant {
		seat := Seat{Subjects: []string{"dana"}, Member: platform.Member{ID: "dana",
			Roles: map[string]string{build.ID: build.Builder}}}
		tenant, err := NewTenant("publish-tenant", NewConsole("publish-tenant", seat), build.New("publish-tenant"))
		if err != nil {
			t.Fatal(err)
		}
		return tenant
	}
	live := compose()
	member, _ := live.Member("dana")
	at := time.Date(2026, 9, 28, 14, 0, 0, 0, time.UTC)
	var entries []Entry
	live.Record = func(e Entry) { entries = append(entries, e) }
	fail := false
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("injected append failure")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	submit := func(key, schema, typ, id string, payload any) error {
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		_, refused := live.Submit(member, &pb.Submission{TenantId: live.ID, PrincipalId: member.ID,
			Authority: build.ID, IdempotencyKey: key, Target: &pb.EntityRef{Type: typ, Id: id},
			Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, at)
		if refused != nil {
			return fmt.Errorf("%s: %s", refused.Code, refused.Message)
		}
		return nil
	}
	must := func(key, schema, typ, id string, payload any) {
		t.Helper()
		if err := submit(key, schema, typ, id, payload); err != nil {
			t.Fatal(err)
		}
	}
	has := func(kind platform.AssetKind, name string) bool {
		return slices.ContainsFunc(live.definitions, func(d platform.Definition) bool {
			return d.Ref.App == build.ID && d.Ref.Kind == kind && d.Ref.Name == name
		})
	}
	must("o-create", build.ObjectType+".create", build.ObjectType, "O1", map[string]any{
		"name": "visit", "title": "Visit", "fields": []map[string]any{{"name": "guest", "title": "Guest", "type": "text"}}})
	fail = true
	if err := submit("o-publish", build.SchemaPublish, build.ObjectType, "O1", map[string]any{}); err == nil {
		t.Fatal("object publication did not fail before the journal append")
	}
	if _, installed := live.entity(build.TypeOf("visit")); installed ||
		has(platform.AssetObject, build.TypeOf("visit")) ||
		slices.ContainsFunc(live.app(build.ID).Declarations(), func(d *pb.AuthorityDeclaration) bool {
			return d.GetDataClass() == build.TypeOf("visit")
		}) {
		t.Fatal("failed object publication installed a definition or authority")
	}
	if object, ok := platform.Get[build.Object](live.caller(member, live.app(build.ID), false), "O1"); !ok ||
		object.State != "draft" || object.Published != "" {
		t.Fatal("failed object publication changed the durable draft record")
	}
	fail = false
	must("o-publish", build.SchemaPublish, build.ObjectType, "O1", map[string]any{})
	if _, installed := live.entity(build.TypeOf("visit")); !installed ||
		!has(platform.AssetObject, build.TypeOf("visit")) {
		t.Fatal("committed object was not installed")
	}
	must("p-create", build.PageType+".create", build.PageType, "P1", map[string]any{
		"name": "visits", "title": "Visits", "object": build.TypeOf("visit"),
		"list": []string{"guest"}, "detail": []string{"guest"}})
	fail = true
	if err := submit("p-publish", build.SchemaRelease, build.PageType, "P1", map[string]any{}); err == nil {
		t.Fatal("page publication did not fail before append")
	}
	if has(platform.AssetPage, "visits") {
		t.Fatal("failed page publication became visible")
	}
	if page, ok := platform.Get[build.Page](live.caller(member, live.app(build.ID), false), "P1"); !ok ||
		page.State != "draft" || page.Published != "" {
		t.Fatal("failed page publication changed the draft record")
	}
	fail = false
	must("p-publish", build.SchemaRelease, build.PageType, "P1", map[string]any{})
	if !has(platform.AssetPage, "visits") {
		t.Fatal("committed page was not installed")
	}
	must("a-create", build.AppType+".create", build.AppType, "A1", map[string]any{
		"name": "frontdesk", "title": "Front Desk", "pages": []string{"visits"}})
	fail = true
	if err := submit("a-publish", build.SchemaHandOver, build.AppType, "A1", map[string]any{}); err == nil {
		t.Fatal("application publication did not fail before append")
	}
	if has(platform.AssetApp, "frontdesk") {
		t.Fatal("failed application publication became visible")
	}
	if app, ok := platform.Get[build.Application](live.caller(member, live.app(build.ID), false), "A1"); !ok ||
		app.State != "draft" || app.Published != "" {
		t.Fatal("failed application publication changed the draft record")
	}
	fail = false
	must("a-publish", build.SchemaHandOver, build.AppType, "A1", map[string]any{})
	if !has(platform.AssetApp, "frontdesk") {
		t.Fatal("committed application was not installed")
	}
	releaseID := func(tenant *Tenant) string {
		t.Helper()
		candidate, err := tenant.ReleaseCandidate([]platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "frontdesk"}})
		if err != nil {
			t.Fatal(err)
		}
		if len(candidate.Assets) != 3 { // application, page, object; the page names no action
			t.Fatalf("published builder closure has %d assets, want 3", len(candidate.Assets))
		}
		return candidate.ID
	}
	publishedID := releaseID(live)
	var publications int
	for _, e := range entries {
		if e.Kind == "accepted-result" {
			result, receipt, err := decodeAcceptedResult(e.Body)
			if err != nil {
				t.Fatal(err)
			}
			if slices.Contains([]string{build.SchemaPublish, build.SchemaRelease, build.SchemaHandOver},
				receipt.GetSubmission().GetSchema().GetName()) {
				if result.Version != 3 {
					t.Fatalf("publication used result format %d", result.Version)
				}
				publications++
			}
		}
	}
	if publications != 3 {
		t.Fatalf("expected three durable publications, found %d", publications)
	}
	CheckReplay(t, live, entries, compose)

	// A crash after the append but before applying any in-memory definition
	// must reconstruct all three kinds directly from the committed rows.
	afterCrash := compose()
	if err := afterCrash.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if recoveredID := releaseID(afterCrash); recoveredID != publishedID {
		t.Fatalf("saved publication changed identity after code-free recovery: %s -> %s", publishedID, recoveredID)
	}
	if _, ok := afterCrash.entity(build.TypeOf("visit")); !ok ||
		!slices.ContainsFunc(afterCrash.definitions, func(d platform.Definition) bool {
			return d.Ref.Kind == platform.AssetApp && d.Ref.Name == "frontdesk"
		}) {
		t.Fatal("committed publication was lost after a restart")
	}

	// A well-formed but incompatible publication must stop this tenant, not
	// allow a partly installed catalog to continue serving requests.
	damaged := slices.Clone(entries)
	for i, e := range damaged {
		if e.Kind != "accepted-result" {
			continue
		}
		result, receipt, err := decodeAcceptedResult(e.Body)
		if err != nil {
			t.Fatal(err)
		}
		if receipt.GetSubmission().GetSchema().GetName() != build.SchemaPublish {
			continue
		}
		result.Row.Value = json.RawMessage(strings.Replace(string(result.Row.Value), `"installed":"build.visit"`,
			`"installed":"build.other"`, 1))
		result.Digest, err = digestAcceptedResult(result)
		if err != nil {
			t.Fatal(err)
		}
		damaged[i].Body, _ = json.Marshal(result)
		break
	}
	isolated := compose()
	if err := isolated.recoverEntries(damaged); err == nil || !isolated.quarantined() {
		t.Fatal("invalid durable publication did not quarantine the tenant")
	}
	if object, ok := platform.Get[build.Object](isolated.caller(member, isolated.app(build.ID), false), "O1"); !ok ||
		object.State != "draft" || object.Published != "" ||
		len(isolated.app(build.ID).(platform.ResultApp).AcceptedLedger().Changes.Records(isolated.ID)) != 1 {
		t.Fatal("incompatible publication changed the recovered record or receipt before quarantine")
	}

	// Crash after each committed publication row but before in-memory apply.
	for _, point := range []struct {
		name, key, schema, typ, target string
		prefix                         int
		kind                           platform.AssetKind
		asset                          string
	}{
		{"object", "o-publish", build.SchemaPublish, build.ObjectType, "O1", 1, platform.AssetObject, build.TypeOf("visit")},
		{"page", "p-publish", build.SchemaRelease, build.PageType, "P1", 3, platform.AssetPage, "visits"},
		{"application", "a-publish", build.SchemaHandOver, build.AppType, "A1", 5, platform.AssetApp, "frontdesk"},
	} {
		t.Run(point.name+"-after-append", func(t *testing.T) {
			crashing := compose()
			before := slices.Clone(entries[:point.prefix])
			if err := crashing.Replay(before); err != nil {
				t.Fatal(err)
			}
			crashing.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
				before = append(before, e)
				panic("injected crash after append")
			}
			crashingMember, _ := crashing.Member("dana")
			func() {
				defer func() {
					if recover() == nil {
						t.Fatal("injected post-append crash was swallowed")
					}
				}()
				crashing.Submit(crashingMember, &pb.Submission{TenantId: crashing.ID, PrincipalId: crashingMember.ID,
					Authority: build.ID, IdempotencyKey: point.key,
					Target: &pb.EntityRef{Type: point.typ, Id: point.target},
					Schema: &pb.SchemaRef{Name: point.schema, Version: 1}, Payload: []byte(`{}`)}, at)
			}()
			hasAsset := func(tenant *Tenant) bool {
				return slices.ContainsFunc(tenant.definitions, func(d platform.Definition) bool {
					return d.Ref.Kind == point.kind && d.Ref.Name == point.asset
				})
			}
			if hasAsset(crashing) {
				t.Fatal("publication was installed before committed result application")
			}
			restarted := compose()
			if err := restarted.Replay(before); err != nil {
				t.Fatalf("committed publication could not recover after crash: %v", err)
			}
			if !hasAsset(restarted) {
				t.Fatal("committed publication was lost at the append/apply crash boundary")
			}
		})
	}
}
