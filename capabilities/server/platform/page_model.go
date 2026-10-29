package platform

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
)

const PageModelProfile = "platform.page.v2.110"

const PageModelMaxFields = 64

type FieldRef struct {
	Name string   `json:"name,omitempty"`
	Path []string `json:"path,omitempty"`

	Slot      string `json:"slot,omitempty"`
	Transform string `json:"transform,omitempty"`
}

func (f FieldRef) Segments() []string {
	if len(f.Path) > 0 {
		return f.Path
	}
	return []string{f.Name}
}

func (f FieldRef) Key() string {
	return strings.Join(f.Segments(), ".")
}

type ModelRelation struct {
	ParentType      string `json:"parentType"`
	ParentSelection string `json:"parentSelection,omitempty"`
	Field           string `json:"field"`
	Name            string `json:"name,omitempty"`
}

type ModelRecordSet struct {
	RecordVariable string        `json:"recordVariable,omitempty"`
	Name           string        `json:"name,omitempty"`
	Query          *AssetBinding `json:"query,omitempty"`
}

type ModelRule struct {
	Action AssetRef          `json:"action"`
	Inputs map[string]string `json:"inputs,omitempty"`
}

type OptionsRef struct {
	Set  *ModelRecordSet `json:"set,omitempty"`
	Rule *ModelRule      `json:"rule,omitempty"`
}

type ModelBinding struct {
	Type       string          `json:"type"`
	Fields     []FieldRef      `json:"fields,omitempty"`
	Relation   *ModelRelation  `json:"relation,omitempty"`
	Set        *ModelRecordSet `json:"set,omitempty"`
	Where      []Expr          `json:"where,omitempty"`
	Label      *Expr           `json:"label,omitempty"`
	Options    *OptionsRef     `json:"options,omitempty"`
	Visibility string          `json:"visibility,omitempty" enum:"hide,mark"`
}

func ModelObject(typ string) AssetRef {
	app, _, _ := strings.Cut(typ, ".")
	return AssetRef{App: app, Kind: AssetObject, Name: typ}
}

func (s Section) ModelObject(page Page) AssetRef {
	if model := s.PrimaryModel(); model != nil {
		return ModelObject(model.Type)
	}
	if s.Object.Name != "" {
		return s.Object
	}
	return page.Object
}

var modelScalarSlots = pageWidgets.Runtime.ModelBinding.FieldSlots

func (p *Page) NormalizeModelBindings(lookup func(string) (EntityInfo, bool)) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	safe = safe.ResolveBindings(lookup)
	safe.retainedVersion = ""
	*p = safe
	return nil
}
func (p Page) CheckModelBindings(lookup func(string) (EntityInfo, bool), criteria func(AssetRef) (*RuleCriteria, bool), engines ...RuleEngine) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	for _, binding := range safe.DataBindings {
		if err := safe.CheckDataBinding(binding, lookup, criteria, engines...); err != nil {
			return err
		}
	}
	return nil
}

func CheckRuleExpression(expr Expr, reads RuleReadSpec, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) error {
	schema, err := RuleSchema(expr, reads, lookup, engines...)
	if err != nil {
		return err
	}
	_, err = ruleEngineOf(engines).CompileExpr(expr, schema)
	return err
}

func PageRuleInputFits(variable, kind string) bool {
	return variable == kind || variable == "string" && (kind == "text" || kind == "choice" || kind == "date" || kind == "datetime") || variable == "number" && (kind == "number" || kind == "integer")
}

func (p Page) ModelDependencies() []AssetRef {
	refs := []AssetRef{}
	safe, err := p.Safety()
	if err != nil {
		return refs
	}
	for _, binding := range safe.DataBindings {
		for _, model := range binding.ModelBindings() {
			if safe.WindowVariableInterface(binding.Ports["collectionVariable"]) == "" {
				ref := ModelObject(model.Type)
				if binding.Object.Name == model.Type {
					ref = binding.Object
				} else if safe.Object.Name == model.Type {
					ref = safe.Object
				}
				refs = append(refs, ref)
			}
			if model.Relation != nil {
				refs = append(refs, ModelObject(model.Relation.ParentType))
			}
			sets := []*ModelRecordSet{model.Set}
			if model.Options != nil {
				sets = append(sets, model.Options.Set)
				if model.Options.Rule != nil {
					refs = append(refs, model.Options.Rule.Action)
				}
			}
			for _, set := range sets {
				if set == nil {
					continue
				}
				if set.Query != nil {
					refs = append(refs, set.Query.Ref)
				}
				if set.Name != "" {
					refs = append(refs, safe.RecordResourceObject(set.RecordVariable))
				}
			}
		}
	}
	return refs
}

func ModelFields(info EntityInfo, model *ModelBinding, lookup func(string) (EntityInfo, bool)) EntityInfo {
	if model == nil {
		return info
	}
	info.Fields = slices.Clone(info.Fields)
	for _, ref := range model.Fields {
		if len(ref.Segments()) < 2 {
			continue
		}
		field, err := RecordPathField(model.Type, ref.Segments(), lookup)
		if err != nil {
			continue
		}
		field.Name, field.Title, field.ReadOnly = ref.Key(), strings.Join(ref.Segments(), " → "), true
		info.Fields = append(info.Fields, field)
	}
	return info
}

type PageDataBinding struct {
	Value      *Expr                       `json:"value,omitempty"`
	Acyclic    bool                        `json:"acyclic,omitempty"`
	ID         string                      `json:"id"`
	Owner      string                      `json:"owner,omitempty"`
	Kind       string                      `json:"kind"`
	Object     AssetRef                    `json:"object"`
	Model      *ModelBinding               `json:"model,omitempty"`
	Models     map[string]ModelViewBinding `json:"models,omitempty"`
	Fields     []FieldRef                  `json:"fields,omitempty"`
	FieldSlots map[string][]FieldRef       `json:"fieldSlots,omitempty"`
	Ports      map[string]string           `json:"ports,omitempty"`
	Query      *AssetBinding               `json:"query,omitempty"`
	Resources  []PageResourceBinding       `json:"resources,omitempty"`
	Limit      int                         `json:"limit,omitempty"`
}

type PageResourceBinding struct {
	ReadOnly         bool                 `json:"readOnly,omitempty"`
	ID               string               `json:"id"`
	Kind             string               `json:"kind"`
	Asset            *AssetBinding        `json:"asset,omitempty"`
	Object           *AssetRef            `json:"object,omitempty"`
	URL              string               `json:"url,omitempty"`
	Origin           string               `json:"origin,omitempty"`
	ContentVersion   string               `json:"contentVersion,omitempty"`
	InterfaceVersion int                  `json:"interfaceVersion,omitempty"`
	Inputs           map[string]PageValue `json:"inputs,omitempty"`
	Outputs          map[string]string    `json:"outputs,omitempty"`
	Fields           []FieldRef           `json:"fields,omitempty"`
	Source           string               `json:"source,omitempty"`
	Limit            int                  `json:"limit,omitempty"`
}

type PageActionTrigger struct {
	ID         string                `json:"id"`
	Owner      string                `json:"owner,omitempty"`
	Binding    string                `json:"binding"`
	Actions    []AssetRef            `json:"actions,omitzero"`
	Function   *AssetBinding         `json:"function,omitempty"`
	Operation  *AssetBinding         `json:"operation,omitempty"`
	Inputs     map[string]Binding    `json:"inputs,omitempty"`
	Defaults   []PageActionParameter `json:"defaults,omitempty"`
	InlineEdit *PageInlineEdit       `json:"inlineEdit,omitempty"`
	Events     []PageEventBinding    `json:"events,omitempty"`
}

type PageInputSchema struct {
	profile    string
	Variables  map[string]PageVariable `json:"variables,omitempty"`
	Queries    map[string]PageQuery    `json:"queries,omitempty"`
	Interface  *PageInterface          `json:"interface,omitempty"`
	Selections []SelectionVariable     `json:"selections,omitempty"`
}

type PageScope struct {
	ID            string            `json:"id"`
	Parent        string            `json:"parent,omitempty"`
	Kind          string            `json:"kind"`
	Binding       string            `json:"binding,omitempty"`
	Bindings      map[string]string `json:"bindings,omitempty"`
	MaxIterations int               `json:"maxIterations,omitempty"`
	Inactive      bool              `json:"inactive,omitempty"`
}

type PageScopeVisibility struct {
	Scopes []PageScope       `json:"scopes"`
	Owners map[string]string `json:"owners"`
	Root   string            `json:"root"`
}

type PageWidgetSpec struct {
	Format string          `json:"format"`
	Body   json.RawMessage `json:"body"`
}

