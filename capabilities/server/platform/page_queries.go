package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// PageQuery is a presentation-owned read plan over the original record API.
// A named query keeps its source version and fixed owner conditions.
type PageQuery struct {
	Owner      string               `json:"owner,omitempty"` // empty: page; otherwise an Overlay identity
	Title      string               `json:"title,omitempty"`
	Object     AssetRef             `json:"object"`
	Query      *AssetBinding        `json:"query,omitempty"`
	Conditions []PageQueryCondition `json:"conditions,omitempty"`
	Search     *PageValue           `json:"search,omitempty"`
	For        *PageValue           `json:"for,omitempty"`
	Sort       []string             `json:"sort,omitempty"`
	Limit      int                  `json:"limit"`
	Offset     int                  `json:"offset,omitempty"`
}
type PageQueryCondition struct {
	Field string    `json:"field"`
	Op    string    `json:"op"`
	Value PageValue `json:"value"`
}

func (q PageQuery) Values() []PageValue {
	values := []PageValue{}
	for _, c := range q.Conditions {
		values = append(values, c.Value)
	}
	if q.Search != nil {
		values = append(values, *q.Search)
	}
	if q.For != nil {
		values = append(values, *q.For)
	}
	return values
}
func (q PageQuery) Variables() []string {
	ids := []string{}
	for _, v := range q.Values() {
		if v.Variable != "" {
			ids = append(ids, v.Variable)
		}
	}
	return ids
}
func (d *PageDocument) CheckQueries(sections []Section) error {
	return d.checkQueries(sections, "page")
}
func (d *PageDocument) checkQueries(sections []Section, inputScope string) error {
	c := pageWidgets.Runtime.Query
	if len(d.Queries) > c.MaxPlans || len(d.Queries) > 0 && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.9") {
		return fmt.Errorf("page query plans need v2.9 and a bounded plan count")
	}
	var dependsOnPlan func(string, map[string]bool) bool
	dependsOnPlan = func(id string, seen map[string]bool) bool {
		if seen[id] {
			return false
		}
		seen[id] = true
		v := d.Variables[id]
		if v.Source != nil {
			if v.Source.Kind == "plan" {
				return true
			}
			if v.Source.Section != "" {
				for _, s := range sections {
					if s.ID == v.Source.Section && s.CollectionVariable != "" {
						return true
					}
				}
			}
		}
		if v.Expression != nil {
			for _, arg := range v.Expression.Args {
				if dependsOnPlan(arg.Variable, seen) {
					return true
				}
			}
		}
		return false
	}
	total := 0
	for id, q := range d.Queries {
		if q.Owner != "" {
			if inputScope == "application" {
				return fmt.Errorf("application query %s cannot belong to an overlay", id)
			}
			if _, ok := d.Overlays[q.Owner]; !ok || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.11") {
				return fmt.Errorf("page query %s needs a v2.11 overlay owner", id)
			}
		}
		if !pageNodeID.MatchString(id) || len(q.Title) > 1024 || q.Object.Check() != nil || q.Object.Kind != AssetObject || q.Limit < 1 || q.Limit > c.MaxLimit || q.Offset < 0 || q.Offset > c.MaxOffset || len(q.Conditions) > c.MaxConditions || len(q.Sort) > c.MaxSort {
			return fmt.Errorf("page query %s has an invalid identity, object or budget", id)
		}
		total += q.Limit
		if q.Query != nil && (q.Query.Ref.Check() != nil || q.Query.Ref.Kind != AssetQuery || q.Query.SourceVersion == "") {
			return fmt.Errorf("page query %s needs an exact named query binding", id)
		}
		if q.For != nil && q.Query == nil {
			return fmt.Errorf("page query %s parent input requires a named query", id)
		}
		for _, term := range q.Conditions {
			if !pageNodeID.MatchString(term.Field) || !slices.Contains(c.Operators, term.Op) {
				return fmt.Errorf("page query %s has an invalid condition", id)
			}
		}
		for _, field := range q.Sort {
			if !pageNodeID.MatchString(strings.TrimPrefix(field, "-")) {
				return fmt.Errorf("page query %s has an invalid sort", id)
			}
		}
		for _, value := range q.Values() {
			if (value.Variable != "") == (len(value.Literal) > 0) {
				return fmt.Errorf("page query %s value needs one variable or literal", id)
			}
			if value.Variable != "" {
				v, ok := d.Variables[value.Variable]
				if !ok || inputScope == "application" && (v.Scope != "application" || v.Type == "record") || (v.Scope != "page" && v.Scope != "application" && !(v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "")) || !slices.Contains([]string{"string", "boolean", "record"}, v.Type) || dependsOnPlan(value.Variable, map[string]bool{}) {
					return fmt.Errorf("page query %s parameter escapes its input scope", id)
				}
			} else {
				var literal any
				if json.Unmarshal(value.Literal, &literal) != nil {
					return fmt.Errorf("page query %s has an invalid literal", id)
				}
				switch literal.(type) {
				case string, bool:
					if pageLiteralType(value.Literal) == "" {
						return fmt.Errorf("page query %s literal exceeds its limit", id)
					}
				case float64:
				default:
					return fmt.Errorf("page query %s needs a scalar literal", id)
				}
			}
		}
	}
	if total > c.MaxTotalLimit {
		return fmt.Errorf("page query plan window budget exceeded")
	}
	for id, v := range d.Variables {
		if v.Source != nil && v.Source.Kind == "plan" {
			q, ok := d.Queries[v.Source.Query]
			if !ok || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.9") || !(q.Owner == "" && v.Scope == inputScope || q.Owner != "" && v.Scope == "overlay" && v.Owner == q.Owner) {
				return fmt.Errorf("page variable %s needs an existing query plan", id)
			}
		}
	}
	return nil
}
func (p Page) QueryReferences() []AssetRef {
	var refs []AssetRef
	if p.Document != nil {
		for _, q := range p.Document.Queries {
			refs = append(refs, q.Object)
			if q.Query != nil {
				refs = append(refs, q.Query.Ref)
			}
		}
	}
	for _, v := range p.DocumentVariables() {
		if (v.Mode == "shared" || v.Scope == "application" && v.Mode == "resource" && v.Type == "record") && v.Source != nil && v.Source.Object != nil {
			refs = append(refs, *v.Source.Object)
		}
	}
	return refs
}
func (p Page) DocumentVariables() map[string]PageVariable {
	if p.Document == nil {
		return nil
	}
	return p.Document.Variables
}
func (p Page) WindowVariableObject(id string) string {
	v := p.DocumentVariables()[id]
	if v.Source == nil {
		return ""
	}
	if v.Mode == "shared" && v.Source.Object != nil {
		return v.Source.Object.Name
	}
	if v.Source.Kind == "plan" && p.Document != nil {
		return p.Document.Queries[v.Source.Query].Object.Name
	}
	return ""
}

