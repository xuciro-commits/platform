package platform

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

func (p Page) BindRuleTypes(lookup func(string) (EntityInfo, bool)) error {
	d := p.inputSchema()
	for name, v := range d.Variables {
		v.RuleType = nil
		object := ""
		if v.Type == "record" {
			object = p.RecordVariableObject(name)
		} else if v.Type == "object-set" {
			object = p.WindowVariableObject(name)
		}
		if object != "" {
			info, ok := lookup(object)
			if !ok {
				return fmt.Errorf("rule resource %s object is unavailable", name)
			}
			fields := map[string]ExprType{}
			for _, field := range info.Fields {
				fields[field.Name] = ExprType{Kind: RuleFieldKind(field.Type)}
			}
			v.RuleType = &ExprType{Kind: "reference", Fields: fields}
		}
		if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			field, found := info.Field(v.Source.Field)
			if !ok || !found {
				return fmt.Errorf("rule property is unavailable")
			}
			v.RuleType = &ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
		}
		d.Variables[name] = v
	}
	var walk func(Expr, string) error
	walk = func(e Expr, set string) error {
		if e.Agg != nil {
			set = e.Agg.Source
			if _, ok := d.PageRuleSchema().Sets[set]; !ok {
				return fmt.Errorf("rule collection %s is not a declared query window", set)
			}
		}
		expand := func(base string, path []string) error {
			item := strings.HasPrefix(base, "item.")
			name := base
			parts := path
			if item {
				name = set
				parts = append([]string{strings.TrimPrefix(base, "item.")}, path...)
			}
			object := p.RecordVariableObject(name)
			if item {
				object = p.WindowVariableObject(name)
			}
			if object == "" {
				return fmt.Errorf("rule path %s needs a declared resource", base)
			}
			if _, err := RecordPathField(object, parts, lookup); err != nil {
				return err
			}
			var tree func(string, []string) ExprType
			tree = func(typ string, segments []string) ExprType {
				info, _ := lookup(typ)
				f, _ := info.Field(segments[0])
				leaf := ExprType{Kind: RuleFieldKind(f.Type)}
				if len(segments) > 1 {
					leaf = tree(f.Ref, segments[1:])
				}
				return ExprType{Kind: "reference", Fields: map[string]ExprType{segments[0]: leaf}}
			}
			v := d.Variables[name]
			if v.RuleType == nil {
				return fmt.Errorf("rule resource is unavailable")
			}
			t := mergeRuleTypes(*v.RuleType, tree(object, parts))
			v.RuleType = &t
			d.Variables[name] = v
			return nil
		}
		if e.Op == "path" && len(e.Args) == 1 {
			if err := expand(e.Args[0].Ref, e.Path); err != nil {
				return err
			}
		}
		if e.Agg != nil && len(e.Agg.Field) > 1 {
			if err := expand("item."+e.Agg.Field[0], e.Agg.Field[1:]); err != nil {
				return err
			}
		}
		for _, a := range e.Args {
			if err := walk(a, set); err != nil {
				return err
			}
		}
		if e.In != nil {
			return walk(*e.In, set)
		}
		return nil
	}
	for _, v := range d.Variables {
		if v.Rule != nil {
			if err := walk(*v.Rule, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p Page) CheckDataBinding(binding PageDataBinding, lookup func(string) (EntityInfo, bool), criteria func(AssetRef) (*RuleCriteria, bool), engines ...RuleEngine) error {
	if binding.Object.Name != "" {
		info, ok := lookup(binding.Object.Name)
		if !ok || info.App != binding.Object.App {
			return fmt.Errorf("page binding object differs from its original owner")
		}
	}
	models := []ModelBinding{}
	if binding.Model != nil {
		models = append(models, *binding.Model)
	}
	for _, view := range binding.Models {
		models = append(models, view.Model)
	}
	if len(models) == 0 && binding.Object.Name != "" {
		models = append(models, ModelBinding{Type: binding.Object.Name, Fields: binding.Fields})
	}
	for _, model := range models {
		if len(model.Fields) > PageModelMaxFields || len(model.Where) > 32 || model.Visibility != "" && model.Visibility != "hide" && model.Visibility != "mark" {
			return fmt.Errorf("model binding exceeds its declared shape")
		}
		seen, slots := map[string]bool{}, map[string]bool{}
		for _, field := range model.Fields {
			key := field.Slot + "/" + field.Key()
			if field.Name != "" && len(field.Path) > 0 || len(field.Path) > 16 || field.Key() == "" || seen[key] || field.Slot != "" && field.Slot != "fields" && slots[field.Slot] || field.Transform != "" && !slices.Contains([]string{"month", "sum", "avg", "min", "max"}, field.Transform) {
				return fmt.Errorf("model field declaration is invalid")
			}
			seen[key] = true
			slots[field.Slot] = true
		}
		if model.Label != nil && model.Label.Kind != "text" {
			return fmt.Errorf("model labels need text expressions")
		}
		for _, expr := range model.Where {
			if expr.Kind != "boolean" {
				return fmt.Errorf("model conditions need boolean expressions")
			}
		}

		info, known := lookup(model.Type)
		if !known {
			return fmt.Errorf("page binding %s object is unavailable", binding.ID)
		}
		metadata := func(typ string) (EntityInfo, bool) {
			value, found := lookup(typ)
			return ConstraintEntity(value), found
		}
		for _, field := range model.Fields {
			if _, err := RecordPathField(info.Type, field.Segments(), metadata); err != nil {
				return err
			}
		}
		for _, expr := range model.Where {
			if err := CheckRuleExpression(expr, RuleReadSpec{Object: info.Type}, lookup, engines...); err != nil {
				return err
			}
		}
		if model.Label != nil {
			if err := CheckRuleExpression(*model.Label, RuleReadSpec{Object: info.Type}, lookup, engines...); err != nil {
				return err
			}
		}
		if model.Relation != nil {
			field, ok := info.Field(model.Relation.Field)
			if !ok || field.Type != "reference" || field.Ref != model.Relation.ParentType {
				return fmt.Errorf("page relation must use its original reference field")
			}
		}
		if model.Options != nil && model.Options.Rule != nil {
			rule := model.Options.Rule
			declared, ok := criteria(rule.Action)
			if !ok || declared == nil {
				return fmt.Errorf("original model rule is unavailable")
			}
			for name, typ := range declared.Reads.Inputs {
				id := rule.Inputs[name]
				variable, ok := p.InputSchema.Variables[id]
				if !ok || (typ.Kind == "reference" || typ.Object != "") && p.RecordVariableObject(id) != typ.Object || typ.Kind != "reference" && typ.Object == "" && !PageRuleInputFits(variable.Type, typ.Kind) {
					return fmt.Errorf("model rule input does not match its declared type")
				}
			}
			for _, id := range rule.Inputs {
				if _, known := p.InputSchema.Variables[id]; !known {
					return fmt.Errorf("model rule input is an unavailable page variable")
				}
			}
		}
	}
	object := binding.Object.Name
	if binding.Model != nil {
		object = binding.Model.Type
	}
	for _, field := range binding.Fields {
		if _, err := RecordPathField(object, field.Segments(), lookup); err != nil {
			return err
		}
	}
	for slot, fields := range binding.FieldSlots {
		if strings.HasPrefix(slot, "observation.") || slot == "ai.replyField" || slot == "scene.mappings.field" {
			continue
		}
		source := object
		if strings.HasPrefix(slot, "scene.sample") {
			source = p.CollectionInputObject(binding.Ports["sceneSampleCollectionVariable"]).Name
			if source == "" {
				source = p.RecordResourceObject(binding.Ports["sceneSampleVariable"]).Name
			}
		}
		for _, field := range fields {
			if _, err := RecordPathField(source, field.Segments(), lookup); err != nil {
				return err
			}
		}
	}
	for _, view := range binding.Models {
		for _, field := range view.Roles {
			if _, err := RecordPathField(view.Model.Type, field.Segments(), lookup); err != nil {
				return err
			}
		}
		for _, expr := range view.Expressions {
			if err := CheckRuleExpression(expr, RuleReadSpec{Object: view.Model.Type}, lookup, engines...); err != nil {
				return err
			}
		}
	}
	for _, resource := range binding.Resources {
		source := object
		if resource.Object != nil {
			source = resource.Object.Name
			if _, ok := lookup(source); !ok {
				return fmt.Errorf("page resource object is unavailable")
			}
		}
		for _, field := range resource.Fields {
			if resource.Kind == "observation" {
				variable := binding.Ports["collectionVariable"]
				if resource.Source == "availability" {
					variable = binding.Ports["observationHistoryVariable"]
				}
				source = p.CollectionInputObject(variable).Name
				if field.Slot == "averageField" {
					source = object
				}
			}
			if resource.Kind == "ai" {
				continue
			}
			if resource.Kind == "record-mapping" && resource.Source == "sample" {
				source = p.CollectionInputObject(binding.Ports["sceneSampleCollectionVariable"]).Name
				if source == "" {
					source = p.RecordResourceObject(binding.Ports["sceneSampleVariable"]).Name
				}
			}
			if field.Name == "" && len(field.Path) == 0 {
				continue
			}
			if _, err := RecordPathField(source, field.Segments(), lookup); err != nil {
				return err
			}
		}
		if resource.Asset != nil {
			if err := resource.Asset.Ref.Check(); err != nil {
				return err
			}
		}
		if resource.Kind == "external-document" && !ValidPageExternalFrame(PageExternalFrame{URL: resource.URL, Origin: resource.Origin}) {
			return fmt.Errorf("page external resource origin is invalid")
		}
		if resource.Kind == "image" && !ValidPageImageURL(resource.URL) {
			return fmt.Errorf("page image resource is invalid")
		}
		if resource.Kind == "page" && (resource.Asset == nil || resource.Asset.SourceVersion == "" || CheckPageContentVersion(resource.ContentVersion) != nil) {
			return fmt.Errorf("embedded page needs its original exact content")
		}
	}
	if binding.Kind == "composite" {
		return p.checkComposite(binding, lookup, engines...)
	}
	return nil
}

func compositeRole(view ModelViewBinding, role string, info EntityInfo) *FieldRef {
	if field, ok := view.Roles[role]; ok {
		return &field
	}
	if _, ok := view.Expressions[role]; ok {
		return nil
	}
	if role == "label" {
		if view.Model.Label != nil {
			return nil
		}
		if name := info.Display; name != "" && name != "id" {
			return &FieldRef{Name: name}
		}
		return nil
	}
	if role == "state" && info.Lifecycle != nil {
		return &FieldRef{Name: info.Lifecycle.Field}
	}
	return nil
}

func (p Page) checkComposite(b PageDataBinding, lookup func(string) (EntityInfo, bool), engines ...RuleEngine) error {
	widget := b.Ports["widget"]
	if widget == "" {
		for _, candidate := range pageWidgets.Widgets {
			fits := len(candidate.ModelPorts) > 0 && len(b.Models) > 0
			for key := range b.Models {
				fits = fits && slices.ContainsFunc(candidate.ModelPorts, func(port pageModelPort) bool { return port.ID == key })
			}
			for _, port := range candidate.ModelPorts {
				_, present := b.Models[port.ID]
				fits = fits && (present || !port.Required)
			}
			if fits {
				widget = candidate.ComponentID
				break
			}
		}
	}
	contract := pageWidget(widget)
	if contract == nil || len(contract.ModelPorts) == 0 || b.Model != nil || len(b.Models) > len(contract.ModelPorts) {
		return fmt.Errorf("binding %s: invalid composite contract", b.ID)
	}
	entity := func(typ string) (EntityInfo, bool) { v, ok := lookup(typ); return ConstraintEntity(v), ok }
	for _, port := range contract.ModelPorts {
		view, exists := b.Models[port.ID]
		if !exists {
			if port.Required {
				return fmt.Errorf("binding %s: model port %s is required", b.ID, port.ID)
			}
			continue
		}
		if view.Model.Options != nil || view.Model.Relation != nil || len(view.Roles) > len(port.Roles) || len(view.Expressions) > len(port.Roles) {
			return fmt.Errorf("binding %s port %s: unsupported binding", b.ID, port.ID)
		}
		info, _ := lookup(view.Model.Type)
		for role, declaration := range port.Roles {
			field := compositeRole(view, role, info)
			expression, computed := view.Expressions[role]
			if _, explicit := view.Roles[role]; explicit && computed {
				return fmt.Errorf("binding %s role %s: use one field or expression", b.ID, role)
			}
			if field == nil && !computed {
				if declaration.Required {
					return fmt.Errorf("binding %s port %s: role %s is required", b.ID, port.ID, role)
				}
				continue
			}
			kind := expression.Kind
			if field != nil {
				if field.Name != "" && len(field.Path) > 0 || field.Slot != "" || field.Transform != "" {
					return fmt.Errorf("binding %s: invalid field reference", b.ID)
				}
				f, err := RecordPathField(view.Model.Type, field.Segments(), entity)
				if err != nil {
					return fmt.Errorf("binding %s port %s role %s: %w", b.ID, port.ID, role, err)
				}
				kind = RuleFieldKind(f.Type)
				if target := declaration.Target; target != "" && f.Type == "reference" && f.Ref != b.Models[target].Model.Type {
					return fmt.Errorf("binding %s role %s: reference target differs from %s", b.ID, role, target)
				}
			} else if err := CheckRuleExpression(expression, RuleReadSpec{Object: view.Model.Type}, lookup, engines...); err != nil {
				return fmt.Errorf("binding %s role %s: %w", b.ID, role, err)
			} else if target := declaration.Target; target != "" {
				schema, err := RuleSchema(expression, RuleReadSpec{Object: view.Model.Type}, lookup, engines...)
				if err != nil {
					return err
				}
				schema.Refs["$visualTarget"] = ExprType{Kind: "reference", Object: b.Models[target].Model.Type}
				if _, err = ruleEngineOf(engines).CompileExpr(Expr{Op: "=", Kind: "boolean", Args: []Expr{expression, ExprReference("reference", "$visualTarget")}}, schema); err != nil {
					return fmt.Errorf("binding %s role %s: reference target differs from %s: %w", b.ID, role, target, err)
				}
			}
			if !slices.Contains(declaration.Kinds, kind) {
				return fmt.Errorf("binding %s role %s: incompatible value kind %s", b.ID, role, kind)
			}
		}
		for key := range view.Roles {
			if _, ok := port.Roles[key]; !ok && key != "label" && key != "state" {
				return fmt.Errorf("binding %s: undeclared role %s", b.ID, key)
			}
		}
		for key := range view.Expressions {
			if _, ok := port.Roles[key]; !ok {
				return fmt.Errorf("binding %s: undeclared expression %s", b.ID, key)
			}
		}
	}
	for key := range b.Models {
		if !slices.ContainsFunc(contract.ModelPorts, func(port pageModelPort) bool { return port.ID == key }) {
			return fmt.Errorf("binding %s: undeclared model port %s", b.ID, key)
		}
	}
	if widget == "model-matrix" {
		if b.Value == nil || !slices.Contains([]string{"text", "boolean", "integer", "number", "decimal"}, b.Value.Kind) {
			return fmt.Errorf("binding %s: matrix needs a scalar cell expression", b.ID)
		}
		if err := CheckRuleExpression(*b.Value, MatrixRuleReads(b), lookup, engines...); err != nil {
			return fmt.Errorf("binding %s cell: %w", b.ID, err)
		}
	} else if b.Value != nil {
		return fmt.Errorf("binding %s: cell expressions belong to matrices", b.ID)
	}
	return nil
}

func ValidPageImageURL(raw string) bool {
	if raw == "" {
		return true
	}
	if len(raw) > pageWidgets.Runtime.ContextViews.MaxURLBytes || !utf8.ValidString(raw) {
		return false
	}
	decoded, err := url.PathUnescape(raw)
	if err != nil || !utf8.ValidString(decoded) {
		return false
	}
	for _, text := range []string{raw, decoded} {
		for _, r := range text {
			if r == '\\' || unicode.IsSpace(r) || unicode.IsControl(r) {
				return false
			}
		}
	}
	if strings.HasPrefix(raw, "/") {
		return !strings.HasPrefix(raw, "//") && !strings.HasPrefix(decoded, "//")
	}
	lower := strings.ToLower(raw)
	if !strings.HasPrefix(lower, "https://") && !strings.HasPrefix(lower, "http://") {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.Host == "" || u.Hostname() == "" {
		return false
	}
	if strings.Contains(u.Host, "[") && net.ParseIP(u.Hostname()) == nil {
		return false
	}
	if strings.HasSuffix(u.Host, ":") {
		return false
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 0 || n > 65535 {
			return false
		}
	}
	return true
}

func PropertyValueType(field FieldInfo) string {
	switch field.Type {
	case "text", "longtext", "choice":
		return "string"
	case "boolean":
		return "boolean"
	case "integer", "decimal":
		return "decimal"
	}
	return ""
}

func (p Page) CheckPropertySchema(variable PageVariable, object EntityInfo) error {
	source := variable.Source
	if source == nil || source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App || p.RecordVariableObject(source.Variable) != object.Type {
		return fmt.Errorf("property record object is unavailable or incompatible")
	}
	field, ok := object.Field(source.Field)
	if !ok || PropertyValueType(field) == "" || PropertyValueType(field) != variable.Type {
		return fmt.Errorf("property field is unavailable or incompatible")
	}
	return nil
}

type PageVariable struct {
	RuleType   *ExprType           `json:"ruleType,omitempty"`
	Rule       *Expr               `json:"rule,omitempty"`
	Writable   bool                `json:"writable,omitempty"`
	Title      string              `json:"title,omitempty"`
	Scope      string              `json:"scope"`
	Owner      string              `json:"owner,omitempty"`
	Type       string              `json:"type"`
	Mode       string              `json:"mode"`
	Initial    json.RawMessage     `json:"initial,omitempty"`
	Expression *PageExpression     `json:"expression,omitempty"`
	Source     *PageResourceSource `json:"source,omitempty"`
}

type PageResourceSource struct {
	Compute *PageComputeResource `json:"compute,omitempty"`
	Port    string               `json:"port,omitempty"`
	Measure string               `json:"measure,omitempty"`
	Field   string               `json:"field,omitempty"`
	Fields  []string             `json:"fields,omitempty"`

	Object   *AssetRef `json:"object,omitempty"`
	Query    string    `json:"query,omitempty"`
	Variable string    `json:"variable,omitempty"`
	Kind     string    `json:"kind"`
	Section  string    `json:"section,omitempty"`
	Node     string    `json:"node,omitempty"`
}

type PageExpression struct {
	Op   string      `json:"op"`
	Args []PageValue `json:"args"`
}

type PageValue struct {
	Variable string          `json:"variable,omitempty"`
	Literal  json.RawMessage `json:"literal,omitempty"`
}

type pageRuntimeContract struct {
	Composite struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxRecords        int    `json:"maxRecords"`
		MaxCells          int    `json:"maxCells"`
		MaxBytes          int    `json:"maxBytes"`
	} `json:"composite"`
	ModelBinding struct {
		RequiredUIProfile string `json:"requiredUIProfile"`

		MaxRecords int `json:"maxRecords"`

		MaxBytes   int      `json:"maxBytes"`
		FieldSlots []string `json:"fieldSlots"`
	} `json:"modelBinding"`

	ComputeResource struct {
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxResources      int    `json:"maxResources"`
	} `json:"computeResource"`

	Embedding struct {
		MaxDepth     int `json:"maxDepth"`
		MaxInstances int `json:"maxInstances"`
		MaxQueries   int `json:"maxQueries"`
		MaxRecords   int `json:"maxRecords"`
		MaxSections  int `json:"maxSections"`
	} `json:"embedding"`
	Exploration struct {
		RequiredUIProfile     string `json:"requiredUIProfile"`
		MaxResourceWindow     int    `json:"maxResourceWindow"`
		SharedVertexUIProfile string `json:"sharedVertexUIProfile"`
	} `json:"exploration"`
	Telemetry struct {
		SharedUIProfile string `json:"sharedUIProfile"`
	} `json:"telemetry"`
	ContextViews struct {
		SharedUIProfile   string `json:"sharedUIProfile"`
		RequiredUIProfile string `json:"requiredUIProfile"`
		MaxAvatarWindow   int    `json:"maxAvatarWindow"`

		MaxURLBytes int `json:"maxURLBytes"`
	} `json:"contextViews"`

	DateInput struct {
		RequiredUIProfile         string `json:"requiredUIProfile"`
		DateTimeRequiredUIProfile string `json:"dateTimeRequiredUIProfile"`
	} `json:"dateInput"`

	RecordSelection struct {
		MaxRecords      int    `json:"maxRecords"`
		SharedUIProfile string `json:"sharedUIProfile"`
	} `json:"recordSelection"`
	TableEditing struct {
		MaxRows int `json:"maxRows"`

		FieldTypes []string `json:"fieldTypes"`
	} `json:"tableEditing"`
	Scope   string `json:"scope"`
	Decimal struct {
		MaxBytes int `json:"maxBytes"`
	} `json:"decimal"`
	Application struct {
		Scope           string   `json:"scope"`
		ValueTypes      []string `json:"valueTypes"`
		Modes           []string `json:"modes"`
		BindingMode     string   `json:"bindingMode"`
		MaxFilterFields int      `json:"maxFilterFields"`
	} `json:"application"`
	Overlay struct {
		Scope      string   `json:"scope"`
		ValueTypes []string `json:"valueTypes"`
		Modes      []string `json:"modes"`
	} `json:"overlay"`
	ValueTypes     []string       `json:"valueTypes"`
	MaxVariables   int            `json:"maxVariables"`
	MaxStringBytes int            `json:"maxStringBytes"`
	Operators      []pageOperator `json:"operators"`
	Resources      []struct {
		Kind    string   `json:"kind"`
		Type    string   `json:"type"`
		Widget  string   `json:"widget"`
		Widgets []string `json:"widgets,omitempty"`
	} `json:"resources"`
	Loop struct {
		MaxDepth int    `json:"maxDepth"`
		Scope    string `json:"scope"`
		Source   string `json:"source"`

		MaxItems int `json:"maxItems"`
	} `json:"loop"`
	Aggregate struct {
		Source           string `json:"source"`
		MaxVariables     int    `json:"maxVariables"`
		MaxExpandedReads int    `json:"maxExpandedReads"`
	} `json:"aggregate"`
	Query struct {
		Set struct {
			Operations    []string `json:"operations"`
			MaxConditions int      `json:"maxConditions"`

			MaxNodes int `json:"maxNodes"`
			MaxDepth int `json:"maxDepth"`
			MaxBytes int `json:"maxBytes"`
		} `json:"set"`
		Source                string   `json:"source"`
		InventoryUIProfile    string   `json:"inventoryUIProfile"`
		MaxDeclaredPlans      int      `json:"maxDeclaredPlans"`
		InterfaceUIProfile    string   `json:"interfaceUIProfile"`
		MaxDeclaredTotalLimit int      `json:"maxDeclaredTotalLimit"`
		MaxPlans              int      `json:"maxPlans"`
		MaxConditions         int      `json:"maxConditions"`
		MaxSort               int      `json:"maxSort"`
		MaxLimit              int      `json:"maxLimit"`
		MaxTotalLimit         int      `json:"maxTotalLimit"`
		MaxOffset             int      `json:"maxOffset"`
		Operators             []string `json:"operators"`
	} `json:"query"`
	Interface struct {
		MaxPorts   int      `json:"maxPorts"`
		MaxVersion int      `json:"maxVersion"`
		ValueTypes []string `json:"valueTypes"`
	} `json:"interface"`
}

type pageOperator struct {
	ID      string `json:"id"`
	Input   string `json:"input"`
	Output  string `json:"output"`
	MinArgs int    `json:"minArgs"`
	MaxArgs int    `json:"maxArgs"`
}

func pageLiteralType(raw json.RawMessage) string {
	if _, ok := NumberLiteral(raw); ok {
		return "number"
	}
	if _, ok := DecimalLiteral(raw); ok {
		return "decimal"
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	if m, ok := value.(map[string]any); ok && len(m) == 2 && m["kind"] == "string-set" {
		if values, ok := m["values"].([]any); ok && len(values) <= 64 {
			seen := map[string]bool{}
			for _, item := range values {
				v, ok := item.(string)
				if !ok || len(v) > 4096 || seen[v] {
					return ""
				}
				seen[v] = true
			}
			return "string-set"
		}
	}
	switch v := value.(type) {
	case bool:
		return "boolean"
	case string:
		if len(v) <= pageWidgets.Runtime.MaxStringBytes {
			return "string"
		}
	}
	return ""
}

func (d PageInputSchema) CheckVariables(engines ...RuleEngine) error {
	contract := pageWidgets.Runtime
	if len(d.Variables) > contract.MaxVariables {
		return fmt.Errorf("page variable limit exceeded")
	}
	ids := make([]string, 0, len(d.Variables))
	for id := range d.Variables {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len((Page{Format: 3, InputSchema: d}).ComputeResources()) > pageWidgets.Runtime.ComputeResource.MaxResources {
		return fmt.Errorf("page compute resources exceed their bound")
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) (string, error)
	visit = func(id string) (string, error) {
		v, ok := d.Variables[id]
		fail := func(reason string) (string, error) {
			return "", fmt.Errorf("page variable %s: %s", id, reason)
		}
		if !ok {
			return fail("missing dependency")
		}
		if visiting[id] {
			return fail("cyclic dependency")
		}
		if visited[id] {
			return v.Type, nil
		}
		if !pageNodeID.MatchString(id) || (v.Scope != contract.Scope && v.Scope != contract.Loop.Scope && v.Scope != contract.Overlay.Scope && v.Scope != contract.Application.Scope) || ((v.Scope == contract.Scope || v.Scope == contract.Application.Scope) && v.Owner != "") || ((v.Scope == contract.Loop.Scope || v.Scope == contract.Overlay.Scope) && !pageNodeID.MatchString(v.Owner)) || !slices.Contains(contract.ValueTypes, v.Type) || len(v.Title) > 1024 {
			return fail("unsupported identity, scope or type")
		}
		if v.Type == "record-set" && !((PageUIProfileSupports(d.profileVersion(), "platform.page.v2.32") && v.Mode == "resource" && v.Source != nil && v.Source.Kind == "records" && slices.Contains([]string{"page", "overlay"}, v.Scope)) || (PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.RecordSelection.SharedUIProfile) && v.Scope == "application" && (v.Mode == "shared" || v.Mode == "resource"))) {
			return fail("record-set needs a v2.32 local table resource")
		}
		if v.Scope == contract.Overlay.Scope && (!slices.Contains(contract.Overlay.ValueTypes, v.Type) || !slices.Contains(contract.Overlay.Modes, v.Mode)) {
			return fail("overlay variable needs a supported local value or resource")
		}
		visiting[id] = true
		if v.Scope == contract.Application.Scope && !slices.Contains(contract.Application.ValueTypes, v.Type) {
			return fail("application variable needs a supported scalar or resource")
		}
		if v.Writable && v.Mode != contract.Application.BindingMode {
			return fail("only shared bindings declare writable")
		}
		if v.Type == "statistics" && (!PageUIProfileSupports(d.profileVersion(), "platform.page.v2.50") || v.Mode != "aggregate" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
			return fail("statistics needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Compute != nil && (v.Mode != "resource" || v.Source.Kind != "compute") {
			return fail("compute needs its original source kind")
		}
		if v.Type == "number" && (!PageUIProfileSupports(d.profileVersion(), "platform.page.v2.49") || !(slices.Contains([]string{"aggregate", "constant", "derived"}, v.Mode) || v.Mode == "resource" && v.Source != nil && v.Source.Kind == "compute" && PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.ComputeResource.RequiredUIProfile)) || v.Scope == "application") {
			return fail("number needs its read-only scoped profile")
		}
		if v.Source != nil && v.Source.Port != "" && (!PageUIProfileSupports(d.profileVersion(), pageWidgets.Runtime.Exploration.RequiredUIProfile) || !pageNodeID.MatchString(v.Source.Port) || v.Type != "record" || v.Mode != "resource" || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
			return fail("record output port needs its scoped finite graph profile")
		}
		if v.Source != nil && v.Source.Measure != "" && (v.Mode != "aggregate" || v.Source.Kind != "aggregate" && v.Source.Kind != "statistics") {
			return fail("measure needs an aggregate scalar")
		}
		if v.Mode != "resource" && v.Mode != "property" && v.Mode != "aggregate" && v.Mode != contract.Application.BindingMode && v.Source != nil {
			return fail("only resource or shared variables may declare a source")
		}
		if v.Source != nil && v.Source.Object != nil && !((v.Mode == "shared" && (v.Type == "object-set" || v.Type == "record" || v.Type == "filter" || v.Type == "record-set")) || (v.Mode == "resource" && v.Scope == "application" && (v.Type == "record" || v.Type == "filter" || v.Type == "record-set")) || v.Mode == "property") {
			return fail("only shared windows declare an object requirement")
		}
		if v.Source != nil && len(v.Source.Fields) > 0 && !(v.Scope == "application" && v.Mode == "resource" && v.Type == "filter") {
			return fail("only an application filter declares fields")
		}
		if v.Source != nil && v.Source.Field != "" && v.Mode != "property" {
			return fail("only a property source declares a field")
		}
		if v.Rule != nil && v.Mode != "derived" {
			return fail("only derived variables carry rules")
		}
		switch v.Mode {
		case "aggregate":
			if v.Source == nil || !((v.Type == "decimal" && v.Source.Kind == contract.Aggregate.Source && v.Source.Measure == "") || (v.Type == "number" && v.Source.Kind == "aggregate") || (v.Type == "statistics" && v.Source.Kind == "statistics" && pageNodeID.MatchString(v.Source.Measure))) || !pageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Variable != "" || v.Expression != nil || len(v.Initial) > 0 {
				return fail("aggregate needs only a count query source")
			}
			if v.Source.Kind == "aggregate" {
				if _, _, ok := stringsCutMeasure(v.Source.Measure); !ok {
					return fail("aggregate needs a supported numeric measure")
				}
			}
		case "property":
			if v.Source == nil || v.Source.Kind != "property" || !pageNodeID.MatchString(v.Source.Variable) || !pageNodeID.MatchString(v.Source.Field) || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || len(v.Source.Fields) > 0 || len(v.Initial) > 0 || v.Expression != nil || !slices.Contains([]string{"string", "boolean", "decimal"}, v.Type) {
				return fail("property needs a typed record and field source")
			}
			parent := d.Variables[v.Source.Variable]
			if parent.Scope == "loop-item" && (v.Scope != "loop-item" || parent.Owner != v.Owner) || parent.Scope == "overlay" && (v.Scope != "overlay" || parent.Owner != v.Owner) {
				return fail("property source escapes its scope")
			}
			typ, err := visit(v.Source.Variable)
			if err != nil {
				return "", err
			}
			if typ != "record" {
				return fail("property source must be a record")
			}
		case "shared":
			if (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject) {
				return fail("shared record needs an object requirement")
			}
			if v.Type == "object-set" && (v.Writable || v.Source == nil || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject) {
				return fail("shared window needs a read-only object requirement")
			}
			if v.Scope != "application" || v.Source == nil || v.Source.Kind != "application" || !pageNodeID.MatchString(v.Source.Variable) || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" || v.Expression != nil || len(v.Initial) != 0 {
				return fail("shared binding needs only an application variable source")
			}
		case "input":
			if v.Type == "object-set" && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.85") {
				return fail("collection input needs v2.85")
			}
			if v.Scope != "page" || v.Source != nil || v.Expression != nil || !slices.Contains(contract.Interface.ValueTypes, v.Type) || (v.Type == "record" || v.Type == "object-set") && len(v.Initial) != 0 || len(v.Initial) != 0 && pageLiteralType(v.Initial) != v.Type {
				return fail("input needs a typed page value without a source")
			}
		case "resource":
			if v.Source != nil && v.Source.Kind == "compute" {
				if err := d.checkComputeResource(v); err != nil {
					return fail(err.Error())
				}
				if _, err := visit(v.Source.Compute.RecordVariable); err != nil {
					return "", err
				}
				break
			}
			if v.Source == nil || v.Source.Variable != "" || v.Expression != nil || len(v.Initial) != 0 {
				return fail("resource variable needs only a typed source")
			}
			if (v.Source.Kind == "record" || v.Source.Kind == "filter" || v.Source.Kind == "record-set") && v.Scope == "application" {
				if v.Type != v.Source.Kind || v.Source.Object == nil || v.Source.Object.Check() != nil || v.Source.Object.Kind != AssetObject || v.Source.Section != "" || v.Source.Node != "" || v.Source.Query != "" {
					return fail("application record needs only an object source")
				}
				if v.Type == "filter" {
					if len(v.Source.Fields) == 0 || len(v.Source.Fields) > contract.Application.MaxFilterFields {
						return fail("application filter needs bounded fields")
					}
					seen := map[string]bool{}
					for _, field := range v.Source.Fields {
						if !pageNodeID.MatchString(field) || seen[field] {
							return fail("invalid filter field")
						}
						seen[field] = true
					}
				}
				break
			}
			if v.Source.Kind == "plan" {
				if (v.Scope != "page" && v.Scope != "overlay" && v.Scope != "application" && v.Scope != "loop-item") || v.Type != "object-set" || !pageNodeID.MatchString(v.Source.Query) || v.Source.Section != "" || v.Source.Node != "" {
					return fail("plan source needs a scoped query window")
				}
				break
			}
			if v.Source.Query != "" {
				return fail("only a plan source may declare a query")
			}
			if v.Scope == contract.Loop.Scope {
				if v.Type != "record" || v.Source.Kind != contract.Loop.Source || v.Source.Node != v.Owner || v.Source.Section != "" {
					return fail("item source needs its loop owner")
				}
				break
			}
			if v.Scope == "application" || !pageNodeID.MatchString(v.Source.Section) || v.Source.Node != "" {
				return fail("resource source needs a section")
			}
			found := false
			for _, resource := range contract.Resources {
				if resource.Kind == v.Source.Kind && resource.Type == v.Type {
					found = true
				}
			}
			if !found {
				return fail("resource source type mismatch")
			}
		case "constant", "state":
			if v.Type == "string-set" && (!PageUIProfileSupports(d.profileVersion(), "platform.page.v2.30") || !slices.Contains([]string{"page", "overlay"}, v.Scope)) {
				return fail("string-set state requires v2.30 page or overlay scope")
			}
			if v.Expression != nil || pageLiteralType(v.Initial) != v.Type {
				return fail("initial value type mismatch")
			}
		case "derived":
			if len(v.Initial) != 0 || v.Expression == nil && v.Rule == nil {
				return fail("derived variable needs only an expression")
			}
			if v.Rule != nil {
				if v.Expression != nil {
					return fail("use one rule expression")
				}
				schema := d.PageRuleSchema()
				var walk func(Expr) error
				walk = func(e Expr) error {
					if e.Agg != nil {
						if _, err := visit(e.Agg.Source); err != nil {
							return err
						}
					}
					if e.Ref != "" && !strings.HasPrefix(e.Ref, "item.") {
						dependency := d.Variables[e.Ref]
						if dependency.Scope == contract.Loop.Scope && (v.Scope != contract.Loop.Scope || v.Owner != dependency.Owner) || dependency.Scope == contract.Overlay.Scope && (v.Scope != contract.Overlay.Scope || v.Owner != dependency.Owner) {
							return fmt.Errorf("rule dependency escapes its scope")
						}
						if _, err := visit(e.Ref); err != nil {
							return err
						}
					}
					for _, arg := range e.Args {
						if err := walk(arg); err != nil {
							return err
						}
					}
					if e.In != nil {
						return walk(*e.In)
					}
					return nil
				}
				if err := walk(*v.Rule); err != nil {
					return fail(err.Error())
				}
				if !PageRuleFits(v.Type, v.Rule.Kind) {
					return fail("rule output type differs")
				}
				if _, err := ruleEngineOf(engines).CompileExpr(*v.Rule, schema); err != nil {
					return fail(err.Error())
				}
				break
			}
			expr := v.Expression
			if expr.Op == "parse-number" && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.49") {
				return fail("parse-number requires its profile")
			}
			if expr != nil && expr.Op == "parse-decimal" && !PageUIProfileSupports(d.profileVersion(), "platform.page.v2.48") {
				return fail("parse-decimal requires its profile")
			}
			at := slices.IndexFunc(contract.Operators, func(op pageOperator) bool {
				return op.ID == expr.Op
			})
			if at < 0 {
				return fail("unsupported operator")
			}
			op := contract.Operators[at]
			if len(expr.Args) < op.MinArgs || len(expr.Args) > op.MaxArgs || v.Type != op.Output {
				return fail("operator arity or output type mismatch")
			}
			first := ""
			for _, arg := range expr.Args {
				if (arg.Variable != "") == (len(arg.Literal) != 0) {
					return fail("argument needs exactly one variable or literal")
				}
				typ := pageLiteralType(arg.Literal)
				if arg.Variable != "" {
					dependency := d.Variables[arg.Variable]
					if dependency.Scope == contract.Loop.Scope && (v.Scope != contract.Loop.Scope || v.Owner != dependency.Owner) {
						return fail("item dependency escapes its loop scope")
					}
					if dependency.Scope == contract.Overlay.Scope && (v.Scope != contract.Overlay.Scope || v.Owner != dependency.Owner) {
						return fail("overlay dependency escapes its owner scope")
					}
					var err error
					typ, err = visit(arg.Variable)
					if err != nil {
						return "", err
					}
				}
				if first == "" {
					first = typ
				}
				resource := typ == "record" || typ == "filter" || typ == "object-set"
				if typ == "statistics" || typ == "" || (op.Input == "same" && (typ != first || resource)) || (op.Input == "resource" && !resource) || (op.Input != "same" && op.Input != "resource" && typ != op.Input) {
					return fail("argument type mismatch")
				}
			}
		default:
			return fail("unsupported variable mode")
		}
		visiting[id] = false
		visited[id] = true
		return v.Type, nil
	}
	for _, id := range ids {
		if _, err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func PageRuleKind(kind string) string {
	switch kind {
	case "string":
		return "text"
	case "record", "filter", "object-set", "record-set":
		return "reference"
	case "string-set":
		return "set"
	default:
		return kind
	}
}

func (d PageInputSchema) PageRuleSchema() ExprSchema {
	schema := ExprSchema{Refs: map[string]ExprType{}, Sets: map[string]ExprSetType{}}
	for name, v := range d.Variables {
		t := ExprType{Kind: PageRuleKind(v.Type)}
		if t.Kind == "reference" {
			t.Resource = v.Type
		}
		if t.Kind == "set" {
			t.Element = &ExprType{Kind: "text"}
		}
		if v.RuleType != nil {
			if v.Mode == "property" {
				t.Kind = v.RuleType.Kind
			}
			t.Fields = v.RuleType.Fields
		}
		if v.Rule != nil {
			t.Kind = v.Rule.Kind
		}
		schema.Refs[name] = t
		if v.Type == "object-set" && v.RuleType != nil {
			schema.Sets[name] = ExprSetType{Element: *v.RuleType, Policy: "visible"}
		}
	}
	return schema
}

func (d PageInputSchema) NormalizePageRules(engines ...RuleEngine) error {
	if err := d.CheckVariables(engines...); err != nil {
		return err
	}
	schema := d.PageRuleSchema()
	for name, v := range d.Variables {
		if v.Mode == "derived" && v.Expression != nil {
			raw, _ := json.Marshal(v.Expression)
			e, err := LowerPageExpr(raw, PageRuleKind(v.Type), schema)
			if err != nil {
				return fmt.Errorf("page variable %s: %w", name, err)
			}
			v.Rule = &e
			v.Expression = nil
			d.Variables[name] = v
		}
	}
	return d.CheckVariables(engines...)
}

func PageRuleFits(carrier, kind string) bool {
	if PageRuleKind(carrier) == kind {
		return true
	}
	if carrier == "decimal" && kind == "integer" {
		return true
	}
	if carrier == "string" {
		return kind == "date" || kind == "datetime" || kind == "choice" || kind == "reference"
	}
	return false
}

func (d *PageDocument) inputSchema() PageInputSchema {
	if d == nil {
		return PageInputSchema{}
	}
	return PageInputSchema{profile: d.UIProfile, Variables: d.Variables, Queries: d.Queries, Interface: d.Interface}
}

func (p Page) inputSchema() PageInputSchema {
	if p.Format == 3 {
		return p.InputSchema
	}
	return p.Document.inputSchema()
}

func (d PageInputSchema) checkProfile() error {
	for id, v := range d.Variables {
		if err := d.checkVariableProfile(id, v); err != nil {
			return err
		}
	}
	return nil
}

func (d PageInputSchema) checkVariableProfile(id string, v PageVariable) error {
	supports := func(feature string) bool {
		return PageUIProfileSupports(d.profileVersion(), pageWidgets.Features[feature])
	}
	switch {
	case v.Mode == "property" && !supports("propertyVariables"):
		return fmt.Errorf("property variable %s requires v2.17", id)
	case v.Type == "decimal" && !supports("decimalVariables"):
		return fmt.Errorf("decimal variable %s requires v2.16", id)
	case v.Scope == "application" && v.Type == "filter" && !supports("applicationFilters"):
		return fmt.Errorf("shared filter %s requires v2.15", id)
	case v.Scope == "application" && v.Type == "record" && !supports("applicationRecords"):
		return fmt.Errorf("shared record %s requires v2.14", id)
	case v.Scope == "application" && v.Type == "object-set" && !supports("applicationCollections"):
		return fmt.Errorf("shared window %s requires v2.12", id)
	case v.Scope == "application" && (!supports("applicationBindings") || v.Mode != "shared"):
		return fmt.Errorf("page variable %s needs v2.8 and a shared application binding", id)
	case v.Scope == "overlay" && v.Type == "filter" && !supports("overlayFilters"):
		return fmt.Errorf("overlay filter %s requires v2.13", id)
	case v.Scope == "overlay" && v.Mode == "resource" && !supports("overlayResources"):
		return fmt.Errorf("overlay resource %s requires UI profile v2.11", id)
	case v.Scope == "overlay" && !supports("overlayVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.7", id)
	case v.Scope == pageWidgets.Runtime.Loop.Scope && !supports("loopVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.5", id)
	case v.Mode == "resource" && !supports("resourceVariables"):
		return fmt.Errorf("page variable %s requires UI profile v2.3", id)
	}
	return nil
}

func (d PageInputSchema) profileVersion() string {
	if d.profile != "" {
		return d.profile
	}
	return "platform.page.v3"
}

func (d *PageDocument) CheckVariables(engines ...RuleEngine) error {
	return d.inputSchema().CheckVariables(engines...)
}

func (d PageDocument) PageRuleSchema() ExprSchema { return d.inputSchema().PageRuleSchema() }

func (d *PageDocument) NormalizePageRules(engines ...RuleEngine) error {
	return d.inputSchema().NormalizePageRules(engines...)
}