func (p Page) Safety() (Page, error) {
	if p.Format == 3 {
		if p.retainedVersion == "" {
			version, err := PageContentVersion(p)
			if err != nil {
				return Page{}, err
			}
			p.retainedVersion = version
		}
		return p, nil
	}
	if p.Format != 0 {
		return Page{}, fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.Document != nil && (p.Document.FormatVersion != 2 || !SupportsPageUIProfile(p.Document.UIProfile)) {
		return Page{}, fmt.Errorf("unsupported retained page profile")
	}
	original := p
	p.Format = 3
	p.DataBindings = []PageDataBinding{}
	p.ActionTriggers = []PageActionTrigger{{ID: "page", Binding: "page", Actions: original.Actions}}
	p.InputSchema = PageInputSchema{Selections: original.Selections}
	p.ScopeVisibility = PageScopeVisibility{Root: "page", Owners: map[string]string{}, Scopes: []PageScope{{ID: "page", Kind: "page"}}}
	if original.Document != nil {
		d := original.Document
		p.ScopeVisibility.Scopes = nil
		p.InputSchema.Variables, p.InputSchema.Queries, p.InputSchema.Interface = d.Variables, d.Queries, d.Interface
		p.InputSchema.profile = d.UIProfile
		parent := map[string]string{}
		for id, node := range d.Nodes {
			for _, child := range node.Children {
				parent[child] = id
			}
		}
		for id, overlay := range d.Overlays {
			parent[overlay.Root] = id
		}
		for _, unused := range d.UnusedWidgets {
			parent[unused.Node] = unused.Parent
		}
		p.ScopeVisibility.Root = d.Root
		for _, id := range slices.Sorted(maps.Keys(d.Nodes)) {
			node := d.Nodes[id]
			scope := PageScope{ID: id, Parent: parent[id], Kind: "page", Binding: node.Section, Bindings: map[string]string{"valueVariable": node.ValueVariable, "activeVariable": node.ActiveVariable, "visibleWhen": node.VisibleWhen, "enabledWhen": node.EnabledWhen}}
			if node.Loop != nil {
				scope.Kind = "loop"
				scope.MaxIterations = node.Loop.Limit
				scope.Bindings["collection"], scope.Bindings["itemVariable"] = node.Loop.Collection, node.Loop.ItemVariable
			}
			for _, unused := range d.UnusedWidgets {
				if unused.Node == id {
					scope.Inactive = true
				}
			}
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, scope)
			if node.Section != "" {
				p.ScopeVisibility.Owners[node.Section] = id
			}
		}
		for _, id := range slices.Sorted(maps.Keys(d.Overlays)) {
			overlay := d.Overlays[id]
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: id, Kind: "overlay", Bindings: map[string]string{"root": overlay.Root, "openVariable": overlay.OpenVariable}})
		}
		for i, event := range d.Events {
			p.ActionTriggers = append(p.ActionTriggers, PageActionTrigger{ID: fmt.Sprintf("event:%s:%d", event.Source, i), Binding: event.Source, Events: []PageEventBinding{event}})
		}
	}
	visualSections := []map[string]any{}
	sections := original.Sections
	if len(sections) == 0 {
		sections = []Section{{ID: "list", Widget: "table", Fields: original.ListFields, Object: original.Object}, {ID: "detail", Widget: "detail", Fields: original.DetailFields, Object: original.Object}}
	}
	for index, s := range sections {
		id := s.ID
		if id == "" {
			id = fmt.Sprintf("section-%d", index+1)
		}
		d := PageDataBinding{ID: id, Owner: p.ScopeVisibility.Owners[id], Kind: "records", Object: s.ModelObject(original), Model: s.Model, Ports: map[string]string{}, FieldSlots: map[string][]FieldRef{}}
		if original.Document == nil {
			scopeID := "binding:" + id
			p.ScopeVisibility.Scopes = append(p.ScopeVisibility.Scopes, PageScope{ID: scopeID, Parent: p.ScopeVisibility.Root, Kind: "page", Binding: id})
			p.ScopeVisibility.Owners[id] = scopeID
			d.Owner = scopeID
		}
		if original.Document != nil && s.CollectionVariable != "" {
			variable := original.Document.Variables[s.CollectionVariable]
			if variable.Source != nil && variable.Source.Kind == "plan" {
				d.Limit = original.Document.Queries[variable.Source.Query].Limit
			}
		}
		d.Ports["widget"] = s.Widget
		if s.RecordVariable != "" || WidgetReadsSelection(s.Widget) {
			d.Kind = "record"
		}

		if s.Query.Name != "" {
			d.Query = &AssetBinding{Ref: s.Query}
		}
		for _, name := range s.Fields {
			d.Fields = append(d.Fields, legacyFieldRef(name))
		}
		bytes, _ := json.Marshal(s)
		if err := checkLegacyPageProfile(original.Document, s, bytes); err != nil {
			return Page{}, err
		}
		var wire map[string]any
		_ = json.Unmarshal(bytes, &wire)
		if composite, ok := wire["composite"]; ok {
			var value struct {
				Bindings map[string]ModelViewBinding
				Value    *Expr
				Acyclic  bool
			}
			if err := json.Unmarshal(Raw(composite), &value); err != nil {
				return Page{}, err
			}
			d.Kind = "composite"
			d.Models = value.Bindings
			d.Value = value.Value
			d.Acyclic = value.Acyclic
		}
		for name, value := range s.variablePorts() {
			d.Ports[name] = value
		}
		for _, key := range []string{"selection", "parentSelection", "relation", "filterSearchVariable", "collectionOutputVariable", "measure"} {
			if value, ok := wire[key].(string); ok && value != "" {
				d.Ports[key] = value
			}
		}
		if contract := pageWidget(s.Widget); contract != nil && (contract.SelectionMode == "read" || contract.SelectionMode == "write") {
			d.Ports["selectionMode"] = contract.SelectionMode
		}
		if slices.Contains([]string{"table", "chart", "metric", "form"}, s.Widget) {
			d.Ports["parentable"] = "true"
		}
		if s.Widget == "form" {
			d.Ports["creates"] = "true"
		}
		for _, path := range modelScalarSlots {
			if _, already := d.FieldSlots[path]; already {
				continue
			}
			for _, value := range legacyPagePathValues(wire, strings.Split(path, ".")) {
				text, ok := value.(string)
				if !ok || text == "" || path == "measure" && text == "count" {
					continue
				}
				field := FieldRef{Name: text}
				if left, right, ok := strings.Cut(text, ":"); ok {
					if path == "measure" {
						field.Name, field.Transform = right, left
					} else {
						field.Name, field.Transform = left, right
					}
				}
				d.FieldSlots[path] = []FieldRef{field}
			}
		}
		for _, path := range []string{"map.latitudeField", "map.longitudeField", "map.labelField", "map.colorField", "avatar.detailFields", "collectionBuilder.fields", "detailPresentation.barcode", "tableColumns.field", "scene.sampleTimeField", "scene.sampleAssetField", "scene.mappings.field"} {
			fields := []FieldRef{}
			for _, value := range legacyPagePathValues(wire, strings.Split(path, ".")) {
				switch v := value.(type) {
				case string:
					if v != "" {
						fields = append(fields, FieldRef{Name: v})
					}
				case []any:
					for _, name := range v {
						if text, ok := name.(string); ok && text != "" {
							fields = append(fields, FieldRef{Name: text})
						}
					}
				}
			}
			if len(fields) > 0 {
				d.FieldSlots[path] = fields
			}
		}
		if contract := pageWidget(s.Widget); contract != nil {
			for _, role := range contract.FieldRoles {
				for _, field := range legacyPagePathValues(wire, strings.Split(role.Path, ".")) {
					if text, ok := field.(string); ok && text != "" {
						d.FieldSlots[role.Path] = append(d.FieldSlots[role.Path], FieldRef{Name: text})
					}
				}
			}
		}
		for index, facet := range s.Facets {
			d.Resources = append(d.Resources, PageResourceBinding{ID: fmt.Sprintf("facet:%d", index), Kind: "filter", Source: facet.Kind, Fields: []FieldRef{{Name: facet.Field}}, Outputs: map[string]string{"value": facet.Variable}})
		}
		for _, path := range []string{"avatar.contextVariable", "avatar.contextCollectionVariable", "ai.questionVariable"} {
			for _, value := range legacyPagePathValues(wire, strings.Split(path, ".")) {
				if text, ok := value.(string); ok && text != "" {
					d.Ports[path] = text
				}
			}
		}
		resources, err := legacyPageResources(bytes, s.Widget)
		if err != nil {
			return Page{}, err
		}
		d.Resources = append(d.Resources, resources...)
		if d.Model == nil && d.Models == nil && d.Object.Name != "" {
			d.Model = &ModelBinding{Type: d.Object.Name, Fields: slices.Clone(d.Fields)}
		}
		for slot, fields := range d.FieldSlots {
			for i, field := range fields {
				if strings.Contains(field.Name, ".") {
					field.Path = strings.Split(field.Name, ".")
					field.Name = ""
				}
				fields[i] = field
			}
			d.FieldSlots[slot] = fields
		}

		if s.Model == nil && d.Models == nil && s.Object.Name == "" && s.Query.Name == "" && len(s.Fields) == 0 && s.CollectionVariable == "" && s.RecordVariable == "" && s.Selection == "" && len(d.FieldSlots) == 0 {
			d.Object = AssetRef{}
			d.Model = nil
			d.Kind = "presentation"
		}
		p.DataBindings = append(p.DataBindings, d)
		defaults := s.ActionDefaults
		var actionTable struct{ Parameters []PageActionParameter }
		if value, ok := wire["actionTable"]; ok {
			if err := json.Unmarshal(Raw(value), &actionTable); err != nil {
				return Page{}, err
			}
			defaults = actionTable.Parameters
		}
		p.ActionTriggers = append(p.ActionTriggers, PageActionTrigger{ID: id, Binding: id, Owner: d.Owner, Actions: s.Actions, Function: s.Function, Operation: s.Operation, Inputs: s.Inputs, Defaults: defaults, InlineEdit: s.InlineEdit})
		paths := append(append([]string{}, pageCompatibility.Section.BindingPaths...), pageCompatibility.Section.ResourcePaths...)
		paths = append(paths, pageCompatibility.ScalarFieldPaths...)
		for path := range d.FieldSlots {
			paths = append(paths, path)
		}
		for _, path := range paths {
			stripLegacyPagePath(wire, strings.Split(path, "."))
		}
		for key := range d.Ports {
			stripLegacyPagePath(wire, strings.Split(key, "."))
		}
		wire["id"], wire["binding"], wire["triggers"] = id, id, []string{id}
		visualSections = append(visualSections, wire)
	}
	var visualDocument any
	if original.Document != nil {
		raw, _ := json.Marshal(original.Document)
		var doc map[string]any
		_ = json.Unmarshal(raw, &doc)
		for _, key := range []string{"variables", "queries", "interface", "events"} {
			delete(doc, key)
		}
		if nodes, ok := doc["nodes"].(map[string]any); ok {
			for _, node := range nodes {
				for _, path := range pageCompatibility.Scope.BindingPaths {
					stripLegacyPagePath(node, strings.Split(path, "."))
				}
			}
		}
		if overlays, ok := doc["overlays"].(map[string]any); ok {
			for _, overlay := range overlays {
				for _, path := range pageCompatibility.Scope.OverlayPaths {
					stripLegacyPagePath(overlay, strings.Split(path, "."))
				}
			}
		}
		visualDocument = doc
	}
	body := map[string]any{"sections": visualSections, "document": visualDocument}
	if original.Layout == "list-detail" && len(original.Sections) == 0 {
		body["layout"] = original.Layout
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Page{}, err
	}
	p.WidgetSpec = PageWidgetSpec{Format: "platform.page.web.v1", Body: raw}
	if version, err := PageContentVersion(original); err == nil {
		p.retainedVersion = version
	}
	p.Sections, p.Document, p.Selections = nil, nil, nil
	p.ListFields, p.DetailFields, p.Actions = nil, nil, nil
	p.Layout = ""
	return p, nil
}

