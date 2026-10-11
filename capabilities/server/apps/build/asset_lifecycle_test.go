package build

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

type lifecycleRows struct {
	platform.Runtime
	rows map[reflect.Type][]any
}

func (r *lifecycleRows) Find(_ platform.Caller, typ reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	rows := r.rows[typ]
	if q.Offset >= len(rows) {
		return nil, len(rows), nil
	}
	end := len(rows)
	if q.Limit > 0 {
		end = min(end, q.Offset+q.Limit)
	}
	return slices.Clone(rows[q.Offset:end]), len(rows), nil
}
func (r *lifecycleRows) Get(_ platform.Caller, typ reflect.Type, id string) (any, bool) {
	for _, row := range r.rows[typ] {
		if reflect.ValueOf(row).FieldByName("Record").Interface().(platform.Record).ID == id {
			return row, true
		}
	}
	return nil, false
}

type lifecycleHost struct {
	host.Host
	runtime *lifecycleRows
	member  platform.Member
	info    platform.EntityInfo
}

func (h lifecycleHost) Automation(platform.Caller, string) platform.Caller {
	return platform.NewCaller(h.runtime, platform.Member{ID: "app:build", Tenant: "target"}, ID, false, true)
}
func (h lifecycleHost) Caller(_ platform.Caller, m platform.Member, app string) platform.Caller {
	return platform.NewCaller(h.runtime, m, app, false, false)
}
func (h lifecycleHost) Member(id string) (platform.Member, bool) { return h.member, id == h.member.ID }
func (h lifecycleHost) Entity(name string) (platform.EntityInfo, bool) {
	return h.info, name == h.info.Type
}
func (h lifecycleHost) Declares(string) bool { return false }
func (h lifecycleHost) Action(schema string) (string, platform.Action, bool) {
	return ID, platform.Action{Schema: schema, Roles: []string{"writer"}}, strings.HasPrefix(schema, h.info.Type+".")
}
func lifecycleBuild() (*Build, *lifecycleRows) {
	rows := &lifecycleRows{rows: map[reflect.Type][]any{}}
	b := New("target")
	b.host = lifecycleHost{runtime: rows, member: platform.Member{ID: "target-builder", Tenant: "target", Roles: map[string]string{ID: Builder}}, info: platform.EntityInfo{App: ID, Type: "build.receipt", Fields: []platform.FieldInfo{{Name: "item", Type: "text"}, {Name: "document", Type: "text"}}}}
	return b, rows
}
func TestMatchingReleaseClosureAndPrivateInstallation(t *testing.T) {
	b, rows := lifecycleBuild()
	rows.rows[reflect.TypeFor[Source]()] = []any{Source{Record: platform.Record{ID: "local-source"}, Name: "feed", State: "draft"}}
	m := Match{Record: platform.Record{ID: "local-match"}, Name: "identity", Title: "Receipt identity", Object: "build.receipt", Keys: []MatchKey{{Fields: []string{"item"}, Normalize: "folded"}}, Prefer: []Preference{{Field: "document", Producer: "feed"}}, State: "published", Publisher: "source-builder"}
	rows.rows[reflect.TypeFor[Match]()] = []any{m}
	asset, err := b.integrationAsset(platform.AssetMatching, m, 1)
	if err != nil {
		t.Fatal(err)
	}
	producer := platform.AssetRef{App: ID, Kind: platform.AssetSource, Name: "feed"}
	object := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: "build.receipt"}
	if !slices.Contains(asset.Requires, producer) || !slices.Contains(asset.Requires, object) {
		t.Fatal("matching rule omitted its producer/object release dependencies")
	}
	if _, err := platform.Candidate([]platform.AssetRef{asset.Ref}, []platform.ReleaseAsset{asset}); err == nil {
		t.Fatal("missing producer/object closure was accepted")
	}
	plans, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{Member: platform.Member{ID: "target-builder"}})
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	saved, err := integrationInstallationRecord(platform.AssetMatching, plans[0].Image)
	if err != nil {
		t.Fatal(err)
	}
	if saved.(Match).Publisher != "target-builder" {
		t.Fatal("source actor was transported")
	}
	if err := b.ValidateAssetInstallation(plans[0]); err != nil {
		t.Fatal(err)
	}
	// A changed mutable editor does not replace the accepted matching rule.
	live := saved.(Match)
	live.Keys = []MatchKey{{Fields: []string{"unknown"}}}
	fixed, err := b.ExecutionMatching(live)
	if err != nil || fixed.Keys[0].Fields[0] != "item" {
		t.Fatal("mutable keys replaced accepted matching", err)
	}
	b.host = lifecycleHost{runtime: rows, member: platform.Member{ID: "target-builder", Tenant: "target", Roles: map[string]string{ID: User}}, info: platform.EntityInfo{App: ID, Type: "build.receipt"}}
	if err := b.ValidateAssetInstallation(plans[0]); err == nil {
		t.Fatal("publication member lacking original matching permission accepted")
	}
	// Saved recovery installs the accepted decision without current grants.
	if err := b.ApplyAssetInstallation(plans[0], platform.AssetApplyContext{Private: true}); err != nil {
		t.Fatal("recovery reran current actor policy", err)
	}
	bad := asset
	bad.Requires = []platform.AssetRef{object}
	raw, _ := json.Marshal(bad)
	live.Published = string(raw)
	if _, err := b.ExecutionMatching(live); err == nil {
		t.Fatal("tampered matching producer closure accepted")
	}
}
func TestPortableIntegrationOmitsEnvironmentAndRuntime(t *testing.T) {
	b, rows := lifecycleBuild()
	conn := Connection{Record: platform.Record{ID: "dev-connection"}, Name: "sap", Title: "SAP", Kind: "http", Address: "https://dev.example", Secret: "DEV_SECRET_NAME", AllowPrivate: true, State: "ready"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{conn}
	ds := Dataset{Record: platform.Record{ID: "dev-data"}, Name: "raw", Title: "Raw", Version: 23, Producer: "source:feed", Schema: []DatasetField{{Name: "private", Type: "text"}}}
	rows.rows[reflect.TypeFor[Dataset]()] = []any{ds}
	source := Source{Record: platform.Record{ID: "dev-source"}, Name: "feed", Title: "Feed", Connection: conn.ID, Dataset: ds.ID, URL: "rows", Cursor: "917", Puller: "dev-principal", State: "published"}
	for _, pair := range []struct {
		kind platform.AssetKind
		row  any
	}{{platform.AssetConnection, conn}, {platform.AssetDataset, ds}, {platform.AssetSource, source}} {
		asset, err := b.integrationAsset(pair.kind, pair.row, 1)
		if err != nil {
			t.Fatal(err)
		}
		for _, private := range []string{"dev-connection", "dev-data", "dev-source", "dev.example", "DEV_SECRET_NAME", "dev-principal", "917", "private"} {
			if strings.Contains(string(asset.Body), private) {
				t.Fatalf("%s portable definition leaked %s", pair.kind, private)
			}
		}
	}
	asset, err := b.integrationAsset(platform.AssetSource, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := retainIntegration(source, asset)
	if err != nil {
		t.Fatal(err)
	}
	live := saved.(Source)
	live.URL = "draft-path"
	live.Cursor = "1000"
	fixed, err := b.ExecutionSource(live)
	if err != nil || fixed.URL != "rows" || fixed.Cursor != "1000" || fixed.Connection != conn.ID || fixed.Dataset != ds.ID {
		t.Fatal("definition and runtime overlay failed", fixed, err)
	}
}
func TestOwnerInstallationOrderFollowsActualDependencies(t *testing.T) {
	object := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: "build.receipt"}
	page := platform.AssetRef{App: ID, Kind: platform.AssetPage, Name: "desk"}
	app := platform.AssetRef{App: ID, Kind: platform.AssetApp, Name: "receiving"}
	assets := []platform.ReleaseAsset{{Ref: app, Requires: []platform.AssetRef{page}}, {Ref: page, Requires: []platform.AssetRef{object}}, {Ref: object}}
	plans := []platform.AssetInstallation{{Ref: app}, {Ref: page}, {Ref: object}}
	ordered, err := orderOwnerInstallations(assets, plans)
	if err != nil || len(ordered) != 3 || ordered[0].Ref != object || ordered[1].Ref != page || ordered[2].Ref != app {
		t.Fatal("application prepared before its actual page/object dependencies", ordered, err)
	}
	assets[2].Requires = []platform.AssetRef{app}
	if _, err := orderOwnerInstallations(assets, plans); err == nil {
		t.Fatal("execution dependency cycle accepted")
	}
}
func TestWritebackRequestPinsOriginalAnswerMapping(t *testing.T) {
	w := Writeback{Record: platform.Record{ID: "writeback"}, Object: "build.receipt", Result: []SourceField{{From: "document", To: "document"}}}
	req, err := w.Request(Connection{Address: "https://target.example"}, "R", json.RawMessage(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	w.Result[0].To = "other"
	if req.Version != 1 || req.Object != "build.receipt" || req.Result[0].To != "document" {
		t.Fatal("in-flight reply inherited changed mapping")
	}
}

func TestSourceUpgradePreservesLocalCursorAndRejectsNewIdentity(t *testing.T) {
	b, rows := lifecycleBuild()
	conn := Connection{Record: platform.Record{ID: "target-connection"}, Name: "sap", Title: "SAP", Kind: "http", State: "ready"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{conn}
	dataset := Dataset{Record: platform.Record{ID: "target-data"}, Name: "raw", Title: "Raw"}
	rows.rows[reflect.TypeFor[Dataset]()] = []any{dataset}
	source := Source{Record: platform.Record{ID: "target-source"}, Name: "feed", Title: "Feed", Connection: conn.ID, Dataset: dataset.ID, URL: "rows", Since: "seq", Cursor: "91", Puller: "target-builder", State: "published"}
	first, err := b.integrationAsset(platform.AssetSource, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := retainIntegration(source, first)
	if err != nil {
		t.Fatal(err)
	}
	source = saved.(Source)
	rows.rows[reflect.TypeFor[Source]()] = []any{source}
	edited := source
	edited.Title = "Improved title"
	second, err := b.integrationAsset(platform.AssetSource, edited, 2)
	if err != nil {
		t.Fatal(err)
	}
	ctx := platform.AssetReleaseContext{Member: platform.Member{ID: "target-builder"}, Upgrade: true, Bindings: []platform.AssetEnvironmentBinding{{Ref: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: "sap"}, TargetRecord: conn.ID}}}
	plans, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{second}, ctx)
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	if err := b.ApplyAssetInstallation(plans[0], platform.AssetApplyContext{Private: true, Validate: true, Upgrade: true}); err != nil {
		t.Fatal("compatible title upgrade refused", err)
	}
	record, err := integrationInstallationRecord(platform.AssetSource, plans[0].Image)
	if err != nil || record.(Source).Cursor != "91" || record.(Source).Puller != "target-builder" {
		t.Fatal("target runtime cursor/actor not preserved", record, err)
	}
	edited.URL = "other-rows"
	changed, err := b.integrationAsset(platform.AssetSource, edited, 2)
	if err != nil {
		t.Fatal(err)
	}
	plans, err = b.PrepareReleaseAssets([]platform.ReleaseAsset{changed}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.ApplyAssetInstallation(plans[0], platform.AssetApplyContext{Private: true, Validate: true, Upgrade: true}); err == nil {
		t.Fatal("source cursor reused for a different input identity")
	}
	// Pure recovery does not reinterpret or reauthorize a previously saved plan.
	b.host = nil
	if _, err := b.DecodeAssetInstallation(plans[0].Schema, plans[0].Image); err != nil {
		t.Fatal("decode touched target runtime", err)
	}
}

func TestIssuedResultsUseRetainedExecutionSemantics(t *testing.T) {
	b, rows := lifecycleBuild()
	conn := Connection{Record: platform.Record{ID: "connection"}, Name: "sap", Title: "SAP", Kind: "http", State: "ready"}
	ds := Dataset{Record: platform.Record{ID: "dataset"}, Name: "raw", Title: "Raw"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{conn}
	rows.rows[reflect.TypeFor[Dataset]()] = []any{ds}
	source := Source{Record: platform.Record{ID: "source"}, Name: "feed", Title: "Feed", Connection: conn.ID, Dataset: ds.ID, URL: "rows", Since: "seq", Cursor: "9"}
	v1, err := b.integrationAsset(platform.AssetSource, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := retainIntegration(source, v1)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		edit       func(*Source)
		equivalent bool
	}{
		{"title", func(s *Source) { s.Title = "Clearer feed title" }, true},
		{"endpoint", func(s *Source) { s.URL = "other-rows" }, false},
		{"filter", func(s *Source) { s.Filter = "active = true" }, false},
		{"mapping", func(s *Source) { s.Mapping = []SourceField{{From: "sku", To: "item"}} }, false},
	} {
		t.Run("source-"+tc.name, func(t *testing.T) {
			edited := source
			tc.edit(&edited)
			v2, err := b.integrationAsset(platform.AssetSource, edited, 2)
			if err != nil {
				t.Fatal(err)
			}
			current, err := retainIntegration(saved, v2)
			if err != nil {
				t.Fatal(err)
			}
			// A later editor change cannot reinterpret either saved definition.
			mutable := current.(Source)
			mutable.URL = "unsaved-editor-path"
			ok, err := retainedExecutionEquivalent(mutable, v1.SourceVersion)
			if err != nil || ok != tc.equivalent {
				t.Fatal("issued result used mutable/latest-version semantics", ok, err)
			}
			if ok, err := retainedExecutionEquivalent(mutable, ""); err != nil || ok {
				t.Fatal("new publication accepted an unpinned legacy result", ok, err)
			}
			if ok, err := retainedExecutionEquivalent(mutable, "1.source-63"); err != nil || ok {
				t.Fatal("unknown issued version accepted", ok, err)
			}
		})
	}
	pipeline := Pipeline{Record: platform.Record{ID: "pipeline"}, Name: "landing", Title: "Landing", Input: ds.ID, OutputObject: "build.receipt", Key: "id", Steps: []Step{}}
	first, err := b.integrationAsset(platform.AssetPipeline, pipeline, 1)
	if err != nil {
		t.Fatal(err)
	}
	prior, err := retainIntegration(pipeline, first)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		edit       func(*Pipeline)
		equivalent bool
	}{
		{"title", func(p *Pipeline) { p.Title = "Clearer pipeline title" }, true},
		{"expectations", func(p *Pipeline) { p.Expectations = []Expectation{{Column: "qty", Rule: "range", Value: "1..20"}} }, false},
		{"transform", func(p *Pipeline) { p.Steps = []Step{{Kind: "cast", Column: "qty", Type: "number"}} }, false},
	} {
		t.Run("pipeline-"+tc.name, func(t *testing.T) {
			edited := pipeline
			tc.edit(&edited)
			second, err := b.integrationAsset(platform.AssetPipeline, edited, 2)
			if err != nil {
				t.Fatal(err)
			}
			current, err := retainIntegration(prior, second)
			if err != nil {
				t.Fatal(err)
			}
			ok, err := retainedExecutionEquivalent(current, first.SourceVersion)
			if err != nil || ok != tc.equivalent {
				t.Fatal("pipeline in-flight execution boundary differs", ok, err)
			}
		})
	}
	if ok, err := retainedExecutionEquivalent(Source{}, ""); err != nil || !ok {
		t.Fatal("original legacy result compatibility was lost", ok, err)
	}
	if ok, err := retainedExecutionEquivalent(Source{}, v1.SourceVersion); err != nil || ok {
		t.Fatal("legacy record claimed a retained typed version", ok, err)
	}
}

func TestBootstrapObjectShapesAreCompleteScopedAndPrivate(t *testing.T) {
	b, _ := lifecycleBuild()
	original := b.jointEntities
	objects := []Object{{Name: "left", Title: "Left", Fields: []Field{{Name: "other", Title: "Other", Type: "reference", Ref: "build.right"}, {Name: "item", Title: "Item", Type: "text"}}, States: []State{{Name: "open", Title: "Open"}}}, {Name: "right", Title: "Right", Fields: []Field{{Name: "other", Title: "Other", Type: "reference", Ref: "build.left"}}}}
	var assets []platform.ReleaseAsset
	for _, object := range objects {
		raw, _ := json.Marshal(object)
		assets = append(assets, platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: TypeOf(object.Name)}, Body: raw})
	}
	catalogSize := len(b.ledger.Catalog.All())
	release, err := b.BeginAssetBootstrap(assets)
	if err != nil {
		t.Fatal(err)
	}
	left, ok := b.lookupEntity("build.left")
	if !ok || len(left.Fields) < 2 || left.Lifecycle == nil || !b.fieldKnown("build.right") {
		t.Fatal("bootstrap used an empty shape or lost forward references", left)
	}
	if len(b.installed) != 0 || len(b.ledger.Catalog.All()) != catalogSize {
		t.Fatal("shape lookup installed definitions or extended the ledger")
	}
	release()
	if b.jointMode || len(b.jointEntities) != len(original) {
		t.Fatal("bootstrap scope leaked after release")
	}
	if _, ok := b.lookupEntity("build.left"); ok {
		t.Fatal("candidate shape remained after bootstrap")
	}
}
func TestFreshDeclarationDraftsRetainOwnerRefAndFunctionPlan(t *testing.T) {
	b, rows := lifecycleBuild()
	f := Function{Name: "advice", Title: "Advice", Description: "Advice from a receipt", Object: "build.receipt", Fields: []string{"item"}, Instructions: "Return a brief comment", Output: []platform.Field{{Name: "comment", Type: "string", Description: "A brief comment"}}, MaxInputBytes: 1024, MaxOutputBytes: 1024, MaxTokens: 128, Roles: []string{Builder}, Version: 1}
	function, err := functionReleaseAsset(f, definitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	property, err := propertyTypeReleaseAsset(PropertyType{Name: "itemcode", Title: "Item code", Description: "The external item identifier", Type: "text", Version: 1}, definitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	query, err := queryReleaseAsset(Query{Name: "receipts", Title: "Receipts", Description: "Receipt rows", Object: "build.receipt", Limit: 50, Version: 1}, definitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	link, err := linkTypeReleaseAsset(LinkType{Name: "receiptitems", Title: "Receipt items", Description: "Receipt items", Parent: "build.receipt", Child: "build.item", Via: "receipt", Forward: "items", Reverse: "receipt", Version: 1}, definitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	assets := []platform.ReleaseAsset{function, property, query, link}
	drafts, err := b.ReleaseDrafts(assets, platform.AssetReleaseContext{})
	if err != nil || len(drafts) != 4 {
		t.Fatal("supported declaration kinds omitted bootstrap", drafts, err)
	}
	expected := map[platform.AssetRef]string{function.Ref: FunctionType, property.Ref: PropertyTypeType, query.Ref: QueryType, link.Ref: LinkTypeType}
	for _, draft := range drafts {
		if draft.Type != expected[draft.Ref] || draft.Schema != draft.Type+".create" {
			t.Fatal("draft lost its candidate owner reference", draft)
		}
	}
	draft := f
	draft.Record = platform.Record{ID: "target-advice"}
	draft.Version = 0
	rows.rows[reflect.TypeFor[Function]()] = []any{draft}
	plans, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{function}, platform.AssetReleaseContext{})
	if err != nil || len(plans) != 1 {
		t.Fatal("first function version omitted its installation plan", plans, err)
	}
	frozen, err := functionImage(plans[0].Image)
	if err != nil || frozen.ID != "target-advice" || frozen.Version != 1 {
		t.Fatal("first function plan did not preserve target identity/version", frozen, err)
	}
	newer := function
	newer.SourceVersion = "1.function-2"
	if _, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{newer}, platform.AssetReleaseContext{}); err == nil {
		t.Fatal("missing source version family was fabricated at first deployment")
	}
	compute := platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetCompute, Name: "algorithm"}}
	if _, err := b.ReleaseDrafts([]platform.ReleaseAsset{compute}, platform.AssetReleaseContext{}); err == nil {
		t.Fatal("module descriptor fabricated source/compilation evidence")
	}
}

func TestWritebackRequestAfterRetainedDefinitionRollback(t *testing.T) {
	b, rows := lifecycleBuild()
	connection := Connection{Record: platform.Record{ID: "connection"}, Name: "erp", Title: "ERP", Kind: "http", Address: "https://target.example", State: "ready"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{connection}
	initial := Writeback{Record: platform.Record{ID: "writeback"}, Name: "delivery", Title: "Delivery", Connection: connection.ID, Object: "build.receipt", On: "create", Path: "v1/receipts", Result: []SourceField{{From: "document", To: "document"}}}
	v1, err := b.integrationAsset(platform.AssetWriteback, initial, 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := retainIntegration(initial, v1)
	if err != nil {
		t.Fatal(err)
	}
	updated := first.(Writeback)
	updated.Path = "v2/receipts"
	v2, err := b.integrationAsset(platform.AssetWriteback, updated, 2)
	if err != nil {
		t.Fatal(err)
	}
	second, err := retainIntegration(updated, v2)
	if err != nil {
		t.Fatal(err)
	}
	rolledBack, err := retainIntegration(second, v1)
	if err != nil {
		t.Fatal(err)
	}
	live := rolledBack.(Writeback)
	if live.DefinitionVersion != 2 || len(live.Versions) != 2 {
		t.Fatal("rollback discarded retained history")
	}
	execution, err := b.ExecutionWriteback(live)
	if err != nil {
		t.Fatal(err)
	}
	request, err := execution.Request(connection, "R", json.RawMessage(`{"item":"A"}`))
	if err != nil || request.DefinitionVersion != 1 || request.URL != "https://target.example/v1/receipts" || request.Result[0].To != "document" {
		t.Fatal("new request claimed the largest historical ordinal instead of its actual accepted definition", request, err)
	}
	// A malformed new publication never falls back to a historical counter.
	execution.Published = execution.Published[:len(execution.Published)-1]
	if _, err := execution.Request(connection, "R", json.RawMessage(`{}`)); err == nil {
		t.Fatal("malformed publication acquired a version from the mutable counter")
	}
	legacy := initial
	request, err = legacy.Request(connection, "R", json.RawMessage(`{}`))
	if err != nil || request.DefinitionVersion != 0 {
		t.Fatal("unversioned legacy request lost its original identity", request, err)
	}
}
