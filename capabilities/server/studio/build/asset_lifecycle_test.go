package build

import (
	"encoding/json"

	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"platformkernel/kernel"

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
	platform.BuildHost
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
func (h lifecycleHost) Declares(string) bool                             { return false }
func (h lifecycleHost) ValidateInstallQuery(q platform.NamedQuery) error { return q.Check() }
func (h lifecycleHost) Action(schema string) (string, platform.Action, bool) {
	return ID, platform.Action{Schema: schema, Roles: []string{"writer"}}, strings.HasPrefix(schema, h.info.Type+".")
}
func lifecycleBuild() (*Build, *lifecycleRows) {
	rows := &lifecycleRows{rows: map[reflect.Type][]any{}}
	b := New("target")
	b.host = lifecycleHost{runtime: rows, member: platform.Member{ID: "target-builder", Tenant: "target", Roles: map[string]string{ID: Builder}}, info: platform.EntityInfo{App: ID, Type: "build.receipt", Fields: []platform.FieldInfo{{Name: "item", Type: "text"}, {Name: "document", Type: "text"}}}}
	return b, rows
}

func TestReleaseConnectionTargetsUseLogicalIdentityWithoutEnvironmentReadiness(t *testing.T) {
	b, _ := lifecycleBuild()
	ref := platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: "external"}
	asset, err := b.integrationAsset(platform.AssetConnection, Connection{Name: ref.Name, Kind: "http", Secret: "source-only"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		rows []Connection
		want string
	}{
		{name: "missing", want: ref.Name},
		{name: "unconfigured", rows: []Connection{{Record: platform.Record{ID: "local"}, Name: ref.Name, Kind: "http", State: "draft"}}, want: "local"},
		{name: "archived", rows: []Connection{{Record: platform.Record{ID: "retired", Archived: true}, Name: ref.Name, Kind: "http"}}, want: ref.Name},
		{name: "ambiguous", rows: []Connection{{Record: platform.Record{ID: "one"}, Name: ref.Name, Kind: "http"}, {Record: platform.Record{ID: "two"}, Name: ref.Name, Kind: "http"}}},
		{name: "incompatible kind", rows: []Connection{{Record: platform.Record{ID: "local"}, Name: ref.Name, Kind: "odata"}}},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner, rows := lifecycleBuild()
			for _, connection := range test.rows {
				rows.rows[reflect.TypeFor[Connection]()] = append(rows.rows[reflect.TypeFor[Connection]()], connection)
			}
			before := string(platform.Raw(rows.rows[reflect.TypeFor[Connection]()]))
			restore, err := owner.integrationTargets([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{})
			if test.want == "" {
				if err == nil {
					restore()
					t.Fatal("ambiguous or incompatible target accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if owner.releaseDraftIDs[ref] != test.want {
				t.Fatal("delivery replaced logical target identity", owner.releaseDraftIDs)
			}
			restore()
			if string(platform.Raw(rows.rows[reflect.TypeFor[Connection]()])) != before {
				t.Fatal("delivery changed local configuration")
			}
			drafts, err := owner.ReleaseDrafts([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{})
			if err != nil || len(drafts) != 1 || drafts[0].Type != ConnectionType {
				t.Fatal("delivery omitted the connection bootstrap", drafts, err)
			}
		})
	}
}

func TestConnectionExecutionRejectsUnreadyAddressAndCredentialWithoutBlockingDelivery(t *testing.T) {
	b, _ := lifecycleBuild()
	connection := Connection{Record: platform.Record{ID: "local"}, Name: "external", Kind: "http", State: "ready", Address: "https://target.example", Secret: "target-only"}
	asset, err := b.integrationAsset(platform.AssetConnection, connection, 1)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := retainIntegration(connection, asset)
	if err != nil {
		t.Fatal(err)
	}
	connection = saved.(Connection)
	for _, change := range []func(*Connection){
		func(c *Connection) { c.State = "draft" },
		func(c *Connection) { c.Address = "" },
		func(c *Connection) { c.Address = "ftp://target.example" },
		func(c *Connection) { c.Address = "https://user:password@target.example" },
		func(c *Connection) { c.Secret = "" },
	} {
		invalid := connection
		change(&invalid)
		if _, err := b.ConnectionConfiguration(invalid); err != nil {
			t.Fatal("local readiness blocked declaration projection", err)
		}
		if _, err := b.ExecutionConnection(invalid); err == nil {
			t.Fatal("unready external operation accepted", invalid)
		}
	}
	if _, err := b.ExecutionConnection(connection); err != nil {
		t.Fatal("configured operation refused", err)
	}
}

func TestFirstIntegrationPublicationDoesNotStartSourceOrPipeline(t *testing.T) {
	b, rows := lifecycleBuild()
	connection := Connection{Record: platform.Record{ID: "connection"}, Name: "external", Kind: "http", State: "draft"}
	input := Dataset{Record: platform.Record{ID: "input"}, Name: "raw", Title: "Raw"}
	output := Dataset{Record: platform.Record{ID: "output"}, Name: "result", Title: "Result"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{connection}
	rows.rows[reflect.TypeFor[Dataset]()] = []any{input, output}
	source := Source{Record: platform.Record{ID: "source"}, Name: "feed", Title: "Feed", Connection: connection.ID, Dataset: input.ID, URL: "rows"}
	pipeline := Pipeline{Record: platform.Record{ID: "pipeline"}, Name: "transform", Title: "Transform", Input: input.ID, OutputDataset: output.ID}
	rows.rows[reflect.TypeFor[Source]()] = []any{source}
	rows.rows[reflect.TypeFor[Pipeline]()] = []any{pipeline}
	for _, item := range []struct {
		kind   platform.AssetKind
		record any
	}{{platform.AssetSource, source}, {platform.AssetPipeline, pipeline}} {
		asset, err := b.integrationAsset(item.kind, item.record, 1)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{Member: platform.Member{ID: "publisher"}})
		if err != nil || len(plans) != 1 {
			t.Fatal("unconfigured integration could not be delivered", plans, err)
		}
		if err := b.ValidateAssetInstallation(plans[0]); err != nil {
			t.Fatal("publication required an external environment or execution grant", err)
		}
		var state struct {
			State, Puller, Runner string
			Requested             bool
		}
		if err := json.Unmarshal(plans[0].Image, &state); err != nil || state.State != "published" || state.Requested || state.Puller != "" || state.Runner != "" {
			t.Fatal("publication started an integration or assigned its publisher as runner", state, err)
		}
		member := platform.Member{ID: "scoped-publisher", Roles: map[string]string{ID: Publisher}, Scopes: []string{integrationType(item.kind) + ".publish"}}
		if _, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{Member: member}); err != nil {
			t.Fatal("publication scope required unrelated business execution permissions", err)
		}
		member.Scopes = []string{"build.unrelated.publish"}
		if _, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{Member: member}); err == nil || !strings.Contains(err.Error(), "token does not permit") {
			t.Fatal("release bypassed owner publication token scope", err)
		}
	}
}

func TestExplicitIntegrationRequestsRetainTheRequestingTokenScopes(t *testing.T) {
	b, rows := lifecycleBuild()
	connection := Connection{Record: platform.Record{ID: "connection"}, Name: "external", Kind: "http", State: "ready", Address: "https://external.example"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{connection}
	input := Dataset{Record: platform.Record{ID: "input"}, Name: "raw", Title: "Raw"}
	output := Dataset{Record: platform.Record{ID: "output"}, Name: "clean", Title: "Clean"}
	rows.rows[reflect.TypeFor[Dataset]()] = []any{input, output}
	source := Source{Record: platform.Record{ID: "source"}, Name: "feed", Connection: connection.ID, Dataset: input.ID, URL: "rows"}
	pipeline := Pipeline{Record: platform.Record{ID: "pipeline"}, Name: "transform", Input: input.ID, OutputDataset: output.ID}
	member := platform.Member{ID: "runner", Scopes: []string{"build.source.*", "build.pipeline.*", "build.dataset.*"}}
	caller := platform.NewCaller(rows, member, ID, false, false)
	if problem := b.requestPull(caller, &source, nil, time.Time{}); problem != nil {
		t.Fatal(problem)
	}
	if problem := requestRun(caller, &pipeline, nil, time.Time{}); problem != nil {
		t.Fatal(problem)
	}
	want := string(platform.Raw(member.Scopes))
	if source.Puller != member.ID || source.PullerScopes != want || pipeline.Runner != member.ID || pipeline.RunnerScopes != want {
		t.Fatal("explicit requests lost their token scopes", source, pipeline)
	}
	for _, item := range []struct {
		kind   platform.AssetKind
		record any
	}{{platform.AssetSource, source}, {platform.AssetPipeline, pipeline}} {
		asset, err := b.integrationAsset(item.kind, item.record, 1)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := retainIntegration(item.record, asset)
		if err != nil {
			t.Fatal(err)
		}
		switch record := saved.(type) {
		case Source:
			fixed, err := b.ExecutionSource(record)
			if err != nil || fixed.PullerScopes != want {
				t.Fatal("accepted source projection lost execution scopes", fixed, err)
			}
		case Pipeline:
			fixed, err := b.ExecutionPipeline(record)
			if err != nil || fixed.RunnerScopes != want {
				t.Fatal("accepted pipeline projection lost execution scopes", fixed, err)
			}
		}
	}
}

func TestNativeInstallationDecodePreservesOuterIdentityAndImage(t *testing.T) {
	b, _ := lifecycleBuild()
	for _, tc := range []struct {
		schema string
		typ    string
		kind   platform.AssetKind
		name   string
	}{
		{SchemaPublish, ObjectType, platform.AssetObject, "build.outer"},
		{SchemaRelease, PageType, platform.AssetPage, "outer"},
		{SchemaHandOver, AppType, platform.AssetApp, "outer"},
		{SchemaProcess, ProcessType, platform.AssetFlow, "build.outer"},
		{SchemaPropertyType, PropertyTypeType, platform.AssetPropertyType, "outer"},
		{SchemaLinkType, LinkTypeType, platform.AssetLinkType, "outer"},
		{SchemaQuery, QueryType, platform.AssetQuery, "outer"},
		{SchemaFunction, FunctionType, platform.AssetFunction, "outer"},
		{SchemaAgentPublish, AgentType, platform.AssetAgent, "outer"},
		{SchemaCodePublish, CodeType, platform.AssetCompute, "outer"},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			image := platform.Raw(map[string]any{"id": "row", "name": "outer", "published": string(platform.Raw(map[string]string{"name": "published"}))})
			decoded, err := b.DecodeAssetInstallation(tc.schema, image)
			if err != nil {
				t.Fatal(err)
			}
			want := platform.AssetRef{App: ID, Kind: tc.kind, Name: tc.name}
			if decoded.Ref != want || decoded.Schema != tc.schema || decoded.Record != tc.typ+"/row" || !slices.Equal(decoded.Image, image) {
				t.Fatal("installation lost its outer record identity or saved image", decoded)
			}
			image[0] = ' '
			if decoded.Image[0] != '{' {
				t.Fatal("installation image aliases its input buffer")
			}
		})
	}
	for _, tc := range []struct {
		name   string
		schema string
		image  json.RawMessage
	}{
		{"unknown schema", "build.unknown.publish", platform.Raw(map[string]string{"id": "row", "name": "outer"})},
		{"empty id", SchemaPublish, platform.Raw(map[string]string{"name": "outer"})},
		{"invalid json", SchemaPublish, json.RawMessage(`{`)},
		{"evaluation publication", SchemaEvaluationSetPublish, platform.Raw(TestPlan{Record: platform.Record{ID: "row"}, Name: "outer", DefinitionVersion: 1, Published: "broken", Versions: []string{"broken"}})},
		{"table publication", SchemaTablePublish, platform.Raw(Table{Record: platform.Record{ID: "row"}, Name: "outer", State: "published", Version: 1, Published: "broken"})},
		{"alert publication", SchemaAlertPublish, platform.Raw(Alert{Record: platform.Record{ID: "row"}, Name: "outer", State: "published", Published: "broken"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := b.DecodeAssetInstallation(tc.schema, tc.image); err == nil {
				t.Fatal("malformed native installation was accepted")
			}
		})
	}
}

func TestIntegrationInstallationDecodePreservesImagesAndRejectsMalformedRecords(t *testing.T) {
	b, rows := lifecycleBuild()
	connection := Connection{Record: platform.Record{ID: "connection"}, Name: "erp", Title: "ERP", Kind: "http", State: "ready"}
	dataset := Dataset{Record: platform.Record{ID: "dataset"}, Name: "raw", Title: "Raw"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{connection}
	rows.rows[reflect.TypeFor[Dataset]()] = []any{dataset}
	for _, tc := range []struct {
		kind   platform.AssetKind
		record any
	}{
		{platform.AssetConnection, connection},
		{platform.AssetSource, Source{Record: platform.Record{ID: "source"}, Name: "feed", Title: "Feed", Connection: connection.ID, Dataset: dataset.ID, URL: "rows"}},
		{platform.AssetDataset, dataset},
		{platform.AssetPipeline, Pipeline{Record: platform.Record{ID: "pipeline"}, Name: "landing", Title: "Landing", Input: dataset.ID, OutputObject: "build.receipt", Key: "id"}},
		{platform.AssetWriteback, Writeback{Record: platform.Record{ID: "writeback"}, Name: "delivery", Title: "Delivery", Connection: connection.ID, Object: "build.receipt", On: "create", Path: "receipts"}},
		{platform.AssetObservation, Observation{Record: platform.Record{ID: "observation"}, Name: "temperature", Title: "Temperature", Connection: connection.ID, Object: "build.receipt", DeviceField: "item", Signal: "temperature", Unit: "C", Path: "/observations", WindowSeconds: 60, ExpectedRetentionSeconds: 3600, ExpectedGranularitySeconds: 1, Roles: []string{Builder}}},
		{platform.AssetMatching, Match{Record: platform.Record{ID: "matching"}, Name: "identity", Title: "Identity", Object: "build.receipt", Keys: []MatchKey{{Fields: []string{"item"}}}}},
	} {
		t.Run(string(tc.kind), func(t *testing.T) {
			rows.rows[reflect.TypeOf(tc.record)] = []any{tc.record}
			inventory, err := b.integrationInventory(tc.kind)
			if err != nil || !reflect.DeepEqual(inventory, []any{tc.record}) {
				t.Fatal("integration inventory changed its record type or value", inventory, err)
			}
			asset, err := b.integrationAsset(tc.kind, tc.record, 1)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := retainIntegration(tc.record, asset)
			if err != nil {
				t.Fatal(err)
			}
			image := platform.Raw(saved)
			schema := integrationType(tc.kind) + ".publish"
			decoded, err := b.DecodeAssetInstallation(schema, image)
			if err != nil {
				t.Fatal(err)
			}
			if decoded.Ref != asset.Ref || decoded.Schema != schema || decoded.Record != integrationType(tc.kind)+"/"+integrationRecord(tc.record).ID || !slices.Equal(decoded.Image, image) {
				t.Fatal("decoded installation changed its identity or saved image", decoded)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(image, &fields); err != nil {
				t.Fatal(err)
			}
			fields["unknownInstallationField"] = json.RawMessage(`true`)
			if _, err := b.DecodeAssetInstallation(schema, platform.Raw(fields)); err == nil {
				t.Fatal("installation accepted an unknown record field")
			}
			delete(fields, "unknownInstallationField")
			_, _, published, _ := integrationMetadata(saved)
			fields["definitionVersions"] = platform.Raw([]string{published, published})
			if _, err := b.DecodeAssetInstallation(schema, platform.Raw(fields)); err == nil {
				t.Fatal("installation accepted a duplicate retained version family")
			}
			image[0] = ' '
			if decoded.Image[0] != '{' {
				t.Fatal("decoded installation retained the caller's mutable image buffer")
			}
		})
	}
}

func TestIntegrationInventoryReadsSecondPageFiltersArchivedAndKeepsLimit(t *testing.T) {
	b, rows := lifecycleBuild()
	records := make([]any, 1001)
	for i := range records {
		records[i] = Source{Record: platform.Record{ID: "source-" + strconv.Itoa(i), Archived: i == 0 || i == 499 || i >= 501}, Name: "feed" + strconv.Itoa(i)}
	}
	rows.rows[reflect.TypeFor[Source]()] = records[:501]
	inventory, err := b.integrationInventory(platform.AssetSource)
	if err != nil || len(inventory) != 499 {
		t.Fatal("integration inventory omitted a page or exposed archived records", len(inventory), err)
	}
	for i, record := range inventory {
		want := i + 1
		if want >= 499 {
			want++
		}
		if !reflect.DeepEqual(record, records[want]) {
			t.Fatal("integration inventory changed record order or included an archived record", record)
		}
	}
	rows.rows[reflect.TypeFor[Source]()] = records
	if _, err := b.integrationInventory(platform.AssetSource); err == nil {
		t.Fatal("integration inventory accepted more than 1000 saved definitions")
	}
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
	source := Source{Record: platform.Record{ID: "target-source"}, Name: "feed", Title: "Feed", Connection: conn.ID, Dataset: dataset.ID, URL: "rows", Since: "seq", Cursor: "91", Puller: "original-runner", PullerScopes: `["build.source.*","build.dataset.*"]`, Requested: true, State: "published"}
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
	ctx := platform.AssetReleaseContext{Member: platform.Member{ID: "target-builder"}, Upgrade: true}
	plans, err := b.PrepareReleaseAssets([]platform.ReleaseAsset{second}, ctx)
	if err != nil || len(plans) != 1 {
		t.Fatal(plans, err)
	}
	if err := b.ApplyAssetInstallation(plans[0], platform.AssetApplyContext{Private: true, Validate: true, Upgrade: true}); err != nil {
		t.Fatal("compatible title upgrade refused", err)
	}
	record, err := integrationInstallationRecord(platform.AssetSource, plans[0].Image)
	if err != nil || record.(Source).Cursor != "91" || record.(Source).Puller != "original-runner" || record.(Source).PullerScopes != source.PullerScopes || !record.(Source).Requested {
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
	pipeline := Pipeline{Record: platform.Record{ID: "pipeline"}, Name: "landing", Title: "Landing", Input: ds.ID, OutputObject: "build.receipt", Key: "id", Steps: []platform.TransformStep{}}
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
		{"expectations", func(p *Pipeline) {
			p.Expectations = []platform.TransformExpectation{{Column: "qty", Rule: "range", Value: "1..20"}}
		}, false},
		{"transform", func(p *Pipeline) {
			p.Steps = []platform.TransformStep{{Kind: "cast", Column: "qty", Type: "number"}}
		}, false},
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
	function, err := functionReleaseAsset(f, ValueDefinitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	property, err := propertyTypeReleaseAsset(PropertyType{Name: "itemcode", Title: "Item code", Description: "The external item identifier", Type: "text", Version: 1}, ValueDefinitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	query, err := queryReleaseAsset(Query{Name: "receipts", Title: "Receipts", Description: "Receipt rows", Object: "build.receipt", Limit: 50, Version: 1}, ValueDefinitionVersion)
	if err != nil {
		t.Fatal(err)
	}
	link, err := linkTypeReleaseAsset(LinkType{Name: "receiptitems", Title: "Receipt items", Description: "Receipt items", Parent: "build.receipt", Child: "build.item", Via: "receipt", Forward: "items", Reverse: "receipt", Version: 1}, ValueDefinitionVersion)
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

type confirmationBindingHost struct {
	lifecycleHost
	records map[string]any
}

func (h confirmationBindingHost) Record(ref string) (any, bool) {
	value, ok := h.records[ref]
	return value, ok
}

func TestConfirmationReleaseBindsStableAssetsOnTheStartingAction(t *testing.T) {
	b, _ := lifecycleBuild()
	b.host = confirmationBindingHost{lifecycleHost: b.host.(lifecycleHost), records: map[string]any{
		WritebackType + "/local-writeback": Writeback{Name: "submit-order"},
		SourceType + "/local-source":       Source{Name: "order-status"},
	}}
	object := Object{Name: "order", Title: "Order", States: []State{{Name: "ready", Title: "Ready"}}, Actions: []Action{
		{Name: "start", Title: "Start", From: []string{"ready"}, To: "ready", Transaction: &platform.BusinessTxn{Subject: "build.order", Confirmation: &platform.Confirmation{
			Parties: []platform.ConfirmationParty{{Name: "warehouse", Writeback: "local-writeback", Reconcile: &platform.ConfirmationRead{Source: "local-source"}}}, OnComplete: "finish",
		}}},
		{Name: "finish", Title: "Finish", From: []string{"ready"}, To: "ready"},
	}}
	object.ID = "order"
	object.Published = published(object)
	assets, err := releaseAssets([]Object{object}, nil, nil, nil, "1")
	if err != nil {
		t.Fatal(err)
	}
	assets, err = b.bindConfirmationAssets(assets)
	if err != nil {
		t.Fatal(err)
	}
	for _, asset := range assets {
		if asset.Ref.Kind == platform.AssetObject {
			if slices.ContainsFunc(asset.Requires, func(ref platform.AssetRef) bool {
				return ref.Kind == platform.AssetWriteback || ref.Kind == platform.AssetSource
			}) {
				t.Fatal("confirmation transports created an object/integration cycle")
			}
		}
		if asset.Ref.Name != "build.order.start" {
			continue
		}
		for _, ref := range []platform.AssetRef{
			{App: ID, Kind: platform.AssetWriteback, Name: "submit-order"},
			{App: ID, Kind: platform.AssetSource, Name: "order-status"},
			{App: ID, Kind: platform.AssetAction, Name: "build.order.finish"},
		} {
			if !slices.Contains(asset.Requires, ref) {
				t.Fatalf("starting action omitted its frozen transport %s", ref)
			}
		}
		if strings.Contains(string(asset.Body), "local-writeback") || strings.Contains(string(asset.Body), "local-source") {
			t.Fatal("portable action bindings retained mutable local identities")
		}
		if err := platform.CheckAssetDependencies(asset); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("starting action was not produced")
}

func TestIssuedResultSurvivesExplicitOwnerBindings(t *testing.T) {
	b, rows := lifecycleBuild()
	conn := Connection{Record: platform.Record{ID: "connection"}, Name: "sap", Kind: "http"}
	rows.rows[reflect.TypeFor[Connection]()] = []any{conn}
	source := Source{Record: platform.Record{ID: "source"}, Name: "feed", Title: "Feed", Connection: conn.ID, URL: "rows", Object: "build.receipt", Key: "id"}
	first, err := b.integrationAsset(platform.AssetSource, source, 1)
	if err != nil {
		t.Fatal(err)
	}
	var legacy SourceDefinition
	if err := json.Unmarshal(first.Body, &legacy); err != nil {
		t.Fatal(err)
	}
	legacy.ObjectBindings = nil
	first.Body, _ = json.Marshal(legacy)
	first.Provides = nil
	accepted, err := retainIntegration(source, first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := b.integrationAsset(platform.AssetSource, source, 2)
	if err != nil {
		t.Fatal(err)
	}
	current, err := retainIntegration(accepted, second)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := retainedExecutionEquivalent(current, first.SourceVersion); err != nil || !ok {
		t.Fatal("explicit owner metadata changed original execution semantics", ok, err)
	}
	// A different resolved owner still changes the frozen dependency identity.
	var changed SourceDefinition
	if err := json.Unmarshal(second.Body, &changed); err != nil {
		t.Fatal(err)
	}
	changed.ObjectBindings["build.receipt"] = platform.AssetRef{App: "other", Kind: platform.AssetObject, Name: "build.receipt"}
	second.Body, _ = json.Marshal(changed)
	second, err = platform.WithDependencies(second)
	if err != nil {
		t.Fatal(err)
	}
	other, err := retainIntegration(accepted, second)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := retainedExecutionEquivalent(other, first.SourceVersion); err != nil || ok {
		t.Fatal("a different object owner reinterpreted an issued result", ok, err)
	}
}

func TestLegacyIntegrationCandidateUsesFrozenReferences(t *testing.T) {
	b, rows := lifecycleBuild()
	object := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: "build.receipt"}
	producer := platform.AssetRef{App: ID, Kind: platform.AssetSource, Name: "feed"}
	definition := MatchingDefinition{Name: "identity", Title: "Receipt identity", Object: object.Name, Keys: []MatchKey{{Fields: []string{"item"}}}, Prefer: []Preference{{Field: "document", Producer: producer.Name}}}
	body, _ := json.Marshal(definition)
	original := platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetMatching, Name: definition.Name}, ContractVersion: 1, SourceVersion: integrationVersion(1, platform.AssetMatching), Requires: []platform.AssetRef{object, producer}, Body: body}
	raw, _ := json.Marshal(original)
	retained := Match{Record: platform.Record{ID: "matching"}, Name: definition.Name, Title: definition.Title, Object: definition.Object, Keys: definition.Keys, Prefer: definition.Prefer, State: "published", DefinitionVersion: 1, Published: string(raw), Versions: []string{string(raw)}}
	// Today's draft names a different object and producer. A pipeline also
	// reuses the old source name. None may reinterpret the accepted version.
	mutable := retained
	mutable.Object = "build.other"
	mutable.Prefer = []Preference{{Field: "document", Producer: "edited-feed"}}
	rows.rows[reflect.TypeFor[Match]()] = []any{mutable}
	rows.rows[reflect.TypeFor[Pipeline]()] = []any{Pipeline{Name: producer.Name, State: "draft"}}
	assets, err := b.integrationAssets()
	if err != nil || len(assets) != 1 {
		t.Fatal("legacy matching was not projected for a new candidate", assets, err)
	}
	projected := assets[0]
	var current MatchingDefinition
	if json.Unmarshal(projected.Body, &current) != nil || current.Producers[producer.Name] != producer || current.ObjectBindings[object.Name] != object || projected.Provides == nil || !slices.Equal(projected.Requires, original.Requires) {
		t.Fatal("projection did not retain the exact accepted identities", projected, current)
	}
	prerequisite := func(ref platform.AssetRef, body string) platform.ReleaseAsset {
		t.Helper()
		asset, err := platform.WithDependencies(platform.ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(body)})
		if err != nil {
			t.Fatal(err)
		}
		return asset
	}
	connection := platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: "sap"}
	available := append(slices.Clone(assets),
		prerequisite(object, `{"type":"build.receipt","fields":[{"name":"item","type":"text"},{"name":"document","type":"text"}]}`),
		prerequisite(producer, `{"name":"feed","title":"Feed","connection":"sap","url":"rows"}`),
		prerequisite(connection, `{"name":"sap","title":"SAP","kind":"http"}`),
	)
	candidate, err := platform.Candidate([]platform.AssetRef{projected.Ref}, available)
	if err != nil {
		t.Fatal("accepted matching could not enter a new candidate", err)
	}
	if _, err := platform.ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	kept, err := retainIntegration(retained, projected)
	if err != nil {
		t.Fatal("derived bindings created another integration version", err)
	}
	_, version, published, versions := integrationMetadata(kept)
	if version != 1 || published != string(raw) || len(versions) != 1 || versions[0] != string(raw) || retained.Published != string(raw) {
		t.Fatal("candidate projection rewrote original accepted bytes")
	}
	bad := original
	bad.Requires = slices.Clone(original.Requires)
	bad.Requires[1].App = "other"
	if _, err := integrationCandidateAsset(bad); err == nil {
		t.Fatal("a changed frozen producer owner was accepted")
	}
	changed := projected
	current.Producers[producer.Name] = platform.AssetRef{App: "other", Kind: platform.AssetSource, Name: producer.Name}
	changed.Body, _ = json.Marshal(current)
	changed, err = platform.WithDependencies(changed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := retainIntegration(retained, changed); err == nil {
		t.Fatal("a new producer owner replaced the same accepted version")
	}

	// The old virtual confirmation edge cannot be silently erased. Reading
	// old execution metadata remains valid; a new publication removes it.
	writebackBody, _ := json.Marshal(WritebackDefinition{Name: "confirm", Title: "Confirm", Connection: connection.Name, Object: object.Name, On: "confirmation"})
	oldWriteback := platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetWriteback, Name: "confirm"}, ContractVersion: 1, SourceVersion: integrationVersion(1, platform.AssetWriteback), Requires: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: object.Name + ".confirmation"}, connection, object}, Body: writebackBody}
	if err := ValueValidateIntegrationDescriptor(oldWriteback); err != nil {
		t.Fatal("original confirmation metadata became unreadable", err)
	}
	if _, err := integrationCandidateAsset(oldWriteback); err == nil || !strings.Contains(err.Error(), "publish a new writeback version") {
		t.Fatal("legacy virtual action did not require its explicit next publication", err)
	}
}

func TestDefinitionDescriptorReadsAcceptedVersionWithoutLiveHost(t *testing.T) {
	owner := New("definition-reader")
	ref := platform.AssetRef{App: ID, Kind: platform.AssetSampleSet, Name: "retained"}
	dependency := platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: "build.oldobject"}
	asset := platform.ReleaseAsset{Ref: ref, SourceVersion: "1.sample-1", ContractVersion: 1, Requires: []platform.AssetRef{dependency}, Provides: []platform.Capability{{Kind: "interface", Name: "retained"}}, Body: platform.Raw(platform.SampleSet{Name: "retained", Title: "Accepted original title"})}
	descriptor, err := owner.DefinitionDescriptor(asset)
	if err != nil || descriptor.Ref != ref || descriptor.Version != "1.sample-1" || descriptor.SampleSet == nil || descriptor.SampleSet.Title != "Accepted original title" || !slices.Equal(descriptor.Requires, asset.Requires) || !reflect.DeepEqual(descriptor.Provides, asset.Provides) {
		t.Fatalf("accepted descriptor changed: %+v %v", descriptor, err)
	}
	descriptor.Requires[0].Name = "changed"
	descriptor.Provides[0].Name = "changed"
	if asset.Requires[0] != dependency || asset.Provides[0].Name != "retained" {
		t.Fatal("discovery modified accepted dependency metadata")
	}
	asset.Body = json.RawMessage(`{damaged`)
	if _, err = owner.DefinitionDescriptor(asset); err == nil {
		t.Fatal("damaged accepted body was hidden by retained capability metadata")
	}
}
