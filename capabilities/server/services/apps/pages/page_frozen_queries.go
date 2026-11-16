package pages

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func CheckFrozenQueries(original platform.Page, assets map[platform.AssetRef]platform.ReleaseAsset) error {
	page, err := Safety(original)
	if err != nil {
		return err
	}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		asset, ok := assets[platform.ModelObject(typ)]
		if !ok {
			for ref, value := range assets {
				if ref.Kind == platform.AssetObject && ref.Name == typ {
					asset = value
					ok = true
					break
				}
			}
		}
		info, err := publication.QueryObjectDescriptor(asset.Body)
		return platform.ConstraintEntity(info), ok && err == nil
	}
	criteria := func(ref platform.AssetRef) (*platform.RuleCriteria, bool) {
		asset, ok := assets[ref]
		action, err := publication.ReleaseActionDescriptor(asset.Body)
		if !ok || err != nil {
			return nil, false
		}
		reads := platform.RuleReadSpec{Object: action.Target, Inputs: map[string]platform.ExprType{}}
		mapped := map[string]bool{}
		for _, binding := range page.DataBindings {
			for _, model := range binding.ModelBindings() {
				if model.Options != nil && model.Options.Rule != nil && model.Options.Rule.Action == ref {
					for name := range model.Options.Rule.Inputs {
						mapped[name] = true
					}
				}
			}
		}
		for _, field := range action.Payload {
			if mapped[field.Name] {
				reads.Inputs[field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
			}
		}
		return &platform.RuleCriteria{Reads: reads}, true
	}
	for _, binding := range page.DataBindings {
		if err := checkDataBinding(page, binding, lookup, criteria); err != nil {
			return err
		}
		for _, model := range binding.ModelBindings() {
			sets := []*platform.ModelRecordSet{model.Set}
			if model.Options != nil {
				sets = append(sets, model.Options.Set)
			}
			for _, set := range sets {
				if set == nil || set.Query == nil {
					continue
				}
				asset, ok := assets[set.Query.Ref]
				var query platform.NamedQuery
				if !ok || asset.SourceVersion != set.Query.SourceVersion || json.Unmarshal(asset.Body, &query) != nil || query.Object != model.Type {
					return fmt.Errorf("binding %s frozen model source differs", binding.ID)
				}
			}
		}
		for _, resource := range binding.Resources {
			if resource.Asset == nil {
				continue
			}
			asset, ok := assets[resource.Asset.Ref]
			if !ok || resource.Asset.SourceVersion != "" && asset.SourceVersion != resource.Asset.SourceVersion {
				return fmt.Errorf("binding %s exact resource is unavailable", binding.ID)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		object := page.Object.Name
		for _, binding := range page.DataBindings {
			if binding.ID == trigger.Binding {
				object = binding.ObjectRef(page).Name
			}
		}
		for _, ref := range trigger.Actions {
			asset, ok := assets[ref]
			action, err := publication.ReleaseActionDescriptor(asset.Body)
			if !ok || err != nil || platform.StaticControlAction(action) || action.Target != object {
				return fmt.Errorf("page action owner or target is unavailable")
			}
		}
		if trigger.InlineEdit != nil {
			asset, ok := assets[trigger.InlineEdit.Action]
			action, err := publication.ReleaseActionDescriptor(asset.Body)
			info, found := lookup(object)
			if !ok || !found || err != nil || CheckTableEdit(trigger.InlineEdit, info, action) != nil {
				return fmt.Errorf("page edit fields differ from original action")
			}
		}
		if len(trigger.Defaults) > 0 {
			if len(trigger.Actions) != 1 {
				return fmt.Errorf("page parameters need one original action")
			}
			action, err := publication.ReleaseActionDescriptor(assets[trigger.Actions[0]].Body)
			if err != nil {
				return err
			}
			info, _ := lookup(object)
			for _, mapping := range trigger.Defaults {
				field, ok := info.Field(mapping.Field)
				index := slices.IndexFunc(action.Payload, func(f platform.Field) bool { return f.Name == mapping.Parameter })
				if !ok || index < 0 || !actionParameterCompatible(field, action.Payload[index]) {
					return fmt.Errorf("page action parameter differs from original readable field")
				}
			}
		}
		if trigger.Function != nil {
			asset, ok := assets[trigger.Function.Ref]
			var function platform.AIFunction
			if !ok || asset.SourceVersion != trigger.Function.SourceVersion || json.Unmarshal(asset.Body, &function) != nil || function.Object != object {
				return fmt.Errorf("page function original source is unavailable")
			}
		}
		if trigger.Operation != nil {
			asset, ok := assets[trigger.Operation.Ref]
			var operation platform.Operation
			if !ok || asset.SourceVersion != trigger.Operation.SourceVersion || json.Unmarshal(asset.Body, &operation) != nil || operation.Check() != nil {
				return fmt.Errorf("page operation original source is unavailable")
			}
		}
	}
	if err := CheckVariables(page.InputSchema); err != nil {
		return err
	}
	if err := page.InputSchema.CheckQuerySets(); err != nil {
		return err
	}
	for id, compute := range page.ComputeResources() {
		asset, ok := assets[compute.Operation.Ref]
		var op platform.Operation
		object, found := lookup(page.RecordResourceObject(compute.RecordVariable).Name)
		if !ok || !found || asset.SourceVersion != compute.Operation.SourceVersion || json.Unmarshal(asset.Body, &op) != nil || CheckComputeResource(page, compute, object, op, page.InputSchema.Variables[id].Type, lookup) != nil {
			return fmt.Errorf("frozen compute resource contract is unavailable")
		}
	}
	for _, v := range page.InputSchema.Variables {
		if v.Source == nil {
			continue
		}
		if v.Mode == "property" && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			if !ok || CheckPropertySchema(page, v, info) != nil {
				return fmt.Errorf("frozen property schema is unavailable")
			}
		}
		if v.Scope == "application" && v.Mode == "resource" && v.Type == "filter" && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			if !ok || CheckFilterSchema(*v.Source, info) != nil {
				return fmt.Errorf("frozen application filter schema is unavailable")
			}
		}
	}
	namedSources := map[platform.AssetBinding]platform.NamedQuery{}
	for id, plan := range page.InputSchema.Queries {
		if plan.Interface != "" {
			named, err := frozenPageInterfaceQuery(plan, assets)
			if err != nil {
				return err
			}
			if err := checkInterfaceQuery(page, plan, named); err != nil {
				return err
			}
			continue
		}
		asset, ok := assets[plan.Object]
		object, err := publication.QueryObjectDescriptor(asset.Body)
		if !ok || err != nil {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		var named *platform.Definition
		if plan.Query != nil {
			asset, ok := assets[plan.Query.Ref]
			if !ok || asset.SourceVersion != plan.Query.SourceVersion {
				return fmt.Errorf("page query %s exact source is unavailable", id)
			}
			if plan.Query.Ref.Kind == platform.AssetLinkType {
				var link platform.LinkType
				if json.Unmarshal(asset.Body, &link) != nil {
					return fmt.Errorf("frozen link source is invalid")
				}
				parentAsset, pok := assets[link.Parent]
				childAsset, cok := assets[link.Child]
				parent, pe := publication.QueryObjectDescriptor(parentAsset.Body)
				child, ce := publication.QueryObjectDescriptor(childAsset.Body)
				parent.App = link.Parent.App
				child.App = link.Child.App
				if !pok || !cok || pe != nil || ce != nil || link.CheckSchema(parent, child) != nil {
					return fmt.Errorf("frozen link endpoints are unavailable")
				}
				named = &platform.Definition{Ref: asset.Ref, Version: asset.SourceVersion, LinkType: &link}
			} else {
				var query platform.NamedQuery
				if json.Unmarshal(asset.Body, &query) != nil {
					return fmt.Errorf("frozen named query is invalid")
				}
				named = &platform.Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &query}
				namedSources[*plan.Query] = query
			}
		}
		for _, v := range page.InputSchema.Variables {
			if v.Source != nil && v.Source.Query == id {
				if err := checkAggregateScalar(v, object); err != nil {
					return err
				}
			}
		}
		if err := CheckBoundQuery(page, id, named); err != nil {
			return err
		}
		if err := CheckQuerySchema(page, plan, object, named); err != nil {
			return err
		}
	}
	return CheckQuerySetConditions(page, namedSources)
}

func frozenPageInterfaceQuery(q platform.PageQuery, lookup map[platform.AssetRef]platform.ReleaseAsset) (*platform.Definition, error) {
	if q.Query == nil {
		return nil, fmt.Errorf("interface page query needs a published binding")
	}
	asset, ok := lookup[q.Query.Ref]
	var named platform.NamedQuery
	if !ok || asset.SourceVersion != q.Query.SourceVersion || json.Unmarshal(asset.Body, &named) != nil || named.InterfaceShape == nil || named.Check() != nil {
		return nil, fmt.Errorf("frozen interface query version or shape is unavailable")
	}
	for _, ref := range named.Dependencies() {
		implementation, exists := lookup[ref]
		info, err := publication.QueryObjectDescriptor(implementation.Body)
		if !exists || err != nil || info.Type != ref.Name || info.App != ref.App || !slices.Contains(info.Implements, named.Interface) || named.InterfaceShape.Implements(info) != nil {
			return nil, fmt.Errorf("frozen interface query implementation %s is unavailable", ref.Name)
		}
	}
	return &platform.Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &named}, nil
}
