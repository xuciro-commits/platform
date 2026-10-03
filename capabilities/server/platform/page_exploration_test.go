package platform

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func explorationPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.asset"}
	sensor := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.sensor"}
	alert := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.alert"}
	sensors := AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetLinkType, Name: "asset-sensors"}, SourceVersion: "1.link-1"}
	alerts := AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetLinkType, Name: "asset-alerts"}, SourceVersion: "1.link-1"}
	p := Page{Name: "explore", Object: object, Layout: "composed", Sections: []Section{
		{ID: "resources", Widget: "resource-list", ConfigVersion: 1, CollectionVariable: "assets", ResourceList: &PageResourceList{LabelField: "name", StatusField: "status", StatusTones: []PageEventTone{{Value: "Active", Tone: "success"}}}},
		{ID: "graph", Widget: "graph-explorer", ConfigVersion: 1, RecordVariable: "active", GraphExplorer: &PageGraphExplorer{Objects: []PageGraphObject{{Object: object, LabelField: "name"}, {Object: sensor, LabelField: "name"}, {Object: alert, LabelField: "name"}}, Relations: []PageGraphRelation{{ID: "sensors", Binding: sensors}, {ID: "alerts", Binding: alerts}}, Outputs: []PageGraphOutput{{ID: "asset", Object: object, Variable: "assetOutput"}, {ID: "sensor", Object: sensor, Variable: "sensorOutput"}, {ID: "alert", Object: alert, Variable: "alertOutput"}}}},
		{ID: "vertex", Widget: "vertex-graph", ConfigVersion: 1, RecordVariable: "active", VertexGraph: &PageVertexGraph{Groups: []PageNeighborhoodGroup{{ID: "sensors", Binding: sensors, Direction: "forward", Limit: 4, Badge: "S", Tone: "success"}, {ID: "alerts", Binding: alerts, Direction: "forward", Limit: 3, Badge: "A", Tone: "danger"}}}},
		{ID: "directory", Widget: "asset-directory", ConfigVersion: 1, AssetDirectory: &PageAssetDirectory{Items: []PageAssetDirectoryItem{{ID: "assets", Label: "Operations / Assets", Asset: AssetBinding{Ref: object, SourceVersion: "1"}}, {ID: "alerts", Label: "Operations / Alerts", Asset: AssetBinding{Ref: alert, SourceVersion: "1"}}, {ID: "playbook", Label: "Playbooks / SOP-14", Asset: sensors}, {ID: "dataset", Label: "Datasets / telemetry_q3", Asset: alerts}}}},
		{ID: "sensorCard", Widget: "record-card", ConfigVersion: 1, Object: sensor, RecordVariable: "sensorOutput", RecordCard: &PageRecordCard{LabelField: "name", Tone: "neutral"}},
	}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"resources", "graph", "vertex", "directory", "sensorCard"}}}, Variables: map[string]PageVariable{
		"assets": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "assets"}},
		"active": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "resources"}},
	}, Queries: map[string]PageQuery{"assets": {Object: object, Limit: 12, Sort: []string{"id"}}}}}
	for _, s := range p.Sections {
		p.Document.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	}
	for _, o := range p.Sections[1].GraphExplorer.Outputs {
		p.Document.Variables[o.Variable] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "graph", Port: o.ID}}
	}
	return p
}
func explorationLookup(p Page) map[AssetRef]ReleaseAsset {
	lookup := map[AssetRef]ReleaseAsset{}
	for _, o := range p.Sections[1].GraphExplorer.Objects {
		fields := []FieldInfo{{Name: "name", Type: "text"}}
		if o.Object == p.Object {
			fields = append(fields, FieldInfo{Name: "status", Type: "choice", Choices: []string{"Active", "Offline"}})
		} else {
			fields = append(fields, FieldInfo{Name: "asset", Type: "reference", Ref: p.Object.Name})
		}
		lookup[o.Object] = ReleaseAsset{Ref: o.Object, SourceVersion: "1", ContractVersion: 1, Body: Raw(EntityInfo{App: o.Object.App, Type: o.Object.Name, Fields: fields})}
	}
	for index, r := range p.Sections[1].GraphExplorer.Relations {
		l := LinkType{Name: r.Binding.Ref.Name, Title: r.ID, Description: "Original relationship", Parent: p.Object, Child: p.Sections[1].GraphExplorer.Objects[index+1].Object, Via: "asset", Forward: r.ID, Reverse: "asset", Storage: "reference", Cardinality: "one-to-many", DeletePolicy: "owner"}
		lookup[r.Binding.Ref] = ReleaseAsset{Ref: r.Binding.Ref, SourceVersion: r.Binding.SourceVersion, ContractVersion: 1, Body: Raw(l), Requires: []AssetRef{l.Parent, l.Child}}
	}
	return lookup
}
func TestExplorationFiniteOriginalContractsAndTypedOutputs(t *testing.T) {
	p := explorationPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	sensor := p.Sections[1].GraphExplorer.Objects[1].Object
	if p.RecordResourceObject("sensorOutput") != sensor || p.RecordVariableObject("sensorOutput") != sensor.Name {
		t.Fatal("heterogeneous graph output used the root type")
	}
	if err := checkFrozenQueries(p, explorationLookup(p)); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.78" }},
		{"wrong list widget", func(p *Page) { p.Sections[0].Widget = "record-list" }},
		{"wrong graph widget", func(p *Page) { p.Sections[1].Widget = "text" }},
		{"wrong directory widget", func(p *Page) { p.Sections[3].Widget = "text" }},
		{"wrong neighborhood widget", func(p *Page) { p.Sections[2].Widget = "text" }},
		{"window count", func(p *Page) { q := p.Document.Queries["assets"]; q.Limit = 13; p.Document.Queries["assets"] = q }},
		{"offset", func(p *Page) { q := p.Document.Queries["assets"]; q.Offset = 1; p.Document.Queries["assets"] = q }},
		{"order", func(p *Page) {
			q := p.Document.Queries["assets"]
			q.Sort = []string{"name", "id"}
			p.Document.Queries["assets"] = q
		}},
		{"status tone", func(p *Page) { p.Sections[0].ResourceList.StatusTones[0].Tone = "purple" }},
		{"duplicate tone", func(p *Page) {
			p.Sections[0].ResourceList.StatusTones = append(p.Sections[0].ResourceList.StatusTones, p.Sections[0].ResourceList.StatusTones[0])
		}},
		{"directory budget", func(p *Page) {
			p.Sections[3].AssetDirectory.Items = append(p.Sections[3].AssetDirectory.Items, p.Sections[3].AssetDirectory.Items[0])
		}},
		{"directory source", func(p *Page) { p.Sections[3].AssetDirectory.Items[0].Asset.SourceVersion = "" }},
		{"directory record", func(p *Page) { p.Sections[3].RecordVariable = "active" }},
		{"neighborhood reverse", func(p *Page) { p.Sections[2].VertexGraph.Groups[0].Direction = "reverse" }},
		{"neighborhood limit", func(p *Page) { p.Sections[2].VertexGraph.Groups[0].Limit = 5 }},
		{"neighborhood fake badge", func(p *Page) { p.Sections[2].VertexGraph.Groups[1].Badge = "X" }},
		{"missing output port", func(p *Page) {
			v := p.Document.Variables["sensorOutput"]
			v.Source.Port = ""
			p.Document.Variables["sensorOutput"] = v
		}},
		{"foreign output port", func(p *Page) {
			v := p.Document.Variables["sensorOutput"]
			v.Source.Port = "foreign"
			p.Document.Variables["sensorOutput"] = v
		}},
		{"wrong output producer", func(p *Page) {
			v := p.Document.Variables["sensorOutput"]
			v.Source.Section = "resources"
			p.Document.Variables["sensorOutput"] = v
		}},
		{"writable output", func(p *Page) {
			v := p.Document.Variables["sensorOutput"]
			v.Writable = true
			p.Document.Variables["sensorOutput"] = v
		}},
		{"unlisted output object", func(p *Page) {
			p.Sections[1].GraphExplorer.Outputs[1].Object = AssetRef{App: "other", Kind: AssetObject, Name: "sample.sensor"}
		}},
		{"duplicate output object", func(p *Page) { p.Sections[1].GraphExplorer.Outputs[1].Object = p.Object }},
		{"duplicate output variable", func(p *Page) { p.Sections[1].GraphExplorer.Outputs[1].Variable = "assetOutput" }},
		{"record port on list", func(p *Page) {
			v := p.Document.Variables["active"]
			v.Source.Port = "asset"
			p.Document.Variables["active"] = v
		}},
		{"graph self-feedback", func(p *Page) { p.Sections[1].RecordVariable = "assetOutput" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := explorationPage()
			tc.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid exploration accepted")
			}
		})
	}
	if (*PageDocument)(nil).Check(p.Sections) == nil {
		t.Fatal("documentless exploration accepted")
	}
	p = explorationPage()
	p.Sections[1].Object = AssetRef{App: "foreign", Kind: AssetObject, Name: p.Object.Name}
	if p.CheckRecordPorts() == nil {
		t.Fatal("matching typename lost full root owner")
	}
}
func TestExplorationFrozenDependenciesAndOriginalSchema(t *testing.T) {
	p := explorationPage()
	lookup := explorationLookup(p)
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	for ref := range lookup {
		if !slices.Contains(asset.Requires, ref) {
			t.Fatal("frozen exploration omitted bound dependency", ref)
		}
	}
	for _, kind := range []string{"missing", "version", "endpoint", "owner", "private-title", "private-status", "private-field"} {
		lookup = explorationLookup(p)
		r := p.Sections[1].GraphExplorer.Relations[0]
		object := p.Sections[1].GraphExplorer.Objects[1].Object
		switch kind {
		case "missing":
			delete(lookup, r.Binding.Ref)
		case "version":
			a := lookup[r.Binding.Ref]
			a.SourceVersion = "1.link-2"
			lookup[r.Binding.Ref] = a
		case "endpoint":
			a := lookup[r.Binding.Ref]
			var l LinkType
			json.Unmarshal(a.Body, &l)
			l.Parent = object
			a.Body = Raw(l)
			lookup[r.Binding.Ref] = a
		case "owner":
			a := lookup[object]
			a.Body = Raw(EntityInfo{App: "foreign", Type: object.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "asset", Type: "reference", Ref: p.Object.Name}}})
			lookup[object] = a
		case "private-title":
			a := lookup[object]
			a.Body = Raw(EntityInfo{App: object.App, Type: object.Name, Fields: []FieldInfo{{Name: "asset", Type: "reference", Ref: p.Object.Name}}})
			lookup[object] = a
		case "private-status":
			a := lookup[p.Object]
			a.Body = Raw(EntityInfo{App: p.Object.App, Type: p.Object.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}}})
			lookup[p.Object] = a
		case "private-field":
			a := lookup[object]
			a.Body = Raw(EntityInfo{App: object.App, Type: object.Name, Fields: []FieldInfo{{Name: "name", Type: "text"}}})
			lookup[object] = a
		}
		if checkFrozenQueries(p, lookup) == nil {
			t.Fatal("tampered original exploration accepted", kind)
		}
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "status", Type: "choice", Choices: []string{"Offline"}}}}
	if p.Sections[0].CheckResourceList(info) == nil {
		t.Fatal("invented original status choice accepted")
	}
	p.Sections[0].ResourceList.StatusTones = nil
	if err := p.Sections[0].CheckResourceList(info); err != nil {
		t.Fatal("unmapped original status formatter rejected", err)
	}
	d := &Definition{Query: &NamedQuery{Limit: 11}}
	if p.CheckResourceListQuery("assets", d) == nil {
		t.Fatal("named source silently truncated twelve records")
	}
}
func TestExplorationPrivateOutputProjectionAndFeedback(t *testing.T) {
	p := explorationPage()
	sensor := p.Sections[1].GraphExplorer.Objects[1].Object
	lookup := explorationLookup(p)
	object := func(ref AssetRef) (EntityInfo, bool) {
		var e EntityInfo
		a, ok := lookup[ref]
		json.Unmarshal(a.Body, &e)
		return e, ok && ref != sensor
	}
	link := func(b AssetBinding) (LinkType, bool) {
		var l LinkType
		a, ok := lookup[b.Ref]
		json.Unmarshal(a.Body, &l)
		return l, ok
	}
	graph, ok := p.ProjectExploration(p.Sections[1], object, link)
	if !ok || len(graph.GraphExplorer.Objects) != 2 || len(graph.GraphExplorer.Relations) != 1 || len(graph.GraphExplorer.Outputs) != 2 {
		t.Fatal("hidden graph target was not cropped")
	}
	p.Sections[1] = graph
	visible := p.Document.Visible(p.Sections)
	if _, ok := visible.Variables["sensorOutput"]; ok {
		t.Fatal("private object retained its typed output resource")
	}
	if _, ok := visible.Nodes["sensorCard"]; ok {
		t.Fatal("private graph output retained consumer")
	}
	// Two graph roots may not feed each other's typed outputs.
	p = explorationPage()
	second := p.Sections[1]
	second.ID = "graphTwo"
	copy := *second.GraphExplorer
	copy.Outputs = []PageGraphOutput{{ID: "asset", Object: p.Object, Variable: "secondOutput"}}
	second.GraphExplorer = &copy
	second.RecordVariable = "assetOutput"
	p.Sections = append(p.Sections, second)
	p.Sections[1].RecordVariable = "secondOutput"
	p.Document.Nodes[second.ID] = PageLayoutNode{Kind: "widget", Section: second.ID}
	root := p.Document.Nodes["root"]
	root.Children = append(root.Children, second.ID)
	p.Document.Nodes["root"] = root
	p.Document.Variables["secondOutput"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: second.ID, Port: "asset"}}
	if err := p.Document.Check(p.Sections); err == nil || !strings.Contains(err.Error(), "cyclic") {
		t.Fatal("mutual graph output feedback accepted", err)
	}
}

