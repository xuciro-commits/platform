package platform

import (
	"bytes"
	"encoding/json"
	"slices"
	"strings"
	"testing"
)

func TestProposalFunctionClosureInstallsItsSourceModelFirst(t *testing.T) {
	object := ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetObject, Name: "build.reviewitem"}, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"type":"build.reviewitem","name":"reviewitem","fields":[{"name":"note","title":"Note","type":"text"}],"actions":[{"name":"adopt","adoption":{"function":{"name":"review"},"input":"proposal","mapping":[{"output":"summary","input":"summary"}]}}]}`)}
	definition := RecordAdviceFunction(object.Ref.Name, []string{"note"}, []string{"reader"})
	definition.Name = "review"
	function := ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetFunction, Name: "review"}, SourceVersion: "1.function-1", ContractVersion: 1, Body: Raw(definition)}
	var assets []ReleaseAsset
	for _, original := range []ReleaseAsset{object, function} {
		bound, err := WithDependencies(original)
		if err != nil {
			t.Fatal(err)
		}
		assets = append(assets, bound)
	}
	graph, err := NewReleaseDependencyGraph(assets)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := graph.Closure([]AssetRef{object.Ref})
	if err != nil || !slices.Contains(closure, object.Ref) || !slices.Contains(closure, function.Ref) {
		t.Fatalf("proposal closure lost an original dependency: %v %v", closure, err)
	}
	order, err := graph.InstallationOrder()
	if err != nil || len(order) != 2 || slices.Index(order, object.Ref) >= slices.Index(order, function.Ref) {
		t.Fatalf("the function was ordered before its source model: %v %v", order, err)
	}
}

func TestEnterpriseReferencesAreNotRecordAssetDependencies(t *testing.T) {
	object := ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetObject, Name: "build.assignment"}, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"type":"build.assignment","name":"assignment","fields":[{"name":"unit","title":"Unit","type":"reference","ref":"enterprise.element","stereotype":"ActualOrganization"}]}`)}
	action := ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetAction, Name: "build.assignment.assign"}, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"schema":"build.assignment.assign","target":"build.assignment","payload":[{"name":"unit","type":"string","ref":"enterprise.element","stereotype":"ActualOrganization"}]}`)}
	for _, asset := range []ReleaseAsset{object, action} {
		described, err := WithDependencies(asset)
		if err != nil {
			t.Fatal(err)
		}
		if slices.Contains(described.Requires, AssetRef{App: "enterprise", Kind: AssetObject, Name: "enterprise.element"}) {
			t.Fatalf("enterprise element became a fabricated record asset: %+v", described.Requires)
		}
		if asset.Ref.Kind == AssetAction && !slices.Contains(described.Requires, object.Ref) {
			t.Fatal("enterprise input removed the action's original object dependency")
		}
	}
}

func TestDependencyDescriptionDrivesBothDirectionsAndCapabilities(t *testing.T) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.order"}
	other := AssetRef{App: "build", Kind: AssetObject, Name: "build.supplier"}
	query := AssetRef{App: "build", Kind: AssetQuery, Name: "orders"}
	descriptions := []Dependencies{{Ref: object, Version: "1"}, {Ref: other, Version: "1"}, {Ref: query, Requires: []AssetRef{object}, Provides: []Capability{{Kind: "query", Name: "orders", Side: CapabilityPure}}, Version: "1"}}
	assert := func(target AssetRef, name string) {
		t.Helper()
		graph, err := NewDependencyGraph(descriptions)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(graph.Requires(query), []AssetRef{target}) || !slices.Equal(graph.UsedBy(target), []AssetRef{query}) {
			t.Fatal("directions disagree with the original description")
		}
		closure, err := graph.Closure([]AssetRef{query})
		if err != nil || !slices.Contains(closure, target) {
			t.Fatalf("closure: %v %v", closure, err)
		}
		impact, err := graph.Impact([]AssetRef{target})
		if err != nil || !slices.Contains(impact, query) {
			t.Fatalf("impact: %v %v", impact, err)
		}
		for _, description := range graph.Descriptors() {
			if description.Ref == query && (len(description.Provides) != 1 || description.Provides[0].Name != name) {
				t.Fatal("capability discovery disagrees with the original description")
			}
		}
	}
	assert(object, "orders")
	descriptions[2].Requires = []AssetRef{other}
	descriptions[2].Provides[0].Name = "supplier-orders"
	assert(other, "supplier-orders")
}

func TestPageDescriptionFollowsReferencesWithoutIndependentList(t *testing.T) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.order"}
	query := AssetRef{App: "build", Kind: AssetQuery, Name: "open-orders"}
	action := AssetRef{App: "build", Kind: AssetAction, Name: "build.order.submit"}
	page := Page{Name: "orders", Title: "Orders", Object: object, Layout: "composed", Actions: []AssetRef{action}, Sections: []Section{{ID: "orders", Widget: "table", Object: object, Query: query}}}
	asset, err := PageReleaseAsset("build", "1", page)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(asset.Requires, []AssetRef{action, object, query}) || asset.Provides == nil {
		t.Fatalf("page references were not derived: %+v", asset)
	}
	page.Sections[0].Query.Name = "all-orders"
	changed, err := PageReleaseAsset("build", "1", page)
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(changed.Requires, query) || !slices.Contains(changed.Requires, AssetRef{App: "build", Kind: AssetQuery, Name: "all-orders"}) {
		t.Fatal("changed page retained its old dependency")
	}
	asset.Requires = append(asset.Requires, AssetRef{App: "build", Kind: AssetDataset, Name: "unmentioned"})
	if err := checkAssetDescription(asset); err == nil || !strings.Contains(err.Error(), "differs") {
		t.Fatalf("independently added dependency accepted: %v", err)
	}
}

func TestDependencyGraphRejectsMissingCyclesAndInvalidCapabilities(t *testing.T) {
	a := AssetRef{App: "build", Kind: AssetQuery, Name: "a"}
	b := AssetRef{App: "build", Kind: AssetQuery, Name: "b"}
	graph, err := NewDependencyGraph([]Dependencies{{Ref: a, Requires: []AssetRef{b}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = graph.Closure([]AssetRef{a}); err == nil || !strings.Contains(err.Error(), a.String()) || !strings.Contains(err.Error(), b.String()) {
		t.Fatalf("missing dependency location lost: %v", err)
	}
	graph, err = NewDependencyGraph([]Dependencies{{Ref: a, Requires: []AssetRef{b}}, {Ref: b, Requires: []AssetRef{a}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = graph.Closure([]AssetRef{a}); err == nil || !strings.Contains(err.Error(), "cycle") || !strings.Contains(err.Error(), a.String()) {
		t.Fatalf("cycle accepted or location lost: %v", err)
	}
	for _, capability := range []Capability{{Kind: "query", Name: "q"}, {Kind: "mystery", Name: "q", Side: CapabilityPure}} {
		if _, err := NewDependencyGraph([]Dependencies{{Ref: a, Provides: []Capability{capability}}}); err == nil {
			t.Fatalf("invalid capability accepted: %+v", capability)
		}
	}
	if _, err := NewDependencyGraph([]Dependencies{{Ref: AssetRef{App: "build", Kind: "unknown", Name: "x"}}}); err == nil {
		t.Fatal("unregistered kind accepted")
	}
}

func TestReadBindingsRejectWritableCapabilities(t *testing.T) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.order"}
	writable := AssetRef{App: "build", Kind: AssetQuery, Name: "bad"}
	page := Page{Name: "orders", Title: "Orders", Object: object, Layout: "composed", Sections: []Section{{ID: "rows", Widget: "table", Object: object, Query: writable}}}
	body, _ := json.Marshal(page)
	for _, side := range []string{CapabilityEffect, CapabilityAuthoritative} {
		graph, err := NewDependencyGraph([]Dependencies{{Ref: object}, {Ref: writable, Provides: []Capability{{Kind: "query", Name: "bad", Side: side}}}})
		if err != nil {
			t.Fatal(err)
		}
		if err := graph.CheckReadBindings(ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetPage, Name: "orders"}, Body: body}); err == nil {
			t.Fatalf("%s accepted in page data binding", side)
		}
	}
}

func TestDependencyDescriptionPreservesHistoricalCandidateBytes(t *testing.T) {
	ref := AssetRef{App: "build", Kind: AssetObject, Name: "build.order"}
	legacy := ReleaseAsset{Ref: ref, ContractVersion: 1, SourceVersion: "1", Body: json.RawMessage(`{"type":"build.order","fields":[]}`)}
	before, err := candidate([]AssetRef{ref}, []ReleaseAsset{legacy}, true)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(before.Bytes, []byte(`"provides"`)) {
		t.Fatal("historical descriptor gained metadata")
	}
	if _, err := ReadCandidate(before.ID, before.Bytes); err != nil {
		t.Fatal(err)
	}
	selected, err := before.Closure([]AssetRef{ref})
	if err != nil || selected.ID != before.ID || !bytes.Equal(selected.Bytes, before.Bytes) {
		t.Fatalf("reading an original closure changed its bytes: %s %v", selected.ID, err)
	}
	current, err := WithDependencies(legacy)
	if err != nil {
		t.Fatal(err)
	}
	after, err := Candidate([]AssetRef{ref}, []ReleaseAsset{current})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(after.Bytes, []byte(`"provides":[`)) || len(after.Assets[0].Provides) != 1 || after.Assets[0].Provides[0].Kind != "interface" {
		t.Fatal("current object structure capability was not sealed")
	}
	if _, err := ReadCandidate(after.ID, after.Bytes); err != nil {
		t.Fatal(err)
	}
	if before.ID == after.ID {
		t.Fatal("descriptor format change retained the old identity")
	}
}

func TestObjectAndActionDescriptionResolveTypedOwnerBindings(t *testing.T) {
	object, err := WithDependencies(ReleaseAsset{Ref: AssetRef{App: "relations", Kind: AssetObject, Name: "platform.comment"}, Body: json.RawMessage(`{"type":"platform.comment","fields":[{"name":"file","type":"reference","ref":"platform.file"}],"objectBindings":{"platform.file":{"app":"files","kind":"object","name":"platform.file"}}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(object.Requires, []AssetRef{{App: "files", Kind: AssetObject, Name: "platform.file"}}) {
		t.Fatalf("object's original authority lost: %+v", object.Requires)
	}
	action, err := WithDependencies(ReleaseAsset{Ref: AssetRef{App: "relations", Kind: AssetAction, Name: "platform.comment.add"}, Body: json.RawMessage(`{"schema":"platform.comment.add","target":"platform.comment","roles":["editor"],"objectBindings":{"platform.comment":{"app":"relations","kind":"object","name":"platform.comment"}},"action":{"schema":"platform.comment.add","target":"platform.comment","payload":[{"name":"text","type":"text","required":true,"description":"Text"}]}}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(action.Requires, []AssetRef{object.Ref}) || len(action.Provides) != 1 || action.Provides[0].Side != CapabilityAuthoritative || !slices.Equal(action.Provides[0].Roles, []string{"editor"}) || action.Provides[0].Inputs.Properties["text"].Type != "string" {
		t.Fatalf("action's original declaration lost: %+v", action)
	}
}

func TestDependencyReadProjectionAcceptsInstalledCycles(t *testing.T) {
	self := AssetRef{App: "build", Kind: AssetObject, Name: "build.tree"}
	first := AssetRef{App: "build", Kind: AssetPage, Name: "first"}
	second := AssetRef{App: "build", Kind: AssetPage, Name: "second"}
	graph, err := NewDependencyGraph([]Dependencies{{Ref: self, Requires: []AssetRef{self}}, {Ref: first, Requires: []AssetRef{self, second}}, {Ref: second, Requires: []AssetRef{first}}})
	if err != nil {
		t.Fatal(err)
	}
	refs, err := graph.Reachable([]AssetRef{first})
	if err != nil || !slices.Equal(refs, []AssetRef{self, first, second}) {
		t.Fatalf("installed read projection rejected retained cycles: %v %v", refs, err)
	}
	if _, err := graph.Closure([]AssetRef{first}); err == nil {
		t.Fatal("publication accepted unproven cycles without original bodies")
	}
}

func TestCandidateCannotBypassCurrentDependenciesByRemovingProvides(t *testing.T) {
	object := ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetObject, Name: "build.order"}, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"type":"build.order"}`)}
	action, err := WithDependencies(ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetAction, Name: "build.order.edit"}, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"schema":"build.order.edit","target":"build.order"}`)})
	if err != nil {
		t.Fatal(err)
	}
	action.Requires = nil
	action.Provides = nil
	if _, err := Candidate([]AssetRef{action.Ref}, []ReleaseAsset{object, action}); err == nil || !strings.Contains(err.Error(), "omits bound dependency") {
		t.Fatalf("deleting provides bypassed body validation: %v", err)
	}
	// The original stored bytes still use their own historical rules.
	historical, err := candidate([]AssetRef{action.Ref}, []ReleaseAsset{object, action}, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCandidate(historical.ID, historical.Bytes); err != nil {
		t.Fatalf("historical accepted bytes were rewritten: %v", err)
	}
}

func TestCandidateForeignActionNeedsItsOriginalProtocol(t *testing.T) {
	foreign := AssetRef{App: "erp", Kind: AssetAction, Name: "erp.order.confirm"}
	protocol := AssetRef{App: "erp", Kind: AssetProtocol, Name: "erp.orders"}
	caller := AssetRef{App: "build", Kind: AssetAction, Name: "build.order.request"}
	foreignAsset := ReleaseAsset{Ref: foreign, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"schema":"erp.order.confirm","target":""}`)}
	body := map[string]any{"schema": caller.Name, "target": "", "confirmationBindings": map[string]any{"parties": []any{map[string]any{"name": "erp", "action": foreign}}}}
	raw, _ := json.Marshal(body)
	asset, err := WithDependencies(ReleaseAsset{Ref: caller, SourceVersion: "1", ContractVersion: 1, Body: raw})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Candidate([]AssetRef{caller}, []ReleaseAsset{asset, foreignAsset}); err == nil || !strings.Contains(err.Error(), "cross-application action") {
		t.Fatalf("unbound foreign action accepted: %v", err)
	}
	body["confirmationBindings"] = map[string]any{"parties": []any{map[string]any{"name": "erp", "action": foreign, "protocol": protocol}}}
	raw, _ = json.Marshal(body)
	asset.Body = raw
	asset, err = WithDependencies(asset)
	if err != nil {
		t.Fatal(err)
	}
	protocolBody, _ := json.Marshal(ProtocolReleaseDescriptor{Name: protocol.Name, Version: 1, Actions: []Action{{Schema: "confirm"}}, ActionBindings: map[string]AssetRef{"confirm": foreign}})
	protocolAsset, err := WithDependencies(ReleaseAsset{Ref: protocol, SourceVersion: "1", ContractVersion: 1, Body: protocolBody})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Candidate([]AssetRef{caller}, []ReleaseAsset{asset, foreignAsset, protocolAsset}); err != nil {
		t.Fatalf("original protocol binding refused: %v", err)
	}
}

