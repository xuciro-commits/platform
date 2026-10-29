package build

import (
	"encoding/json"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"platformserver/platform"
)

func TestPortableDefinitionsKeepPinnedHistoryAndInstallationBytes(t *testing.T) {
	sample := SampleSetRecord{Name: "example", Title: "Example", Samples: []platform.SampleRecords{{Type: "build.receipt", Records: []json.RawMessage{json.RawMessage(`{"id":"R","item":"sample"}`)}}}}
	for _, tc := range []struct {
		prefix      string
		kind        platform.AssetKind
		record      any
		descriptor  any
		current     func(*Build) ([]platform.ReleaseAsset, error)
		pinned      func(*Build, platform.AssetRef, string) (platform.ReleaseAsset, error)
		install     func(json.RawMessage) (platform.AssetInstallation, error)
		typ, schema string
	}{
		{"samples", platform.AssetSampleSet, &sample, sample.descriptor(), (*Build).sampleSetAssets, (*Build).sampleSetPinned, sampleSetInstallation, SampleSetType, SchemaSampleSet},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			b, rows := lifecycleBuild()
			asset, err := definitionReleaseAsset(platform.AssetRef{App: ID, Kind: tc.kind, Name: "example"}, ValueDefinitionVersion+"."+tc.prefix+"-1", tc.descriptor)
			if err != nil {
				t.Fatal(err)
			}
			second := asset
			second.SourceVersion = ValueDefinitionVersion + "." + tc.prefix + "-2"
			firstRaw, secondRaw := string(platform.Raw(asset)), string(platform.Raw(second))
			damaged := asset
			damaged.Body = json.RawMessage(`{}`)
			unrelated := damaged
			unrelated.SourceVersion = "not-requested"
			row := map[string]any{}
			if err := json.Unmarshal(platform.Raw(tc.record), &row); err != nil {
				t.Fatal(err)
			}
			row["id"], row["state"], row["definitionVersion"], row["published"], row["archived"] = "row", "published", 2, secondRaw, true
			for i, history := range [][]string{{"{", string(platform.Raw(unrelated)), firstRaw, secondRaw}, {string(platform.Raw(damaged)), firstRaw, secondRaw}} {
				row["versions"] = history
				if err := json.Unmarshal(platform.Raw(row), tc.record); err != nil {
					t.Fatal(err)
				}
				rows.rows[reflect.TypeOf(tc.record).Elem()] = []any{reflect.ValueOf(tc.record).Elem().Interface()}
				if current, err := tc.current(b); err != nil || len(current) != 0 {
					t.Fatal("archived asset appeared in current assets", current, err)
				}
				found, err := tc.pinned(b, asset.Ref, asset.SourceVersion)
				if i == 0 && (err != nil || !reflect.DeepEqual(found, asset)) || i == 1 && err == nil {
					t.Fatal("pinned read skipped matching damage or rejected unrelated history", found, err)
				}
			}
			row["archived"], row["versions"] = false, []string{firstRaw, secondRaw}
			image := json.RawMessage(" \n" + string(platform.Raw(row)) + "\n")
			installed, err := tc.install(image)
			if err != nil || installed.Ref != asset.Ref || installed.Schema != tc.schema || installed.Record != tc.typ+"/row" || string(installed.Image) != string(image) {
				t.Fatal("installation changed its identity or original image bytes", installed, err)
			}
			for _, invalid := range []struct {
				field string
				value any
			}{{"versions", []string{secondRaw, secondRaw}}, {"versions", []string{strings.ReplaceAll(firstRaw, `"name":"example"`, `"name":"other"`), secondRaw}}, {"published", " " + secondRaw}} {
				saved := row[invalid.field]
				row[invalid.field] = invalid.value
				if _, err := tc.install(platform.Raw(row)); err == nil {
					t.Fatal("installation accepted inconsistent history", invalid.field)
				}
				row[invalid.field] = saved
			}
		})
	}
}