func TestExplorationOriginalCodeAliasOwnerAndOverlayScope(t *testing.T) {
	p := explorationPage()
	lookup := explorationLookup(p)
	prior := p.Object
	alias := AssetRef{App: "relations", Kind: AssetObject, Name: "platform.comment"}
	p.Object = alias
	p.Sections[0].ResourceList.LabelField = "text"
	p.Sections[0].ResourceList.StatusField = "target"
	p.Sections[1].GraphExplorer.Objects[0] = PageGraphObject{Object: alias, LabelField: "text"}
	p.Sections[1].GraphExplorer.Outputs[0].Object = alias
	p.Sections[3].AssetDirectory.Items[0].Asset.Ref = alias
	q := p.Document.Queries["assets"]
	q.Object = alias
	p.Document.Queries["assets"] = q
	delete(lookup, prior)
	info := EntityInfo{App: alias.App, Type: alias.Name, Fields: []FieldInfo{{Name: "text", Type: "longtext"}, {Name: "target", Type: "text"}}}
	lookup[alias] = ReleaseAsset{Ref: alias, SourceVersion: "1", ContractVersion: 1, Body: Raw(map[string]any{"type": alias.Name, "fields": info.Fields, "entity": info})}
	for _, r := range p.Sections[1].GraphExplorer.Relations {
		a := lookup[r.Binding.Ref]
		var l LinkType
		json.Unmarshal(a.Body, &l)
		l.Parent = alias
		a.Body = Raw(l)
		lookup[r.Binding.Ref] = a
		a = lookup[l.Child]
		var child EntityInfo
		json.Unmarshal(a.Body, &child)
		for i, f := range child.Fields {
			if f.Name == "asset" {
				child.Fields[i].Ref = alias.Name
			}
		}
		a.Body = Raw(child)
		lookup[l.Child] = a
	}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := checkFrozenQueries(p, lookup); err != nil {
		t.Fatal("actual code alias owner was replaced by its typename prefix", err)
	}
	wrong := lookup[alias]
	wrong.Body = Raw(map[string]any{"type": alias.Name, "fields": info.Fields, "entity": EntityInfo{App: "platform", Type: alias.Name, Fields: info.Fields}})
	lookup[alias] = wrong
	if checkFrozenQueries(p, lookup) == nil {
		t.Fatal("frozen code actual owner was ignored")
	}
	p = explorationPage()
	d := p.Document
	d.Nodes["panelRoot"] = d.Nodes["root"]
	d.Nodes["root"] = PageLayoutNode{Kind: "rows", Children: []string{"trigger"}}
	d.Nodes["trigger"] = PageLayoutNode{Kind: "widget", Section: "trigger"}
	p.Sections = append(p.Sections, Section{ID: "trigger", Widget: "button", ConfigVersion: 1})
	for id, v := range d.Variables {
		v.Scope = "overlay"
		v.Owner = "panel"
		d.Variables[id] = v
	}
	q = d.Queries["assets"]
	q.Owner = "panel"
	d.Queries["assets"] = q
	d.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Overlays = map[string]PageOverlay{"panel": {Root: "panelRoot", Kind: "drawer", Title: "Original exploration", OpenVariable: "open"}}
	d.Events = []PageEventBinding{{Source: "trigger", Event: "click", Target: "open", Value: Raw(true)}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("original overlay exploration refused", err)
	}
	v := d.Variables["sensorOutput"]
	v.Owner = "foreign"
	d.Variables["sensorOutput"] = v
	if d.Check(p.Sections) == nil {
		t.Fatal("graph output escaped its original owner")
	}
	p = explorationPage()
	p.Sections[4].Object.App = "wrong"
	if p.CheckRecordPorts() == nil {
		t.Fatal("typed graph output consumer lost full object owner")
	}
}

