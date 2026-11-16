package pages

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func checkDataBinding(p platform.Page, binding platform.PageDataBinding, lookup func(string) (platform.EntityInfo, bool), criteria func(platform.AssetRef) (*platform.RuleCriteria, bool), engines ...platform.RuleEngine) error {
	if binding.Object.Name != "" {
		info, ok := lookup(binding.Object.Name)
		if !ok || info.App != binding.Object.App {
			return fmt.Errorf("page binding object %s expects owner %s but the installed owner is %s", binding.Object.Name, binding.Object.App, info.App)
		}
	}
	models := []platform.ModelBinding{}
	if binding.Model != nil {
		models = append(models, *binding.Model)
	}
	for _, view := range binding.Models {
		models = append(models, view.Model)
	}
	if len(models) == 0 && binding.Object.Name != "" {
		models = append(models, platform.ModelBinding{Type: binding.Object.Name, Fields: binding.Fields})
	}
	for _, model := range models {
		if len(model.Fields) > platform.PageModelMaxFields || len(model.Where) > 32 || model.Visibility != "" && model.Visibility != "hide" && model.Visibility != "mark" {
			return fmt.Errorf("model binding exceeds its declared shape")
		}
		seen, slots := map[string]bool{}, map[string]bool{}
		for _, field := range model.Fields {
			key := field.Slot + "/" + fieldKey(field)
			if field.Name != "" && len(field.Path) > 0 || len(field.Path) > 16 || fieldKey(field) == "" || seen[key] || field.Slot != "" && field.Slot != "fields" && slots[field.Slot] || field.Transform != "" && !slices.Contains([]string{"month", "sum", "avg", "min", "max"}, field.Transform) {
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
		metadata := func(typ string) (platform.EntityInfo, bool) {
			value, found := lookup(typ)
			return platform.ConstraintEntity(value), found
		}
		for _, field := range model.Fields {
			if _, err := platform.RecordPathField(info.Type, fieldSegments(field), metadata); err != nil {
				return err
			}
		}
		for _, expr := range model.Where {
			if err := checkRuleExpression(expr, platform.RuleReadSpec{Object: info.Type}, lookup, engines...); err != nil {
				return err
			}
		}
		if model.Label != nil {
			if err := checkRuleExpression(*model.Label, platform.RuleReadSpec{Object: info.Type}, lookup, engines...); err != nil {
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
				if !ok || (typ.Kind == "reference" || typ.Object != "") && recordVariableObject(p, id) != typ.Object || typ.Kind != "reference" && typ.Object == "" && !pageRuleInputFits(variable.Type, typ.Kind) {
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
		if _, err := platform.RecordPathField(object, fieldSegments(field), lookup); err != nil {
			return err
		}
	}
	for slot, fields := range binding.FieldSlots {
		if strings.HasPrefix(slot, "observation.") || slot == "ai.replyField" || slot == "scene.mappings.field" {
			continue
		}
		source := object
		if strings.HasPrefix(slot, "scene.sample") {
			source = collectionInputObject(p, binding.Ports["sceneSampleCollectionVariable"]).Name
			if source == "" {
				source = recordResourceObject(p, binding.Ports["sceneSampleVariable"]).Name
			}
		}
		for _, field := range fields {
			if _, err := platform.RecordPathField(source, fieldSegments(field), lookup); err != nil {
				return err
			}
		}
	}
	for _, view := range binding.Models {
		for _, field := range view.Roles {
			if _, err := platform.RecordPathField(view.Model.Type, fieldSegments(field), lookup); err != nil {
				return err
			}
		}
		for _, expr := range view.Expressions {
			if err := checkRuleExpression(expr, platform.RuleReadSpec{Object: view.Model.Type}, lookup, engines...); err != nil {
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
				source = collectionInputObject(p, variable).Name
				if field.Slot == "averageField" {
					source = object
				}
			}
			if resource.Kind == "ai" {
				continue
			}
			if resource.Kind == "record-mapping" && resource.Source == "sample" {
				source = collectionInputObject(p, binding.Ports["sceneSampleCollectionVariable"]).Name
				if source == "" {
					source = recordResourceObject(p, binding.Ports["sceneSampleVariable"]).Name
				}
			}
			if field.Name == "" && len(field.Path) == 0 {
				continue
			}
			if _, err := platform.RecordPathField(source, fieldSegments(field), lookup); err != nil {
				return err
			}
		}
		if resource.Asset != nil {
			if err := resource.Asset.Ref.Check(); err != nil {
				return err
			}
		}
		if resource.Kind == "external-document" && !validPageExternalFrame(platform.PageExternalFrame{URL: resource.URL, Origin: resource.Origin}) {
			return fmt.Errorf("page external resource origin is invalid")
		}
		if resource.Kind == "image" && !validPageImageURL(resource.URL) {
			return fmt.Errorf("page image resource is invalid")
		}
		if resource.Kind == "page" && (resource.Asset == nil || resource.Asset.SourceVersion == "" || CheckPageContentVersion(resource.ContentVersion) != nil) {
			return fmt.Errorf("embedded page needs its original exact content")
		}
	}
	if binding.Kind == "composite" {
		return checkComposite(binding, lookup, engines...)
	}
	return nil
}

func checkRuleExpression(expr platform.Expr, reads platform.RuleReadSpec, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	schema, err := platform.RuleSchema(expr, reads, lookup, engines...)
	if err != nil {
		return err
	}
	_, err = platform.RuleEngineOf(engines).CompileExpr(expr, schema)
	return err
}

func pageRuleInputFits(variable, kind string) bool {
	return variable == kind || variable == "string" && (kind == "text" || kind == "choice" || kind == "date" || kind == "datetime") || variable == "number" && (kind == "number" || kind == "integer")
}