func TestRetainedDeclarationAssetsPreserveDescriptorsAndRejectInvalidVersions(t *testing.T) {
	b, _ := lifecycleBuild()
	source := b.Manifest().Version
	query := Query{Name: "lookup", Title: "Lookup", Object: "build.receipt"}
	function := Function{Name: "advice", Title: "Advice", Description: "Review a receipt", Object: "build.receipt", Fields: []string{"item"}, Instructions: "Use the item", Output: []platform.Field{{Name: "summary", Type: "string", Required: true, Description: "Summary"}}, MaxInputBytes: 1024, MaxOutputBytes: 256, MaxTokens: 64, Roles: []string{User}}
	property := PropertyType{Name: "label", Title: "Label", Description: "Receipt label", Type: "text"}
	link := LinkType{Name: "related", Title: "Related", Description: "Related receipts", Parent: "build.receipt", Child: "build.receipt", Forward: "children", Reverse: "parent", Storage: "edge", Cardinality: "one-to-many", DeletePolicy: "owner", Roles: []string{Builder}}
	for _, tc := range []struct {
		prefix     string
		name       string
		kind       platform.AssetKind
		definition any
		contract   int
		release    func(int) (platform.ReleaseAsset, error)
		install    func(*Build, platform.ReleaseAsset) error
	}{
		{"query", "lookup", platform.AssetQuery, query.Definition(), 1, func(version int) (platform.ReleaseAsset, error) {
			f := query
			f.Version = version
			return queryReleaseAsset(f, source)
		}, (*Build).InstallQueryAsset},
		{"function", "advice", platform.AssetFunction, function.Definition(), 1, func(version int) (platform.ReleaseAsset, error) {
			f := function
			f.Version = version
			return functionReleaseAsset(f, source)
		}, (*Build).InstallFunctionAsset},
		{"property", "label", platform.AssetPropertyType, property.Definition(), 1, func(version int) (platform.ReleaseAsset, error) {
			f := property
			f.Version = version
			return propertyTypeReleaseAsset(f, source)
		}, (*Build).InstallPropertyTypeAsset},
		{"link", "related", platform.AssetLinkType, link.Definition(), 4, func(version int) (platform.ReleaseAsset, error) {
			f := link
			f.Version = version
			return linkTypeReleaseAsset(f, source)
		}, (*Build).InstallLinkTypeAsset},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			body, err := json.Marshal(tc.definition)
			if err != nil {
				t.Fatal(err)
			}
			for _, version := range []int{1, 64} {
				t.Run(strconv.Itoa(version), func(t *testing.T) {
					asset, err := tc.release(version)
					if err != nil {
						t.Fatal(err)
					}
					want, err := platform.WithDependencies(platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: tc.kind, Name: tc.name}, ContractVersion: tc.contract, SourceVersion: source + "." + tc.prefix + "-" + strconv.Itoa(version), Body: body})
					if err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(asset, want) || asset.Ref.App != ID || asset.Ref.Kind != tc.kind || b.definitionAssetVersion(asset, tc.kind, tc.prefix) != version {
						t.Fatal("release descriptor or version changed", asset, want)
					}
				})
			}
			asset, err := tc.release(1)
			if err != nil {
				t.Fatal(err)
			}
			for _, invalid := range []struct {
				name   string
				change func(*platform.ReleaseAsset)
			}{
				{"wrong prefix", func(a *platform.ReleaseAsset) { a.SourceVersion = source + ".other-1" }},
				{"zero", func(a *platform.ReleaseAsset) { a.SourceVersion = source + "." + tc.prefix + "-0" }},
				{"above limit", func(a *platform.ReleaseAsset) { a.SourceVersion = source + "." + tc.prefix + "-65" }},
				{"leading zero", func(a *platform.ReleaseAsset) { a.SourceVersion = source + "." + tc.prefix + "-01" }},
				{"wrong app", func(a *platform.ReleaseAsset) { a.Ref.App = "other" }},
				{"wrong kind", func(a *platform.ReleaseAsset) { a.Ref.Kind = platform.AssetPage }},
				{"different dependencies", func(a *platform.ReleaseAsset) {
					a.Requires = []platform.AssetRef{{App: ID, Kind: platform.AssetObject, Name: "build.other"}}
				}},
			} {
				t.Run(invalid.name, func(t *testing.T) {
					candidate := asset
					invalid.change(&candidate)
					b, _ := lifecycleBuild()
					if b.definitionAssetVersion(candidate, tc.kind, tc.prefix) != 0 || tc.install(b, candidate) == nil {
						t.Fatal("invalid release asset was accepted", candidate)
					}
					if len(b.queries)+len(b.functions)+len(b.propertyTypes)+len(b.linkTypes) != 0 {
						t.Fatal("rejected release asset changed installed definitions")
					}
				})
			}
		})
	}
}

