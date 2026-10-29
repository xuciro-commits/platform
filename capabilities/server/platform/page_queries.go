package platform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

type PageQuery struct {
	Input      string               `json:"input,omitempty"`
	Direction  string               `json:"direction,omitempty"`
	ItemOwner  string               `json:"itemOwner,omitempty"`
	Set        *PageQuerySet        `json:"set,omitempty"`
	Owner      string               `json:"owner,omitempty"`
	Title      string               `json:"title,omitempty"`
	Object     AssetRef             `json:"object"`
	Interface  string               `json:"interface,omitempty"`
	Query      *AssetBinding        `json:"query,omitempty"`
	Conditions []PageQueryCondition `json:"conditions,omitempty"`

	Search *PageValue `json:"search,omitempty"`
	For    *PageValue `json:"for,omitempty"`
	Sort   []string   `json:"sort,omitempty"`
	Limit  int        `json:"limit"`
	Offset int        `json:"offset,omitempty"`
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

func (p Page) checkQueries(inputScope string) error {
	d := p.inputSchema()
	sections := p.DataBindings
	scopes := map[string]PageScope{}
	for _, scope := range p.ScopeVisibility.Scopes {
		scopes[scope.ID] = scope
	}
	c := pageWidgets.Runtime.Query
	activePlans := p.ActiveQueryPlans()
	declaredMax := c.MaxPlans
	if PageUIProfileSupports(d.profileVersion(), c.InventoryUIProfile) && p.ScopeVisibility.Root != "" {
		declaredMax = c.MaxDeclaredPlans
	}
	if len(activePlans) > c.MaxPlans || len(d.Queries) > declaredMax || len(d.Queries) > 0 && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.9") {
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
					if s.ID == v.Source.Section && s.Ports["collectionVariable"] != "" {
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
	if err := p.checkCollectionBuilderGraph(); err != nil {
		return err
	}
	if err := d.CheckQuerySets(); err != nil {
		return err
	}
	total, declaredTotal := 0, 0
	for id, q := range d.Queries {
		if q.Owner != "" {
			if inputScope == "application" {
				return fmt.Errorf("application query %s cannot belong to an overlay", id)
			}
			if _, ok := scopes[q.Owner]; !ok || !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.11") {
				return fmt.Errorf("page query %s needs a v2.11 overlay owner", id)
			}
		}
		validSource := q.Object.Check() == nil && q.Object.Kind == AssetObject && q.Interface == ""
		if q.Interface != "" {
			validSource = PageUIProfileSupports(d.profileVersion(), c.InterfaceUIProfile) && pageNodeID.MatchString(q.Interface) && q.Object == (AssetRef{}) && q.Query != nil && q.Query.Ref.Kind == AssetQuery && q.Input == "" && q.For == nil && q.Set == nil && q.Direction == "" && q.ItemOwner == "" && len(q.Conditions) == 0 && inputScope != "application"
		}
		if !pageNodeID.MatchString(id) || len(q.Title) > 1024 || !validSource || q.Limit < 1 || q.Limit > c.MaxLimit || q.Offset < 0 || q.Offset > c.MaxOffset || len(q.Conditions) > c.MaxConditions || len(q.Sort) > c.MaxSort {
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
			if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.85") || inputScope == "application" || !ok || (v.Mode != "input" || object != q.Object) && !p.collectionBuilderInput(q.Input, q.Owner, q.Object) || v.Type != "object-set" || q.Query != nil || q.For != nil || q.Direction != "" || q.ItemOwner != "" || q.Set != nil {
				return fmt.Errorf("query %s needs its exact original collection input", id)
			}
		}
		factor := 1
		if q.ItemOwner != "" {
			if inputScope == "application" || !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.20") || scopes[q.ItemOwner].Kind != "loop" || p.scopeOwner(q.ItemOwner, "overlay") != q.Owner {
				return fmt.Errorf("item query %s needs a matching parent loop scope", id)
			}
			var err error
			factor, err = p.LoopFactor(q.ItemOwner)
			if err != nil {
				return err
			}
			if q.Set == nil && (q.Query == nil || q.For == nil || q.For.Variable != scopes[q.ItemOwner].Bindings["itemVariable"]) {
				return fmt.Errorf("item query %s needs its parent record", id)
			}
		}
		declaredTotal += q.Limit * factor
		if activePlans[id] {
			total += q.Limit * factor
		}
		if q.Query != nil && (q.Query.Ref.Check() != nil || (q.Query.Ref.Kind != AssetQuery && q.Query.Ref.Kind != AssetLinkType) || q.Query.SourceVersion == "") {
			return fmt.Errorf("page query %s needs an exact named query binding", id)
		}
		if q.Direction != "" && (q.Query == nil || q.Query.Ref.Kind != AssetLinkType) {
			return fmt.Errorf("direction requires a link binding")
		}
		if q.Query != nil && q.Query.Ref.Kind == AssetLinkType {
			if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.22") || q.For == nil || q.For.Variable == "" || d.Variables[q.For.Variable].Type != "record" || (q.Direction != "forward" && q.Direction != "reverse") || q.Set != nil {
				return fmt.Errorf("link plan needs v2.22, a direction and typed start record")
			}
		}
		if q.For != nil && q.Query == nil {
			return fmt.Errorf("page query %s parent input requires a named query", id)
		}
		for _, term := range q.Conditions {
			if (term.Optional || term.AsDecimal || term.Op == "in" || term.Op == "not in") && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.30") {
				return fmt.Errorf("optional/set query conditions require v2.30")
			}
			if term.AsDateTime && (!PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.DateInput.DateTimeRequiredUIProfile) || term.AsDate || term.AsDecimal || term.Value.Variable == "" || d.Variables[term.Value.Variable].Type != "string") {
				return fmt.Errorf("datetime condition needs its profile and exclusive string binding")
			}
			if term.AsDate && (!PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.DateInput.RequiredUIProfile) || term.AsDecimal || term.Value.Variable == "" || d.Variables[term.Value.Variable].Type != "string") {
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
				if plannedInput && slices.Contains([]string{"page", "overlay"}, inputScope) && p.recordContextQueryInput(id, value.Variable) && !p.variableDependsOnQuery(value.Variable, id) {
					plannedInput = false
				}
				if !ok || inputScope == "application" && (v.Scope != "application" || v.Type == "record" && (q.Query == nil || q.Query.Ref.Kind != AssetLinkType)) || (v.Scope != "page" && v.Scope != "application" && !(v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "") && !(v.Scope == "loop-item" && v.Owner == q.ItemOwner && q.ItemOwner != "")) || !slices.Contains([]string{"string", "boolean", "record", "decimal", "string-set"}, v.Type) || plannedInput {
					return fmt.Errorf("page query %s parameter escapes its input scope", id)
				}
			} else {
				var literal any
				if _, exact := DecimalLiteral(value.Literal); exact {
					if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.16") {
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
	if PageUIProfileSupports(d.profileVersion(), c.InventoryUIProfile) && p.ScopeVisibility.Root != "" && declaredTotal > c.MaxDeclaredTotalLimit {
		return fmt.Errorf("declared inventory query budget exceeded")
	}
	if total > c.MaxTotalLimit {
		return fmt.Errorf("page query plan window budget exceeded")
	}
	aggregates, expanded := 0, 0
	seenAggregates := map[string]bool{}
	for id, v := range d.Variables {
		if v.Mode == "aggregate" {
			if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.21") {
				return fmt.Errorf("count variable requires v2.21")
			}
			aggregates++
			if v.Source != nil && !seenAggregates[v.Source.Query+"/"+v.Source.Kind+"/"+v.Source.Measure] {
				seenAggregates[v.Source.Query+"/"+v.Source.Kind+"/"+v.Source.Measure] = true
				factor, err := p.LoopFactor(d.Queries[v.Source.Query].ItemOwner)
				if err != nil {
					return err
				}
				expanded += factor
			}
		}
		if v.Source != nil && (v.Source.Kind == "plan" || v.Mode == "aggregate") {
			q, ok := d.Queries[v.Source.Query]
			if !ok || !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.9") || !(q.ItemOwner != "" && v.Scope == "loop-item" && v.Owner == q.ItemOwner || q.ItemOwner == "" && (q.Owner == "" && v.Scope == inputScope || q.Owner != "" && v.Scope == "overlay" && v.Owner == q.Owner)) {
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
	refs := []AssetRef{}
	for _, q := range p.inputSchema().Queries {
		if q.Object.Name != "" {
			refs = append(refs, q.Object)
		}
		if q.Query != nil {
			refs = append(refs, q.Query.Ref)
		}
	}
	for _, v := range p.inputSchema().Variables {
		if v.Source != nil && v.Source.Object != nil {
			refs = append(refs, *v.Source.Object)
		}
	}
	return refs
}

func (p Page) DocumentVariables() map[string]PageVariable {
	if p.Format == 3 {
		return p.InputSchema.Variables
	}
	if p.Document == nil {
		return nil
	}
	return p.Document.Variables
}

func (p Page) WindowVariableObject(id string) string {
	if p.Format == 3 {
		return p.CollectionInputObject(id).Name
	}

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
	if v.Source.Kind == "query" {
		for _, s := range p.Sections {
			if s.ID == v.Source.Section && s.PrimaryModel() != nil {
				return s.PrimaryModel().Type
			}
		}
	}
	return ""
}

func (p Page) CheckQuerySchema(q PageQuery, object EntityInfo, named *Definition) error {
	if q.Interface != "" {
		return p.CheckInterfaceQuery(q, named)
	}
	if object.Type != q.Object.Name {
		return fmt.Errorf("query object is unavailable")
	}
	valueType := func(v PageValue) string {
		if v.Variable != "" {
			return p.inputSchema().Variables[v.Variable].Type
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

func (p Page) recordContextQueryInput(query, variable string) bool {
	d := p.inputSchema()
	sections := p.DataBindings
	q := d.Queries[query]
	v := d.Variables[variable]
	if q.For == nil || q.For.Variable != variable || v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !(v.Scope == "page" || v.Scope == "overlay" && v.Owner == q.Owner && q.Owner != "") {
		return false
	}
	consumer := false
	for _, section := range sections {
		for _, id := range section.Ports {
			window := d.Variables[id]
			if window.Source != nil && window.Source.Kind == "plan" && window.Source.Query == query {
				for _, input := range section.Ports {
					if input == variable {
						consumer = true
					}
				}
			}
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
		return s.Ports["selectionVariable"] == variable || s.Ports["recordVariable"] == variable || v.Source.Port != "" || s.Object.Name != "" || s.PrimaryModel() != nil

	}
	return false
}

func (p Page) variableDependsOnQuery(variable, target string) bool {
	d := p.inputSchema()
	sections := p.DataBindings
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
		if q.Input != "" && variableDepends(q.Input) {
			return true
		}
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
				for _, input := range pageBindingVariables(s) {
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

func (p Page) CheckBoundQuery(id string, named *Definition) error {
	if named == nil || named.Query == nil {
		return nil
	}
	q, exists := p.InputSchema.Queries[id]
	if !exists {
		return fmt.Errorf("page query %s is unavailable", id)
	}
	if len(named.Query.Sort) > 0 && len(q.Sort) > 0 && !slices.Equal(named.Query.Sort, q.Sort) || named.Query.Limit > 0 && named.Query.Limit < q.Limit {
		return fmt.Errorf("page query %s source ordering or capacity differs from its declaration", id)
	}
	return nil
}

func (d PageInputSchema) CheckApplicationQueries() error {
	return (Page{Format: 3, InputSchema: d}).checkQueries("application")
}
func frozenPageInterfaceQuery(q PageQuery, lookup map[AssetRef]ReleaseAsset) (*Definition, error) {
	if q.Query == nil {
		return nil, fmt.Errorf("interface page query needs a published binding")
	}
	asset, ok := lookup[q.Query.Ref]
	var named NamedQuery
	if !ok || asset.SourceVersion != q.Query.SourceVersion || json.Unmarshal(asset.Body, &named) != nil || named.InterfaceShape == nil || named.Check() != nil {
		return nil, fmt.Errorf("frozen interface query version or shape is unavailable")
	}
	for _, ref := range named.Dependencies() {
		implementation, exists := lookup[ref]
		info, err := queryObjectDescriptor(implementation.Body)
		if !exists || err != nil || info.Type != ref.Name || info.App != ref.App || !slices.Contains(info.Implements, named.Interface) || named.InterfaceShape.Implements(info) != nil {
			return nil, fmt.Errorf("frozen interface query implementation %s is unavailable", ref.Name)
		}
	}
	return &Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &named}, nil
}

func (p Page) CheckInterfaceQuery(q PageQuery, named *Definition) error {
	if q.Query == nil || named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.Query == nil || named.Query.Interface != q.Interface || named.Query.InterfaceShape == nil || named.Query.Check() != nil {
		return fmt.Errorf("interface page query needs its exact published query and frozen shape")
	}
	if q.Object != (AssetRef{}) || q.For != nil || q.Input != "" || q.Set != nil || q.Direction != "" || q.ItemOwner != "" || len(q.Conditions) != 0 {
		return fmt.Errorf("interface page query cannot replace the published source or conditions")
	}
	for _, sort := range q.Sort {
		field := sort
		if len(field) > 0 && field[0] == '-' {
			field = field[1:]
		}
		if field != "id" && field != "created" && field != "changed" && !slices.ContainsFunc(named.Query.InterfaceShape.Fields, func(f InterfaceField) bool {
			return f.Name == field
		}) {
			return fmt.Errorf("interface query sort needs a common field")
		}
	}
	ordering := named.Query.Sort
	if len(ordering) == 0 {
		ordering = []string{"id"}
	}
	if len(q.Sort) > 0 && !slices.Equal(ordering, q.Sort) {
		return fmt.Errorf("the published interface query owns its ordering")
	}
	if named.Query.Limit > 0 && q.Limit > named.Query.Limit {
		return fmt.Errorf("interface page window exceeds its published query limit")
	}
	if q.Search != nil {
		typ := pageLiteralType(q.Search.Literal)
		if q.Search.Variable != "" {
			typ = p.inputSchema().Variables[q.Search.Variable].Type
		}
		if typ != "string" {
			return fmt.Errorf("interface query search requires text")
		}
	}
	return nil
}

func (p Page) ActiveQueryPlans() map[string]bool {
	active := map[string]bool{}
	if p.ScopeVisibility.Root == "" {
		for id := range p.InputSchema.Queries {
			active[id] = true
		}
		return active
	}
	variables := map[string]bool{}
	var visit func(string)
	visit = func(id string) {
		if variables[id] {
			return
		}
		variables[id] = true
		if q, ok := p.InputSchema.Queries[id]; ok {
			active[id] = true
			for _, source := range q.Variables() {
				visit(source)
			}
			if q.Set != nil {
				for _, source := range q.Set.Inputs {
					visit(source)
				}
			}
		}
		if v, ok := p.InputSchema.Variables[id]; ok {
			if v.Source != nil {
				visit(v.Source.Query)
				if v.Source.Kind != "application" {
					visit(v.Source.Variable)
				}
				if v.Source.Compute != nil {
					visit(v.Source.Compute.RecordVariable)
				}
			}
			if v.Expression != nil {
				for _, arg := range v.Expression.Args {
					visit(arg.Variable)
				}
			}
			if v.Rule != nil {
				var scan func(Expr)
				scan = func(e Expr) {
					visit(e.Ref)
					if e.Agg != nil {
						visit(e.Agg.Source)
					}
					for _, a := range e.Args {
						scan(a)
					}
					if e.In != nil {
						scan(*e.In)
					}
				}
				scan(*v.Rule)
			}
		}
	}
	dormant := map[string]bool{}
	for _, scope := range p.ScopeVisibility.Scopes {
		if scope.Inactive {
			dormant[scope.Binding] = true
		}
	}
	for _, binding := range p.DataBindings {
		if dormant[binding.ID] {
			continue
		}
		for _, id := range binding.Ports {
			visit(id)
		}
		for _, resource := range binding.Resources {
			for _, input := range resource.Inputs {
				visit(input.Variable)
			}
			for _, id := range resource.Outputs {
				visit(id)
			}
		}
	}
	for _, scope := range p.ScopeVisibility.Scopes {
		if !scope.Inactive {
			for _, id := range scope.Bindings {
				visit(id)
			}
		}
	}
	for _, trigger := range p.ActionTriggers {
		for _, event := range trigger.Events {
			for _, id := range append(event.Reads(), event.Variables()...) {
				visit(id)
			}
		}
	}
	if p.InputSchema.Interface != nil {
		for _, ports := range []map[string]PagePort{p.InputSchema.Interface.Inputs, p.InputSchema.Interface.Outputs} {
			for _, port := range ports {
				visit(port.Variable)
			}
		}
	}
	return active
}

func (p Page) LoopFactor(owner string) (int, error) {
	factor := 1
	seen := map[string]bool{}
	for owner != "" {
		if seen[owner] {
			return 0, fmt.Errorf("page loop owner cycle")
		}
		seen[owner] = true
		found := false
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID != owner {
				continue
			}
			found = true
			if scope.Kind == "loop" {
				if scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || factor > pageWidgets.Runtime.Query.MaxTotalLimit/scope.MaxIterations {
					return 0, fmt.Errorf("page loop read budget exceeded")
				}
				factor *= scope.MaxIterations
			}
			owner = scope.Parent
			break
		}
		if !found {
			return 0, fmt.Errorf("page loop owner is unavailable")
		}
	}
	return factor, nil
}

type PageCollectionBuilder struct {
	Fields []string `json:"fields"`
}

func (p Page) collectionBuilderInput(id, owner string, object AssetRef) bool {
	d := p.inputSchema()
	sections := p.DataBindings
	if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.88") {
		return false
	}
	v := d.Variables[id]
	scope := "page"
	if owner != "" {
		scope = "overlay"
	}
	if v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "query" || v.Owner != owner || v.Scope != scope || v.Writable || v.Source.Query != "" || v.Source.Variable != "" || v.Source.Node != "" || v.Source.Object != nil || len(v.Source.Fields) > 0 || v.Source.Measure != "" || v.Source.Field != "" || v.Source.Port != "" {
		return false
	}
	for _, s := range sections {
		if s.ID != v.Source.Section || s.Ports["collectionOutputVariable"] != id {
			continue
		}
		input := d.Variables[s.Ports["collectionVariable"]]
		if input.Source == nil || input.Source.Kind != "plan" {
			return false
		}
		q, ok := d.Queries[input.Source.Query]
		return ok && q.Owner == owner && q.Object == object
	}
	return false
}

func (p Page) checkCollectionBuilderGraph() error {
	d := p.inputSchema()
	sections := p.DataBindings
	c := pageWidgets.Runtime.Query
	for root := range d.Queries {
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if !ok || path[id] || depth > c.Set.MaxDepth || nodes > c.Set.MaxNodes {
				return fmt.Errorf("collection sources are cyclic or exceed their budget")
			}
			path[id] = true
			defer delete(path, id)
			if q.Set != nil {
				for _, input := range q.Set.Inputs {
					if err := visit(input, depth+1, path); err != nil {
						return err
					}
				}
			}
			v := d.Variables[q.Input]
			if q.Input != "" && v.Mode != "input" {
				if !p.collectionBuilderInput(q.Input, q.Owner, q.Object) {
					return fmt.Errorf("collection input producer is unavailable")
				}
				for _, s := range sections {
					if s.ID == v.Source.Section {
						if err := visit(d.Variables[s.Ports["collectionVariable"]].Source.Query, depth+1, path); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		if err := visit(root, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	for _, s := range sections {
		v := d.Variables[s.Ports["collectionVariable"]]
		if v.Source != nil && v.Source.Kind == "query" {
			for _, producer := range sections {
				if producer.ID == v.Source.Section && producer.Ports["collectionOutputVariable"] != "" {
					return fmt.Errorf("builder output needs an independent consumer plan")
				}
			}
		}
	}
	return nil
}

type PageCollectionInput struct {
	Kind      string             `json:"kind"`
	Object    AssetRef           `json:"object"`
	Predicate RecordSetPredicate `json:"predicate"`
	Sort      []string           `json:"sort"`

	SortLocked bool           `json:"sortLocked,omitempty"`
	Traversal  *LinkTraversal `json:"traversal,omitempty"`
	Bindings   []AssetBinding `json:"bindings,omitempty"`
}

func (p Page) CollectionInputObject(variable string) AssetRef {
	return p.collectionInputObject(variable, map[string]bool{})
}

func (p Page) collectionInputObject(variable string, seen map[string]bool) AssetRef {
	if p.Format == 3 {
		if seen[variable] {
			return AssetRef{}
		}
		seen[variable] = true
		schema := p.InputSchema
		v := schema.Variables[variable]
		if v.Type != "object-set" {
			return AssetRef{}
		}
		if v.Mode == "input" && schema.Interface != nil {
			for _, port := range schema.Interface.Inputs {
				if port.Variable == variable && port.Object != nil {
					return *port.Object
				}
			}
		}
		if v.Source == nil {
			return AssetRef{}
		}
		if v.Source.Object != nil && v.Mode == "shared" {
			return *v.Source.Object
		}
		if v.Source.Kind == "plan" {
			return schema.Queries[v.Source.Query].Object
		}
		if v.Source.Kind == "query" {
			for _, binding := range p.DataBindings {
				if binding.ID == v.Source.Section {
					if source := binding.Ports["collectionVariable"]; source != "" && source != variable {
						return p.collectionInputObject(source, seen)
					}
					return binding.ObjectRef(p)
				}
			}
		}
		return AssetRef{}
	}

	if seen[variable] {
		return AssetRef{}
	}
	seen[variable] = true
	v := p.DocumentVariables()[variable]
	if v.Type != "object-set" {
		return AssetRef{}
	}
	if v.Mode == "input" && p.Document != nil && p.Document.Interface != nil {
		for _, port := range p.Document.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return *port.Object
			}
		}
	}
	if v.Source != nil {
		if v.Mode == "shared" && v.Source.Object != nil {
			return *v.Source.Object
		}
		if v.Source.Kind == "plan" && p.Document != nil {
			return p.Document.Queries[v.Source.Query].Object
		}
		if v.Source.Kind == "query" {
			for _, s := range p.Sections {
				if s.ID == v.Source.Section && s.CollectionVariable != variable {
					return p.collectionInputObject(s.CollectionVariable, seen)
				}
			}
		}
	}
	return AssetRef{}
}

type PageComputeResource struct {
	Operation      AssetBinding       `json:"operation"`
	RecordVariable string             `json:"recordVariable"`
	Inputs         map[string]Binding `json:"inputs"`
	OutputField    string             `json:"outputField,omitempty"`
}

func (d PageInputSchema) checkComputeResource(v PageVariable) error {
	c := v.Source.Compute
	if !PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.ComputeResource.RequiredUIProfile) || !slices.Contains([]string{"number", "decimal"}, v.Type) || v.Mode != "resource" || v.Writable || !slices.Contains([]string{"page", "overlay"}, v.Scope) || c == nil || !pageNodeID.MatchString(c.RecordVariable) || c.Operation.Ref.Kind != AssetCompute || c.Operation.Ref.Check() != nil || c.Operation.SourceVersion == "" || len(c.Inputs) == 0 || len(c.Inputs) > 64 || c.OutputField != "" && !pageNodeID.MatchString(c.OutputField) {
		return fmt.Errorf("compute resource needs its finite typed operation and original record")
	}
	parent := d.Variables[c.RecordVariable]
	if parent.Type != "record" || !(d.sharedContextRecord(c.RecordVariable) || parent.Mode == "resource" && parent.Source != nil && parent.Source.Kind == "record" && parent.Scope == v.Scope && parent.Owner == v.Owner) {
		return fmt.Errorf("compute input needs its confirmed record in the original owner")
	}
	subject := false
	for name, b := range c.Inputs {
		if !pageNodeID.MatchString(name) || b.Check() != nil || b.Source != "literal" && b.Source != "subject" || b.Source == "subject" && (len(b.Path) < 1 || len(b.Path) > 16 || slices.ContainsFunc(b.Path, func(part string) bool {
			return !pageNodeID.MatchString(part)
		})) {
			return fmt.Errorf("compute input needs a scalar source field or typed literal")
		}
		subject = subject || b.Source == "subject"
	}
	if !subject {
		return fmt.Errorf("record computation needs an original protected source field")
	}
	s := v.Source
	if s.Section != "" || s.Node != "" || s.Query != "" || s.Variable != "" || s.Object != nil || s.Field != "" || len(s.Fields) > 0 || s.Measure != "" || s.Port != "" {
		return fmt.Errorf("compute resource cannot carry another source")
	}
	return nil
}

func (p Page) ComputeResources() map[string]PageComputeResource {
	out := map[string]PageComputeResource{}
	for id, v := range p.inputSchema().Variables {
		if v.Source != nil && v.Source.Kind == "compute" && v.Source.Compute != nil {
			out[id] = *v.Source.Compute
		}
	}
	return out
}

func (p Page) CheckComputeResource(c PageComputeResource, object EntityInfo, op Operation, valueType string, lookups ...func(string) (EntityInfo, bool)) error {
	lookup := func(typ string) (EntityInfo, bool) {
		return object, typ == object.Type
	}
	if len(lookups) > 0 {
		lookup = lookups[0]
	}
	ref := p.RecordResourceObject(c.RecordVariable)
	if ref.App != object.App || ref.Name != object.Type || ref.Kind != AssetObject || op.Check() != nil || op.Name != c.Operation.Ref.Name || op.Input.Type != "object" || op.Input.Nullable {
		return fmt.Errorf("compute resource original object or operation differs")
	}
	for _, name := range op.Input.Required {
		if _, ok := c.Inputs[name]; !ok {
			return fmt.Errorf("compute input %s is missing", name)
		}
	}
	for name, b := range c.Inputs {
		schema, ok := op.Input.Properties[name]
		if !ok {
			return fmt.Errorf("compute input %s is unknown", name)
		}
		if b.Source == "literal" {
			if schema.Validate(b.Value, 64<<10) != nil {
				return fmt.Errorf("compute literal input differs")
			}
			continue
		}
		if b.Source != "subject" || len(b.Path) < 1 || len(b.Path) > 16 {
			return fmt.Errorf("compute source binding differs")
		}
		f, err := RecordPathField(object.Type, b.Path, lookup)
		if err != nil {
			return fmt.Errorf("compute source field is unavailable")
		}
		typ := map[string]string{"text": "string", "longtext": "string", "choice": "string", "boolean": "boolean", "integer": "integer", "decimal": "number", "date": "string", "datetime": "string"}[f.Type]
		if typ == "" || schema.Type != typ && !(typ == "integer" && schema.Type == "number") {
			return fmt.Errorf("compute source field and input type differ")
		}
	}
	output := op.Output
	if c.OutputField != "" {
		if output.Type != "object" || output.Nullable || !slices.Contains(output.Required, c.OutputField) {
			return fmt.Errorf("compute scalar output must be required")
		}
		output = output.Properties[c.OutputField]
	}
	if !slices.Contains([]string{"number", "integer"}, output.Type) || output.Nullable || valueType == "decimal" && output.Type != "integer" {
		return fmt.Errorf("compute output needs a nonnullable numeric value")
	}
	return nil
}

func (d *PageDocument) checkComputeResource(v PageVariable) error {
	return d.inputSchema().checkComputeResource(v)
}

type PageUnusedWidget struct {
	Node   string `json:"node"`
	Parent string `json:"parent"`
}

type PageDocument struct {
	UnusedWidgets []PageUnusedWidget `json:"unusedWidgets,omitempty"`

	FormatVersion int                       `json:"formatVersion"`
	UIProfile     string                    `json:"uiProfile"`
	Root          string                    `json:"root"`
	Nodes         map[string]PageLayoutNode `json:"nodes"`
	Variables     map[string]PageVariable   `json:"variables,omitempty"`
	Overlays      map[string]PageOverlay    `json:"overlays,omitempty"`
	Events        []PageEventBinding        `json:"events,omitempty"`
	Queries       map[string]PageQuery      `json:"queries,omitempty"`
	Interface     *PageInterface            `json:"interface,omitempty"`
	Device        string                    `json:"device,omitempty"`
}

type PageLayoutNode struct {
	LegacyVisual   map[string]json.RawMessage `json:"-"`
	Kind           string                     `json:"kind"`
	Children       []string                   `json:"children,omitempty"`
	Section        string                     `json:"section,omitempty"`
	ValueVariable  string                     `json:"valueVariable,omitempty"`
	ActiveVariable string                     `json:"activeVariable,omitempty"`
	VisibleWhen    string                     `json:"visibleWhen,omitempty"`
	EnabledWhen    string                     `json:"enabledWhen,omitempty"`
	Loop           *PageLoop                  `json:"loop,omitempty"`
}

var pageNodeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,79}$`)

func (d *PageDocument) Check(sections []Section) error {
	if d == nil {
		return nil
	}
	return (Page{Sections: sections, Document: d}).CheckSafety()
}
func (n *PageLayoutNode) UnmarshalJSON(raw []byte) error {
	type value PageLayoutNode
	return RetainPageSection(raw, (*value)(n), &n.LegacyVisual)
}

func (n PageLayoutNode) MarshalJSON() ([]byte, error) {
	type value PageLayoutNode
	return MarshalPageSection(value(n), n.LegacyVisual)
}

type PageEmbedding struct {
	ReadOnly         bool                 `json:"readOnly,omitempty"`
	Kind             string               `json:"kind"`
	Page             AssetBinding         `json:"page"`
	ContentVersion   string               `json:"contentVersion"`
	InterfaceVersion int                  `json:"interfaceVersion"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Results          map[string]string    `json:"results,omitempty"`
}

func (e PageEmbedding) Navigation() PageNavigation {
	return PageNavigation{Page: e.Page.Ref, InterfaceVersion: e.InterfaceVersion, Inputs: e.Inputs, Results: e.Results}
}

func (p Page) EmbeddingBindings() []PageEmbedding {
	safe, err := p.Safety()
	if err != nil {
		return nil
	}
	result := []PageEmbedding{}
	for _, binding := range safe.DataBindings {
		for _, resource := range binding.Resources {
			if resource.Kind == "page" && resource.Asset != nil {
				result = append(result, PageEmbedding{ReadOnly: resource.ReadOnly, Kind: resource.Source, Page: *resource.Asset, ContentVersion: resource.ContentVersion, InterfaceVersion: resource.InterfaceVersion, Inputs: resource.Inputs, Results: resource.Outputs})
			}
		}
	}
	return result
}

func CheckPageEmbedding(parent Page, child Page, e PageEmbedding) error {
	digest, err := PageContentVersion(child)
	if err != nil || digest != e.ContentVersion || child.Name != e.Page.Ref.Name {
		return fmt.Errorf("embedded content identity differs")
	}
	return CheckPageNavigation(parent, child, e.Navigation())
}

func CheckPageEmbeddingGraph(root AssetRef, lookup map[AssetRef]ReleaseAsset) error {
	active := map[AssetRef]bool{}
	instances, queries, records, sections := 0, 0, 0, 0
	var visit func(AssetRef, int) error
	visit = func(ref AssetRef, depth int) error {
		if depth > pageWidgets.Runtime.Embedding.MaxDepth || active[ref] {
			return fmt.Errorf("embedded page cycle or depth budget exceeded")
		}
		asset, ok := lookup[ref]
		var p Page
		if !ok || json.Unmarshal(asset.Body, &p) != nil {
			return fmt.Errorf("embedded page is unavailable")
		}
		instances++
		safe, err := p.Safety()
		if err != nil {
			return err
		}
		for _, binding := range safe.DataBindings {
			for _, resource := range binding.Resources {
				if resource.Kind == "external-document" {
					instances++
				}
			}
		}
		sections += len(safe.DataBindings)
		if instances > pageWidgets.Runtime.Embedding.MaxInstances || sections > pageWidgets.Runtime.Embedding.MaxSections {
			return fmt.Errorf("embedded page instance budget exceeded")
		}
		if len(safe.InputSchema.Queries) > 0 {
			plans := safe.ActiveQueryPlans()
			queries += len(plans)
			for id, q := range safe.InputSchema.Queries {
				if !plans[id] {
					continue
				}
				factor := 1
				if q.ItemOwner != "" {
					var err error
					factor, err = safe.LoopFactor(q.ItemOwner)
					if err != nil {
						return err
					}
				}
				records += q.Limit * factor
			}
		}
		if queries > pageWidgets.Runtime.Embedding.MaxQueries || records > pageWidgets.Runtime.Embedding.MaxRecords {
			return fmt.Errorf("embedded page read budget exceeded")
		}
		active[ref] = true
		defer delete(active, ref)
		for _, e := range p.EmbeddingBindings() {
			child, ok := lookup[e.Page.Ref]
			var target Page
			if !ok || child.SourceVersion != e.Page.SourceVersion || json.Unmarshal(child.Body, &target) != nil {
				return fmt.Errorf("embedded original page version is unavailable")
			}
			if err := CheckPageEmbedding(p, target, e); err != nil {
				return err
			}
			if err := visit(e.Page.Ref, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return visit(root, 0)
}

type PageOverlay struct {
	LegacyVisual map[string]json.RawMessage `json:"-"`
	Root         string                     `json:"root"`
	Kind         string                     `json:"kind"`
	OpenVariable string                     `json:"openVariable"`
}

type PageEventBinding struct {
	Control string       `json:"control,omitempty"`
	Source  string       `json:"source"`
	Event   string       `json:"event"`
	Effects []PageEffect `json:"effects"`
}

type PageEffect struct {
	Kind     string            `json:"kind"`
	Target   string            `json:"target,omitempty"`
	Value    json.RawMessage   `json:"value,omitempty"`
	From     string            `json:"from,omitempty"`
	Action   *PageActionEffect `json:"action,omitempty"`
	Navigate *PageNavigation   `json:"navigate,omitempty"`
}

type PageActionEffect struct {
	Ref            AssetRef             `json:"ref"`
	RecordVariable string               `json:"recordVariable,omitempty"`
	Inputs         map[string]PageValue `json:"inputs,omitempty"`
}

const maxPageEffects = 8

func (b PageEventBinding) Navigation() *PageNavigation {
	for _, effect := range b.Effects {
		if effect.Kind == "navigate" {
			return effect.Navigate
		}
	}
	return nil
}

func (b PageEventBinding) Variables() []string {
	var ids []string
	for _, effect := range b.Effects {
		switch effect.Kind {
		case "set":
			ids = append(ids, effect.Target)
		case "navigate":
			if effect.Navigate != nil {
				for _, id := range effect.Navigate.Results {
					ids = append(ids, id)
				}
			}
		}
	}
	return ids
}

func (b PageEventBinding) Reads() []string {
	var ids []string
	for _, effect := range b.Effects {
		switch effect.Kind {
		case "set":
			if effect.From != "" {
				ids = append(ids, effect.From)
			}
		case "action":
			if effect.Action != nil && effect.Action.RecordVariable != "" {
				ids = append(ids, effect.Action.RecordVariable)
			}
			if effect.Action != nil {
				for _, arg := range effect.Action.Inputs {
					if arg.Variable != "" {
						ids = append(ids, arg.Variable)
					}
				}
			}
		case "navigate":
			if effect.Navigate != nil {
				for _, arg := range effect.Navigate.Inputs {
					if arg.Variable != "" {
						ids = append(ids, arg.Variable)
					}
				}
			}
		}
	}
	return ids
}

func (p Page) checkEvents() error {
	d := p.inputSchema()
	events := []PageEventBinding{}
	overlays := map[string]PageScope{}
	sources := map[string]bool{}
	for _, binding := range p.DataBindings {
		sources[binding.ID] = true
	}
	for _, scope := range p.ScopeVisibility.Scopes {
		sources[scope.ID] = true
		if scope.Kind == "overlay" {
			overlays[scope.ID] = scope
		}
	}
	for _, trigger := range p.ActionTriggers {
		events = append(events, trigger.Events...)
	}
	if len(overlays) > 16 || len(events) > 256 {
		return fmt.Errorf("page overlay or event limit exceeded")
	}
	if (len(overlays) > 0 || len(events) > 0) && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.4") {
		return fmt.Errorf("page overlays and events require UI profile v2.4")
	}
	open := map[string]bool{}
	for id, overlay := range overlays {
		variable := d.Variables[overlay.Bindings["openVariable"]]
		var initial bool
		if !pageNodeID.MatchString(id) || !pageNodeID.MatchString(overlay.Bindings["root"]) || variable.Mode != "state" || variable.Type != "boolean" || json.Unmarshal(variable.Initial, &initial) != nil || initial || open[overlay.Bindings["openVariable"]] {
			return fmt.Errorf("page overlay %s needs a separate root, title, supported kind and unique boolean state initialized false", id)
		}
		open[overlay.Bindings["openVariable"]] = true
	}
	bound := map[string]bool{}
	for _, event := range events {
		found := sources[event.Source]

		key := event.Source + "/" + event.Control
		if !found || bound[key] || len(event.Effects) == 0 || len(event.Effects) > maxPageEffects {
			return fmt.Errorf("page event %s needs one button click or registered selection binding and one to %d effects", event.Source, maxPageEffects)
		}
		bound[key] = true
		if (len(event.Effects) > 1 || slices.ContainsFunc(event.Effects, func(e PageEffect) bool {
			return e.Kind == "action"
		})) && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.106") {
			return fmt.Errorf("page effect chains require UI profile v2.106")
		}
		for at, effect := range event.Effects {
			last := at == len(event.Effects)-1
			switch effect.Kind {
			case "set":
				if effect.Action != nil || effect.Navigate != nil {
					return fmt.Errorf("page effect %s is not one effect", event.Source)
				}
				variable := d.Variables[effect.Target]
				if event.Event == "select" && (!(variable.Mode == "state" && slices.Contains([]string{"page", "overlay"}, variable.Scope) || PageUIProfileSupports(d.profileVersion(), "platform.page.v2.89") && variable.Mode == "shared" && variable.Scope == "application" && variable.Writable && p.rootPresentationSource(event.Source)) || !slices.Contains([]string{"string", "boolean"}, variable.Type) || open[effect.Target]) {
					return fmt.Errorf("selection event needs writable presentation scalar state without navigation or overlay control")
				}
				if effect.From != "" {
					source := d.Variables[effect.From]
					if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.108") || len(effect.Value) != 0 || effect.From == effect.Target || source.Type == "" || source.Type != variable.Type || !variable.IsWritable() {
						return fmt.Errorf("page event %s needs a readable source variable of the target's type", event.Source)
					}
				} else if !variable.IsWritable() || pageLiteralType(effect.Value) != variable.Type {
					return fmt.Errorf("page event %s needs a matching state value", event.Source)
				}

			case "action":
				a := effect.Action
				if event.Event == "select" || effect.Target != "" || len(effect.Value) != 0 || effect.Navigate != nil || a == nil || a.Ref.Check() != nil || a.Ref.Kind != AssetAction {
					return fmt.Errorf("page action effect %s needs one original action on a click", event.Source)
				}
				if v := d.Variables[a.RecordVariable]; a.RecordVariable != "" && v.Type != "record" {
					return fmt.Errorf("page action effect %s needs a record variable", event.Source)
				}
				if len(a.Inputs) > 0 && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.108") || len(a.Inputs) > pageWidgets.Runtime.Interface.MaxPorts {
					return fmt.Errorf("page action effect %s inputs need UI profile v2.108 and a bounded count", event.Source)
				}
				for parameter, arg := range a.Inputs {
					if !pageNodeID.MatchString(parameter) || (arg.Variable != "") == (len(arg.Literal) != 0) || arg.Variable != "" && d.Variables[arg.Variable].Type == "" || arg.Variable == "" && pageLiteralType(arg.Literal) == "" {
						return fmt.Errorf("page action effect %s input %s is invalid", event.Source, parameter)
					}
				}
			case "navigate", "return":
				if event.Event == "select" || !last {
					return fmt.Errorf("page event %s navigation must end a click chain", event.Source)
				}
				if err := d.checkNavigationEffect(effect); err != nil {
					return err
				}
			default:
				return fmt.Errorf("page effect %s has an unknown kind", event.Source)
			}
		}
	}

	return nil
}

func (p *PageOverlay) UnmarshalJSON(raw []byte) error {
	type value PageOverlay
	return RetainPageSection(raw, (*value)(p), &p.LegacyVisual)
}

func (p PageOverlay) MarshalJSON() ([]byte, error) {
	type value PageOverlay
	return MarshalPageSection(value(p), p.LegacyVisual)
}

type PageInterface struct {
	Version int                 `json:"version"`
	Inputs  map[string]PagePort `json:"inputs,omitempty"`
	Outputs map[string]PagePort `json:"outputs,omitempty"`
}

type PagePort struct {
	Variable string    `json:"variable"`
	Type     string    `json:"type"`
	Object   *AssetRef `json:"object,omitempty"`
	Required bool      `json:"required,omitempty"`
}

type PageNavigation struct {
	Page             AssetRef             `json:"page"`
	InterfaceVersion int                  `json:"interfaceVersion"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Results          map[string]string    `json:"results,omitempty"`
}

func (d PageInputSchema) checkInterface() error {
	if d.Interface == nil {
		for id, v := range d.Variables {
			if v.Mode == "input" {
				return fmt.Errorf("page input %s needs an interface", id)
			}
		}
		return nil
	}
	i, c := d.Interface, pageWidgets.Runtime.Interface
	if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.6") || i.Version < 1 || i.Version > c.MaxVersion || len(i.Inputs) > c.MaxPorts || len(i.Outputs) > c.MaxPorts {
		return fmt.Errorf("page interface needs v2.6, a version and bounded ports")
	}
	inputs := map[string]bool{}
	for _, ports := range []map[string]PagePort{i.Inputs, i.Outputs} {
		for id, p := range ports {
			v, ok := d.Variables[p.Variable]
			if !pageNodeID.MatchString(id) || !ok || v.Scope != "page" || v.Type != p.Type || !slices.Contains(c.ValueTypes, p.Type) || (p.Type == "record" || p.Type == "object-set") && (p.Object == nil || p.Object.Check() != nil || p.Object.Kind != AssetObject) || p.Type != "record" && p.Type != "object-set" && p.Object != nil {
				return fmt.Errorf("page port %s needs a typed page variable and record object", id)
			}
		}
	}
	for _, p := range i.Outputs {
		if p.Type == "object-set" {
			return fmt.Errorf("collection interfaces are input-only")
		}
	}
	for id, p := range i.Inputs {
		if p.Type == "object-set" && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.85") {
			return fmt.Errorf("collection input needs v2.85")
		}
		if d.Variables[p.Variable].Mode != "input" || inputs[p.Variable] {
			return fmt.Errorf("page input %s needs a unique input variable", id)
		}
		inputs[p.Variable] = true
	}
	for id, v := range d.Variables {
		if v.Mode == "input" && !inputs[id] {
			return fmt.Errorf("page input variable %s is unbound", id)
		}
	}
	return nil
}

func (p Page) RecordVariableObject(variable string) string {
	return p.RecordResourceObject(variable).Name
}

func CheckPageNavigation(source Page, target Page, nav PageNavigation) error {
	sourceSchema, targetSchema := source.inputSchema(), target.inputSchema()
	version := 0
	inputs, outputs := map[string]PagePort{}, map[string]PagePort{}
	if targetSchema.Interface != nil {
		version = targetSchema.Interface.Version
		inputs = targetSchema.Interface.Inputs
		outputs = targetSchema.Interface.Outputs
	}
	if nav.InterfaceVersion != version {
		return fmt.Errorf("page navigation to %s interface version differs", nav.Page)
	}
	for id, port := range inputs {
		if _, ok := nav.Inputs[id]; port.Required && !ok {
			return fmt.Errorf("page navigation to %s misses input %s", nav.Page, id)
		}
	}
	for id, value := range nav.Inputs {
		port, ok := inputs[id]
		typ := pageLiteralType(value.Literal)
		if value.Variable != "" {
			typ = sourceSchema.Variables[value.Variable].Type
		}
		if !ok || typ != port.Type || port.Type == "object-set" && (port.Object == nil || source.CollectionInputObject(value.Variable) != *port.Object || !PageUIProfileSupports(sourceSchema.profileVersion(), "platform.page.v2.85")) || port.Type == "record" && (port.Object == nil || source.RecordVariableObject(value.Variable) != port.Object.Name) {
			return fmt.Errorf("page navigation input %s type or object differs", id)
		}
	}
	for id, variable := range nav.Results {
		port, ok := outputs[id]
		v := sourceSchema.Variables[variable]
		if !ok || !v.IsWritable() || v.Type != port.Type || v.Type == "record" {
			return fmt.Errorf("page navigation output %s type differs", id)
		}
	}
	return nil
}

func (p Page) NavigationTargets() []AssetRef {
	refs := []AssetRef{}
	safe, err := p.Safety()
	if err != nil {
		return refs
	}
	for _, trigger := range safe.ActionTriggers {
		for _, event := range trigger.Events {
			if nav := event.Navigation(); nav != nil {
				refs = append(refs, nav.Page)
			}
		}
	}
	if iface := safe.InputSchema.Interface; iface != nil {
		for _, ports := range []map[string]PagePort{iface.Inputs, iface.Outputs} {
			for _, port := range ports {
				if port.Object != nil {
					refs = append(refs, *port.Object)
				}
			}
		}
	}
	return refs
}

// These widgets show the record their original producer holds, so the page
// object they declare must be that producer's exact object identity.
var recordProducerWidgets = []string{"record-card", "breadcrumb", "graph-explorer", "vertex-graph", "record-comments", "record-uploader", "media-preview", "pdf-viewer", "image-annotation", "scene-3d"}

func (p Page) CheckRecordPorts() error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	selections := map[string]AssetRef{}
	for _, selection := range safe.InputSchema.Selections {
		if !pageNodeID.MatchString(selection.Name) || selections[selection.Name].Name != "" || selection.Object.Check() != nil {
			return fmt.Errorf("page selections need unique typed record names")
		}
		selections[selection.Name] = selection.Object
	}
	for _, binding := range safe.DataBindings {
		if selected := binding.Ports["selection"]; selected != "" {
			if selections[selected] != binding.ObjectRef(safe) {
				return fmt.Errorf("selection %s does not hold %s", selected, binding.ObjectRef(safe).Name)
			}
		}
		if parent := binding.Ports["parentSelection"]; parent != "" {
			if selections[parent].Name == "" {
				return fmt.Errorf("parent selection %s does not hold a record", parent)
			}
		}
	}
	for _, binding := range safe.DataBindings {
		variable := binding.Ports["recordVariable"]
		producerBound := slices.Contains(recordProducerWidgets, binding.Ports["widget"]) || safe.InputSchema.Variables[variable].Source != nil && safe.InputSchema.Variables[variable].Source.Port != "" && binding.Ports["widget"] != "observation"
		if variable != "" && producerBound && safe.RecordVariableInterface(variable) == "" {
			object := safe.RecordResourceObject(variable)
			if object.Name == "" || object != binding.ObjectRef(safe) {
				return fmt.Errorf("page record binding differs from its original producer")
			}
		}
	}
	for _, binding := range safe.DataBindings {
		variable := binding.Ports["recordVariable"]
		if binding.Ports["widget"] != "observation" || variable == "" {
			continue
		}
		for _, resource := range binding.Resources {
			if resource.Kind == "observation" && resource.Source == "statistics" && (resource.Object == nil || safe.RecordResourceObject(variable) != *resource.Object) {
				return fmt.Errorf("statistics context differs from its original full asset identity")
			}
		}
	}
	if safe.InputSchema.Interface != nil {
		for _, port := range safe.InputSchema.Interface.Outputs {
			if port.Type == "record" && (port.Object == nil || safe.RecordVariableObject(port.Variable) != port.Object.Name) {
				return fmt.Errorf("page output differs from original record object")
			}
		}
	}
	return nil
}

func (d PageInputSchema) checkNavigationEffect(effect PageEffect) error {
	if effect.Target != "" || len(effect.Value) != 0 || effect.Action != nil || (effect.Kind == "return") == (effect.Navigate != nil) {
		return fmt.Errorf("page navigation needs one handler")
	}
	if effect.Kind == "return" {
		if d.Interface == nil {
			return fmt.Errorf("page return needs an interface")
		}
		return nil
	}
	n := effect.Navigate
	c := pageWidgets.Runtime.Interface
	if n == nil || n.Page.Check() != nil || n.Page.Kind != AssetPage || n.InterfaceVersion < 0 || n.InterfaceVersion > c.MaxVersion || len(n.Inputs) > c.MaxPorts || len(n.Results) > c.MaxPorts {
		return fmt.Errorf("page navigation needs a page and bounded interface bindings")
	}
	for id, arg := range n.Inputs {
		if !pageNodeID.MatchString(id) || (arg.Variable != "") == (len(arg.Literal) != 0) || arg.Variable != "" && d.Variables[arg.Variable].Type == "" || arg.Variable == "" && pageLiteralType(arg.Literal) == "" {
			return fmt.Errorf("page navigation input %s is invalid", id)
		}
	}
	return nil
}

func pageNavigationEdge(asset ReleaseAsset, target AssetRef) bool {
	if asset.Ref.Kind != AssetPage || target.Kind != AssetPage {
		return false
	}
	var p Page
	if json.Unmarshal(asset.Body, &p) != nil {
		return false
	}
	for _, ref := range p.NavigationTargets() {
		if ref == target {
			return true
		}
	}
	return false
}

type PageLoop struct {
	Collection   string `json:"collection"`
	ItemVariable string `json:"itemVariable"`
	Limit        int    `json:"limit"`
}

func (d *PageDocument) ActiveQueryPlans(sections []Section) map[string]bool {
	active := map[string]bool{}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Query.InventoryUIProfile) || d.Root == "" {
		for id := range d.Queries {
			active[id] = true
		}
		return active
	}
	widgets := map[string]Section{}
	for _, s := range sections {
		widgets[s.ID] = s
	}
	seen := map[string]bool{}
	var scan func(any)
	var visit func(string)
	visit = func(id string) {
		if seen[id] {
			return
		}
		seen[id] = true
		if q, ok := d.Queries[id]; ok {
			active[id] = true
			scan(q)
		}
		if v, ok := d.Variables[id]; ok {
			scan(v)
		}
		if n, ok := d.Nodes[id]; ok {
			scan(n)
		}
		if s, ok := widgets[id]; ok {
			scan(s)
		}
	}
	var walkJSON func(any)
	walkJSON = func(value any) {
		switch v := value.(type) {
		case string:
			visit(v)
		case []any:
			for _, x := range v {
				walkJSON(x)
			}
		case map[string]any:
			for _, x := range v {
				walkJSON(x)
			}
		}
	}
	scan = func(value any) {
		raw, err := json.Marshal(value)
		if err != nil {
			for id := range d.Queries {
				active[id] = true
			}
			return
		}
		var decoded any
		if json.Unmarshal(raw, &decoded) != nil {
			for id := range d.Queries {
				active[id] = true
			}
			return
		}
		walkJSON(decoded)
	}
	visit(d.Root)
	for _, overlay := range d.Overlays {
		scan(overlay)
	}
	scan(d.Interface)
	scan(d.Events)
	main := active
	active = map[string]bool{}
	seen = map[string]bool{}
	for _, entry := range d.UnusedWidgets {
		visit(entry.Node)
	}
	for id := range d.Queries {
		if !active[id] {
			main[id] = true
		}
	}
	return main
}

func (p Page) CheckQuerySetConditions(named map[AssetBinding]NamedQuery) error {
	schema := p.inputSchema()
	seen := map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		q := schema.Queries[id]
		count := len(q.Conditions)
		if q.Query != nil {
			decl := named[*q.Query]
			var terms []json.RawMessage
			if json.Unmarshal(decl.Domain, &terms) == nil {
				count += len(terms)
			}
			if decl.By != "" {
				count++
			}
		}
		if count > QuerySetMaxConditions {
			return fmt.Errorf("query set source %s exceeds its condition budget", id)
		}
		if q.Set != nil {
			for _, input := range q.Set.Inputs {
				if err := visit(input); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for id, q := range schema.Queries {
		if q.Set != nil {
			if err := visit(id); err != nil {
				return err
			}
		}
	}
	return nil
}

func (q PageQuery) MissingSetInput(plans map[string]PageQuery) bool {
	if q.Set == nil {
		return false
	}
	for _, id := range q.Set.Inputs {
		if _, ok := plans[id]; !ok {
			return true
		}
	}
	return false
}

func (d PageInputSchema) CheckQuerySets() error {
	for root, plan := range d.Queries {
		if plan.Set == nil {
			continue
		}
		if !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.18") {
			return fmt.Errorf("query set %s requires v2.18", root)
		}
		nodes := 0
		var visit func(string, int, map[string]bool) error
		visit = func(id string, depth int, path map[string]bool) error {
			nodes++
			q, ok := d.Queries[id]
			if q.Query != nil && q.Query.Ref.Kind == AssetLinkType {
				return fmt.Errorf("link traversal is not a set predicate source")
			}
			if !ok || !pageNodeID.MatchString(id) || path[id] || depth > QuerySetMaxDepth || nodes > QuerySetMaxNodes || q.Object != plan.Object || q.Owner != plan.Owner || q.ItemOwner != plan.ItemOwner {
				return fmt.Errorf("query set %s has a missing, cyclic, incompatible or oversized source", root)
			}
			if q.Set == nil {
				return nil
			}
			if !slices.Contains(pageWidgets.Runtime.Query.Set.Operations, q.Set.Op) || len(q.Set.Inputs) != 2 {
				return fmt.Errorf("query set %s needs a binary supported operation", id)
			}
			path[id] = true
			defer delete(path, id)
			for _, input := range q.Set.Inputs {
				if err := visit(input, depth+1, path); err != nil {
					return err
				}
			}
			return nil
		}
		if err := visit(root, 0, map[string]bool{}); err != nil {
			return err
		}
	}
	return nil
}

func (d *PageDocument) CheckQuerySets() error { return d.inputSchema().CheckQuerySets() }

func (d *PageDocument) sharedContextRecord(id string) bool {
	if d == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.ContextViews.SharedUIProfile) {
		return false
	}
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == AssetObject && v.Source.Object.Check() == nil
}

func (d PageInputSchema) sharedContextRecord(id string) bool {
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == AssetObject && v.Source.Object.Check() == nil
}