func TestApplicationMembershipRetainsForeignActionsWithoutInvokingThem(t *testing.T) {
	foreign := AssetRef{App: "erp", Kind: AssetAction, Name: "erp.order.confirm"}
	foreignAsset, err := WithDependencies(ReleaseAsset{Ref: foreign, SourceVersion: "1", ContractVersion: 1, Body: json.RawMessage(`{"schema":"erp.order.confirm","target":""}`)})
	if err != nil {
		t.Fatal(err)
	}
	application := ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetApp, Name: "shared"}, SourceVersion: "1", ContractVersion: 1}
	application.Body, _ = json.Marshal(Application{Name: "shared", Title: "Shared action", Resources: []AssetRef{foreign}})
	application, err = WithDependencies(application)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := Candidate([]AssetRef{application.Ref}, []ReleaseAsset{application, foreignAsset})
	if err != nil {
		t.Fatalf("structural membership was treated as execution: %v", err)
	}
	if _, err := ReadCandidate(closed.ID, closed.Bytes); err != nil {
		t.Fatalf("shared membership could not be read from its sealed bytes: %v", err)
	}
	caller := AssetRef{App: "build", Kind: AssetAction, Name: "build.order.request"}
	callBody, _ := json.Marshal(map[string]any{"schema": caller.Name, "target": "", "confirmationBindings": map[string]any{"parties": []any{map[string]any{"name": "erp", "action": foreign}}}})
	call, err := WithDependencies(ReleaseAsset{Ref: caller, SourceVersion: "1", ContractVersion: 1, Body: callBody})
	if err != nil {
		t.Fatal(err)
	}
	application.Body, _ = json.Marshal(Application{Name: "shared", Title: "Shared action", Resources: []AssetRef{caller}})
	application, err = WithDependencies(application)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Candidate([]AssetRef{application.Ref}, []ReleaseAsset{application, call, foreignAsset}); err == nil || !strings.Contains(err.Error(), "cross-application action") {
		t.Fatalf("membership bypassed the original action's protocol boundary: %v", err)
	}
}