func legacyPagePathValues(value any, path []string) []any {
	if len(path) == 0 {
		return []any{value}
	}
	switch current := value.(type) {
	case map[string]any:
		key := strings.TrimSuffix(path[0], "[]")
		return legacyPagePathValues(current[key], path[1:])
	case []any:
		result := []any{}
		for _, child := range current {
			result = append(result, legacyPagePathValues(child, path)...)
		}
		return result
	}
	return nil
}

func (p Page) WithSafetyProjection(projected Page) Page {
	result := projected
	result.WidgetSpec = p.WidgetSpec
	result.retainedVersion = p.retainedVersion
	return result
}

func (p Page) MarshalJSON() ([]byte, error) {
	type legacyPage Page
	if p.Format != 3 {
		return json.Marshal(legacyPage(p))
	}
	type envelope struct {
		Format            int                 `json:"format"`
		Name              string              `json:"name"`
		Title             string              `json:"title"`
		Description       string              `json:"description,omitempty"`
		Object            AssetRef            `json:"object"`
		DataBindings      []PageDataBinding   `json:"dataBindings"`
		ActionTriggers    []PageActionTrigger `json:"actionTriggers"`
		InputSchema       PageInputSchema     `json:"inputSchema"`
		ScopeVisibility   PageScopeVisibility `json:"scopeVisibility"`
		WidgetSpec        PageWidgetSpec      `json:"widgetSpec"`
		PresentationWords map[string]string   `json:"presentationWords,omitempty"`
	}
	return json.Marshal(envelope{p.Format, p.Name, p.Title, p.Description, p.Object, p.DataBindings, p.ActionTriggers, p.InputSchema, p.ScopeVisibility, p.WidgetSpec, p.PresentationWords})
}

func (p *Page) UnmarshalJSON(raw []byte) error {
	type legacyPage Page
	var decoded legacyPage
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*p = Page(decoded)
	if p.Format != 0 && p.Format != 3 {
		return fmt.Errorf("unsupported page format %d", p.Format)
	}
	if p.Format == 3 {
		return p.checkEnvelope()
	}
	return nil
}

func RetainPageSection(raw []byte, target any, visual *map[string]json.RawMessage) error {
	if err := json.Unmarshal(raw, target); err != nil {
		return err
	}
	values := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	known := map[string]bool{}
	var fields func(reflect.Type)
	fields = func(typ reflect.Type) {
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if field.Anonymous {
				fields(field.Type)
				continue
			}
			name := strings.Split(field.Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				known[name] = true
			}
		}
	}
	typ := reflect.TypeOf(target)
	if typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	fields(typ)
	for name := range known {
		delete(values, name)
	}
	*visual = values
	return nil
}

func MarshalPageSection(value any, visual map[string]json.RawMessage) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	values := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, err
	}
	for name, raw := range visual {
		if _, known := values[name]; !known {
			values[name] = raw
		}
	}
	return json.Marshal(values)
}

func (s *Section) UnmarshalJSON(raw []byte) error {
	type value Section
	return RetainPageSection(raw, (*value)(s), &s.LegacyVisual)
}

func (s Section) MarshalJSON() ([]byte, error) {
	type value Section
	return MarshalPageSection(value(s), s.LegacyVisual)
}

var pageCompatibility = func() struct {
	Section struct {
		BindingPaths  []string `json:"bindingPaths"`
		ResourcePaths []string `json:"resourcePaths"`
	} `json:"section"`
	Scope struct {
		BindingPaths  []string `json:"bindingPaths"`
		OverlayPaths  []string `json:"overlayPaths"`
		DocumentPaths []string `json:"documentPaths"`
	} `json:"scope"`
	ScalarFieldPaths []string `json:"scalarFieldPaths"`
	ArrayFieldPaths  []string `json:"arrayFieldPaths"`
} {
	var result struct {
		Section struct {
			BindingPaths  []string `json:"bindingPaths"`
			ResourcePaths []string `json:"resourcePaths"`
		} `json:"section"`
		Scope struct {
			BindingPaths  []string `json:"bindingPaths"`
			OverlayPaths  []string `json:"overlayPaths"`
			DocumentPaths []string `json:"documentPaths"`
		} `json:"scope"`
		ScalarFieldPaths []string `json:"scalarFieldPaths"`
		ArrayFieldPaths  []string `json:"arrayFieldPaths"`
	}
	if err := json.Unmarshal(pageWidgetJSON, &result); err != nil {
		panic(err)
	}
	return result
}()

func stripLegacyPagePath(value any, path []string) {
	if len(path) == 0 {
		return
	}
	switch current := value.(type) {
	case map[string]any:
		key := strings.TrimSuffix(path[0], "[]")
		if len(path) == 1 {
			delete(current, key)
			return
		}
		stripLegacyPagePath(current[key], path[1:])
	case []any:
		for _, child := range current {
			stripLegacyPagePath(child, path)
		}
	}
}

func (b PageDataBinding) PrimaryModel() *ModelBinding {
	if b.Model != nil {
		return b.Model
	}
	for _, key := range []string{"nodes", "rows"} {
		if view, ok := b.Models[key]; ok {
			return &view.Model
		}
	}
	return nil
}

func (b PageDataBinding) ModelBindings() []*ModelBinding {
	result := []*ModelBinding{}
	if b.Model != nil {
		result = append(result, b.Model)
	}
	keys := make([]string, 0, len(b.Models))
	for key := range b.Models {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		model := b.Models[key].Model
		result = append(result, &model)
	}
	return result
}

