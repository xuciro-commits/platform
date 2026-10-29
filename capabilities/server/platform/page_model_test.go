package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestFrozenQueryObjectChoiceProjection(t *testing.T) {
	for _, body := range []string{`{"type":"sample.note","fields":[{"name":"bucket","type":"choice","choices":"A, B"}]}`, `{"type":"sample.note","fields":[{"name":"bucket","type":"choice","choices":["A","B"]}]}`} {
		object, err := queryObjectDescriptor([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		p := Page{Document: &PageDocument{Variables: map[string]PageVariable{}}}
		q := PageQuery{Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}, Limit: 10, Conditions: []PageQueryCondition{{Field: "bucket", Op: "=", Value: PageValue{Literal: json.RawMessage(`"B"`)}}}}
		if err := p.CheckQuerySchema(q, object, nil); err != nil {
			t.Fatal(err)
		}
		if len(object.Fields[0].Choices) != 2 || object.Fields[0].Choices[1] != "B" {
			t.Fatal("source choices changed during projection")
		}
	}
}
func TestQueryObjectDescriptorReadsConditionalFields(t *testing.T) {
	info, err := queryObjectDescriptor([]byte(`{"type":"build.item","fields":[{"name":"kind","type":"choice","choices":"refund,return,other"},{"name":"why","type":"text","when":"kind=refund,return"},{"name":"note","type":"text","when":""}]}`))
	if err != nil {
		t.Fatal(err)
	}
	why, ok := info.Field("why")
	if !ok || why.When == nil || why.When.Field != "kind" || len(why.When.In) != 2 {
		t.Fatalf("condition not read: %+v", why.When)
	}
	if note, _ := info.Field("note"); note.When != nil {
		t.Fatalf("an empty condition became %+v", note.When)
	}
}
func TestFrozenParameterizedLifecycleRetainsItsOriginalDestination(t *testing.T) {
	info, err := queryObjectDescriptor([]byte(`{"type":"sample.task","states":[{"name":"open"},{"name":"done"}],"actions":[{"name":"update","from":["open","done"],"toInput":"status","inputs":[{"name":"status","type":"choice","required":true,"choices":"open,done"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	move := info.Lifecycle.Transitions[0]
	if move.ToInput != "status" || len(move.To) != 2 {
		t.Fatal("frozen descriptor lost state input", move)
	}
	if _, err := queryObjectDescriptor([]byte(`{"type":"sample.task","states":[{"name":"open"}],"actions":[{"name":"update","from":["open"],"toInput":"status","inputs":[{"name":"status","type":"choice","required":true,"choices":"fake"}]}]}`)); err == nil {
		t.Fatal("frozen destination escaped original states")
	}
}
func TestCanonicalTransactionKanbanDestinationsRemainInLiveAndFrozenDescriptions(t *testing.T) {
	type task struct {
		Record
		State string `json:"state" field:"readonly"`
	}
	condition := ExprLiteralValue("boolean", true)
	for _, tc := range []struct {
		name     string
		value    Expr
		guard    *Expr
		when     *Expr
		repeated bool
		to       []string
		input    string
	}{
		{name: "input", value: ExprReference("text", "input.status"), to: []string{"open", "done"}, input: "status"},
		{name: "literal", value: ExprLiteralValue("text", "done"), to: []string{"done"}},
		{name: "guard", value: ExprReference("text", "input.status"), guard: &condition, to: []string{"open", "done"}, input: "status"},
		{name: "conditional update", value: ExprReference("text", "input.status"), when: &condition, to: []string{"open", "done"}},
		{name: "repeated update", value: ExprReference("text", "input.status"), repeated: true, to: []string{"open", "done"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := TxnStep{Name: "move", Kind: "update", Target: TxnRef{Type: "sample.task", ID: "$subject"}, When: tc.when, Changes: []TxnSet{{Field: "state", Expr: &tc.value}}}
			txn := &BusinessTxn{Subject: "sample.task", Guard: tc.guard, Steps: []TxnStep{step}}
			if tc.repeated {
				step.Name = "again"
				txn.Steps = append(txn.Steps, step)
			}
			transition := Transition{Name: "update", From: []string{"open", "done"}, To: []string{"open", "done"}, Payload: []Field{{Name: "status", Type: "string", Required: true, Choices: []string{"open", "done"}}}, Txn: txn,
				ApplyTxn: func(Caller, *pb.ChangeRecord, time.Time) *kernel.Error { return nil }}
			e := Entity{Type: "sample.task", Model: task{}, Lifecycle: &Lifecycle{Field: "state", Initial: "open", States: []State{{Name: "open"}, {Name: "done"}}, Transitions: []Transition{transition}}}
			live, err := Describe("sample", e, func(reflect.Type) string { return "" })
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{
				"type": e.Type, "states": e.Lifecycle.States,
				"actions": []any{map[string]any{"name": transition.Name, "from": transition.From, "transaction": txn,
					"inputs": []any{map[string]any{"name": "status", "type": "choice", "required": true, "choices": "open,done"}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := queryObjectDescriptor(body)
			if err != nil {
				t.Fatal(err)
			}
			for _, info := range []EntityInfo{live, frozen} {
				move := info.Lifecycle.Transitions[0]
				if move.ToInput != tc.input || !reflect.DeepEqual(move.To, tc.to) {
					t.Fatalf("lost canonical destination: got %#v, want %v from %q", move, tc.to, tc.input)
				}

			}
			if tc.input != "" {
				e.Lifecycle.Transitions[0].ApplyTxn = nil
				if _, err := Describe("sample", e, func(reflect.Type) string { return "" }); err == nil {
					t.Fatal("accepted canonical dynamic destination without its transaction executor")
				}
			}
		})
	}
}
func TestPageVariableSharedVectors(t *testing.T) {
	raw, err := os.ReadFile("pageui/variables.vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name      string
		UIProfile string
		Variables map[string]PageVariable
		Valid     bool
	}
	if err := json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, vector := range vectors {
		t.Run(vector.Name, func(t *testing.T) {
			d := &PageDocument{UIProfile: vector.UIProfile, Variables: vector.Variables}
			err := d.CheckVariables()
			if (err == nil) != vector.Valid {
				t.Fatalf("valid=%v: %v", vector.Valid, err)
			}
		})
	}
}
func interfacePage(name string) Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.record"}
	return Page{Name: name, Layout: "composed", Object: object, Sections: []Section{{ID: "detail", Widget: "detail", ConfigVersion: 1, RecordVariable: "record"}, {ID: "button", Widget: "button", ConfigVersion: 1}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"detail", "button"}}, "detail": {Kind: "widget", Section: "detail"}, "button": {Kind: "widget", Section: "button"}}, Variables: map[string]PageVariable{"record": {Scope: "page", Type: "record", Mode: "input"}, "visited": {Scope: "page", Type: "boolean", Mode: "constant", Initial: json.RawMessage(`true`)}, "returned": {Scope: "page", Type: "boolean", Mode: "state", Initial: json.RawMessage(`false`)}}, Interface: &PageInterface{Version: 1, Inputs: map[string]PagePort{"record": {Variable: "record", Type: "record", Object: &object, Required: true}}, Outputs: map[string]PagePort{"visited": {Variable: "visited", Type: "boolean", Required: true}}}, Events: []PageEventBinding{{Source: "button", Event: "click", Effects: []PageEffect{{Kind: "return"}}}}}}
}
func navigation(target string) *PageNavigation {
	return &PageNavigation{Page: AssetRef{App: "sample", Kind: AssetPage, Name: target}, InterfaceVersion: 1, Inputs: map[string]PageValue{"record": {Variable: "record"}}, Results: map[string]string{"visited": "returned"}}
}
func TestPageInterfacesAndNavigationTypes(t *testing.T) {
	p := interfacePage("source")
	target := interfacePage("target")
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*PageNavigation)
		want   string
	}{
		{"version", func(n *PageNavigation) { n.InterfaceVersion = 2 }, "version"},
		{"required", func(n *PageNavigation) { n.Inputs = nil }, "misses"},
		{"wrong input", func(n *PageNavigation) { n.Inputs["record"] = PageValue{Literal: json.RawMessage(`"A"`)} }, "type or object"},
		{"unknown input", func(n *PageNavigation) { n.Inputs["unknown"] = PageValue{Variable: "visited"} }, "type or object"},
		{"output", func(n *PageNavigation) { n.Results["visited"] = "record" }, "output"},
	} {
		t.Run(test.name, func(t *testing.T) {
			n := navigation("target")
			test.change(n)
			if err := CheckPageNavigation(p, target, *n); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("want %s, got %v", test.want, err)
			}
		})
	}
	n := navigation("target")
	if err := CheckPageNavigation(p, target, *n); err != nil {
		t.Fatal(err)
	}
	other := *target.Document.Interface.Inputs["record"].Object
	other.Name = "sample.other"
	port := target.Document.Interface.Inputs["record"]
	port.Object = &other
	target.Document.Interface.Inputs["record"] = port
	if err := CheckPageNavigation(p, target, *n); err == nil {
		t.Fatal("accepted wrong record object")
	}
}
func TestCandidateClosesExplicitPageNavigationCycles(t *testing.T) {
	a, b := interfacePage("a"), interfacePage("b")
	a.Document.Events[0].Effects[0] = PageEffect{Kind: "navigate", Navigate: navigation("b")}
	b.Document.Events[0].Effects[0] = PageEffect{Kind: "navigate", Navigate: navigation("a")}
	aa, err := PageReleaseAsset("sample", "1", a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := PageReleaseAsset("sample", "1", b)
	if err != nil {
		t.Fatal(err)
	}
	object := ReleaseAsset{Ref: a.Object, ContractVersion: 1, SourceVersion: "1", Requires: []AssetRef{}, Body: json.RawMessage(`{"type":"sample.record"}`)}
	candidate, err := Candidate([]AssetRef{aa.Ref}, []ReleaseAsset{aa, bb, object})
	if err != nil || len(candidate.Assets) != 3 {
		t.Fatalf("closure: %v %+v", err, candidate)
	}
	if _, err := ReadCandidate(candidate.ID, candidate.Bytes); err != nil {
		t.Fatal(err)
	}
	// An owner-added structural edge must not inherit the navigation-cycle exception.
	a.Document.Events[0] = PageEventBinding{Source: "button", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "returned", Value: json.RawMessage(`true`)}}}
	aa, _ = PageReleaseAsset("sample", "1", a)
	aa.Requires = append(aa.Requires, bb.Ref)
	if _, err := Candidate([]AssetRef{aa.Ref}, []ReleaseAsset{aa, bb, object}); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal(err)
	}
}

func TestRetainedPageProfilesAndOriginalContents(t *testing.T) {
	for _, profile := range pageWidgets.SupportedProfiles {
		t.Run(profile, func(t *testing.T) {
			raw := fmt.Sprintf(`{"name":"retained","title":"Retained","object":{"app":"sample","kind":"object","name":"sample.note"},"layout":"composed","listFields":null,"detailFields":null,"actions":null,"sections":[{"id":"body","widget":"text","configVersion":1,"text":"Original","width":"half","notice":{"title":"","enabled":false,"limit":0},"unknown":{"zero":0}}],"document":{"formatVersion":2,"uiProfile":%q,"root":"root","nodes":{"root":{"kind":"rows","children":["body"]},"body":{"kind":"widget","section":"body"}}}}`, profile)
			var original Page
			if err := json.Unmarshal([]byte(raw), &original); err != nil {
				t.Fatal(err)
			}
			before := Raw(original)
			digest, err := PageContentVersion(original)
			if err != nil {
				t.Fatal(err)
			}
			safe, err := original.Safety()
			if err != nil {
				t.Fatal(err)
			}
			if err := safe.CheckSafety(); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, Raw(original)) {
				t.Fatal("legacy projection changed original bytes")
			}
			if got, _ := PageContentVersion(safe); got != digest {
				t.Fatal("projected identity drifted")
			}
			var restored Page
			if err := json.Unmarshal(before, &restored); err != nil {
				t.Fatal(err)
			}
			if restored.Format != 0 || !bytes.Equal(before, Raw(restored)) {
				t.Fatal("persisted original was rewritten to the projection")
			}
			if got, _ := PageContentVersion(restored); got != digest {
				t.Fatal("original restore changed identity")
			}
			if !bytes.Contains(safe.WidgetSpec.Body, []byte(`"zero":0`)) || !bytes.Contains(safe.WidgetSpec.Body, []byte(`"enabled":false`)) {
				t.Fatal("opaque zero/false presentation disappeared")
			}
		})
	}
	p := queryPlanPage()
	p.Document.UIProfile = "platform.page.v2.999"
	if _, err := p.Safety(); err == nil {
		t.Fatal("unknown profile silently projected")
	}
	p.Document.UIProfile = PageUIProfile()
	p.Document.FormatVersion = 99
	if _, err := p.Safety(); err == nil {
		t.Fatal("unknown document format silently projected")
	}
}

func TestPageContentIdentityUsesCanonicalLiteralBytes(t *testing.T) {
	p := queryPlanPage()
	p.Document.Variables["raw"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: json.RawMessage(`{"b":2,"a":1}`)}
	first, err := PageContentVersion(p)
	if err != nil {
		t.Fatal(err)
	}
	v := p.Document.Variables["raw"]
	v.Initial = json.RawMessage(`{ "a":1,"b":2 }`)
	p.Document.Variables["raw"] = v
	second, err := PageContentVersion(p)
	if err != nil || first != second {
		t.Fatal("canonical literals changed original identity")
	}
	p.PresentationWords = map[string]string{"English": "中文"}
	if got, _ := PageContentVersion(p); got != first {
		t.Fatal("discovery dictionary altered original identity")
	}
}

func TestFrozenDocumentlessSectionsPreserveRetainedFields(t *testing.T) {
	var p Page
	raw := `{"name":"flat","title":"Flat","object":{"app":"sample","kind":"object","name":"sample.note"},"layout":"list-detail","listFields":["bucket"],"detailFields":["bucket"],"actions":[]}`
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatal(err)
	}
	safe := typedPage(t, p)
	if err := safe.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	if len(safe.DataBindings) != 2 || safe.ScopeVisibility.Owners["list"] == "" {
		t.Fatal("flat original did not retain scoped list/detail bindings")
	}
	if safe.DataBindings[0].Model == nil || safe.DataBindings[0].Model.Type != p.Object.Name {
		t.Fatal("flat source lost its original object")
	}
}

func TestModelBindingDefaultsMigrationAndDeclaredPaths(t *testing.T) {
	info := EntityInfo{Type: "sample.item", App: "sample", Display: "name", Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "parent", Type: "reference", Ref: "sample.item", Inverse: "children"}, {Name: "state", Type: "choice", ReadOnly: true}}, Lifecycle: &LifecycleInfo{Field: "state", States: []State{{Name: "ready"}, {Name: "done"}}}}
	lookup := func(typ string) (EntityInfo, bool) { return info, typ == info.Type }
	p := Page{Name: "model", Object: ModelObject(info.Type), Layout: "composed", Sections: []Section{{ID: "list", Widget: "table", Fields: []string{"parent.parent.name"}}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "list", Nodes: map[string]PageLayoutNode{"list": {Kind: "widget", Section: "list"}}}}
	before := Raw(p)
	safe := typedPage(t, p)
	if safe.DataBindings[0].Fields[0].Name != "" || !slices.Equal(safe.DataBindings[0].Fields[0].Path, []string{"parent", "parent", "name"}) {
		t.Fatal("legacy multihop field became a literal name")
	}
	if err := safe.CheckModelBindings(lookup, nil); err != nil {
		t.Fatal(err)
	}
	if err := p.NormalizeModelBindings(lookup); err != nil {
		t.Fatal(err)
	}
	if p.Format != 3 || bytes.Equal(before, Raw(p)) {
		t.Fatal("authoring did not produce a new typed revision")
	}
	one := Raw(p)
	if err := p.NormalizeModelBindings(lookup); err != nil || !bytes.Equal(one, Raw(p)) {
		t.Fatal("typed normalization is not idempotent")
	}
	for _, change := range []func(*ModelBinding){func(m *ModelBinding) { m.Type = "missing.type" }, func(m *ModelBinding) { m.Fields = []FieldRef{{Path: []string{"name", "name"}}} }, func(m *ModelBinding) { m.Fields = []FieldRef{{Path: slices.Repeat([]string{"parent"}, 17)}} }, func(m *ModelBinding) { m.Where = []Expr{ExprLiteralValue("text", "true")} }, func(m *ModelBinding) { value := ExprLiteralValue("boolean", true); m.Label = &value }, func(m *ModelBinding) {
		m.Fields = []FieldRef{{Name: "name", Slot: "label"}, {Name: "parent", Slot: "label"}}
	}} {
		copy := clonePage(t, p)
		change(copy.DataBindings[0].Model)
		if copy.CheckModelBindings(lookup, nil) == nil {
			t.Fatal("invalid model source survived publication validation")
		}
	}
	composite := p
	composite.DataBindings = []PageDataBinding{{ID: "graph", Kind: "composite", Models: map[string]ModelViewBinding{"nodes": {Model: ModelBinding{Type: info.Type}}}}}
	resolved := composite.ResolveBindings(lookup)
	roles := resolved.DataBindings[0].Models["nodes"].Roles
	if roles["label"].Name != "name" || roles["state"].Name != "state" {
		t.Fatal("original model metadata defaults were lost")
	}
	if len(composite.DataBindings[0].Models["nodes"].Roles) != 0 {
		t.Fatal("metadata resolution mutated original roles")
	}
}

func TestPageOpaquePresentationCannotCreateBindings(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	p.WidgetSpec.Body = Raw(map[string]any{"sections": []any{map[string]any{"object": ModelObject("secret.rows"), "function": map[string]any{"ref": map[string]any{"app": "ai", "kind": "function", "name": "hidden"}}, "actions": []any{map[string]any{"app": "core", "kind": "action", "name": "admin"}}}}, "document": map[string]any{"variables": map[string]any{"hidden": map[string]any{"type": "record"}}}})
	before := Raw(p.DataBindings)
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	safe := typedPage(t, p)
	if !bytes.Equal(before, Raw(safe.DataBindings)) {
		t.Fatal("opaque data manufactured a typed grant")
	}
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	for _, ref := range asset.Requires {
		if ref.App == "secret" || ref.App == "core" || ref.App == "ai" {
			t.Fatal("opaque presentation entered the dependency closure", ref)
		}
	}
	var encoded map[string]json.RawMessage
	if err := json.Unmarshal(Raw(safe), &encoded); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"sections", "document", "layout", "listFields", "actions"} {
		if _, exists := encoded[key]; exists {
			t.Fatal("current envelope leaked a legacy execution shape", key)
		}
	}
}

func TestPageExplicitEmptyActionCollectionsRemainDistinct(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	p.ActionTriggers = []PageActionTrigger{{ID: "inherit", Binding: "table"}, {ID: "forbid", Binding: "table", Actions: []AssetRef{}}}
	copy := clonePage(t, p)
	if copy.ActionTriggers[0].Actions != nil || copy.ActionTriggers[1].Actions == nil {
		t.Fatal("omitted and explicitly empty action collections collapsed")
	}
}

func TestPageStaticImageLiteralURLPolicy(t *testing.T) {
	for _, url := range []string{"", "/files/image", "https://example.com/image.png", "http://localhost:8495/image"} {
		if !ValidPageImageURL(url) {
			t.Fatal("valid image URL refused", url)
		}
	}
	for _, url := range []string{"javascript:alert(1)", "data:text/html,x", "//evil.example/image", "https://user@example.com/x", "https://example.com/a b", "/\\evil", "/%2fexternal", "https://example.com:99999/x"} {
		if ValidPageImageURL(url) {
			t.Fatal("unsafe image URL accepted", url)
		}
	}
}

func TestRetainedResourceBindingsKeepOriginalSources(t *testing.T) {
	for _, tc := range []struct{ name, widget, config, kind string }{
		{"scene", "scene-3d", `"scene":{"layers":[{"opacity":0}],"mappings":[{"id":"rotation","source":"sample","field":"temperature","inputMin":0}]}`, "record-mapping"},
		{"AI", "ai-assistant", `"ai":{"kind":"chatbot","replyField":"reply","questionVariable":"question","suggestions":["Explain"]}`, "ai"},
		{"image", "static-image", `"image":{"url":"/files/image","height":0,"caption":""}`, "image"},
		{"external", "external-frame", `"externalFrame":{"url":"https://example.com/report","origin":"https://example.com","height":0}`, "external-document"},
		{"graph", "graph-explorer", `"graphExplorer":{"objects":[{"object":{"app":"sample","kind":"object","name":"sample.note"},"labelField":"bucket"}],"relations":[],"outputs":[{"id":"row","object":{"app":"sample","kind":"object","name":"sample.note"},"variable":"selected"}]}`, "record-output"},
		{"observation", "observation", `"observation":{"kind":"table","timeField":"at","signals":[{"field":"value","unit":"C","group":"plant"}],"rowHeight":32,"metadata":{"plant":"plant","device":"device","asset":"asset","status":"status"}}`, "observation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{"name":"resources","title":"Resources","object":{"app":"sample","kind":"object","name":"sample.note"},"layout":"composed","listFields":null,"detailFields":null,"actions":null,"sections":[{"id":"view","widget":%q,"configVersion":1,%s}],"document":{"formatVersion":2,"uiProfile":"platform.page.v2.111","root":"view","nodes":{"view":{"kind":"widget","section":"view"}}}}`, tc.widget, tc.config)
			var original Page
			if err := json.Unmarshal([]byte(raw), &original); err != nil {
				t.Fatal(err)
			}
			before := Raw(original)
			safe := typedPage(t, original)
			found := false
			for _, resource := range safe.DataBindings[0].Resources {
				found = found || resource.Kind == tc.kind
			}
			if !found {
				t.Fatal("original resource was discarded", safe.DataBindings[0].Resources)
			}
			if !bytes.Equal(before, Raw(original)) {
				t.Fatal("resource projection rewrote original bytes")
			}
		})
	}
}

