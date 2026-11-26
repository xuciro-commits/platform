package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"platformserver/kernel/protocol"
	"platformserver/services/devops/packages"
	releaseowner "platformserver/services/devops/releases"
	"reflect"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel/trace"
	"platformserver/kernel/records"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func TestInvariantK13ConsoleForksRetainMembersProfilesAndDelegations(t *testing.T) {
	live := newInvariantTenant(t, "invariant-console-forks",
		protocol.Seat{Subjects: []string{"admin"}, Member: platform.Member{ID: "admin", Roles: map[string]string{"platform": "admin", build.ID: build.Builder}}},
		protocol.Seat{Subjects: []string{"member"}, Member: platform.Member{ID: "member", Roles: map[string]string{build.ID: build.Builder}}},
		protocol.Seat{Subjects: []string{"departing"}, Member: platform.Member{ID: "departing", Roles: map[string]string{build.ID: build.Builder}}},
		protocol.Seat{Subjects: []string{"delegatee"}, Member: platform.Member{ID: "delegatee", Roles: map[string]string{}}},
	)
	submit := func(actor, schema, typ, id string, payload any) {
		t.Helper()
		member, found := live.Member(actor)
		if !found {
			t.Fatalf("missing actor %s", actor)
		}
		request := live.submission(schema, typ, id, payload)
		request.PrincipalId, request.Authority = actor, controlowner.PlatformApp
		if _, refusal := live.Submit(member, request, invariantClock); refusal != nil {
			t.Fatalf("accept %s for %s: %v", schema, id, refusal)
		}
	}
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, "invariantproject.alpha.create", "invariantproject.alpha", "A1", map[string]any{"note": "retained"})
	submit("admin", controlowner.SchemaProfileUpdate, controlowner.ProfileType, "member", map[string]any{"displayName": "Before"})
	submit("departing", protocol.SchemaDelegate, controlowner.MemberType, "delegatee", map[string]any{"app": build.ID, "until": "2099-01-01"})
	image := live.readImage.Load()
	left, err := image.state.forkReadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	right, err := image.state.forkReadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fork := left.App(controlowner.PlatformApp).(*controlowner.Console)
	mutateFork := func(schema, typ, id string, payload any) {
		t.Helper()
		admin, _ := left.Member("admin")
		request := live.submission(schema, typ, id, payload)
		request.PrincipalId, request.Authority = admin.ID, controlowner.PlatformApp
		if _, refusal := fork.Submit(platform.NewCaller(nil, admin, controlowner.PlatformApp, false, false), request, invariantClock); refusal != nil {
			t.Fatalf("fork %s for %s: %v", schema, id, refusal)
		}
	}
	mutateFork(controlowner.SchemaGrant, controlowner.MemberType, "member", map[string]any{"app": "platform", "role": "confidential"})
	mutateFork(controlowner.SchemaProfileUpdate, controlowner.ProfileType, "member", map[string]any{"displayName": "Fork"})
	mutateFork(controlowner.SchemaOffboard, controlowner.MemberType, "departing", map[string]any{})
	changed, _ := left.Member("member")
	delegated, _ := left.Member("delegatee")
	if !changed.Holds("platform", "confidential") || fork.Account("member").DisplayName != "Fork" || delegated.Holds(build.ID, build.Builder) {
		t.Fatalf("fork did not apply its isolated grant/profile/offboarding: member=%+v delegatee=%+v", changed, delegated)
	}
	for _, scope := range []*Tenant{live.Tenant, image.state, right} {
		member, _ := scope.Member("member")
		departing, _ := scope.Member("departing")
		delegatee, _ := scope.Member("delegatee")
		if member.Holds("platform", "confidential") || !departing.Active() || !slices.ContainsFunc(delegatee.Grants, func(grant platform.Grant) bool { return grant.By == "departing" }) || scope.App("platform").(*controlowner.Console).Account("member").DisplayName != "Before" {
			t.Fatalf("a fork changed its source or sibling: member=%+v departing=%+v delegatee=%+v", member, departing, delegatee)
		}
	}
	retainedMember, _ := right.Member("member")
	submit("admin", controlowner.SchemaGrant, controlowner.MemberType, "member", map[string]any{"app": "platform", "role": "confidential"})
	submit("admin", controlowner.SchemaRevoke, controlowner.MemberType, "member", map[string]any{"app": build.ID})
	submit("admin", controlowner.SchemaProfileUpdate, controlowner.ProfileType, "member", map[string]any{"displayName": "After"})
	submit("admin", controlowner.SchemaOffboard, controlowner.MemberType, "departing", map[string]any{})
	for _, scope := range []*Tenant{image.state, right} {
		member, _ := scope.Member("member")
		departing, _ := scope.Member("departing")
		delegatee, _ := scope.Member("delegatee")
		if !member.Holds(build.ID, build.Builder) || member.Holds("platform", "confidential") || !departing.Active() || !delegatee.Holds(build.ID, build.Builder) || scope.App("platform").(*controlowner.Console).Account("member").DisplayName != "Before" {
			t.Fatalf("source acceptance changed a retained identity: member=%+v departing=%+v delegatee=%+v", member, departing, delegatee)
		}
	}
	page, refusal := right.Records(t.Context(), retainedMember, "invariantproject.alpha", platform.Query{Sort: []string{"id"}}, invariantClock)
	if refusal != nil || page.Total != 1 {
		t.Fatalf("retained authorization was lost: total=%d refusal=%v", page.Total, refusal)
	}
	if _, refusal := live.Records(t.Context(), retainedMember, "invariantproject.alpha", platform.Query{}, invariantClock); refusal == nil {
		t.Fatal("the current scope retained the revoked builder authorization")
	}
	current, _ := live.Member("member")
	delegatee, _ := live.Member("delegatee")
	if current.Holds(build.ID, build.Builder) || !current.Holds("platform", "confidential") || delegatee.Holds(build.ID, build.Builder) || live.App("platform").(*controlowner.Console).Account("member").DisplayName != "After" {
		t.Fatalf("current scope did not apply its identity changes: member=%+v delegatee=%+v", current, delegatee)
	}
}