func (p Page) VisibleSafety() Page {
	result := p
	result.DataBindings = append([]PageDataBinding{}, p.DataBindings...)
	result.InputSchema.Variables = maps.Clone(p.InputSchema.Variables)
	result.InputSchema.Queries = maps.Clone(p.InputSchema.Queries)
	scopes := map[string]PageScope{}
	for _, s := range p.ScopeVisibility.Scopes {
		scopes[s.ID] = s
	}
	for changed := true; changed; {
		changed = false
		allowed := map[string]bool{}
		for _, b := range result.DataBindings {
			allowed[b.ID] = true
		}
		for id, v := range result.InputSchema.Variables {
			missing := false
			if src := v.Source; src != nil {
				missing = src.Section != "" && !allowed[src.Section]
				if src.Variable != "" && src.Kind != "application" {
					_, ok := result.InputSchema.Variables[src.Variable]
					missing = missing || !ok
				}
				if src.Query != "" {
					_, ok := result.InputSchema.Queries[src.Query]
					missing = missing || !ok
				}
				if src.Compute != nil {
					_, ok := result.InputSchema.Variables[src.Compute.RecordVariable]
					missing = missing || !ok
				}
			}
			if v.Scope == "loop-item" {
				scope, ok := scopes[v.Owner]
				_, input := result.InputSchema.Variables[scope.Bindings["collection"]]
				missing = missing || !ok || !input
			}
			if v.Expression != nil {
				for _, arg := range v.Expression.Args {
					if arg.Variable != "" {
						_, ok := result.InputSchema.Variables[arg.Variable]
						missing = missing || !ok
					}
				}
			}
			var reads func(Expr)
			reads = func(e Expr) {
				if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") {
					_, ok := result.InputSchema.Variables[e.Ref]
					missing = missing || !ok
				}
				if e.Agg != nil {
					_, ok := result.InputSchema.Variables[e.Agg.Source]
					missing = missing || !ok
				}
				for _, a := range e.Args {
					reads(a)
				}
				if e.In != nil {
					reads(*e.In)
				}
			}
			if v.Rule != nil {
				reads(*v.Rule)
			}
			if missing {
				delete(result.InputSchema.Variables, id)
				changed = true
			}
		}
		for id, q := range result.InputSchema.Queries {
			missing := q.MissingSetInput(result.InputSchema.Queries)
			for _, v := range q.Variables() {
				_, ok := result.InputSchema.Variables[v]
				missing = missing || !ok
			}
			if missing {
				delete(result.InputSchema.Queries, id)
				changed = true
			}
		}
		bindings := result.DataBindings[:0]
		for _, b := range result.DataBindings {
			valid := true
			for key, id := range b.Ports {
				if strings.HasSuffix(key, "Variable") && id != "" {
					_, ok := result.InputSchema.Variables[id]
					valid = valid && ok
				}
			}
			if valid {
				bindings = append(bindings, b)
			} else {
				changed = true
			}
		}
		result.DataBindings = bindings
	}
	allowed := map[string]bool{}
	for _, b := range result.DataBindings {
		allowed[b.ID] = true
	}
	kept := map[string]bool{p.ScopeVisibility.Root: true}
	for _, s := range p.ScopeVisibility.Scopes {
		valid := s.Binding == "" || allowed[s.Binding]
		for _, key := range []string{"valueVariable", "activeVariable", "visibleWhen", "enabledWhen", "collection", "itemVariable", "openVariable"} {
			if id := s.Bindings[key]; id != "" {
				_, ok := result.InputSchema.Variables[id]
				valid = valid && ok
			}
		}
		if valid {
			kept[s.ID] = true
		}
	}
	for changed := true; changed; {
		changed = false
		for id := range kept {
			if parent := scopes[id].Parent; parent != "" && !kept[parent] {
				delete(kept, id)
				changed = true
			}
		}
	}
	result.ScopeVisibility.Scopes = nil
	result.ScopeVisibility.Owners = map[string]string{}
	for _, s := range p.ScopeVisibility.Scopes {
		if kept[s.ID] {
			result.ScopeVisibility.Scopes = append(result.ScopeVisibility.Scopes, s)
			if s.Binding != "" {
				result.ScopeVisibility.Owners[s.Binding] = s.ID
			}
		}
	}
	result.ActionTriggers = nil
	for _, trigger := range p.ActionTriggers {
		if trigger.Binding != "page" && !allowed[trigger.Binding] && !kept[trigger.Binding] {
			continue
		}
		copy := trigger
		copy.Events = nil
		for _, event := range trigger.Events {
			valid := true
			for _, id := range append(event.Variables(), event.Reads()...) {
				_, ok := result.InputSchema.Variables[id]
				valid = valid && ok
			}
			if valid {
				copy.Events = append(copy.Events, event)
			}
		}
		result.ActionTriggers = append(result.ActionTriggers, copy)
	}
	if iface := p.InputSchema.Interface; iface != nil {
		copy := *iface
		copy.Inputs = maps.Clone(iface.Inputs)
		copy.Outputs = maps.Clone(iface.Outputs)
		for _, ports := range []map[string]PagePort{copy.Inputs, copy.Outputs} {
			for id, port := range ports {
				if _, ok := result.InputSchema.Variables[port.Variable]; !ok {
					delete(ports, id)
				}
			}
		}
		result.InputSchema.Interface = &copy
	}
	return p.WithSafetyProjection(result)
}

func (b PageDataBinding) ObjectRef(p Page) AssetRef {
	if model := b.PrimaryModel(); model != nil {
		if b.Object.Name == model.Type {
			return b.Object
		}
		return ModelObject(model.Type)
	}
	if b.Object.Name != "" {
		return b.Object
	}
	return p.Object
}

func (p Page) CheckSafety() error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	ids := map[string]bool{}
	for _, binding := range safe.DataBindings {
		if !pageNodeID.MatchString(binding.ID) || ids[binding.ID] {
			return fmt.Errorf("page data bindings need unique stable IDs")
		}
		ids[binding.ID] = true
	}
	scopes := map[string]bool{}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if !slices.Contains([]string{"page", "application", "overlay", "loop"}, scope.Kind) {
			return fmt.Errorf("unknown page scope kind")
		}
		if !pageNodeID.MatchString(scope.ID) || scopes[scope.ID] {
			return fmt.Errorf("page scopes need unique stable IDs")
		}
		scopes[scope.ID] = true
		if scope.Binding != "" && !ids[scope.Binding] {
			return fmt.Errorf("page scope binding is unavailable")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		if scope.Parent != "" && !scopes[scope.Parent] {
			return fmt.Errorf("page scope parent is unavailable")
		}
		if scope.ID == safe.ScopeVisibility.Root && scope.Parent != "" {
			return fmt.Errorf("the page root scope has no parent")
		}
	}
	for _, scope := range safe.ScopeVisibility.Scopes {
		seen := map[string]bool{}
		owner := scope.ID
		for owner != "" {
			if seen[owner] {
				return fmt.Errorf("page scope graph contains a cycle")
			}
			seen[owner] = true
			next := ""
			for _, parent := range safe.ScopeVisibility.Scopes {
				if parent.ID == owner {
					next = parent.Parent
					break
				}
			}
			owner = next
		}
		if scope.Kind == "loop" && (scope.MaxIterations < 1 || scope.MaxIterations > pageWidgets.Runtime.Loop.MaxItems || safe.InputSchema.Variables[scope.Bindings["collection"]].Type != "object-set" || safe.InputSchema.Variables[scope.Bindings["itemVariable"]].Type != "record") {
			return fmt.Errorf("page loop needs its bounded typed collection and item")
		}
	}
	for _, binding := range safe.DataBindings {
		owner := safe.ScopeVisibility.Owners[binding.ID]
		if owner == "" {
			owner = binding.Owner
		}
		for _, id := range pageBindingVariables(binding) {
			if id == "" {
				continue
			}
			variable, ok := safe.InputSchema.Variables[id]
			if !ok {
				return fmt.Errorf("page binding variable %s is unavailable", id)
			}
			if variable.Scope == "overlay" && safe.scopeOwner(owner, "overlay") != variable.Owner || variable.Scope == "loop-item" && safe.scopeOwner(owner, "loop") != variable.Owner {
				return fmt.Errorf("page binding variable escapes its original owner")
			}
		}
		if err := safe.checkWidgetPorts(binding, owner); err != nil {
			return err
		}
	}
	triggers := map[string]bool{}
	for _, trigger := range safe.ActionTriggers {
		if trigger.ID == "" || triggers[trigger.ID] || trigger.Binding != "page" && !ids[trigger.Binding] && !scopes[trigger.Binding] {
			return fmt.Errorf("page trigger needs a unique ID and original owner")
		}
		triggers[trigger.ID] = true
	}
	if err := safe.checkEnvelope(); err != nil {
		return err
	}
	if err := safe.InputSchema.checkProfile(); err != nil {
		return err
	}
	if err := safe.InputSchema.CheckVariables(); err != nil {
		return err
	}
	if err := safe.checkQueries("page"); err != nil {
		return err
	}
	if err := safe.InputSchema.checkInterface(); err != nil {
		return err
	}
	// A bare document declares no page object, selections or producers to compare.
	if safe.Object.Name != "" {
		if err := safe.CheckRecordPorts(); err != nil {
			return err
		}
	}
	return safe.checkEvents()
}