func TestCapabilitySchemasRetainOriginalInvocationInputs(t *testing.T) {
	if got := CapabilityFieldsSchema([]Field{{Name: "items", Type: "string[]"}}); got.Properties["items"].Type != "array" || got.Properties["items"].Items.Type != "string" {
		t.Fatal("string array advertised as a scalar")
	}
	if got := CapabilityFieldsSchema([]Field{{Name: "opaque", Type: "json"}}); got.Type != "" {
		t.Fatal("opaque JSON advertised as a made-up schema")
	}
	queryBody, _ := json.Marshal(NamedQuery{Name: "orders", Title: "Orders", Object: "build.order", By: "supplier"})
	description, err := DescribeAsset(ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetQuery, Name: "orders"}, Body: queryBody})
	if err != nil {
		t.Fatal(err)
	}
	if input := description.Provides[0].Inputs; input.Properties["for"].Type != "string" || !slices.Equal(input.Required, []string{"for"}) {
		t.Fatalf("query's original input missing: %+v", input)
	}
}

func TestOperationDescriptionIncludesRuleSources(t *testing.T) {
	operation := Operation{Name: "eligibility", Title: "Eligibility", Input: ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}, Output: ValueSchema{Type: "boolean"}, Roles: []string{"user"}, Binding: OperationBinding{Kind: "native"}, Limits: OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 1024, MaxOutputBytes: 1024}, Rules: &RuleTablePlan{Reads: RuleReadSpec{Object: "build.order", Collections: map[string]RuleCollection{"lines": {App: "build", Query: "order-lines", Object: "build.line", Policy: "complete"}}}}}
	asset, err := WithDependencies(ReleaseAsset{Ref: AssetRef{App: "build", Kind: AssetCompute, Name: operation.Name}, Body: Raw(operation)})
	if err != nil {
		t.Fatal(err)
	}
	want := []AssetRef{{App: "build", Kind: AssetObject, Name: "build.line"}, {App: "build", Kind: AssetObject, Name: "build.order"}, {App: "build", Kind: AssetQuery, Name: "order-lines"}}
	if !slices.Equal(asset.Requires, want) || len(asset.Provides) != 1 || asset.Provides[0].Side != CapabilityPure {
		t.Fatalf("rule source references were lost: %+v", asset)
	}
}