func TestRetainedOverlayAndUnusedWidgetKeepOriginalOwner(t *testing.T) {
	raw := []byte(`{"name":"retained","title":"Retained","object":{"app":"sample","kind":"object","name":"sample.note"},"layout":"composed","sections":[{"id":"date","widget":"date-input","configVersion":1,"dateKind":"date","dateVariable":"local"},{"id":"unused","widget":"date-input","configVersion":1,"dateKind":"date","dateVariable":"local"}],"document":{"formatVersion":2,"uiProfile":"platform.page.v2.111","root":"root","nodes":{"root":{"kind":"rows"},"panel-root":{"kind":"rows","children":["date-node"]},"date-node":{"kind":"widget","section":"date"},"unused-node":{"kind":"widget","section":"unused"}},"overlays":{"panel":{"root":"panel-root","kind":"modal","title":"Dates","openVariable":"open"}},"unusedWidgets":[{"node":"unused-node","parent":"panel-root"}],"variables":{"open":{"scope":"page","type":"boolean","mode":"state","initial":false},"local":{"scope":"overlay","owner":"panel","type":"string","mode":"state","initial":""}}}}`)
	var original Page
	if err := json.Unmarshal(raw, &original); err != nil {
		t.Fatal(err)
	}
	before := Raw(original)
	safe, err := original.Safety()
	if err != nil {
		t.Fatal(err)
	}
	if err := safe.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	parents := map[string]string{}
	for _, scope := range safe.ScopeVisibility.Scopes {
		parents[scope.ID] = scope.Parent
	}
	if parents["panel-root"] != "panel" || parents["unused-node"] != "panel-root" || safe.scopeOwner(safe.ScopeVisibility.Owners["unused"], "overlay") != "panel" {
		t.Fatal("overlay or dormant widget lost its original owner", safe.ScopeVisibility)
	}
	if !bytes.Equal(before, Raw(original)) {
		t.Fatal("scope projection rewrote retained input")
	}
}
