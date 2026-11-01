package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
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
	if p.Format == 3 || p.Format == 4 {
		return p.InputSchema
	}
	return p.Document.inputSchema()
}

func (d PageDocument) PageRuleSchema() ExprSchema { return d.inputSchema().PageRuleSchema() }

func (d *PageDocument) NormalizePageRules(engines ...RuleEngine) error {
	return d.inputSchema().NormalizePageRules(engines...)
}
