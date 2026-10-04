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
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
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
			if len(s.Scene.Layers) != 4 || len(s.Scene.Mappings) != 9 || p.Document.Variables[s.FileVariable].Scope != "page" || p.Document.Variables[s.ScenePartVariable].Scope != "page" {
				t.Fatal("scene lost owned original configuration")
			}
			shared = s.RecordVariable
		}
	}
	for _, s := range p.Sections {
		if s.Observation == nil {
			continue
		}
		if len(s.Observation.Signals) != 100 {
			t.Fatal("original signal mapping was reduced")
		}
		if s.Observation.Kind == "table" && s.Observation.AssetOutput != shared || s.Observation.Kind == "statistics" && s.RecordVariable != shared {
			t.Fatal("telemetry no longer shares the original application asset")
		}
	}
}
func TestCompleteDefaultTelemetryRejectsWrongOwnerAndOldSharedProfile(t *testing.T) {
	for _, change := range []func(*platform.Page){func(p *platform.Page) { p.Document.UIProfile = "platform.page.v2.102" }, func(p *platform.Page) {
		for _, s := range p.Sections {
			if s.Observation != nil && s.Observation.Kind == "table" {
				v := p.Document.Variables[s.Observation.AssetOutput]
				v.Writable = false
				p.Document.Variables[s.Observation.AssetOutput] = v
			}
		}
	}, func(p *platform.Page) {
		for _, s := range p.Sections {
			if s.Widget == "scene-3d" {
				v := p.Document.Variables[s.ScenePartVariable]
				v.Scope = "application"
				p.Document.Variables[s.ScenePartVariable] = v
			}
		}
	}, func(p *platform.Page) {
		for i := range p.Sections {
			if p.Sections[i].Observation != nil && p.Sections[i].Observation.Kind == "statistics" {
				wrong := *p.Sections[i].Observation.AssetObject
				wrong.App = "foreign"
				p.Sections[i].Observation.AssetObject = &wrong
			}
		}
	}} {
		p := completeTelemetry(t)
		change(&p)
		if p.Document.Check(p.Sections) == nil && p.CheckRecordPorts() == nil {
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
	p.Document.Overlays["twin"] = platform.PageOverlay{Title: "Twin", Kind: "drawer", Root: "twinOverlay", OpenVariable: "twinOpen"}
	for _, id := range []string{scene.FileVariable, scene.ScenePartVariable, scene.SceneSampleCollectionVariable} {
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
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	// Reusing the original main-page part state cannot leak across owners.
	v := p.Document.Variables[scene.ScenePartVariable]
	v.Scope = "page"
	v.Owner = ""
	p.Document.Variables[scene.ScenePartVariable] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("overlay consumed another owner's scene state")
	}
	p = completeTelemetry(t)
	var table platform.Section
	for _, s := range p.Sections {
		if s.Observation != nil && s.Observation.Kind == "table" {
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
	p.Document.Overlays["table"] = platform.PageOverlay{Title: "Samples", Kind: "drawer", Root: "tableOverlay", OpenVariable: "tableOpen"}
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("shared asset writer entered an overlay")
	}
}
