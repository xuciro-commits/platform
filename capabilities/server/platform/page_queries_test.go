package platform

import (
	"encoding/json"
	"fmt"
	"testing"
)

func setPlanPage() Page {
	p := queryPlanPage()
	a := p.Document.Queries["read"]
	p.Document.Queries["left"] = a
	p.Document.Queries["right"] = a
	a.Set = &PageQuerySet{Op: "union", Inputs: []string{"left", "right"}}
	p.Document.Queries["read"] = a
	return p
}
func TestPageQuerySetExpandedAndFixedConditionBudgets(t *testing.T) {
	p := setPlanPage()
	d := p.Document
	leaf := d.Queries["left"]
	d.Queries = map[string]PageQuery{"q0": leaf}
	for i := 1; i <= 4; i++ {
		q := leaf
		input := fmt.Sprintf("q%d", i-1)
		q.Set = &PageQuerySet{Op: "union", Inputs: []string{input, input}}
		d.Queries[fmt.Sprintf("q%d", i)] = q
	}
	if err := d.CheckQuerySets(); err != nil {
		t.Fatal("legal expanded graph rejected", err)
	}
	q := leaf
	q.Set = &PageQuerySet{Op: "union", Inputs: []string{"q4", "q4"}}
	d.Queries["q5"] = q
	if d.CheckQuerySets() == nil {
		t.Fatal("reused sources evaded expanded budget")
	}
	p = setPlanPage()
	binding := AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetQuery, Name: "fixed"}, SourceVersion: "q1"}
	left := p.Document.Queries["left"]
	left.Query = &binding
	p.Document.Queries["left"] = left
	terms := make([]any, 32)
	for i := range terms {
		terms[i] = []any{"bucket", "=", "A"}
	}
	named := map[AssetBinding]NamedQuery{binding: {Domain: Raw(terms)}}
	if p.CheckQuerySetConditions(named) == nil {
		t.Fatal("fixed and additional conditions exceeded the owner budget")
	}
	left.Conditions = nil
	p.Document.Queries["left"] = left
	if err := p.CheckQuerySetConditions(named); err != nil {
		t.Fatal("legal fixed conditions rejected", err)
	}
}
func TestDateConditionOriginalFieldAndExclusiveType(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["day"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	q := d.Queries["read"]
	q.Conditions = []PageQueryCondition{{Field: "due", Op: ">=", Value: PageValue{Variable: "day"}, AsDate: true, Optional: true}}
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "due", Type: "date"}}}
	if err := p.CheckQuerySchema(q, info, nil); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Type = "datetime"
	if p.CheckQuerySchema(q, info, nil) == nil {
		t.Fatal("instant relabelled civil")
	}
	info.Fields[0].Type = "date"
	q.Conditions[0].AsDecimal = true
	if p.CheckQuerySchema(q, info, nil) == nil {
		t.Fatal("mixed conversion accepted")
	}
}

func queryPlanPage() Page {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	return Page{Name: "work", Object: object, Sections: []Section{{ID: "table", Widget: "table", Fields: []string{"bucket"}, CollectionVariable: "window"}}, Document: &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}, Variables: map[string]PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}, "bucket": {Scope: "page", Type: "string", Mode: "state", Initial: Raw("A")}}, Queries: map[string]PageQuery{"read": {Title: "Read notes", Object: object, Limit: 20, Sort: []string{"id"}, Conditions: []PageQueryCondition{{Field: "bucket", Op: "=", Value: PageValue{Variable: "bucket"}}}}}}}
}

func typedPage(t *testing.T, original Page) Page {
	t.Helper()
	safe, err := original.Safety()
	if err != nil {
		t.Fatal(err)
	}
	return safe
}
func clonePage(t *testing.T, p Page) Page {
	t.Helper()
	var copy Page
	if err := json.Unmarshal(Raw(p), &copy); err != nil {
		t.Fatal(err)
	}
	return copy
}