func legacyPageResources(raw []byte, widget string) ([]PageResourceBinding, error) {
	var old struct {
		Scene *struct {
			Mappings []struct{ ID, Source, Field string }
		}
		AI          *struct{ Kind, ReplyField, QuestionVariable string }
		RecordLinks []struct {
			Object AssetRef
			Field  string
		}
		Embedding *struct {
			ReadOnly         bool
			Kind             string
			Page             AssetBinding
			ContentVersion   string
			InterfaceVersion int
			Inputs           map[string]PageValue
			Results          map[string]string
		}
		ExternalFrame  *struct{ URL, Origin string }
		Image          *struct{ URL string }
		AssetDirectory *struct {
			Items []struct {
				ID    string
				Asset AssetBinding
			}
		}
		GraphExplorer *struct {
			Objects []struct {
				Object     AssetRef
				LabelField string
			}
			Relations []struct {
				ID      string
				Binding AssetBinding
			}
			Outputs []struct {
				ID       string
				Object   AssetRef
				Variable string
			}
		}
		VertexGraph *struct {
			Groups []struct {
				ID        string
				Binding   AssetBinding
				Direction string
				Limit     int
			}
		}
		Observation *struct {
			Kind, TimeField, AssetField, AverageField, RowOutput, AssetOutput string
			AssetObject                                                       *AssetRef
			Signals                                                           []struct{ Field string }
			Metadata                                                          *struct{ Plant, Device, Asset, Status string }
		}
	}
	if err := json.Unmarshal(raw, &old); err != nil {
		return nil, err
	}
	resources := []PageResourceBinding{}
	if old.Scene != nil {
		for _, m := range old.Scene.Mappings {
			resources = append(resources, PageResourceBinding{ID: m.ID, Kind: "record-mapping", Source: m.Source, Fields: []FieldRef{{Name: m.Field}}})
		}
	}
	if r := old.AI; r != nil {
		v := PageResourceBinding{ID: "ai", Kind: "ai", Source: r.Kind, Outputs: map[string]string{"question": r.QuestionVariable}}
		if r.ReplyField != "" {
			v.Fields = []FieldRef{{Name: r.ReplyField, Slot: "replyField"}}
		}
		resources = append(resources, v)
	}
	for _, r := range old.RecordLinks {
		object := r.Object
		resources = append(resources, PageResourceBinding{ID: r.Object.String() + "/" + r.Field, Kind: "incoming-reference", Object: &object, Fields: []FieldRef{{Name: r.Field}}})
	}
	if r := old.Embedding; r != nil {
		resources = append(resources, PageResourceBinding{ReadOnly: r.ReadOnly, ID: "embedding", Kind: "page", Asset: &r.Page, ContentVersion: r.ContentVersion, InterfaceVersion: r.InterfaceVersion, Inputs: r.Inputs, Outputs: r.Results, Source: r.Kind})
	}
	if r := old.ExternalFrame; r != nil {
		resources = append(resources, PageResourceBinding{ID: "externalFrame", Kind: "external-document", URL: r.URL, Origin: r.Origin})
	}
	if r := old.Image; r != nil {
		resources = append(resources, PageResourceBinding{ID: "image", Kind: "image", URL: r.URL})
	}
	if r := old.AssetDirectory; r != nil {
		for _, item := range r.Items {
			binding := item.Asset
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "asset", Asset: &binding})
		}
	}
	if r := old.GraphExplorer; r != nil {
		for _, item := range r.Objects {
			object := item.Object
			resources = append(resources, PageResourceBinding{ID: object.String(), Kind: "graph-object", Object: &object, Fields: []FieldRef{{Name: item.LabelField}}})
		}
		for _, item := range r.Relations {
			binding := item.Binding
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "graph-link", Asset: &binding})
		}
		for _, item := range r.Outputs {
			object := item.Object
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "record-output", Object: &object, Outputs: map[string]string{"record": item.Variable}})
		}
	}
	if r := old.VertexGraph; r != nil {
		for _, item := range r.Groups {
			binding := item.Binding
			resources = append(resources, PageResourceBinding{ID: item.ID, Kind: "neighborhood", Asset: &binding, Source: item.Direction, Limit: item.Limit})
		}
	}
	if r := old.Observation; r != nil {
		v := PageResourceBinding{ID: "observation", Kind: "observation", Object: r.AssetObject, Source: r.Kind, Outputs: map[string]string{"row": r.RowOutput, "asset": r.AssetOutput}}
		for slot, name := range map[string]string{"timeField": r.TimeField, "assetField": r.AssetField, "averageField": r.AverageField} {
			if name != "" {
				v.Fields = append(v.Fields, FieldRef{Name: name, Slot: slot})
			}
		}
		if r.Metadata != nil {
			for slot, name := range map[string]string{"metadata.plant": r.Metadata.Plant, "metadata.device": r.Metadata.Device, "metadata.asset": r.Metadata.Asset, "metadata.status": r.Metadata.Status} {
				if name != "" {
					v.Fields = append(v.Fields, FieldRef{Name: name, Slot: slot})
				}
			}
		}
		for _, signal := range r.Signals {
			v.Fields = append(v.Fields, FieldRef{Name: signal.Field, Slot: "signals"})
		}
		resources = append(resources, v)
	}
	if frozen := pageWidget(widget); frozen != nil {
		resources = append(resources, frozen.Resources...)
	}
	return resources, nil
}

func (s Section) PrimaryModel() *ModelBinding { return s.Model }

func (s Section) ModelBindings() []*ModelBinding {
	if s.Model == nil {
		return nil
	}
	return []*ModelBinding{s.Model}
}

func (p Page) RecordResourceObject(variable string) AssetRef {
	if p.Format != 3 {
		safe, err := p.Safety()
		if err != nil {
			return AssetRef{}
		}
		return safe.RecordResourceObject(variable)
	}
	schema := p.InputSchema
	v := schema.Variables[variable]
	if schema.Interface != nil {
		for _, port := range schema.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return *port.Object
			}
		}
	}
	if v.Source == nil {
		return AssetRef{}
	}
	if v.Source.Object != nil && (v.Mode == "shared" || v.Scope == "application") {
		return *v.Source.Object
	}
	if v.Source.Kind == "item" {
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID == v.Owner {
				return p.CollectionInputObject(scope.Bindings["collection"])
			}
		}
	}
	if v.Source.Kind != "record" {
		return AssetRef{}
	}
	for _, binding := range p.DataBindings {
		if binding.ID != v.Source.Section {
			continue
		}
		if binding.Kind == "presentation" {
			return AssetRef{}
		}
		if v.Source.Port != "" {
			for _, resource := range binding.Resources {
				if resource.Kind == "record-output" && resource.ID == v.Source.Port && resource.Outputs["record"] == variable && resource.Object != nil {
					return *resource.Object
				}
				if resource.Kind == "observation" {
					if v.Source.Port == "asset" && resource.Outputs["asset"] == variable && resource.Object != nil {
						return *resource.Object
					}
					if v.Source.Port == "row" && resource.Outputs["row"] == variable {
						return p.CollectionInputObject(binding.Ports["collectionVariable"])
					}
				}
			}
			return AssetRef{}
		}
		return binding.ObjectRef(p)
	}
	return AssetRef{}
}

func (p Page) WindowVariableInterface(variable string) string {
	schema := p.inputSchema()
	v := schema.Variables[variable]
	if v.Source != nil && v.Source.Kind == "plan" {
		return schema.Queries[v.Source.Query].Interface
	}
	return ""
}

func (p Page) RecordVariableInterface(variable string) string {
	safe, err := p.Safety()
	if err != nil {
		return ""
	}
	v := safe.InputSchema.Variables[variable]
	if v.Source != nil {
		for _, binding := range safe.DataBindings {
			if binding.ID == v.Source.Section {
				return safe.WindowVariableInterface(binding.Ports["collectionVariable"])
			}
		}
	}
	return ""
}

func (b PageDataBinding) EffectiveFields(info EntityInfo) []FieldRef {
	if len(b.Fields) > 0 {
		return slices.Clone(b.Fields)
	}
	if b.Model != nil && len(b.Model.Fields) > 0 {
		fields := []FieldRef{}
		for _, field := range b.Model.Fields {
			if field.Slot == "" || field.Slot == "fields" {
				fields = append(fields, field)
			}
		}
		return fields
	}
	fields := []FieldRef{}
	for _, field := range info.Fields {
		if !field.Aside {
			fields = append(fields, FieldRef{Name: field.Name})
		}
	}
	return fields
}

func (s Section) HasRecordOutput(port, variable string) bool {
	resources := s.RuntimeResources
	if resources == nil {
		resources, _ = legacyPageResources(Raw(s), s.Widget)
	}
	for _, resource := range resources {
		if resource.Kind == "record-output" && resource.ID == port && resource.Outputs["record"] == variable {
			return true
		}
		if resource.Kind == "observation" && (port == "row" && resource.Outputs["row"] == variable || port == "asset" && resource.Outputs["asset"] == variable) {
			return true
		}
	}
	return false
}

const PageCompositeProfile = "platform.page.v2.111"

type ModelViewBinding struct {
	Model       ModelBinding        `json:"model"`
	Roles       map[string]FieldRef `json:"roles,omitempty"`
	Expressions map[string]Expr     `json:"expressions,omitempty"`
}

func CompositeLimits() (records, cells, bytes int) {
	limits := pageWidgets.Runtime.Composite
	return limits.MaxRecords, limits.MaxCells, limits.MaxBytes
}

func MatrixRuleReads(c PageDataBinding) RuleReadSpec {
	return RuleReadSpec{Inputs: map[string]ExprType{"row": {Kind: "reference", Object: c.Models["rows"].Model.Type}, "column": {Kind: "reference", Object: c.Models["columns"].Model.Type}}}
}