func TestInvariantK13DefinitionPlansFollowPublishedReadImages(t *testing.T) {
	live := newInvariantTenant(t, "invariant-definition-plans")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	live.accept(t, build.FolderType+".create", build.FolderType, "PLANFOLDER", map[string]any{"name": "Plans", "project": invariantProject})
	image := live.readImage.Load()
	left, err := image.state.forkReadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	right, err := image.state.forkReadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	request := live.submission("invariantproject.alpha.create", "invariantproject.alpha", "A1", map[string]any{"note": "before"})
	plan, err := left.decisionDefinition(build.ID, request, "")
	if err != nil || plan == nil || plan.DependenciesState != "closed" || len(plan.Dependencies) == 0 {
		t.Fatalf("published action has no closed dependency plan: %+v, %v", plan, err)
	}
	want := *plan
	cache := left.readScope.definitions
	if cache == nil || cache != right.readScope.definitions {
		t.Fatal("independent scopes did not share their published definition cache")
	}
	plan.Observation = &platform.ObservationDecisionEvidence{Key: "changed-by-caller"}
	plan.Contract.Family = 64
	plan.Contract.Version = "changed-by-caller"
	finish := left.begin("invariant definition read", trace.SpanContext{})
	during, err := left.decisionDefinition(build.ID, request, "")
	if err != nil || during == nil || during.Observation != nil || during.Contract != want.Contract || &during.Dependencies[0] != &want.Dependencies[0] || left.readScope.definitions != cache {
		t.Fatalf("tracing lost or contaminated the shared plan: %+v, %v", during, err)
	}
	finish("ok")
	after, err := right.decisionDefinition(build.ID, request, "")
	if err != nil || after == nil || after.Observation != nil || after.Contract != want.Contract || &after.Dependencies[0] != &want.Dependencies[0] || left.readScope.definitions != cache {
		t.Fatalf("another scope rebuilt or inherited a caller mutation: %+v, %v", after, err)
	}
	live.accept(t, "build.object.edit", build.ObjectType, "OBJECT", map[string]any{"title": "Changed alpha"})
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	published, err := live.readImage.Load().state.forkReadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	updated, err := published.decisionDefinition(build.ID, request, "")
	if err != nil || updated == nil || updated.Closure == want.Closure || published.readScope.definitions == cache {
		t.Fatalf("a publication reused the old execution plan: %+v, %v", updated, err)
	}
	rid := protocol.ResourceID(live.ID, platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.alpha"})
	live.accept(t, build.SchemaResourceMove, build.ResourceType, rid, map[string]any{"project": invariantProject, "folder": "PLANFOLDER"})
	moved, err := live.readImage.Load().state.forkReadState(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if moved.definitionsFingerprint() != published.definitionsFingerprint() || moved.readScope.definitions == published.readScope.definitions {
		t.Fatal("resource membership changes did not invalidate the published plan cache independently of definitions")
	}
	current, err := moved.decisionDefinition(build.ID, request, "")
	if err != nil || current == nil || current.Closure != updated.Closure {
		t.Fatalf("moving a resource changed its immutable execution definition: %+v, %v", current, err)
	}
	retained, err := right.decisionDefinition(build.ID, request, "")
	if err != nil || retained == nil || retained.Closure != want.Closure || retained.Contract != want.Contract || right.readScope.definitions != cache {
		t.Fatalf("a newer publication or resource change replaced the old scope's plan: %+v, %v", retained, err)
	}
}

func TestInvariantK13LaneFallbackReadsOnlyCachedDefinitionPlans(t *testing.T) {
	live := newInvariantTenant(t, "invariant-lane-plan")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	typ := "invariantproject.alpha"
	request := live.submission(typ+".create", typ, "A1", map[string]any{"note": "before"})
	image := live.readImage.Load()
	warm, err := image.state.decisionDefinition(build.ID, request, "")
	if err != nil || warm == nil || len(warm.Dependencies) == 0 {
		t.Fatalf("could not prime the published cache: %+v, %v", warm, err)
	}
	fallback := func(schema string) (*platform.DecisionDefinition, error) {
		candidate := live.submission(schema, typ, "A1", nil)
		attempts := 0
		return decideWithError(live.Tenant, "invariant-fallback", func(scope *Tenant) (*platform.DecisionDefinition, error) {
			attempts++
			if attempts <= 3 {
				live.StateEpoch.Add(1)
				return nil, nil
			}
			return scope.decisionDefinition(build.ID, candidate, "")
		}, nil)
	}
	hit, err := fallback(typ + ".create")
	if err != nil || hit == nil || len(hit.Dependencies) == 0 || &hit.Dependencies[0] != &warm.Dependencies[0] {
		t.Fatalf("lane fallback rebuilt a cached plan: %+v, %v", hit, err)
	}
	cold, err := fallback(typ + ".edit")
	if err != nil || cold == nil || len(cold.Dependencies) == 0 {
		t.Fatalf("lane fallback could not build a missing plan: %+v, %v", cold, err)
	}
	again, err := fallback(typ + ".edit")
	if err != nil || again == nil || len(again.Dependencies) == 0 || &again.Dependencies[0] == &cold.Dependencies[0] || !reflect.DeepEqual(again, cold) {
		t.Fatalf("lane fallback populated the cache or rebuilt a different plan: %+v, %v", again, err)
	}

	locked, unlock, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	defer close(unlock)
	go func() {
		defer close(finished)
		_, _ = image.state.readScope.definitions.Lookup(image.definitions, image.resources, releaseowner.DefinitionPlanKey{}, func() (*platform.DecisionDefinition, error) {
			close(locked)
			<-unlock
			return nil, nil
		})
	}()
	select {
	case <-locked:
	case <-finished:
		t.Fatal("the published plan cache was not locked by a fill")
	case <-time.After(10 * time.Second):
		t.Fatal("cache fill did not start")
	}
	resolved := make(chan readAnswer[*platform.DecisionDefinition], 1)
	go func() {
		plan, err := fallback(typ + ".create")
		resolved <- readAnswer[*platform.DecisionDefinition]{value: plan, err: err}
	}()
	select {
	case answer := <-resolved:
		if answer.err != nil || !reflect.DeepEqual(answer.value, warm) {
			t.Fatalf("busy cache fallback rebuilt a different plan: %+v, %v", answer.value, answer.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("lane fallback waited for the published plan cache")
	}
}

func TestInvariantK13PublicLiveReadsShareOnlyTenantScopedResults(t *testing.T) {
	info := platform.RecordTypeInfo{Type: "build.loaditem", App: build.ID, Scope: platform.Scope{Owner: platform.OwnerCreated, Default: platform.ScopeNone, Levels: map[string]string{build.User: platform.ScopeTenant}}}
	entity := func(typ string) (platform.RecordTypeInfo, bool) { return info, typ == info.Type }
	member := func(id string) platform.Member {
		return platform.Member{ID: id, Roles: map[string]string{build.ID: build.User}}
	}
	left, shared := build.PublicLiveKey(member("left"), "/v1/records/build.loaditem?limit=25", "version", "en", entity)
	right, sharedRight := build.PublicLiveKey(member("right"), "/v1/records/build.loaditem?limit=25", "version", "en", entity)
	if !shared || !sharedRight || left != right {
		t.Fatal("equivalent tenant-scope reads did not share a live result")
	}
	marked := member("left")
	marked.Markings = []string{"confidential"}
	if markedKey, ok := build.PublicLiveKey(marked, "/v1/records/build.loaditem?limit=25", "version", "en", entity); !ok || markedKey == left {
		t.Fatal("a different marking clearance shared a live result")
	}
	scoped := info
	scoped.Scope.Levels = map[string]string{build.User: platform.ScopeOwn}
	if _, ok := build.PublicLiveKey(member("left"), "/v1/records/build.loaditem?limit=25", "version", "en", func(string) (platform.RecordTypeInfo, bool) { return scoped, true }); ok {
		t.Fatal("an owner-scoped read was shared across members")
	}
	participant := info
	participant.Scope.Participants = func(any) []string { return nil }
	if _, ok := build.PublicLiveKey(member("left"), "/v1/records/build.loaditem?limit=25", "version", "en", func(string) (platform.RecordTypeInfo, bool) { return participant, true }); ok {
		t.Fatal("a participant-scoped read was shared across members")
	}
}

func TestInvariantK13SQLRecordWindowsRespectVisibilityAndPrivateRows(t *testing.T) {
	live := newInvariantTenant(t, "invariant-sql-record-windows")
	live.accept(t, "build.object.create", build.ObjectType, "OBJECT", invariantObject("alpha", "Alpha"))
	live.accept(t, "build.object.publish", build.ObjectType, "OBJECT", map[string]any{})
	for _, row := range []struct{ id, note string }{{"A", "keep"}, {"B", "skip"}, {"C", "keep"}, {"D", "skip"}, {"E", "keep"}} {
		live.accept(t, "invariantproject.alpha.create", "invariantproject.alpha", row.id, map[string]any{"note": row.note})
	}
	live.accept(t, "invariantproject.alpha.archive", "invariantproject.alpha", "E", map[string]any{})
	memory := live.Projection.ForkRecords()
	info := memory.Type("invariantproject.alpha").Info()
	template := memory.Type("invariantproject.alpha").Rows().Get("A").Value.Interface()
	reads := &sqlShapedReads{rows: map[string]map[string]persistence.StateCurrent{"invariantproject.alpha": {}}}
	for id, row := range memory.Type("invariantproject.alpha").Rows().All() {
		raw, err := row.Image()
		if err != nil {
			t.Fatal(err)
		}
		reads.rows["invariantproject.alpha"][id] = persistence.StateCurrent{Image: platform.Raw(records.Image{Type: "invariantproject.alpha", ID: id, Format: row.Format, Value: raw})}
	}
	withSQLShapedReads(t, live, reads)
	if memory.Type("invariantproject.alpha").Rows().Sourced() {
		t.Fatal("the live SQL source changed the retained memory draft")
	}
	if _, err := live.Projection.Current().Open(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer func() { live.Projection.Current().Detach().Close() }()
	privateSQL, privateMemory := live.Projection.ForkRecords(), memory.ForkRecords()
	note, _ := info.Field("note")
	for _, store := range []*records.Store{privateSQL, privateMemory} {
		for _, id := range []string{"B", "F"} {
			value := records.Copy(info.Go, template)
			records.Header(value).ID = id
			value.FieldByIndex(note.Index).SetString("private")
			store.Type("invariantproject.alpha").SetRow(id, &records.Row{Value: value})
			store.TrackWrite("invariantproject.alpha/" + id)
		}
		store.Type("invariantproject.alpha").SetRow("C", nil)
		store.TrackWrite("invariantproject.alpha/C")
	}
	read := func(store *records.Store, query platform.Query, visible func(reflect.Value) bool) ([]string, int) {
		t.Helper()
		store.Lock()
		defer store.Unlock()
		rows, total, refusal := store.Find(store.Type("invariantproject.alpha"), query, visible)
		if refusal != nil {
			t.Fatal(refusal)
		}
		values := make([]string, len(rows))
		for i, row := range rows {
			values[i] = fmt.Sprintf("%s:%s:%t", records.Header(row).ID, row.FieldByIndex(note.Index).String(), records.Header(row).Archived)
		}
		return values, total
	}
	for _, check := range []struct {
		name        string
		query       platform.Query
		visible     func(reflect.Value) bool
		private     bool
		windowIDs   []string
		windowTotal int
		point       bool
	}{
		{name: "ascending offset", query: platform.Query{Sort: []string{"id"}, Offset: 1, Limit: 2}, windowIDs: []string{"B", "C"}, windowTotal: 4},
		{name: "descending offset", query: platform.Query{Sort: []string{"-id"}, Offset: 1, Limit: 2}, windowIDs: []string{"C", "B"}, windowTotal: 4},
		{name: "archived window", query: platform.Query{Sort: []string{"id"}, Archived: true, Offset: 3, Limit: 2}, windowIDs: []string{"D", "E"}, windowTotal: 5},
		{name: "past end", query: platform.Query{Sort: []string{"id"}, Offset: 20, Limit: 2}, windowIDs: []string{}, windowTotal: 4},
		{name: "restricted visibility", query: platform.Query{Sort: []string{"id"}, Limit: 2}, visible: func(row reflect.Value) bool { return records.Header(row).ID != "B" }},
		{name: "filtered rows", query: platform.Query{Sort: []string{"id"}, Limit: 2, Domain: json.RawMessage(`[["note","=","keep"]]`)}},
		{name: "prefix filtered rows", query: platform.Query{Sort: []string{"id"}, Limit: 2, Domain: json.RawMessage(`[["note","starts-with","ke"]]`)}},
		{name: "field ordering", query: platform.Query{Sort: []string{"-note"}, Limit: 2}},
		{name: "unbounded", query: platform.Query{Sort: []string{"id"}}},
		{name: "private overlay", query: platform.Query{Sort: []string{"id"}, Limit: 20}, private: true},
		{name: "private filtered IDs", query: platform.Query{Sort: []string{"id"}, Limit: 20, Domain: json.RawMessage(`[["id","in",["A","B","C","F"]]]`)}, private: true},
		{name: "primary key", query: platform.Query{Limit: 1, Domain: json.RawMessage(`[["id","=","A"]]`)}, point: true},
	} {
		t.Run(check.name, func(t *testing.T) {
			actualStore, expectedStore := live.Projection, memory
			if check.private {
				actualStore, expectedStore = privateSQL, privateMemory
			}
			want, total := read(expectedStore, check.query, check.visible)
			reads.singles, reads.current, reads.batches, reads.windows = 0, 0, nil, nil
			reads.window = persistence.StateWindowResult{IDs: check.windowIDs, Total: check.windowTotal}
			actual, actualTotal := read(actualStore, check.query, check.visible)
			if !slices.Equal(actual, want) || actualTotal != total {
				t.Fatalf("SQL window differs from the complete visible scan: rows=%v total=%d; want rows=%v total=%d", actual, actualTotal, want, total)
			}
			if check.point {
				actualStore.Current().ReusePoint("invariantproject.alpha", "A")
				_, _ = read(actualStore, check.query, check.visible)
				_, _ = read(actualStore, check.query, check.visible)
				if reads.singles != 2 || len(reads.batches) != 0 || len(reads.windows) != 0 {
					t.Fatalf("primary key materialization used singles=%d batches=%v windows=%v", reads.singles, reads.batches, reads.windows)
				}
				return
			}
			if reads.singles != 0 {
				t.Fatalf("record materialization made %d single-row SQL reads", reads.singles)
			}
			if check.windowIDs != nil {
				wantWindow := persistence.StateWindow{Archived: check.query.Archived, Descending: check.query.Sort[0] == "-id", Offset: check.query.Offset, Limit: check.query.Limit}
				if !slices.Equal(reads.windows, []persistence.StateWindow{wantWindow}) || reads.current != len(check.windowIDs) {
					t.Fatalf("safe pagination did not read exactly its requested SQL window: windows=%+v materialized=%d", reads.windows, reads.current)
				}
			} else if len(reads.windows) != 0 || len(reads.batches) == 0 {
				t.Fatalf("a restricted or private query bypassed visible scanning: windows=%v batches=%v", reads.windows, reads.batches)
			}
			if check.private {
				for _, batch := range reads.batches {
					for _, id := range []string{"B", "C", "F"} {
						if slices.Contains(batch, id) {
							t.Fatalf("private row or tombstone %s was reloaded from SQL: %v", id, batch)
						}
					}
				}
			}
		})
	}
}

func TestInvariantK13PlatformAcceptanceVisibleOnNextRead(t *testing.T) {
	source := newInvariantTenant(t, "k13-pkg-src")
	source.accept(t, "build.object.create", build.ObjectType, "OBJ1", invariantObject("alpha", "Alpha"))
	source.accept(t, "build.app.create", protocol.AppType, "APP1", map[string]any{
		"name": "alphaapp", "title": "Alpha app",
		"resources": []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: "invariantproject.alpha"}},
	})
	candidateID := invariantPublishData(t, source, source.member(t), []build.JointDraftRef{
		{Kind: platform.AssetObject, ID: "OBJ1"},
		{Kind: platform.AssetApp, ID: "APP1"},
	}, "k13-pkg")
	candidate, err := PublicationResolver().ReadCandidate(candidateID, source.releases.Raw(candidateID))
	if err != nil {
		t.Fatal(err)
	}
	makeDescriptor := func(version string) platform.PackageDescriptor {
		return platform.PackageDescriptor{
			ID: "readafterwrite", Version: version, Namespace: "readafterwrite", Title: "Read After Write",
			Blueprint: &platform.PackageBlueprint{
				Roots:  []platform.AssetRef{{App: build.ID, Kind: platform.AssetApp, Name: "alphaapp"}},
				Assets: candidate.Assets,
			},
		}
	}
	vNew := makeDescriptor("1.1.0")
	vOld := makeDescriptor("1.0.0")
	vExtra := makeDescriptor("1.2.0")

	seat := protocol.Seat{Subjects: []string{"user:admin@example.com"}, Member: platform.Member{
		ID: "admin", Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin},
	}}
	target := newInvariantTenant(t, "k13-pkg-target", seat)
	target.App(controlowner.PlatformApp).(packages.Catalog).BindPackageIndex(&protocol.PackageIndex{Packages: []platform.PackageDescriptor{vNew}})
	target.invalidateReadImage()
	h := NewHost(Tokens(map[string]string{"admin-token": "user:admin@example.com"}), target.Tenant)
	handler := h.Handler()

	doGet := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set(TenantHeader, target.ID)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}
	listPackages := func() []protocol.PackageView {
		t.Helper()
		rec := doGet("/v1/packages")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /v1/packages: status=%d body=%s", rec.Code, rec.Body.String())
		}
		var views []protocol.PackageView
		if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
			t.Fatalf("decode /v1/packages: %v", err)
		}
		return views
	}
	submitHTTP := func(schema, key string, payload any) {
		t.Helper()
		reqBody := map[string]any{
			"tenantId":       target.ID,
			"principalId":    "admin",
			"authority":      controlowner.PlatformApp,
			"target":         map[string]any{"type": protocol.PackageType, "id": vOld.ID},
			"schema":         map[string]any{"name": schema, "version": 1},
			"idempotencyKey": key,
			"payload":        platform.Raw(payload),
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(platform.Raw(reqBody)))
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set(TenantHeader, target.ID)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /v1/submissions %s: status=%d body=%s", schema, rec.Code, rec.Body.String())
		}
		var ans SubmissionAnswer
		if err := json.Unmarshal(rec.Body.Bytes(), &ans); err != nil || ans.Record == nil || ans.Error != nil {
			t.Fatalf("POST /v1/submissions %s answer: err=%v body=%s", schema, err, rec.Body.String())
		}
	}

	initial := listPackages()
	if len(initial) != 1 || initial[0].Descriptor.Version != "1.1.0" {
		t.Fatalf("initial packages: %+v", initial)
	}
	if target.readImage.Load() == nil {
		t.Fatal("expected warm readImage before platform submission")
	}

	previewOld, err := target.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(vOld)})
	if err != nil {
		t.Fatal(err)
	}
	submitHTTP(packages.SchemaPackageImport, "k13-import-old", map[string]any{"id": vOld.ID, "digest": previewOld.Digest})
	afterImport := listPackages()
	if !slices.ContainsFunc(afterImport, func(v protocol.PackageView) bool {
		return v.Descriptor.ID == vOld.ID && v.Descriptor.Version == "1.0.0"
	}) {
		t.Fatalf("GET /v1/packages immediately after import missed 1.0.0: %+v", afterImport)
	}
	if rec := doGet("/v1/packages/" + vOld.ID + "/review?version=1.0.0"); rec.Code != http.StatusOK {
		t.Fatalf("GET review immediately after import failed: status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := doGet("/v1/packages/" + vOld.ID + "/preparation?version=1.0.0"); rec.Code != http.StatusOK {
		t.Fatalf("GET preparation immediately after import failed: status=%d body=%s", rec.Code, rec.Body.String())
	}

	submitHTTP(packages.SchemaPackageInstall, "k13-install-old", map[string]any{"id": vOld.ID, "version": "1.0.0", "namespace": "readafterwrite"})
	afterInstall := listPackages()
	if !slices.ContainsFunc(afterInstall, func(v protocol.PackageView) bool {
		return v.Descriptor.ID == vOld.ID && v.Installed != nil && v.Installed.State == "active" && v.Installed.Version == "1.0.0"
	}) {
		t.Fatalf("GET /v1/packages immediately after install missed active 1.0.0: %+v", afterInstall)
	}

	previewExtra, err := target.InspectPackage(t.Context(), protocol.PackageInspectRequest{Content: platform.Raw(vExtra)})
	if err != nil {
		t.Fatal(err)
	}
	submitHTTP(packages.SchemaPackageImport, "k13-import-extra", map[string]any{"id": vExtra.ID, "digest": previewExtra.Digest})
	if views := listPackages(); !slices.ContainsFunc(views, func(v protocol.PackageView) bool {
		return v.Descriptor.ID == vExtra.ID && v.Descriptor.Version == "1.2.0"
	}) {
		t.Fatalf("GET /v1/packages immediately after importing 1.2.0 missed it")
	}
	submitHTTP(packages.SchemaPackageRemoveSource, "k13-remove-extra", map[string]any{"id": vExtra.ID, "digest": previewExtra.Digest})
	if views := listPackages(); slices.ContainsFunc(views, func(v protocol.PackageView) bool {
		return v.Descriptor.ID == vExtra.ID && v.Descriptor.Version == "1.2.0"
	}) {
		t.Fatalf("GET /v1/packages immediately after removing 1.2.0 still showed it")
	}

	submitSetting := func(key, value string) {
		t.Helper()
		reqBody := map[string]any{
			"tenantId":       target.ID,
			"principalId":    "admin",
			"authority":      controlowner.PlatformApp,
			"target":         map[string]any{"type": controlowner.SettingType, "id": controlowner.PlatformApp + "/" + controlowner.SettingLanguage},
			"schema":         map[string]any{"name": controlowner.SchemaSettingSet, "version": 1},
			"idempotencyKey": key,
			"payload":        platform.Raw(map[string]any{"value": value}),
		}
		req := httptest.NewRequest(http.MethodPost, "/v1/submissions", bytes.NewReader(platform.Raw(reqBody)))
		req.Header.Set("Authorization", "Bearer admin-token")
		req.Header.Set(TenantHeader, target.ID)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("POST /v1/submissions setting: status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	submitSetting("k13-setting-lang", "zh-CN")
	settingsRec := doGet("/v1/settings")
	if settingsRec.Code != http.StatusOK || !bytes.Contains(settingsRec.Body.Bytes(), []byte(`"zh-CN"`)) {
		t.Fatalf("GET /v1/settings immediately after platform.setting.set missed zh-CN: status=%d body=%s", settingsRec.Code, settingsRec.Body.String())
	}
}
