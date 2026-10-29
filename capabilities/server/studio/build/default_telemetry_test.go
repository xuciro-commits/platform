package build

import (
	"encoding/json"
	"os"
	"platformserver/platform"
	"testing"
)

func completeTelemetry(t *testing.T) platform.Page {
	t.Helper()
	raw, err := os.ReadFile("testdata/default-telemetry.json")
	if err != nil {
		t.Fatal(err)
	}
	var p Page
	if err = json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return descriptor(p)
}
func TestCompleteDefaultTelemetryRetainsSceneSamplesAndSharedAsset(t *testing.T) {
	p := completeTelemetry(t)
	if len(p.Sections) != 15 || len(p.Document.Overlays) != 4 || len(p.Document.UnusedWidgets) != 2 {
		t.Fatal("complete original page was lost")
	}
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	asset, err := platform.PageReleaseAsset(ID, "1.page-1", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []platform.AssetRef{{App: "build", Kind: platform.AssetQuery, Name: "plant-readings"}, {App: "build", Kind: platform.AssetQuery, Name: "asset-readings"}, {App: "files", Kind: platform.AssetObject, Name: "files.file"}} {
		found := false
		for _, got := range asset.Requires {
			found = found || got == want
		}
		if !found {
			t.Fatalf("missing original dependency %v", want)
		}
	}
	var shared string
	for _, s := range p.Sections {
		if s.Widget == "scene-3d" {
			scene := retainedSectionConfig(t, s, "scene")
			if len(scene["layers"].([]any)) != 4 || len(scene["mappings"].([]any)) != 9 || p.Document.Variables[retainedSectionString(t, s, "fileVariable")].Scope != "page" || p.Document.Variables[retainedSectionString(t, s, "scenePartVariable")].Scope != "page" {
				t.Fatal("scene lost owned original configuration")
			}
			shared = s.RecordVariable
		}
	}
	for _, s := range p.Sections {
		if len(s.LegacyVisual["observation"]) == 0 {
			continue
		}
		observation := retainedSectionConfig(t, s, "observation")
		if len(observation["signals"].([]any)) != 100 {
			t.Fatal("original signal mapping was reduced")
		}
		if observation["kind"] == "table" && observation["assetOutput"] != shared || observation["kind"] == "statistics" && s.RecordVariable != shared {
			t.Fatal("telemetry no longer shares the original application asset")
		}
	}
}
func TestCompleteDefaultTelemetryRejectsWrongOwnerAndOldSharedProfile(t *testing.T) {
	for _, change := range []func(*platform.Page){func(p *platform.Page) { p.Document.UIProfile = "platform.page.v2.102" }, func(p *platform.Page) {
		for _, s := range p.Sections {
			if len(s.LegacyVisual["observation"]) > 0 {
				observation := retainedSectionConfig(t, s, "observation")
				if observation["kind"] != "table" {
					continue
				}
				id := observation["assetOutput"].(string)
				v := p.Document.Variables[id]
				v.Writable = false
				p.Document.Variables[id] = v
			}
		}
	}, func(p *platform.Page) {
		for _, s := range p.Sections {
			if s.Widget == "scene-3d" {
				v := p.Document.Variables[retainedSectionString(t, s, "scenePartVariable")]
				v.Scope = "application"
				p.Document.Variables[retainedSectionString(t, s, "scenePartVariable")] = v
			}
		}
	}, func(p *platform.Page) {
		for i := range p.Sections {
			if len(p.Sections[i].LegacyVisual["observation"]) > 0 {
				observation := retainedSectionConfig(t, p.Sections[i], "observation")
				if observation["kind"] != "statistics" {
					continue
				}
				observation["assetObject"].(map[string]any)["app"] = "foreign"
				p.Sections[i].LegacyVisual["observation"] = platform.Raw(observation)
			}
		}
	}} {
		p := completeTelemetry(t)
		change(&p)
		if p.CheckSafety() == nil && p.CheckRecordPorts() == nil {
			t.Fatal("accepted incompatible telemetry binding")
		}
	}
}

func TestTelemetryOverlayOwnsSceneStateWhileSharedTableRemainsMainPage(t *testing.T) {
	p := completeTelemetry(t)
	var scene platform.Section
	var leaf string
	for _, s := range p.Sections {
		if s.Widget == "scene-3d" {
			scene = s
		}
	}
	for id, n := range p.Document.Nodes {
		if n.Section == scene.ID {
			leaf = id
		}
	}
	for id, n := range p.Document.Nodes {
		var children []string
		for _, child := range n.Children {
			if child != leaf {
				children = append(children, child)
			}
		}
		if len(children) != len(n.Children) {
			n.Children = children
			p.Document.Nodes[id] = n
		}
	}
	p.Document.Nodes["twinOverlay"] = platform.PageLayoutNode{Kind: "rows", Children: []string{leaf}}
	p.Document.Variables["twinOpen"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
	p.Document.Overlays["twin"] = platform.PageOverlay{LegacyVisual: map[string]json.RawMessage{"title": platform.Raw("Twin")}, Kind: "drawer", Root: "twinOverlay", OpenVariable: "twinOpen"}
	for _, id := range []string{retainedSectionString(t, scene, "fileVariable"), retainedSectionString(t, scene, "scenePartVariable"), retainedSectionString(t, scene, "sceneSampleCollectionVariable")} {
		v := p.Document.Variables[id]
		v.Scope = "overlay"
		v.Owner = "twin"
		p.Document.Variables[id] = v
		if v.Source != nil && v.Source.Kind == "plan" {
			q := p.Document.Queries[v.Source.Query]
			q.Owner = "twin"
			p.Document.Queries[v.Source.Query] = q
		}
	}
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	// Reusing the original main-page part state cannot leak across owners.
	v := p.Document.Variables[retainedSectionString(t, scene, "scenePartVariable")]
	v.Scope = "page"
	v.Owner = ""
	p.Document.Variables[retainedSectionString(t, scene, "scenePartVariable")] = v
	if p.CheckSafety() == nil {
		t.Fatal("overlay consumed another owner's scene state")
	}
	p = completeTelemetry(t)
	var table platform.Section
	for _, s := range p.Sections {
		if len(s.LegacyVisual["observation"]) > 0 && retainedSectionConfig(t, s, "observation")["kind"] == "table" {
			table = s
		}
	}
	for id, n := range p.Document.Nodes {
		if n.Section == table.ID {
			leaf = id
		}
	}
	for id, n := range p.Document.Nodes {
		var children []string
		for _, child := range n.Children {
			if child != leaf {
				children = append(children, child)
			}
		}
		if len(children) != len(n.Children) {
			n.Children = children
			p.Document.Nodes[id] = n
		}
	}
	// A shared asset writer cannot acquire an overlay merely by moving the layout.
	p.Document.Nodes["tableOverlay"] = platform.PageLayoutNode{Kind: "rows", Children: []string{leaf}}
	p.Document.Variables["tableOpen"] = platform.PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: platform.Raw(false)}
	p.Document.Overlays["table"] = platform.PageOverlay{LegacyVisual: map[string]json.RawMessage{"title": platform.Raw("Samples")}, Kind: "drawer", Root: "tableOverlay", OpenVariable: "tableOpen"}
	if p.CheckSafety() == nil {
		t.Fatal("shared asset writer entered an overlay")
	}
}

func retainedSectionConfig(t *testing.T, section platform.Section, key string) map[string]any {
	t.Helper()
	var values map[string]any
	if err := json.Unmarshal(section.LegacyVisual[key], &values); err != nil {
		t.Fatal(err)
	}
	return values
}

func retainedSectionString(t *testing.T, section platform.Section, key string) string {
	t.Helper()
	var value string
	if err := json.Unmarshal(section.LegacyVisual[key], &value); err != nil {
		t.Fatal(err)
	}
	return value
}