func TestIndependentQueryPlansAndScopes(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	info := EntityInfo{Type: p.Object.Name, Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckQuerySchema(p.InputSchema.Queries["read"], info, nil); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Page)
	}{
		{"unbounded window", func(p *Page) { q := p.InputSchema.Queries["read"]; q.Limit = 101; p.InputSchema.Queries["read"] = q }},
		{"missing source", func(p *Page) { delete(p.InputSchema.Queries, "read") }},
		{"query output dependency", func(p *Page) {
			p.InputSchema.Variables["present"] = PageVariable{Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "window"}}}}
			q := p.InputSchema.Queries["read"]
			q.Conditions[0].Value = PageValue{Variable: "present"}
			p.InputSchema.Queries["read"] = q
		}},
		{"unknown parameter", func(p *Page) {
			q := p.InputSchema.Queries["read"]
			q.Conditions[0].Value = PageValue{Variable: "missing"}
			p.InputSchema.Queries["read"] = q
		}},
		{"foreign owner", func(p *Page) {
			q := p.InputSchema.Queries["read"]
			q.Owner = "foreign"
			p.InputSchema.Queries["read"] = q
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := clonePage(t, p)
			tc.change(&copy)
			if copy.CheckSafety() == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
	hidden := info
	hidden.Fields = nil
	if p.CheckQuerySchema(p.InputSchema.Queries["read"], hidden, nil) == nil {
		t.Fatal("hidden condition field accepted")
	}
	info.Fields[0].Type = "boolean"
	if p.CheckQuerySchema(p.InputSchema.Queries["read"], info, nil) == nil {
		t.Fatal("parameter type mismatch accepted")
	}
	delete(p.InputSchema.Queries, "read")
	visible := p.VisibleSafety()
	if _, ok := visible.InputSchema.Variables["window"]; ok {
		t.Fatal("missing plan retained its output")
	}
}

func TestPageQuerySetGraphAndMemberClosure(t *testing.T) {
	p := typedPage(t, setPlanPage())
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){
		func(p *Page) { delete(p.InputSchema.Queries, "left") },
		func(p *Page) {
			q := p.InputSchema.Queries["read"]
			q.Set.Inputs[0] = "read"
			p.InputSchema.Queries["read"] = q
		},
		func(p *Page) {
			q := p.InputSchema.Queries["left"]
			q.Object.Name = "sample.other"
			p.InputSchema.Queries["left"] = q
		},
		func(p *Page) {
			q := p.InputSchema.Queries["left"]
			q.Owner = "foreign"
			p.InputSchema.Queries["left"] = q
		},
		func(p *Page) {
			q := p.InputSchema.Queries["read"]
			q.Set.Op = "script"
			p.InputSchema.Queries["read"] = q
		},
	} {
		copy := clonePage(t, p)
		change(&copy)
		if copy.CheckSafety() == nil {
			t.Fatal("invalid source graph accepted")
		}
	}
	delete(p.InputSchema.Queries, "left")
	visible := p.VisibleSafety()
	if _, ok := visible.InputSchema.Queries["read"]; ok {
		t.Fatal("query set survived missing member source")
	}
	if _, ok := visible.InputSchema.Variables["window"]; ok {
		t.Fatal("query set output survived missing source")
	}
	c := pageWidgets.Runtime.Query.Set
	if c.MaxNodes != QuerySetMaxNodes || c.MaxDepth != QuerySetMaxDepth || c.MaxBytes != QuerySetMaxBytes || c.MaxConditions != QuerySetMaxConditions {
		t.Fatal("declared and executed set budgets differ")
	}
	for _, op := range c.Operations {
		if err := (Query{Set: &QuerySet{Op: op, Inputs: []*RecordSetPredicate{{}, {}}}}).CheckSet(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestQueryBindingFreezesOriginalNamedVersion(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	q := p.InputSchema.Queries["read"]
	ref := AssetRef{App: "sample", Kind: AssetQuery, Name: "notes"}
	q.Query = &AssetBinding{Ref: ref, SourceVersion: "query-1"}
	p.InputSchema.Queries["read"] = q
	info := EntityInfo{Type: q.Object.Name, Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}
	decl := NamedQuery{Name: ref.Name, Title: "Notes", Object: q.Object.Name, Domain: Raw([]any{[]any{"bucket", "!=", "private"}})}
	named := Definition{Ref: ref, Version: "query-1", Query: &decl}
	if err := p.CheckQuerySchema(q, info, &named); err != nil {
		t.Fatal(err)
	}
	named.Version = "query-2"
	if p.CheckQuerySchema(q, info, &named) == nil {
		t.Fatal("changed named version accepted")
	}
	asset, err := PageReleaseAsset("sample", "page-1", p)
	if err != nil {
		t.Fatal(err)
	}
	object := ReleaseAsset{Ref: q.Object, SourceVersion: "object-1", ContractVersion: 1, Body: Raw(info)}
	query := ReleaseAsset{Ref: ref, SourceVersion: "query-1", ContractVersion: 1, Requires: []AssetRef{q.Object}, Body: Raw(decl)}
	if _, err := Candidate([]AssetRef{asset.Ref}, []ReleaseAsset{asset, object, query}); err != nil {
		t.Fatal(err)
	}
	query.SourceVersion = "query-2"
	if _, err := Candidate([]AssetRef{asset.Ref}, []ReleaseAsset{asset, object, query}); err == nil {
		t.Fatal("candidate ignored exact source version")
	}
}

func TestComputeResourceThreeConsumersRetainTypedInputAndFrozenOperation(t *testing.T) {
	p, info, op := computeResourcePage()
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	for id, c := range p.ComputeResources() {
		if err := p.CheckComputeResource(c, info, op, p.InputSchema.Variables[id].Type); err != nil {
			t.Fatal(err)
		}
	}
	asset, err := PageReleaseAsset("sample", "1", p)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range asset.Requires {
		found = found || r.Kind == AssetCompute && r.Name == "score"
	}
	if !found {
		t.Fatal("operation was not retained as a candidate dependency")
	}
	lookup := map[AssetRef]ReleaseAsset{p.Object: {Ref: p.Object, SourceVersion: "1", Body: Raw(info)}, p.InputSchema.Variables["number"].Source.Compute.Operation.Ref: {Ref: p.InputSchema.Variables["number"].Source.Compute.Operation.Ref, SourceVersion: "1", Body: Raw(op)}}
	if err := checkFrozenQueries(p, lookup); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){func(p *Page) { p.InputSchema.profile = "platform.page.v2.99" }, func(p *Page) {
		v := p.InputSchema.Variables["number"]
		v.Scope = "application"
		p.InputSchema.Variables["number"] = v
	}, func(p *Page) {
		p.InputSchema.Variables["number"].Source.Compute.Inputs["qty"] = Binding{Source: "input", Path: []string{"qty"}}
	}, func(p *Page) {
		v := p.InputSchema.Variables["number"]
		v.Source.Section = "table"
		p.InputSchema.Variables["number"] = v
	}} {
		p, _, _ := computeResourcePage()
		change(&p)
		if p.CheckSafety() == nil {
			t.Fatal("accepted invalid computation owner")
		}
	}
	invalidPath := *p.InputSchema.Variables["number"].Source.Compute
	invalidPath.Inputs = map[string]Binding{"qty": {Source: "subject", Path: []string{"qty", "nested"}}}
	if p.CheckComputeResource(invalidPath, info, op, "number") == nil {
		t.Fatal("a non-reference field admitted a further computation hop")
	}
	hidden := info
	hidden.Fields = nil
	if p.CheckComputeResource(*p.InputSchema.Variables["number"].Source.Compute, hidden, op, "number") == nil {
		t.Fatal("hidden fields admitted a calculation")
	}
	op.Output = ValueSchema{Type: "number"}
	if p.CheckComputeResource(*p.InputSchema.Variables["exact"].Source.Compute, info, op, "decimal") == nil {
		t.Fatal("binary fractional output acquired exact decimal semantics")
	}
}
func TestModelComputeResourceFollowsOriginalReferences(t *testing.T) {
	p, info, op := computeResourcePage()
	info.Fields = append(info.Fields, FieldInfo{Name: "source", Type: "reference", Ref: info.Type})
	c := *p.InputSchema.Variables["number"].Source.Compute
	c.Inputs = map[string]Binding{"qty": {Source: "subject", Path: []string{"source", "source", "qty"}}}
	lookup := func(typ string) (EntityInfo, bool) { return info, typ == info.Type }
	if err := p.CheckComputeResource(c, info, op, "number", lookup); err != nil {
		t.Fatal(err)
	}
	c.Inputs["qty"] = Binding{Source: "subject", Path: []string{"source", "source", "unknown"}}
	if p.CheckComputeResource(c, info, op, "number", lookup) == nil {
		t.Fatal("computation accepted an undeclared final field")
	}
}

func computeResourcePage() (Page, EntityInfo, Operation) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}
	compute := &PageComputeResource{Operation: AssetBinding{Ref: AssetRef{App: "sample", Kind: AssetCompute, Name: "score"}, SourceVersion: "1"}, RecordVariable: "selected", Inputs: map[string]Binding{"qty": {Source: "subject", Path: []string{"qty"}}}}
	p := Page{Format: 3, Name: "compute", Title: "Compute", Object: object, WidgetSpec: PageWidgetSpec{Format: "platform.page.web.v1", Body: Raw(map[string]any{})}, InputSchema: PageInputSchema{Variables: map[string]PageVariable{"selected": {Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}, "number": {Scope: "page", Type: "number", Mode: "resource", Source: &PageResourceSource{Kind: "compute", Compute: compute}}, "exact": {Scope: "page", Type: "decimal", Mode: "resource", Source: &PageResourceSource{Kind: "compute", Compute: compute}}}}, ScopeVisibility: PageScopeVisibility{Root: "root", Owners: map[string]string{}, Scopes: []PageScope{{ID: "root", Kind: "page"}}}, DataBindings: []PageDataBinding{{ID: "table", Object: object, Kind: "records", Model: &ModelBinding{Type: object.Name, Fields: []FieldRef{{Name: "qty"}}}}, {ID: "gauge", Kind: "presentation", Ports: map[string]string{"gaugeValueVariable": "number"}}, {ID: "mini", Kind: "presentation", Ports: map[string]string{"sparklineNumberVariable": "number"}}, {ID: "notice", Kind: "presentation", Ports: map[string]string{"alertValueVariable": "exact"}}}}
	for _, binding := range p.DataBindings {
		node := binding.ID + "-scope"
		p.ScopeVisibility.Owners[binding.ID] = node
		p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: node, Kind: "page", Parent: "root", Binding: binding.ID})
	}
	info := EntityInfo{App: "sample", Type: object.Name, Fields: []FieldInfo{{Name: "qty", Type: "integer"}}}
	op := Operation{Name: "score", Title: "Original score", Input: ValueSchema{Type: "object", Properties: map[string]ValueSchema{"qty": {Type: "integer"}}, Required: []string{"qty"}}, Output: ValueSchema{Type: "integer"}, Roles: []string{"reader"}, Binding: OperationBinding{Kind: "native"}, Limits: OperationLimits{TimeoutMillis: 1000, MemoryPages: 1, MaxInputBytes: 4096, MaxOutputBytes: 4096}}
	return p, info, op
}
func TestComputeVisibilityRetainsConsumersAndWithdrawsLostRecordInput(t *testing.T) {
	p, _, _ := computeResourcePage()
	visible := p.VisibleSafety()
	if len(visible.DataBindings) != 4 || len(visible.ComputeResources()) != 2 {
		t.Fatal("visible computation or consumers were lost")
	}
	if err := visible.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	p.DataBindings = p.DataBindings[1:]
	retired := p.VisibleSafety()
	if len(retired.ComputeResources()) != 0 || len(retired.DataBindings) != 0 {
		t.Fatal("computation or consumer survived its lost record source")
	}
}

