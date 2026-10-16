package build

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"platformserver/platform"
)

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
