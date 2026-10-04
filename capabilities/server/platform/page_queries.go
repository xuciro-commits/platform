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
	Input      string               `json:"input,omitempty"`
	Direction  string               `json:"direction,omitempty"`
	ItemOwner  string               `json:"itemOwner,omitempty"`
	Set        *PageQuerySet        `json:"set,omitempty"`
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
type PageQuerySet struct {
	Op     string   `json:"op"`
	Inputs []string `json:"inputs"`
}

type PageQueryCondition struct {
	Optional   bool      `json:"optional,omitempty"`
	AsDateTime bool      `json:"asDateTime,omitempty"`
	AsDate     bool      `json:"asDate,omitempty"`
	AsDecimal  bool      `json:"asDecimal,omitempty"`
	Field      string    `json:"field"`
	Op         string    `json:"op"`
	Value      PageValue `json:"value"`
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
	if q.Input != "" {
		ids = append(ids, q.Input)
	}
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
			if v.Mode == "property" {
				return dependsOnPlan(v.Source.Variable, seen)
			}
			if v.Source.Kind == "plan" || v.Mode == "aggregate" {
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
	if err := d.CheckQuerySets(); err != nil {
		return err
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
		if q.Input != "" {
			v, ok := d.Variables[q.Input]
			var object AssetRef
			if d.Interface != nil {
				for _, port := range d.Interface.Inputs {
					if port.Variable == q.Input && port.Object != nil {
						object = *port.Object
					}
				}
			}
			if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.85") || inputScope == "application" || !ok || v.Mode != "input" || v.Type != "object-set" || object != q.Object || q.Query != nil || q.For != nil || q.Direction != "" || q.ItemOwner != "" || q.Set != nil {
				return fmt.Errorf("query %s needs its exact original collection input", id)
			}
		}
		factor := 1
		if q.ItemOwner != "" {
			if inputScope == "application" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.20") || d.Nodes[q.ItemOwner].Loop == nil || d.overlayOwners()[q.ItemOwner] != q.Owner {
				return fmt.Errorf("item query %s needs a matching parent loop scope", id)
			}
			var err error
			factor, err = d.loopFactor(q.ItemOwner)
			if err != nil {
				return err
			}
			if q.Set == nil && (q.Query == nil || q.For == nil || q.For.Variable != d.Nodes[q.ItemOwner].Loop.ItemVariable) {
				return fmt.Errorf("item query %s needs its parent record", id)
			}
		}
		total += q.Limit * factor
		if q.Query != nil && (q.Query.Ref.Check() != nil || (q.Query.Ref.Kind != AssetQuery && q.Query.Ref.Kind != AssetLinkType) || q.Query.SourceVersion == "") {
			return fmt.Errorf("page query %s needs an exact named query binding", id)
		}
		if q.Direction != "" && (q.Query == nil || q.Query.Ref.Kind != AssetLinkType) {
			return fmt.Errorf("direction requires a link binding")
		}
		if q.Query != nil && q.Query.Ref.Kind == AssetLinkType {
			if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.22") || q.For == nil || q.For.Variable == "" || d.Variables[q.For.Variable].Type != "record" || (q.Direction != "forward" && q.Direction != "reverse") || q.Set != nil {
				return fmt.Errorf("link plan needs v2.22, a direction and typed start record")
			}
		}
		if q.For != nil && q.Query == nil {
			return fmt.Errorf("page query %s parent input requires a named query", id)
		}
		for _, term := range q.Conditions {
			if (term.Optional || term.AsDecimal || term.Op == "in" || term.Op == "not in") && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.30") {
				return fmt.Errorf("optional/set query conditions require v2.30")
			}
			if term.AsDateTime && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.DateInput.DateTimeRequiredUIProfile) || term.AsDate || term.AsDecimal || term.Value.Variable == "" || d.Variables[term.Value.Variable].Type != "string") {
				return fmt.Errorf("datetime condition needs its profile and exclusive string binding")
			}
			if term.AsDate && (!PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.DateInput.RequiredUIProfile) || term.AsDecimal || term.Value.Variable == "" || d.Variables[term.Value.Variable].Type != "string") {
				return fmt.Errorf("date text condition needs its profile and exclusive string binding")
			}
			if term.AsDecimal && (term.Value.Variable == "" || d.Variables[term.Value.Variable].Type != "string") {
				return fmt.Errorf("decimal text condition needs a string variable")
			}
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
				plannedInput := dependsOnPlan(value.Variable, map[string]bool{})
				if plannedInput && slices.Contains([]string{"page", "overlay"}, inputScope) && d.recordContextQueryInput(id, value.Variable, sections) && !d.variableDependsOnQuery(value.Variable, id, sections) {
					plannedInput = false
				}
				if !ok || inputScope == "application" && (v.Scope != "application" || v.Type == "record" && (q.Query == nil || q.Query.Ref.Kind != AssetLinkType)) || (v.Scope != "page" && v.Scope != "application" && !(v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "") && !(v.Scope == "loop-item" && v.Owner == q.ItemOwner && q.ItemOwner != "")) || !slices.Contains([]string{"string", "boolean", "record", "decimal", "string-set"}, v.Type) || plannedInput {
					return fmt.Errorf("page query %s parameter escapes its input scope", id)
				}
			} else {
				var literal any
				if _, exact := DecimalLiteral(value.Literal); exact {
					if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.16") {
						return fmt.Errorf("decimal query literal requires v2.16")
					}
					continue
				}
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
	aggregates, expanded := 0, 0
	seenAggregates := map[string]bool{}
	for id, v := range d.Variables {
		if v.Mode == "aggregate" {
			if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.21") {
				return fmt.Errorf("count variable requires v2.21")
			}
			aggregates++
			if v.Source != nil && !seenAggregates[v.Source.Query+"/"+v.Source.Kind+"/"+v.Source.Measure] {
				seenAggregates[v.Source.Query+"/"+v.Source.Kind+"/"+v.Source.Measure] = true
				factor, err := d.loopFactor(d.Queries[v.Source.Query].ItemOwner)
				if err != nil {
					return err
				}
				expanded += factor
			}
		}
		if v.Source != nil && (v.Source.Kind == "plan" || v.Mode == "aggregate") {
			q, ok := d.Queries[v.Source.Query]
			if !ok || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.9") || !(q.ItemOwner != "" && v.Scope == "loop-item" && v.Owner == q.ItemOwner || q.ItemOwner == "" && (q.Owner == "" && v.Scope == inputScope || q.Owner != "" && v.Scope == "overlay" && v.Owner == q.Owner)) {
				return fmt.Errorf("page variable %s needs an existing query plan", id)
			}
		}
	}
	if aggregates > pageWidgets.Runtime.Aggregate.MaxVariables || expanded > pageWidgets.Runtime.Aggregate.MaxExpandedReads {
		return fmt.Errorf("count variable read budget exceeded")
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
	for _, s := range p.Sections {
		if s.Observation != nil && s.Observation.AssetObject != nil {
			refs = append(refs, *s.Observation.AssetObject)
		}
	}
	for _, v := range p.DocumentVariables() {
		if (v.Mode == "shared" || v.Mode == "property" || v.Scope == "application" && v.Mode == "resource" && (v.Type == "record" || v.Type == "filter")) && v.Source != nil && v.Source.Object != nil {
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
	for _, section := range p.Sections {
		if section.SceneSampleCollectionVariable == "" || section.Scene == nil || p.Document == nil {
			continue
		}
		v := p.Document.Variables[section.SceneSampleCollectionVariable]
		if v.Source != nil && string(Raw(p.Document.Queries[v.Source.Query])) == string(Raw(q)) {
			if named == nil || named.Query == nil || named.Query.By != section.Scene.SampleAssetField {
				return fmt.Errorf("scene latest sample query uses another asset reference")
			}
		}
	}
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
			return typ == "number" || typ == "decimal"
		case "reference":
			return typ == "string" || typ == "record" && p.RecordVariableObject(value.Variable) == field.Ref
		case "text", "choice", "date", "datetime":
			return typ == "string"
		}
		return false
	}
	for _, term := range q.Conditions {
		field, ok := fieldType(term.Field)
		compatible := checkValue(field, term.Value)
		if term.AsDate {
			compatible = valueType(term.Value) == "string" && field.Type == "date" && !term.AsDecimal && !term.AsDateTime
		}
		if term.AsDateTime {
			compatible = valueType(term.Value) == "string" && field.Type == "datetime" && !term.AsDate && !term.AsDecimal
		}
		if term.AsDecimal {
			compatible = valueType(term.Value) == "string" && slices.Contains([]string{"integer", "decimal"}, field.Type) && !term.AsDate && !term.AsDateTime
		}
		if term.Op == "in" || term.Op == "not in" {
			compatible = valueType(term.Value) == "string-set" && slices.Contains([]string{"text", "choice"}, field.Type)
		}
		if !ok || !compatible || term.Op == "like" && !slices.Contains([]string{"text", "choice"}, field.Type) || slices.Contains([]string{"boolean", "reference"}, field.Type) && term.Op != "=" && term.Op != "!=" {
			return fmt.Errorf("query condition field or value type is unavailable")
		}
	}
	if q.Search != nil && valueType(*q.Search) != "string" {
		return fmt.Errorf("query search requires text")
	}
	if q.Query != nil && q.Query.Ref.Kind == AssetLinkType {
		if named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.LinkType == nil || q.For == nil || q.For.Variable == "" {
			return fmt.Errorf("link plan requires its retained relation and start record")
		}
		l := named.LinkType
		start, target := l.Parent, l.Child
		if q.Direction == "reverse" {
			start, target = l.Child, l.Parent
		}
		if q.Object != target || p.RecordVariableObject(q.For.Variable) != start.Name {
			return fmt.Errorf("link plan record or target object is incompatible")
		}
		return nil
	}
	if q.Query != nil {
		if named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.Query == nil || named.Query.Object != q.Object.Name {
			return fmt.Errorf("query requires its exact named query version and object")
		}
		decl := named.Query
		if q.ItemOwner != "" && decl.By == "" {
			return fmt.Errorf("item query requires its named parent reference")
		}
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
		if s.FilterVariable != "" {
			if p.Document == nil {
				return fmt.Errorf("shared filter needs a document")
			}
			v := p.Document.Variables[s.FilterVariable]
			object := s.Object.Name
			if object == "" {
				object = p.Object.Name
			}
			if v.Source == nil || v.Source.Object == nil || v.Source.Object.Name != object {
				return fmt.Errorf("shared filter object does not match its widget")
			}
		}
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

// A contextual view consumes the confirmed record of an original producer,
// including one with its own independent query window. It may not consume a
// record whose actual read dependencies lead back to this query.
func (d *PageDocument) recordContextQueryInput(query, variable string, sections []Section) bool {
	q := d.Queries[query]
	v := d.Variables[variable]
	if q.For == nil || q.For.Variable != variable || v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !(v.Scope == "page" || v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "") {
		return false
	}
	consumer := false
	for _, s := range sections {
		windowID := ""
		if s.Widget == "avatar-stack" && s.Avatar != nil && s.Avatar.ContextVariable == variable && (v.Scope != "page" || q.Owner == "") {
			windowID = s.Avatar.ContextCollectionVariable
		} else if s.Widget == "scene-3d" && s.RecordVariable == variable {
			windowID = s.SceneSampleCollectionVariable
		} else if s.Widget == "observation" && s.Observation != nil && s.Observation.Kind == "statistics" && s.RecordVariable == variable {
			windowID = s.ObservationContextVariable
		}
		window := d.Variables[windowID]
		if window.Mode == "resource" && window.Source != nil && window.Source.Kind == "plan" && window.Source.Query == query {
			consumer = true
			break
		}
	}
	if !consumer {
		return false
	}
	for _, s := range sections {
		if s.ID != v.Source.Section {
			continue
		}
		if v.Source.Port != "" && !s.HasRecordOutput(v.Source.Port, variable) {
			return false
		}
		for _, r := range pageWidgets.Runtime.Resources {
			if r.Kind == "record" && (r.Widget == s.Widget || slices.Contains(r.Widgets, s.Widget)) {
				return true
			}
		}
	}
	return false
}
func (d *PageDocument) variableDependsOnQuery(variable, target string, sections []Section) bool {
	seenVariables, seenQueries := map[string]bool{}, map[string]bool{}
	var variableDepends func(string) bool
	var queryDepends func(string) bool
	queryDepends = func(id string) bool {
		if id == target {
			return true
		}
		if seenQueries[id] {
			return false
		}
		seenQueries[id] = true
		q := d.Queries[id]
		for _, value := range q.Values() {
			if value.Variable != "" && variableDepends(value.Variable) {
				return true
			}
		}
		if q.Set != nil {
			for _, input := range q.Set.Inputs {
				if queryDepends(input) {
					return true
				}
			}
		}
		return false
	}
	variableDepends = func(id string) bool {
		if id == "" || seenVariables[id] {
			return false
		}
		seenVariables[id] = true
		v := d.Variables[id]
		if v.Expression != nil {
			for _, value := range v.Expression.Args {
				if variableDepends(value.Variable) {
					return true
				}
			}
		}
		if v.Source == nil {
			return false
		}
		if v.Source.Kind == "property" && variableDepends(v.Source.Variable) {
			return true
		}
		if (v.Source.Kind == "plan" || v.Mode == "aggregate") && queryDepends(v.Source.Query) {
			return true
		}
		if slices.Contains([]string{"record", "records", "query", "filter"}, v.Source.Kind) {
			for _, s := range sections {
				if s.ID != v.Source.Section {
					continue
				}
				for _, input := range append(append([]string{s.CollectionVariable, s.FilterVariable, s.RecordVariable}, s.ObservationVariables()...), s.SpatialVariables()...) {
					if variableDepends(input) {
						return true
					}
				}
			}
		}
		return false
	}
	return variableDepends(variable)
}