func TestExplorationDirectoryOriginalNavigationAndDependencyClosure(t *testing.T) {
	p := explorationPage()
	home := AssetRef{App: "sample", Kind: AssetPage, Name: "home"}
	p.Sections[3].AssetDirectory.Items[0].Asset = AssetBinding{Ref: home, SourceVersion: "1"}
	p.Document.Events = []PageEventBinding{{Source: "directory", Control: "assets", Event: "click", Navigate: &PageNavigation{Page: home}}}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal("explicit original page navigation rejected", err)
	}
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	missing := slices.DeleteFunc(slices.Clone(asset.Requires), func(r AssetRef) bool { return r == home })
	if err := checkReleaseBindings(asset.Ref, asset.Body, missing); err == nil {
		t.Fatal("candidate accepted omitted original directory/navigation dependency")
	}
	for _, change := range []func(*PageDocument){
		func(d *PageDocument) { d.Events[0].Control = "playbook" },
		func(d *PageDocument) { d.Events[0].Navigate.Page.Name = "other" },
		func(d *PageDocument) {
			d.Events[0].Navigate.Inputs = map[string]PageValue{"record": {Variable: "active"}}
		},
	} {
		p := explorationPage()
		p.Sections[3].AssetDirectory.Items[0].Asset = AssetBinding{Ref: home, SourceVersion: "1"}
		p.Document.Events = []PageEventBinding{{Source: "directory", Control: "assets", Event: "click", Navigate: &PageNavigation{Page: home}}}
		change(p.Document)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("directory navigation did not retain exact original item")
		}
	}
	// An unavailable original reference field retires the typed output even when
	// its object's other fields are still readable.
	p = explorationPage()
	lookup := explorationLookup(p)
	sensor := p.Sections[1].GraphExplorer.Objects[1].Object
	a := lookup[sensor]
	var info EntityInfo
	json.Unmarshal(a.Body, &info)
	info.Fields = info.Fields[:1]
	a.Body = Raw(info)
	lookup[sensor] = a
	object := func(ref AssetRef) (EntityInfo, bool) {
		var e EntityInfo
		a, ok := lookup[ref]
		json.Unmarshal(a.Body, &e)
		return e, ok
	}
	link := func(b AssetBinding) (LinkType, bool) {
		var l LinkType
		a, ok := lookup[b.Ref]
		json.Unmarshal(a.Body, &l)
		return l, ok
	}
	graph, ok := p.ProjectExploration(p.Sections[1], object, link)
	if !ok || slices.ContainsFunc(graph.GraphExplorer.Outputs, func(o PageGraphOutput) bool { return o.Object == sensor }) {
		t.Fatal("hidden original relation field retained output provenance")
	}
}