func TestRetainedDeclarationsSelectOldRuntimeAndKeepNextOrdinal(t *testing.T) {
	t.Run("query", func(t *testing.T) {
		b, rows := lifecycleBuild()
		b.installed["build.receipt"] = Entity(Object{Name: "receipt", Title: "Receipt", Fields: []Field{{Name: "item", Title: "Item", Type: "text"}}})
		v1 := Query{Record: platform.Record{ID: "Q"}, Name: "lookup", Title: "Original", Object: "build.receipt", State: "published", Version: 1}
		v2 := v1
		v2.Version, v2.Title = 2, "Latest"
		record := v2
		record.Published, record.Versions = published(v2), []string{published(v1), published(v2)}
		rows.rows[reflect.TypeFor[Query]()] = []any{record}
		b.queries[v2.Name] = v2
		asset, err := queryReleaseAsset(v1, b.Manifest().Version)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := b.prepareQueryReleasePublications([]platform.ReleaseAsset{asset})
		if err != nil || len(plans) != 1 {
			t.Fatal(plans, err)
		}
		selected, err := queryImage(plans[0].Image)
		if err != nil || selected.Version != 1 || selected.Title != "Original" {
			t.Fatal(selected, err)
		}
		if json.Unmarshal(plans[0].Image, &record) != nil || record.Version != 2 || len(record.Versions) != 2 {
			t.Fatal("rollback lost query history")
		}
		rows.rows[reflect.TypeFor[Query]()] = []any{record}
		b.queries[v1.Name] = selected
		_, next, _, ref, _, err := b.queryDraftAssets("Q")
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range next {
			if a.Ref == ref && !strings.HasSuffix(a.SourceVersion, ".query-3") {
				t.Fatal("query reused a historical ordinal")
			}
		}
	})
	t.Run("function", func(t *testing.T) {
		b, rows := lifecycleBuild()
		v1 := Function{Record: platform.Record{ID: "F"}, Name: "advice", Title: "Original", Description: "Review a record", Object: "build.receipt", Fields: []string{"item"}, Instructions: "Use the item", Output: []platform.Field{{Name: "summary", Type: "string", Required: true, Description: "Summary"}}, MaxInputBytes: 1024, MaxOutputBytes: 256, MaxTokens: 64, Roles: []string{User}, State: "published", Version: 1}
		v2 := v1
		v2.Version, v2.Title = 2, "Latest"
		record := v2
		record.Published, record.Versions = published(v2), []string{published(v1), published(v2)}
		rows.rows[reflect.TypeFor[Function]()] = []any{record}
		b.functions[v2.Name] = v2
		asset, err := functionReleaseAsset(v1, b.Manifest().Version)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := b.prepareFunctionReleasePublications([]platform.ReleaseAsset{asset})
		if err != nil || len(plans) != 1 {
			t.Fatal(plans, err)
		}
		selected, err := functionImage(plans[0].Image)
		if err != nil || selected.Version != 1 || selected.Title != "Original" {
			t.Fatal(selected, err)
		}
		if json.Unmarshal(plans[0].Image, &record) != nil || record.Version != 2 || len(record.Versions) != 2 {
			t.Fatal("rollback lost function history")
		}
	})
	t.Run("property", func(t *testing.T) {
		b, rows := lifecycleBuild()
		v1 := PropertyType{Record: platform.Record{ID: "P"}, Name: "label", Title: "Original", Description: "Shared receipt label", Type: "text", State: "published", Version: 1}
		v2 := v1
		v2.Version, v2.Title = 2, "Latest"
		record := v2
		record.Published, record.Versions = published(v2), []string{published(v1), published(v2)}
		rows.rows[reflect.TypeFor[PropertyType]()] = []any{record}
		b.propertyTypes[v2.Name] = v2
		asset, err := propertyTypeReleaseAsset(v1, b.Manifest().Version)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := b.preparePropertyTypeReleasePublications([]platform.ReleaseAsset{asset})
		if err != nil || len(plans) != 1 {
			t.Fatal(plans, err)
		}
		selected, err := propertyTypeImage(plans[0].Image)
		if err != nil || selected.Version != 1 || selected.Title != "Original" {
			t.Fatal(selected, err)
		}
		if json.Unmarshal(plans[0].Image, &record) != nil || record.Version != 2 || len(record.Versions) != 2 {
			t.Fatal("rollback lost property history")
		}
	})
	t.Run("link", func(t *testing.T) {
		b, rows := lifecycleBuild()
		v1 := LinkType{Record: platform.Record{ID: "L"}, Name: "related", Title: "Original", Description: "Related receipts", Parent: "build.receipt", Child: "build.receipt", Forward: "children", Reverse: "parent", Storage: "edge", Cardinality: "one-to-many", DeletePolicy: "owner", Roles: []string{Builder}, State: "published", Version: 1}
		v2 := v1
		v2.Version, v2.Title = 2, "Latest"
		record := v2
		record.Published, record.Versions = published(v2), []string{published(v1), published(v2)}
		rows.rows[reflect.TypeFor[LinkType]()] = []any{record}
		b.linkTypes[v2.Name] = v2
		asset, err := linkTypeReleaseAsset(v1, b.Manifest().Version)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := b.prepareLinkTypeReleasePublications([]platform.ReleaseAsset{asset})
		if err != nil || len(plans) != 1 {
			t.Fatal(plans, err)
		}
		selected, err := linkTypeImage(plans[0].Image)
		if err != nil || selected.Version != 1 || selected.Title != "Original" {
			t.Fatal(selected, err)
		}
		if json.Unmarshal(plans[0].Image, &record) != nil || record.Version != 2 || len(record.Versions) != 2 {
			t.Fatal("rollback lost link history")
		}
	})
	t.Run("agent", func(t *testing.T) {
		b, rows := lifecycleBuild()
		b.queries["lookup"] = Query{Name: "lookup", Title: "Lookup", Object: "build.receipt", Version: 1}
		v1 := Agent{Record: platform.Record{ID: "A"}, Name: "assistant", Title: "Original", Description: "Review receipts", Instructions: "Use the lookup", Tools: []string{"query:lookup"}, Version: 1, State: "published"}
		v2 := v1
		v2.Version, v2.Title = 2, "Latest"
		record := v2
		record.Published, record.Versions = published(v2), []string{published(v1), published(v2)}
		rows.rows[reflect.TypeFor[Agent]()] = []any{record}
		b.agents[v2.Name] = v2
		asset, err := agentReleaseAsset(v1, b.Manifest().Version)
		if err != nil {
			t.Fatal(err)
		}
		plans, err := b.prepareAgentReleasePublications([]platform.ReleaseAsset{asset})
		if err != nil || len(plans) != 1 {
			t.Fatal(plans, err)
		}
		if err := b.ApplyAcceptedPublication(SchemaAgentPublish, plans[0].Image); err != nil {
			t.Fatal(err)
		}
		if b.agents[v1.Name].Version != 1 || b.agents[v1.Name].Title != "Original" {
			t.Fatal("agent runtime did not select the original definition")
		}
		if json.Unmarshal(plans[0].Image, &record) != nil || record.Version != 2 || len(record.Versions) != 2 {
			t.Fatal("rollback lost agent history")
		}
	})
}