// CheckQuerySchema compares original member-visible descriptors. Compilation
// uses unfiltered descriptors; discovery calls it with this member's fields.
func (p Page) CheckQuerySchema(q PageQuery, object EntityInfo, named *Definition) error {
	if object.Type != q.Object.Name {
		return fmt.Errorf("query object is unavailable")
	}
	valueType := func(v PageValue) string {
		if v.Variable != "" {
			return p.Document.Variables[v.Variable].Type
		}
		if typ := pageLiteralType(v.Literal); typ != "" {
			return typ
		}
		return "number"
	}
	fieldType := func(field string) (FieldInfo, bool) {
		if field == "id" {
			return FieldInfo{Name: "id", Type: "text"}, true
		}
		if field == "created" || field == "changed" {
			return FieldInfo{Name: field, Type: "datetime"}, true
		}
		return object.Field(field)
	}
	for _, field := range q.Sort {
		if _, ok := fieldType(strings.TrimPrefix(field, "-")); !ok {
			return fmt.Errorf("query sort field is unavailable")
		}
	}
	checkValue := func(field FieldInfo, value PageValue) bool {
		typ := valueType(value)
		switch field.Type {
		case "boolean":
			return typ == "boolean"
		case "integer", "decimal":
			return typ == "number"
		case "reference":
			return typ == "string" || typ == "record" && p.RecordVariableObject(value.Variable) == field.Ref
		case "text", "choice", "date", "datetime":
			return typ == "string"
		}
		return false
	}
	for _, term := range q.Conditions {
		field, ok := fieldType(term.Field)
		if !ok || !checkValue(field, term.Value) || term.Op == "like" && !slices.Contains([]string{"text", "choice"}, field.Type) || slices.Contains([]string{"boolean", "reference"}, field.Type) && term.Op != "=" && term.Op != "!=" {
			return fmt.Errorf("query condition field or value type is unavailable")
		}
	}
	if q.Search != nil && valueType(*q.Search) != "string" {
		return fmt.Errorf("query search requires text")
	}
	if q.Query != nil {
		if named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.Query == nil || named.Query.Object != q.Object.Name {
			return fmt.Errorf("query requires its exact named query version and object")
		}
		decl := named.Query
		if decl.By != "" {
			field, ok := fieldType(decl.By)
			if !ok || q.For == nil || !checkValue(field, *q.For) {
				return fmt.Errorf("named query needs its typed parent input")
			}
		} else if q.For != nil {
			return fmt.Errorf("named query has no parent input")
		}
		for _, field := range decl.Sort {
			if _, ok := fieldType(strings.TrimPrefix(field, "-")); !ok {
				return fmt.Errorf("named query sort field is unavailable")
			}
		}
		var terms []json.RawMessage
		if len(decl.Domain) > 0 && json.Unmarshal(decl.Domain, &terms) != nil {
			return fmt.Errorf("named query domain is unavailable")
		}
		for _, raw := range terms {
			var logic string
			if json.Unmarshal(raw, &logic) == nil {
				continue
			}
			var term []json.RawMessage
			var field string
			if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &field) != nil {
				return fmt.Errorf("named query domain is unavailable")
			}
			if _, ok := fieldType(field); !ok {
				return fmt.Errorf("named query field is unavailable")
			}
		}
	}
	return nil
}

// CheckCollectionPorts keeps table object identity on the same frozen edge.
func (p Page) CheckCollectionPorts() error {
	for _, s := range p.Sections {
		if s.SelectionVariable != "" {
			if p.Document == nil {
				return fmt.Errorf("shared selection needs a document")
			}
			object := s.Object.Name
			if object == "" {
				object = p.Object.Name
			}
			if object != p.RecordVariableObject(s.SelectionVariable) {
				return fmt.Errorf("shared selection object does not match its table")
			}
		}
		if s.CollectionVariable == "" {
			continue
		}
		if p.Document == nil {
			return fmt.Errorf("table window needs a document")
		}
		v := p.Document.Variables[s.CollectionVariable]
		if v.Source == nil {
			return fmt.Errorf("table window source is unavailable")
		}
		object := s.Object.Name
		if object == "" {
			object = p.Object.Name
		}
		if object != p.WindowVariableObject(s.CollectionVariable) {
			return fmt.Errorf("table window object does not match its plan")
		}
	}
	return nil
}