func TestTypedScopeGraphRetainsOriginalReadOwners(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	p.InputSchema.Variables["local"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "string", Mode: "state", Initial: Raw("")}
	p.InputSchema.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: "panel", Kind: "overlay", Bindings: map[string]string{"root": "panel-root", "openVariable": "open"}}, PageScope{ID: "panel-root", Kind: "page", Parent: "panel"}, PageScope{ID: "local-scope", Kind: "page", Parent: "panel-root", Binding: "local"})
	p.ScopeVisibility.Owners["local"] = "local-scope"
	p.DataBindings = append(p.DataBindings, PageDataBinding{ID: "local", Kind: "presentation", Ports: map[string]string{"dateVariable": "local"}})
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*Page)
	}{
		{"foreign overlay", func(p *Page) {
			v := p.InputSchema.Variables["local"]
			v.Owner = "foreign"
			p.InputSchema.Variables["local"] = v
		}},
		{"page escape", func(p *Page) {
			for i, s := range p.ScopeVisibility.Scopes {
				if s.ID == "local-scope" {
					p.ScopeVisibility.Scopes[i].Parent = "root"
				}
			}
		}},
		{"scope cycle", func(p *Page) { p.ScopeVisibility.Scopes[0].Parent = "local-scope" }},
		{"unknown source", func(p *Page) { p.DataBindings[1].Ports["dateVariable"] = "missing" }},
		{"duplicate identity", func(p *Page) {
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, p.ScopeVisibility.Scopes[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			copy := clonePage(t, p)
			tc.change(&copy)
			if copy.CheckSafety() == nil {
				t.Fatal("invalid original owner graph accepted")
			}
		})
	}
}

