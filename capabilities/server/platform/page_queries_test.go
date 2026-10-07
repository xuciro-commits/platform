package platform

import (
	"encoding/json"
	"slices"
	"testing"
)

func queryPlanPage() Page {
	d, sections := loopDocument()
	d.Queries = map[string]PageQuery{"read": {Title: "Read notes", Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}, Limit: 20, Sort: []string{"id"}, Conditions: []PageQueryCondition{{Field: "bucket", Op: "=", Value: PageValue{Variable: "bucket"}}}}}
	d.Variables["window"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}
	d.Variables["bucket"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: json.RawMessage(`"A"`)}
	return Page{Name: "work", Object: d.Queries["read"].Object, Layout: "composed", Sections: sections, Document: d}
}

func TestIndependentQueryPlansAndScopes(t *testing.T) {
	p := queryPlanPage()
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckQuerySchema(p.Document.Queries["read"], info, nil); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.8" }},
		{"unbounded window", func(p *Page) { q := p.Document.Queries["read"]; q.Limit = 101; p.Document.Queries["read"] = q }},
		{"missing source", func(p *Page) { delete(p.Document.Queries, "read") }},
		{"item parameter", func(p *Page) {
			q := p.Document.Queries["read"]
			q.Conditions[0].Value = PageValue{Variable: "expanded"}
			p.Document.Queries["read"] = q
		}},
		{"query output dependency", func(p *Page) {
			p.Document.Variables["present"] = PageVariable{Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "window"}}}}
			q := p.Document.Queries["read"]
			q.Conditions[0].Value = PageValue{Variable: "present"}
			p.Document.Queries["read"] = q
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := queryPlanPage()
			test.change(&p)
			if p.Document.Check(p.Sections) == nil {
				t.Fatal("invalid plan accepted")
			}
		})
	}
	hidden := info
	hidden.Fields = nil
	if p.CheckQuerySchema(p.Document.Queries["read"], hidden, nil) == nil {
		t.Fatal("hidden condition field accepted")
	}
	mismatch := info
	mismatch.Fields = []FieldInfo{{Name: "bucket", Type: "boolean"}}
	if p.CheckQuerySchema(p.Document.Queries["read"], mismatch, nil) == nil {
		t.Fatal("parameter type mismatch accepted")
	}
	doc := *p.Document
	doc.Queries = map[string]PageQuery{}
	shown := doc.Visible(p.Sections)
	if _, ok := shown.Variables["window"]; ok || shown.Nodes["loop"].Loop != nil {
		t.Fatal("unavailable plan left dependent loop")
	}
}

func TestQueryBindingFreezesOriginalNamedVersion(t *testing.T) {
	p := queryPlanPage()
	q := p.Document.Queries["read"]
	ref := AssetRef{App: "sample", Kind: AssetQuery, Name: "notes"}
	q.Query = &AssetBinding{Ref: ref, SourceVersion: "query-1"}
	p.Document.Queries["read"] = q
	info := EntityInfo{Type: q.Object.Name, Fields: []FieldInfo{{Name: "bucket", Type: "text"}}}
	decl := NamedQuery{Name: ref.Name, Title: "Notes", Object: q.Object.Name, Domain: json.RawMessage(`[["bucket","!=","private"]]`)}
	named := Definition{Ref: ref, Version: "query-1", Query: &decl}
	if err := p.CheckQuerySchema(q, info, &named); err != nil {
		t.Fatal(err)
	}
	named.Version = "query-2"
	if p.CheckQuerySchema(q, info, &named) == nil {
		t.Fatal("changed named query version accepted")
	}
	page, err := PageReleaseAsset("sample", "page-1", p)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(info)
	queryBody, _ := json.Marshal(decl)
	object := ReleaseAsset{Ref: q.Object, SourceVersion: "object-1", ContractVersion: 1, Body: body}
	query := ReleaseAsset{Ref: ref, SourceVersion: "query-1", ContractVersion: 1, Requires: []AssetRef{q.Object}, Body: queryBody}
	if _, err = Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object, query}); err != nil {
		t.Fatal(err)
	}
	query.SourceVersion = "query-2"
	if _, err = Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object, query}); err == nil {
		t.Fatal("candidate ignored exact query binding")
	}
}

func TestTableCollectionPortIsVersionedAndExclusive(t *testing.T) {
	p := queryPlanPage()
	for i := range p.Sections {
		if p.Sections[i].Widget == "table" {
			p.Sections[i].CollectionVariable = "window"
		}
	}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name   string
		change func(*Page)
	}{
		{"old profile", func(p *Page) { p.Document.UIProfile = "platform.page.v2.9" }},
		{"wrong variable", func(p *Page) {
			for i := range p.Sections {
				if p.Sections[i].Widget == "table" {
					p.Sections[i].CollectionVariable = "bucket"
				}
			}
		}},
		{"mixed query", func(p *Page) {
			for i := range p.Sections {
				if p.Sections[i].Widget == "table" {
					p.Sections[i].Query = AssetRef{App: "sample", Kind: AssetQuery, Name: "other"}
				}
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			q := p
			q.Sections = slices.Clone(p.Sections)
			doc := *p.Document
			q.Document = &doc
			test.change(&q)
			if q.Document.Check(q.Sections) == nil {
				t.Fatal("invalid collection port accepted")
			}
		})
	}
	wrong := p
	wrong.Sections = slices.Clone(p.Sections)
	for i := range wrong.Sections {
		if wrong.Sections[i].Widget == "table" {
			wrong.Sections[i].Object = AssetRef{App: "sample", Kind: AssetObject, Name: "sample.other"}
		}
	}
	if wrong.CheckCollectionPorts() == nil {
		t.Fatal("object mismatch accepted")
	}
	if _, err := PageReleaseAsset("sample", "page-1", wrong); err == nil {
		t.Fatal("candidate allowed object mismatch")
	}
	cycle := p
	cycle.Document = &PageDocument{}
	raw, _ := json.Marshal(p.Document)
	json.Unmarshal(raw, cycle.Document)
	cycle.Document.Variables["tableWindow"] = PageVariable{Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "query", Section: "table"}}
	cycle.Document.Variables["present"] = PageVariable{Scope: "page", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "tableWindow"}}}}
	query := cycle.Document.Queries["read"]
	query.Conditions[0].Value = PageValue{Variable: "present"}
	cycle.Document.Queries["read"] = query
	if cycle.Document.Check(cycle.Sections) == nil {
		t.Fatal("table output introduced a plan dependency cycle")
	}
}

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

// A builder object's "only when" is the tag form in its saved body; the query
// checker reads it as the parsed condition instead of refusing the candidate.
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
