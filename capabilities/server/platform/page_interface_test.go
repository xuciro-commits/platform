package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

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