func TestObjectDescriptionProvidesOriginalPureShape(t *testing.T) {
	info := EntityInfo{App: "build", Type: "build.order", Fields: []FieldInfo{{Name: "title", Type: "text"}, {Name: "amount", Type: "integer"}, {Name: "lines", Type: "lines", Fields: []FieldInfo{{Name: "quantity", Type: "decimal"}}}}}
	asset, err := WithDependencies(ReleaseAsset{Ref: AssetRef{App: info.App, Kind: AssetObject, Name: info.Type}, Body: Raw(info)})
	if err != nil {
		t.Fatal(err)
	}
	if len(asset.Provides) != 1 {
		t.Fatalf("object shape is undiscoverable: %+v", asset.Provides)
	}
	shape := asset.Provides[0]
	if shape.Kind != "interface" || shape.Name != info.Type || shape.Side != CapabilityPure || shape.Outputs.Properties["title"].Type != "string" || shape.Outputs.Properties["amount"].Type != "integer" || shape.Outputs.Properties["lines"].Items.Properties["quantity"].Type != "number" {
		t.Fatalf("original object fields changed in structural capability: %+v", shape)
	}
	info.Fields = []FieldInfo{{Name: "opaque", Type: "json"}}
	asset.Body = Raw(info)
	asset, err = WithDependencies(asset)
	if err != nil || asset.Provides[0].Outputs.Type != "" {
		t.Fatalf("opaque shape was invented: %+v %v", asset.Provides, err)
	}
}