func PageContentVersion(page Page) (string, error) {
	if page.retainedVersion != "" {
		return page.retainedVersion, nil
	}
	page.PresentationWords = nil
	body, err := json.Marshal(page)
	if err != nil {
		return "", err
	}
	body, err = canonicalBody(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(body)
	return "page.sha256." + hex.EncodeToString(sum[:]), nil
}

func CheckPageContentVersion(version string) error {
	text, ok := strings.CutPrefix(version, "page.sha256.")
	raw, err := hex.DecodeString(text)
	if !ok || err != nil || len(raw) != sha256.Size || hex.EncodeToString(raw) != text {
		return fmt.Errorf("invalid page content version")
	}
	return nil
}

type PageExternalFrame struct {
	LegacyVisual map[string]json.RawMessage `json:"-"`
	URL          string                     `json:"url"`
	Origin       string                     `json:"origin"`
}

var externalFrameASCII = regexp.MustCompile(`^[!-~]+$`)

var externalFrameOrigin = regexp.MustCompile(`^https://[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?(?::[1-9][0-9]{0,4})?$`)

func ValidPageExternalFrame(c PageExternalFrame) bool {
	if len(c.URL) > 2048 || !externalFrameASCII.MatchString(c.URL) || !externalFrameOrigin.MatchString(c.Origin) || strings.ContainsAny(c.URL, "\\ \t\r\n") || !strings.HasPrefix(c.URL, c.Origin+"/") {
		return false
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Scheme != "https" || u.User != nil || "https://"+u.Host != c.Origin || u.Port() == "443" || u.String() != c.URL {
		return false
	}
	for _, segment := range strings.Split(u.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	if u.Port() != "" {
		var port int
		if _, err := fmt.Sscan(u.Port(), &port); err != nil || port > 65535 {
			return false
		}
	}
	return true
}

func (p *PageExternalFrame) UnmarshalJSON(raw []byte) error {
	type value PageExternalFrame
	return RetainPageSection(raw, (*value)(p), &p.LegacyVisual)
}

func (p PageExternalFrame) MarshalJSON() ([]byte, error) {
	type value PageExternalFrame
	return MarshalPageSection(value(p), p.LegacyVisual)
}

type PageActionParameter struct {
	Parameter string `json:"parameter"`
	Field     string `json:"field"`
}

type PageActionTable struct {
	Parameters []PageActionParameter `json:"parameters"`
}

func ActionParameterCompatible(f FieldInfo, p Field) bool {
	if p.From != "" {
		return false
	}
	switch p.Type {
	case "string":
		if p.Ref != "" {
			return f.Type == "reference" && f.Ref == p.Ref
		}
		return slices.Contains([]string{"text", "longtext", "choice"}, f.Type)
	case "number":
		return f.Type == "decimal" || f.Type == "integer"
	case "integer":
		return f.Type == "integer"
	case "boolean", "date", "datetime":
		return f.Type == p.Type
	}
	return false
}

func (s Section) CheckTableEdit(info EntityInfo, action Action) error {
	if s.InlineEdit == nil {
		return nil
	}
	edit := s.InlineEdit
	if edit.Action.App != info.App || edit.Action.Name != info.Type+".edit" || action.Schema != edit.Action.Name || action.Target != info.Type || action.New || action.NeedsApproval || action.Approval != nil || !slices.Contains(info.Standard, action.Schema) || slices.ContainsFunc(action.Payload, func(f Field) bool {
		return f.Required
	}) {
		return fmt.Errorf("table editing requires its original standard field patch action without extra required inputs or approval")
	}
	for _, name := range edit.Fields {
		f, ok := info.Field(name)
		if !ok || f.ReadOnly || f.Aside || info.Lifecycle != nil && info.Lifecycle.Field == name || !slices.Contains(pageWidgets.Runtime.TableEditing.FieldTypes, f.Type) || !slices.ContainsFunc(action.Payload, func(p Field) bool {
			return p.Name == name
		}) {
			return fmt.Errorf("table edit field is unavailable or not accepted by its original action")
		}
	}
	return nil
}

func sectionJSONValue(v reflect.Value, path []string) reflect.
	Value {
	if len(path) == 0 {
		return v
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return reflect.Value{}
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}
	}
	for i := 0; i < v.NumField(); i++ {
		field := v.Type().Field(i)
		if field.Anonymous {
			if found := sectionJSONValue(v.Field(i), path); found.IsValid() {
				return found
			}
		}
		if strings.Split(field.Tag.Get("json"), ",")[0] == path[0] {
			return sectionJSONValue(v.Field(i), path[1:])
		}
	}
	return reflect.Value{}
}

//go:embed page_compat.json
var pageWidgetJSON []byte

type pageModelRole struct {
	Kinds    []string `json:"kinds"`
	Required bool     `json:"required,omitempty"`
	Target   string   `json:"target,omitempty"`
}

type pageModelPort struct {
	ID       string                   `json:"id"`
	Required bool                     `json:"required,omitempty"`
	Roles    map[string]pageModelRole `json:"roles"`
}

type pageWidgetContract struct {
	Resources           []PageResourceBinding `json:"resources,omitempty"`
	FieldRoles          []pageWidgetFieldRole `json:"fieldRoles,omitempty"`
	PortGroups          []pageWidgetPortGroup `json:"portGroups,omitempty"`
	ModelPorts          []pageModelPort       `json:"modelPorts,omitempty"`
	ConfigurationFields []string              `json:"configurationFields,omitempty"`
	ComponentID         string                `json:"componentID"`
	ConfigVersion       int                   `json:"configVersion"`
	RequiredUIProfile   string                `json:"requiredUIProfile"`
	SelectionMode       string                `json:"selectionMode"`
	BindingKinds        []string              `json:"bindingKinds"`
	InputPorts          []pageWidgetPort      `json:"inputPorts"`
	OutputPorts         []pageWidgetPort      `json:"outputPorts"`
	Events              []pageWidgetEvent     `json:"events"`
	Slots               []pageWidgetSlot      `json:"slots"`
}

type pageWidgetSlot struct {
	ID                string   `json:"id"`
	Title             string   `json:"title"`
	Placement         string   `json:"placement"`
	RequiredUIProfile string   `json:"requiredUIProfile"`
	AllowedLayouts    []string `json:"allowedLayouts"`
}

type pageWidgetPort struct {
	Required          bool            `json:"required,omitempty"`
	Modes             []string        `json:"modes,omitempty"`
	Scopes            []string        `json:"scopes,omitempty"`
	Source            string          `json:"source,omitempty"`
	SameOwner         string          `json:"sameOwner,omitempty"`
	ScopeRule         string          `json:"scopeRule,omitempty"`
	Plan              *pageWidgetPlan `json:"plan,omitempty"`
	ID                string          `json:"id"`
	BindingField      string          `json:"bindingField"`
	Type              string          `json:"type"`
	RequiredUIProfile string          `json:"requiredUIProfile"`
	Writable          bool            `json:"writable"`
	StateWritable     bool            `json:"stateWritable,omitempty"`
}

type pageWidgetPlan struct {
	Ordered  bool `json:"ordered,omitempty"`
	MaxLimit int  `json:"maxLimit,omitempty"`
}

type pageWidgetPortGroup struct {
	Fields   []string `json:"fields"`
	Min      int      `json:"min"`
	Max      int      `json:"max"`
	Distinct bool     `json:"distinct,omitempty"`
}

type pageWidgetFieldRole struct {
	Path     string   `json:"path"`
	Kinds    []string `json:"kinds"`
	Optional bool     `json:"optional,omitempty"`
	AllowID  bool     `json:"allowID,omitempty"`
	When     string   `json:"when,omitempty"`
	Distinct bool     `json:"distinct,omitempty"`
}

type pageWidgetEvent struct {
	RequiredUIProfile string `json:"requiredUIProfile,omitempty"`
	ID                string `json:"id"`
	Payload           string `json:"payload"`
	Required          bool   `json:"required"`
	MaxBindings       int    `json:"maxBindings"`
}

type pageUIContract struct {
	Documentless struct {
		Widgets []string `json:"widgets"`
		Fields  []string `json:"fields"`
	} `json:"documentless"`
	Features map[string]string `json:"features"`
	Layout   struct {
		PresentationProfile string `json:"presentationProfile"`
		MaxPadding          int    `json:"maxPadding"`
		RequiredUIProfile   string `json:"requiredUIProfile"`
		UnusedProfile       string `json:"unusedProfile"`
		MaxUnused           int    `json:"maxUnused"`
		MinSize             int    `json:"minSize"`
		MaxSize             int    `json:"maxSize"`
		MaxWeight           int    `json:"maxWeight"`
		MaxGap              int    `json:"maxGap"`
		StackBelow          int    `json:"stackBelow"`
	} `json:"layout"`
	UIProfile         string               `json:"uiProfile"`
	SupportedProfiles []string             `json:"supportedProfiles"`
	Widgets           []pageWidgetContract `json:"widgets"`
	Runtime           pageRuntimeContract  `json:"runtime"`
}

var pageWidgets = func() pageUIContract {
	var frozen struct {
		RetainedContract pageUIContract `json:"retainedContract"`
	}
	if err := json.Unmarshal(pageWidgetJSON, &frozen); err != nil {
		panic(err)
	}
	return frozen.RetainedContract
}()

func PageUIProfile() string {
	return pageWidgets.UIProfile
}

func WidgetReadsSelection(id string) bool {
	w := pageWidget(id)
	return w != nil && (w.SelectionMode == "read" || w.SelectionMode == "write")
}

func SupportsPageUIProfile(profile string) bool {
	if profile == "platform.page.v3" {
		return true
	}
	return slices.Contains(pageWidgets.SupportedProfiles, profile)
}

func pageWidget(id string) *pageWidgetContract {
	for i := range pageWidgets.Widgets {
		if pageWidgets.Widgets[i].ComponentID == id {
			return &pageWidgets.Widgets[i]
		}
	}
	return nil
}

// checkWidgetPorts holds each variable port to its widget contract: the port
// exists, carries the declared value type, and is writable where it writes.
func (p Page) checkWidgetPorts(binding PageDataBinding, owner string) error {
	widget := binding.Ports["widget"]
	if pageWidget(widget) == nil {
		return nil
	}
	loop, overlay := p.scopeOwner(owner, "loop"), p.scopeOwner(owner, "overlay")
	for field, id := range binding.Ports {
		port := widgetPort(widget, field)
		if port != nil && port.ScopeRule != "" && field != "enabledWhen" {
			v, ok := p.InputSchema.Variables[id]
			reachable := ok && (v.Scope == "page" || v.Scope == "application" || v.Scope == "loop-item" && loop != "" && v.Owner == loop || v.Scope == "overlay" && overlay != "" && v.Owner == overlay)
			if !reachable || port.ScopeRule == "local" && loop != "" || port.ScopeRule == "exact" && (loop != "" || v.Scope == "page" && overlay != "" || v.Scope == "overlay" && v.Owner != overlay) {
				return fmt.Errorf("widget %s %s escapes its presentation owner", widget, field)
			}
		}
		if port == nil {
			if pageVariablePortFields[field] {
				return fmt.Errorf("widget %s has an invalid %s port", widget, field)
			}
			continue
		}
		v, ok := p.InputSchema.Variables[id]
		if ok && v.Scope == "application" && v.Type == "record" && !PageUIProfileSupports(p.InputSchema.profileVersion(), sharedRecordProfile(widget)) {
			return fmt.Errorf("widget %s shares an application record only from its profile", widget)
		}
		readonlyChoice := field == "choiceVariable" && v.Mode == "constant"
		if !ok || v.Type != port.Type || port.Writable && field != "selectionSetVariable" && !readonlyChoice && !(v.Writable || port.StateWritable && v.Mode == "state") {
			return fmt.Errorf("widget %s has an invalid %s port", widget, field)
		}
	}
	// A bare document check has no page object to compare against.
	object, known := binding.ObjectRef(p).Name, binding.ObjectRef(p).Name != ""
	if id := binding.Ports["selectionSetVariable"]; id != "" && known {
		if v := p.InputSchema.Variables[id]; v.Mode == "shared" && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Name != object) {
			return fmt.Errorf("shared record-set object does not match its table")
		}
	}
	if id := binding.Ports["collectionVariable"]; id != "" && known && p.WindowVariableObject(id) != object {
		return fmt.Errorf("table window object does not match its plan")
	}
	local := p.scopeOwner(owner, "loop") != "" || p.scopeOwner(owner, "overlay") != ""
	if id := binding.Ports["filterVariable"]; id != "" {
		v := p.InputSchema.Variables[id]
		if p.scopeOwner(owner, "loop") != "" || binding.Ports["collectionVariable"] != "" || v.Type != "filter" || v.Mode != "shared" || widget == "filter" && (!v.Writable || p.scopeOwner(owner, "overlay") != "") {
			return fmt.Errorf("page section %s has an invalid shared filter port", binding.ID)
		}
		if known && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Name != object) {
			return fmt.Errorf("shared filter object does not match its widget")
		}
	}
	if id := binding.Ports["selectionVariable"]; id != "" {
		v := p.InputSchema.Variables[id]
		if local || binding.Ports["selection"] != "" || v.Type != "record" || v.Mode != "shared" || !v.Writable {
			return fmt.Errorf("page section %s needs a writable shared record output on a root table", binding.ID)
		}
		if known && object != p.RecordVariableObject(id) {
			return fmt.Errorf("shared selection object does not match its table")
		}
	}
	return nil
}

