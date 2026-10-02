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

func TestPageQuerySetGraphAndMemberClosure(t *testing.T) {
	p := setPlanPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	c := pageWidgets.Runtime.Query.Set
	if c.MaxNodes != QuerySetMaxNodes || c.MaxDepth != QuerySetMaxDepth || c.MaxBytes != QuerySetMaxBytes || c.MaxConditions != QuerySetMaxConditions || c.MaxSearchBytes != QuerySetMaxSearchBytes {
		t.Fatal("described and executable set budgets drifted")
	}
	for _, op := range c.Operations {
		if err := (Query{Set: &QuerySet{Op: op, Inputs: []*RecordSetPredicate{{}, {}}}}).CheckSet(); err != nil {
			t.Fatal("described set operation is not executable", op, err)
		}
	}
	for _, change := range []func(*PageDocument){
		func(d *PageDocument) { d.UIProfile = "platform.page.v2.17" },
		func(d *PageDocument) { delete(d.Queries, "left") },
		func(d *PageDocument) { q := d.Queries["read"]; q.Set.Inputs[0] = "read"; d.Queries["read"] = q },
		func(d *PageDocument) { q := d.Queries["left"]; q.Object.Name = "sample.other"; d.Queries["left"] = q },
		func(d *PageDocument) { q := d.Queries["left"]; q.Owner = "foreign"; d.Queries["left"] = q },
		func(d *PageDocument) { q := d.Queries["read"]; q.Set.Op = "script"; d.Queries["read"] = q },
	} {
		raw, _ := json.Marshal(p.Document)
		var d PageDocument
		json.Unmarshal(raw, &d)
		change(&d)
		if d.Check(p.Sections) == nil {
			t.Fatal("invalid source graph accepted")
		}
	}
	delete(p.Document.Queries, "left")
	visible := p.Document.Visible(p.Sections)
	if _, ok := visible.Queries["read"]; ok {
		t.Fatal("combination survived missing member source")
	}
	if _, ok := visible.Variables["window"]; ok {
		t.Fatal("combination output survived missing source")
	}
	app := Application{UIProfile: PageUIProfile(), Queries: setPlanPage().Document.Queries, Variables: map[string]PageVariable{"bucket": {Scope: "application", Type: "string", Mode: "state", Initial: json.RawMessage(`"A"`)}}}
	if err := app.CheckVariables(); err != nil {
		t.Fatal("application graph rejected", err)
	}
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