func TestAgentDescriptionRequiresDeclaredToolBindings(t *testing.T) {
	ref := AssetRef{App: "sample", Kind: AssetAgent, Name: "helper"}
	action := AssetRef{App: "sample", Kind: AssetAction, Name: "review"}
	agent := AgentReleaseDescriptor{Name: "helper", Title: "Helper", Instructions: "Review the record", Native: true, Tools: []string{"review"}}
	asset := ReleaseAsset{Ref: ref, Body: Raw(agent)}
	if _, err := DescribeAsset(asset); err == nil || !strings.Contains(err.Error(), "binding") {
		t.Fatalf("native action vanished from dependencies: %v", err)
	}
	agent.ToolBindings = map[string]AssetRef{"review": action}
	asset.Body = Raw(agent)
	description, err := DescribeAsset(asset)
	if err != nil || !slices.Equal(description.Requires, []AssetRef{action}) {
		t.Fatalf("declared native action missing: %+v %v", description, err)
	}
	agent.Tools = []string{"orders/1#confirm"}
	agent.ToolBindings = nil
	asset.Body = Raw(agent)
	if _, err := DescribeAsset(asset); err == nil {
		t.Fatal("unbound protocol tool accepted")
	}
	protocol := AssetRef{App: "erp", Kind: AssetProtocol, Name: "orders"}
	body := map[string]any{}
	_ = json.Unmarshal(Raw(agent), &body)
	body["protocolBindings"] = map[string]AssetRef{"orders/1": protocol}
	asset.Body = Raw(body)
	description, err = DescribeAsset(asset)
	if err != nil || !slices.Equal(description.Requires, []AssetRef{protocol}) {
		t.Fatalf("original provider protocol missing: %+v %v", description, err)
	}
	agent.Tools = []string{"read:inventory", "emit:notice"}
	asset.Body = Raw(agent)
	if _, err := DescribeAsset(asset); err != nil {
		t.Fatalf("owner-pinned read/effect declarations refused: %v", err)
	}
	agent.Native = false
	agent.Tools = []string{"undeclared"}
	asset.Body = Raw(agent)
	if _, err := DescribeAsset(asset); err == nil {
		t.Fatal("undeclared Build action tool accepted")
	}
}

func TestDependencyClosureIncludesReciprocalTypedModelWithoutAllowingExecutionCycles(t *testing.T) {
	object := AssetRef{App: "build", Kind: AssetObject, Name: "build.event"}
	subject := AssetRef{App: "build", Kind: AssetObject, Name: "build.device"}
	link := AssetRef{App: "build", Kind: AssetLinkType, Name: "predecessors"}
	for _, middle := range []AssetKind{AssetObject, AssetQuery, AssetAction, AssetFlow, AssetObservation} {
		t.Run(string(middle), func(t *testing.T) {
			via := subject
			via.Kind = middle
			graph, err := NewDependencyGraph([]Dependencies{{Ref: object, Requires: []AssetRef{via}}, {Ref: via, Requires: []AssetRef{link}}, {Ref: link, Requires: []AssetRef{object}}})
			if err != nil {
				t.Fatal(err)
			}
			closure, err := graph.Closure([]AssetRef{object})
			if middle == AssetObject {
				if err != nil || len(closure) != 3 {
					t.Fatalf("typed model omitted: %v %v", closure, err)
				}
			} else if err == nil {
				t.Fatal("executable dependency cycle accepted")
			}
		})
	}
}