func TestNestedLoopUsesTypedParentAndExpandedBudgets(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: "outer", Kind: "loop", Parent: "root", MaxIterations: 20, Bindings: map[string]string{"collection": "window", "itemVariable": "item"}}, PageScope{ID: "inner", Kind: "loop", Parent: "outer", MaxIterations: 10, Bindings: map[string]string{"collection": "window", "itemVariable": "nested"}})
	p.InputSchema.Variables["item"] = PageVariable{Scope: "loop-item", Owner: "outer", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "outer"}}
	p.InputSchema.Variables["nested"] = PageVariable{Scope: "loop-item", Owner: "inner", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "item", Node: "inner"}}
	if factor, err := p.LoopFactor("inner"); err != nil || factor != 200 {
		t.Fatal("nested read expansion changed", factor, err)
	}
	p.ScopeVisibility.Scopes[3].MaxIterations = 101
	if _, err := p.LoopFactor("inner"); err == nil {
		t.Fatal("multiplied loop budget accepted")
	}
}

func TestRecordPropertyGraphAndSchema(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	object := p.Object
	p.InputSchema.Variables["selected"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}
	p.InputSchema.Variables["active"] = PageVariable{Scope: "page", Type: "boolean", Mode: "property", Source: &PageResourceSource{Kind: "property", Variable: "selected", Object: &object, Field: "active"}}
	info := EntityInfo{App: "sample", Type: object.Name, Fields: []FieldInfo{{Name: "active", Type: "boolean"}}}
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckPropertySchema(p.InputSchema.Variables["active"], info); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Type = "text"
	if p.CheckPropertySchema(p.InputSchema.Variables["active"], info) == nil {
		t.Fatal("property field type drift accepted")
	}
	copy := clonePage(t, p)
	v := copy.InputSchema.Variables["active"]
	v.Source.Variable = "active"
	copy.InputSchema.Variables["active"] = v
	if copy.CheckSafety() == nil {
		t.Fatal("cyclic property graph accepted")
	}
	p.DataBindings = nil
	if _, ok := p.VisibleSafety().InputSchema.Variables["active"]; ok {
		t.Fatal("property survived its lost record source")
	}
}

func TestNamedRecordSelectionKeepsItsOriginalObject(t *testing.T) {
	p := typedPage(t, queryPlanPage())
	p.InputSchema.Selections = []SelectionVariable{{Name: "chosen", Object: p.Object}}
	p.DataBindings[0].Ports["selection"] = "chosen"
	if err := p.CheckSafety(); err != nil {
		t.Fatal(err)
	}
	p.DataBindings[0].Ports["selection"] = "missing"
	if p.CheckSafety() == nil {
		t.Fatal("unknown record selection accepted")
	}
	p.DataBindings[0].Ports["selection"] = "chosen"
	p.InputSchema.Selections[0].Object.Name = "sample.other"
	if p.CheckSafety() == nil {
		t.Fatal("foreign record selection accepted")
	}
}