// sharedRecordProfile is the profile that first let a widget consume an
// application-scope record; widgets without one share it from the earliest.
func sharedRecordProfile(widget string) string {
	switch widget {
	case "scene-3d", "observation":
		return pageWidgets.Runtime.Telemetry.SharedUIProfile
	case "vertex-graph":
		return pageWidgets.Runtime.Exploration.SharedVertexUIProfile
	case "graph-explorer", "breadcrumb", "avatar-stack":
		return pageWidgets.Runtime.ContextViews.SharedUIProfile
	}
	return "platform.page.v2.14"
}

var pageVariablePortFields = func() map[string]bool {
	fields := map[string]bool{}
	for _, widget := range pageWidgets.Widgets {
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.BindingField != "enabledWhen" {
				fields[port.BindingField] = true
			}
		}
	}
	return fields
}()

func widgetPort(id, field string) *pageWidgetPort {
	w := pageWidget(id)
	if w == nil {
		return nil
	}
	for _, ports := range [][]pageWidgetPort{w.InputPorts, w.OutputPorts} {
		for i := range ports {
			if ports[i].BindingField == field {
				return &ports[i]
			}
		}
	}
	return nil
}

func (s Section) variablePorts() map[string]string {
	values := map[string]any{}
	_ = json.Unmarshal(Raw(s), &values)
	ports := map[string]string{}
	for _, widget := range pageWidgets.Widgets {
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.BindingField == "enabledWhen" {
				continue
			}
			for _, value := range legacyPagePathValues(values, strings.Split(port.BindingField, ".")) {
				if text, ok := value.(string); ok && text != "" {
					ports[port.BindingField] = text
				}
			}
		}
	}
	return ports
}

func PageUIProfileSupports(profile, required string) bool {
	if profile == "platform.page.v3" {
		return true
	}
	current, minimum := slices.Index(pageWidgets.SupportedProfiles, profile), slices.Index(pageWidgets.SupportedProfiles, required)
	return current >= 0 && minimum >= 0 && current >= minimum
}

var Widgets = func() []string {
	ids := make([]string, 0, len(pageWidgets.Widgets))
	for _, widget := range pageWidgets.Widgets {
		ids = append(ids, widget.ComponentID)
	}
	return ids
}()

func (p Page) checkEnvelope() error {
	if len(p.DataBindings) > 128 || len(p.ActionTriggers) > 256 || len(p.ScopeVisibility.Scopes) > 512 || len(p.WidgetSpec.Body) > 2<<20 || !json.Valid(p.WidgetSpec.Body) {
		return fmt.Errorf("page safety declaration exceeds its bounded format")
	}
	return nil
}
func (b PageDataBinding) HasRecordOutput(port, variable string) bool {
	for _, r := range b.Resources {
		if r.Kind == "record-output" && r.ID == port && r.Outputs["record"] == variable {
			return true
		}
		if r.Kind == "observation" && (port == "row" && r.Outputs["row"] == variable || port == "asset" && r.Outputs["asset"] == variable) {
			return true
		}
	}
	return false
}
func (p Page) scopeOwner(owner, kind string) string {
	seen := map[string]bool{}
	for owner != "" && !seen[owner] {
		seen[owner] = true
		found := false
		for _, scope := range p.ScopeVisibility.Scopes {
			if scope.ID == owner {
				if scope.Kind == kind {
					return scope.ID
				}
				owner = scope.Parent
				found = true
				break
			}
		}
		if !found {
			return ""
		}
	}
	return ""
}
func (p Page) rootPresentationSource(source string) bool {
	owner := p.ScopeVisibility.Owners[source]
	if owner == "" {
		owner = source
	}
	return p.scopeOwner(owner, "overlay") == "" && p.scopeOwner(owner, "loop") == ""
}
func pageBindingVariables(b PageDataBinding) []string {
	out := []string{}
	for key, id := range b.Ports {
		if strings.HasSuffix(key, "Variable") {
			out = append(out, id)
		}
	}
	for _, r := range b.Resources {
		for _, v := range r.Inputs {
			out = append(out, v.Variable)
		}
		for _, id := range r.Outputs {
			out = append(out, id)
		}
	}
	return out
}
func checkLegacyPageProfile(document *PageDocument, section Section, raw []byte) error {
	contract := pageWidget(section.Widget)
	if contract == nil {
		return fmt.Errorf("unsupported retained page component %s", section.Widget)
	}
	if section.ConfigVersion != 0 && section.ConfigVersion != contract.ConfigVersion {
		return fmt.Errorf("unsupported retained component version")
	}
	if document == nil {
		if !slices.Contains(pageWidgets.Documentless.Widgets, section.Widget) {
			return fmt.Errorf("retained component needs a page document")
		}
		return nil
	}
	if !PageUIProfileSupports(document.UIProfile, contract.RequiredUIProfile) {
		return fmt.Errorf("retained component is newer than its original profile")
	}
	var values map[string]any
	if err := json.Unmarshal(raw, &values); err != nil {
		return err
	}
	for _, port := range append(slices.Clone(contract.InputPorts), contract.OutputPorts...) {
		for _, value := range legacyPagePathValues(values, strings.Split(port.BindingField, ".")) {
			if text, ok := value.(string); ok && text != "" && port.RequiredUIProfile != "" && !PageUIProfileSupports(document.UIProfile, port.RequiredUIProfile) {
				return fmt.Errorf("retained binding is newer than its original profile")
			}
		}
	}
	return nil
}
func legacyFieldRef(name string) FieldRef {
	if strings.Contains(name, ".") {
		return FieldRef{Path: strings.Split(name, ".")}
	}
	return FieldRef{Name: name}
}

