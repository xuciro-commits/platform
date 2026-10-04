package platform

import "testing"

func TestThreeSpatialWidgetsShareOriginalResourcesAndValidateSampleIdentity(t *testing.T) {
	p := embeddedTestPage("spatial")
	asset := p.Object
	sample := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.reading"}
	p.Sections = []Section{{ID: "map", Widget: "record-map", ConfigVersion: 1, CollectionVariable: "assets", Map: &PageRecordMap{LatitudeField: "lat", LongitudeField: "lng", LabelField: "name", ClusterEnabled: true}}, {ID: "image", Widget: "image-annotation", ConfigVersion: 1, RecordVariable: "active", FileVariable: "imageFile"}, {ID: "scene", Widget: "scene-3d", ConfigVersion: 1, RecordVariable: "active", FileVariable: "modelFile", ScenePartVariable: "part", SceneSampleCollectionVariable: "samples", Scene: &PageSceneConfig{Background: "dark", Quality: "balanced", Layers: []PageSceneLayer{}, Mappings: []PageSceneMapping{{ID: "drive", Node: "Part", Source: "sample", Field: "signal", Mode: "position", Axis: "x", InputMin: 0, InputMax: 20, OutputMin: 0, OutputMax: 1, Enabled: true}}, SampleAssetField: "asset", SampleTimeField: "at"}}}
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"map", "image", "scene"}}, "map": {Kind: "widget", Section: "map"}, "image": {Kind: "widget", Section: "image"}, "scene": {Kind: "widget", Section: "scene"}}
	p.Document.Queries = map[string]PageQuery{"assets": {Object: asset, Sort: []string{"id"}, Limit: 100}, "samples": {Object: sample, Query: &AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetQuery, Name: "latest"}, SourceVersion: "1.query-1"}, For: &PageValue{Variable: "active"}, Sort: []string{"-at", "id"}, Limit: 1}}
	p.Document.Variables = map[string]PageVariable{"assets": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "assets"}}, "active": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "map"}}, "samples": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "samples"}}}
	for _, id := range []string{"imageFile", "modelFile", "part"} {
		p.Document.Variables[id] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Type: asset.Name, App: asset.App, Fields: []FieldInfo{{Name: "lat", Type: "decimal"}, {Name: "lng", Type: "decimal"}, {Name: "name", Type: "text"}}}
	reading := EntityInfo{Type: sample.Name, App: sample.App, Fields: []FieldInfo{{Name: "asset", Type: "reference", Ref: asset.Name}, {Name: "at", Type: "datetime"}, {Name: "signal", Type: "decimal"}}}
	lookup := func(ref AssetRef) (EntityInfo, bool) {
		if ref == asset {
			return info, true
		}
		return reading, ref == sample
	}
	if err := p.Sections[0].CheckMap(info); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckSceneBinding(p.Sections[2], lookup); err != nil {
		t.Fatal(err)
	}
	visible := info
	visible.Fields = visible.Fields[:1]
	if p.Sections[0].CheckMap(visible) == nil {
		t.Fatal("hidden map coordinate accepted")
	}
	private := reading
	private.Fields = private.Fields[:2]
	if p.CheckSceneBinding(p.Sections[2], func(ref AssetRef) (EntityInfo, bool) {
		if ref == asset {
			return info, true
		}
		return private, ref == sample
	}) == nil {
		t.Fatal("hidden scene signal accepted")
	}
	reading.Fields[0].Ref = "sample.other"
	if p.CheckSceneBinding(p.Sections[2], lookup) == nil {
		t.Fatal("other asset reference accepted")
	}
	reading.Fields[0].Ref = asset.Name
	q := p.Document.Queries["samples"]
	q.Sort = []string{"at", "id"}
	p.Document.Queries["samples"] = q
	if p.CheckSceneBinding(p.Sections[2], lookup) == nil {
		t.Fatal("oldest sample became latest")
	}
	q.Sort = []string{"-at", "id"}
	p.Document.Queries["samples"] = q
	parent := p.Document.Variables["assets"]
	parent.Source = &PageResourceSource{Kind: "plan", Query: "samples"}
	p.Document.Variables["assets"] = parent
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("scene sample feedback accepted")
	}
	parent.Source = &PageResourceSource{Kind: "plan", Query: "assets"}
	p.Document.Variables["assets"] = parent
	p.Document.UIProfile = "platform.page.v2.86"
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("old profile accepted spatial widgets")
	}
	p.Document.UIProfile = PageUIProfile()
	v := p.Document.Variables["part"]
	v.Type = "record"
	p.Document.Variables["part"] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("record replaced writable part state")
	}
}