// defaultModelFields are the columns a widget shows when its model names none:
// only the field-listing widgets derive them; the others keep an explicit choice.
func defaultModelFields(widget string, info EntityInfo) []FieldRef {
	fields := []FieldRef{}
	if !slices.Contains([]string{"table", "detail", "form", "filter"}, widget) {
		return fields
	}
	for _, field := range info.Fields {
		if field.Aside || widget == "form" && field.ReadOnly || widget == "filter" && !slices.Contains(Filterable, field.Type) {
			continue
		}
		fields = append(fields, FieldRef{Name: field.Name})
	}
	return fields
}

func (p Page) ResolveBindings(lookup func(string) (EntityInfo, bool)) Page {
	p.DataBindings = slices.Clone(p.DataBindings)
	for i, binding := range p.DataBindings {
		if binding.Model == nil && len(binding.Models) == 0 && binding.Object.Name != "" && (binding.Kind == "record" || binding.Kind == "records") {
			binding.Model = &ModelBinding{Type: binding.Object.Name, Fields: slices.Clone(binding.Fields)}
		}
		binding.Models = maps.Clone(binding.Models)
		if binding.Model != nil {
			model := *binding.Model
			model.Fields = slices.Clone(model.Fields)
			for n, field := range model.Fields {
				if strings.Contains(field.Name, ".") {
					field.Path = strings.Split(field.Name, ".")
					field.Name = ""
				}
				model.Fields[n] = field
			}
			info, ok := lookup(model.Type)
			if ok {
				binding.Fields = binding.EffectiveFields(info)
				if len(model.Fields) == 0 {
					model.Fields = defaultModelFields(binding.Ports["widget"], info)
				}
				if model.Relation == nil && (binding.Ports["relation"] != "" || binding.Ports["parentSelection"] != "") {
					parentType := p.Object.Name
					for _, selection := range p.InputSchema.Selections {
						if selection.Name == binding.Ports["parentSelection"] {
							parentType = selection.Object.Name
						}
					}
					for _, field := range info.Fields {
						if field.Type == "reference" && field.Ref == parentType && (binding.Ports["relation"] == "" || field.Inverse == binding.Ports["relation"]) {
							model.Relation = &ModelRelation{ParentType: parentType, ParentSelection: binding.Ports["parentSelection"], Field: field.Name, Name: field.Inverse}
							break
						}
					}
				}
			}
			binding.Model = &model
		}
		for port, view := range binding.Models {
			info, ok := lookup(view.Model.Type)
			if !ok {
				continue
			}
			view.Roles = maps.Clone(view.Roles)
			if view.Roles == nil {
				view.Roles = map[string]FieldRef{}
			}
			if _, explicit := view.Roles["label"]; !explicit {
				if _, computed := view.Expressions["label"]; !computed && view.Model.Label == nil && info.Display != "" && info.Display != "id" {
					view.Roles["label"] = FieldRef{Name: info.Display}
				}
			}
			if _, explicit := view.Roles["state"]; !explicit {
				if _, computed := view.Expressions["state"]; !computed && info.Lifecycle != nil {
					view.Roles["state"] = FieldRef{Name: info.Lifecycle.Field}
				}
			}
			binding.Models[port] = view
		}
		p.DataBindings[i] = binding
	}
	return p
}

// Page stores the current five-element contract. The api:retained fields only
// decode original declarations; execution never derives bindings from them.
type Page struct {
	Format            int                 `json:"format,omitempty"`
	DataBindings      []PageDataBinding   `json:"dataBindings,omitempty"`
	ActionTriggers    []PageActionTrigger `json:"actionTriggers,omitempty"`
	InputSchema       PageInputSchema     `json:"inputSchema,omitzero"`
	ScopeVisibility   PageScopeVisibility `json:"scopeVisibility,omitzero"`
	WidgetSpec        PageWidgetSpec      `json:"widgetSpec,omitzero"`
	retainedVersion   string
	PresentationWords map[string]string   `json:"presentationWords,omitempty"`
	Name              string              `json:"name"`
	Title             string              `json:"title"`
	Description       string              `json:"description,omitempty"`
	Object            AssetRef            `json:"object"`
	Layout            string              `json:"layout" api:"retained"` // list-detail in 13b
	ListFields        []string            `json:"listFields" api:"retained"`
	DetailFields      []string            `json:"detailFields" api:"retained"`
	Actions           []AssetRef          `json:"actions" api:"retained"`
	Sections          []Section           `json:"sections,omitempty" api:"retained"`
	Document          *PageDocument       `json:"document,omitempty" api:"retained"`
	Selections        []SelectionVariable `json:"selections,omitempty" api:"retained"`
}
type SelectionVariable struct {
	Name   string   `json:"name"`
	Object AssetRef `json:"object"`
}

// Section is one place on a composed page: a widget, what it is bound to, and
// how wide it sits. A table outputs the record someone selects; a
// detail and the actions read it.
type PageFacet struct {
	Field    string `json:"field"`
	Variable string `json:"variable"`
	Kind     string `json:"kind"`
}
type PageInlineEdit struct {
	Action AssetRef `json:"action"`
	Fields []string `json:"fields"`
}

type Section struct {
	RuntimeResources     []PageResourceBinding      `json:"-"`
	RuntimePorts         map[string]string          `json:"-"`
	LegacyVisual         map[string]json.RawMessage `json:"-"`
	Model                *ModelBinding              `json:"model,omitempty"`
	ActionDefaults       []PageActionParameter      `json:"actionDefaults,omitempty"`
	InlineEdit           *PageInlineEdit            `json:"inlineEdit,omitempty"`
	Facets               []PageFacet                `json:"facets,omitempty"`
	FilterSearchVariable string                     `json:"filterSearchVariable,omitempty"`
	// ID is required when Document references this section; older pages omit it.
	ID            string `json:"id,omitempty"`
	ConfigVersion int    `json:"configVersion,omitempty"`
	// Widget is table, detail, actions, chart, metric, text, filter, form,
	// timeline, tasks or a published function.
	Widget string `json:"widget"`
	// Width is full or half, in the order the sections are laid out.
	// Object is what it shows; empty: the page's own object.
	Object AssetRef `json:"object,omitempty"`
	// Selection is the record variable a table writes or detail/actions read.
	Selection          string `json:"selection,omitempty"`
	CollectionVariable string `json:"collectionVariable,omitempty"`
	RecordVariable     string `json:"recordVariable,omitempty"`
	SelectionVariable  string `json:"selectionVariable,omitempty"`
	FilterVariable     string `json:"filterVariable,omitempty"`
	// ParentSelection supplies a typed parent record to a related section;
	// empty uses the page object's shared selection.
	ParentSelection string `json:"parentSelection,omitempty"`
	// Relation is the named inverse (FieldInfo.Inverse) of Object's reference to
	// the parent selection's object: the section shows its related records
	// through it, or a form supplies that reference when creating a related
	// record. Empty: no declared relation binding.
	Relation string `json:"relation,omitempty"`
	// Query is a named query (AssetQuery) the section lists instead of all of
	// Object: its conditions, and the selected record when it takes one (21c).
	Query       AssetRef           `json:"query,omitempty"`
	Fields      []string           `json:"fields,omitempty"` // table, detail, filter, form
	Actions     []AssetRef         `json:"actions,omitzero"` // nil inherits page actions; an empty slice forbids section writes
	Group       string             `json:"group,omitempty"`  // chart: the field it groups by, or "<field>:month"
	ColumnGroup string             `json:"columnGroup,omitempty"`
	TimeStart   string             `json:"timeStart,omitempty"`
	TimeEnd     string             `json:"timeEnd,omitempty"`
	TimeLabel   string             `json:"timeLabel,omitempty"`
	TimeGroup   string             `json:"timeGroup,omitempty"`
	CardLabel   string             `json:"cardLabel,omitempty"`
	Measure     string             `json:"measure,omitempty"`   // chart, metric: count, sum:<field>, avg:<field>, min:<field>, max:<field>
	Function    *AssetBinding      `json:"function,omitempty"`  // exact published function
	Operation   *AssetBinding      `json:"operation,omitempty"` // exact compute owner revision
	Inputs      map[string]Binding `json:"inputs,omitempty"`    // compute inputs or form fields supplied by constants/parent record paths
}
