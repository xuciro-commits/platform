package pages

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
	"platformserver/services/objects"
)

func (b Bindings) CheckInstall(p platform.Page, id string, targets bool, engines ...platform.RuleEngine) (platform.Page, platform.EntityInfo, error) {
	canonical := p
	safe, err := p.Safety()
	if err != nil {
		return p, platform.EntityInfo{}, err
	}
	p = safe
	if err := p.CheckSafety(); err != nil {
		return p, platform.EntityInfo{}, err
	}
	for _, trigger := range p.ActionTriggers {
		refs := slices.Clone(trigger.Actions)
		if trigger.InlineEdit != nil {
			refs = append(refs, trigger.InlineEdit.Action)
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					refs = append(refs, effect.Action.Ref)
				}
			}
		}
		for _, ref := range refs {
			if _, action, ok := b.Action(ref.Name); ok && platform.StaticControlAction(action) {
				return p, platform.EntityInfo{}, fmt.Errorf("page %s cannot bind static control action %s", p.Name, ref.Name)
			}
		}
	}
	if err := p.CheckModelBindings(b.Entity, b.Criteria, engines...); err != nil {
		return p, platform.EntityInfo{}, err
	}
	if err := CheckModelQueries(p, b.Definitions, b.Entity); err != nil {
		return p, platform.EntityInfo{}, err
	}
	if err := b.CheckQueries(p); err != nil {
		return p, platform.EntityInfo{}, err
	}
	if iface := p.InputSchema.Interface; iface != nil {
		for _, ports := range []map[string]platform.PagePort{iface.Inputs, iface.Outputs} {
			for id, port := range ports {
				if port.Object != nil {
					info, ok := b.Entity(port.Object.Name)
					if !ok || info.App != port.Object.App {
						return p, platform.EntityInfo{}, fmt.Errorf("page port %s has an unknown object", id)
					}
				}
			}
		}
	}
	info, known := b.Entity(p.Object.Name)
	if p.Name == "" || !known {
		return p, platform.EntityInfo{}, fmt.Errorf("page %s object %s is unavailable", p.Name, p.Object.Name)
	}
	if err := b.CheckSections(p, info); err != nil {
		return p, platform.EntityInfo{}, err
	}
	if targets {
		for _, d := range b.Definitions {
			if d.Application != nil && d.Ref.App == id && slices.Contains(d.Application.Pages, p.Name) {
				if err := d.Application.CheckPageVariables(p); err != nil {
					return p, platform.EntityInfo{}, err
				}
			}
		}
		if err := CheckNavigation(p, id, b.Definitions); err != nil {
			return p, platform.EntityInfo{}, err
		}
	}
	return canonical, info, nil
}

func (b Bindings) CheckSections(p platform.Page, page platform.EntityInfo) error {
	safe, err := p.Safety()
	if err != nil {
		return err
	}
	if err := CheckSelections(safe, b.Entity, func(name string) (platform.Action, bool) {
		_, action, ok := b.Action(name)
		return action, ok
	}); err != nil {
		return err
	}
	if err := safe.CheckSafety(); err != nil {
		return err
	}
	if err := safe.CheckRecordPorts(); err != nil {
		return err
	}
	for _, binding := range safe.DataBindings {
		if err := safe.CheckDataBinding(binding, b.Entity, b.Criteria, b.Rules); err != nil {
			return fmt.Errorf("page %s binding %s: %w", p.Name, binding.ID, err)
		}
		if binding.Query != nil {
			query := objects.QuerySourceDefinition(b.Definitions, binding.Query)
			if query == nil || query.Query == nil || query.Query.Object != binding.Object.Name {
				return fmt.Errorf("page binding query owner or version is unavailable")
			}
		}
		for _, resource := range binding.Resources {
			if resource.Asset == nil {
				continue
			}
			ref := resource.Asset.Ref
			if resource.Kind == "service" {
				switch ref.Kind {
				case platform.AssetObject:
					info, ok := b.Entity(ref.Name)
					if !ok || info.App != ref.App {
						return fmt.Errorf("page service owner is unavailable")
					}
				case platform.AssetAction:
					if owner, _, ok := b.Action(ref.Name); !ok || owner != ref.App {
						return fmt.Errorf("page service action owner is unavailable")
					}
				}
			} else if !ExplorationAssetAvailable(b.Definitions, *resource.Asset) {
				return fmt.Errorf("page resource version is unavailable")
			}
		}
	}
	for _, trigger := range safe.ActionTriggers {
		object := p.Object.Name
		for _, binding := range safe.DataBindings {
			if binding.ID == trigger.Binding {
				if binding.Object.Name != "" {
					object = binding.Object.Name
				}
				if binding.Model != nil {
					object = binding.Model.Type
				}
			}
		}
		for _, action := range trigger.Actions {
			if err := b.checkAction(p.Name, action.Name, object); err != nil {
				return err
			}
		}
		if trigger.InlineEdit != nil {
			if err := b.checkAction(p.Name, trigger.InlineEdit.Action.Name, object); err != nil {
				return err
			}
			_, action, _ := b.Action(trigger.InlineEdit.Action.Name)
			info, _ := b.Entity(object)
			if err := trigger.InlineEdit.CheckTableEdit(info, action); err != nil {
				return err
			}
		}
		if len(trigger.Defaults) > 0 {
			if len(trigger.Actions) != 1 {
				return fmt.Errorf("action parameters require one exact action")
			}
			_, action, found := b.Action(trigger.Actions[0].Name)
			if !found {
				return fmt.Errorf("action parameter owner is unavailable")
			}
			info, _ := b.Entity(object)
			for _, mapping := range trigger.Defaults {
				field, found := info.Field(mapping.Field)
				parameter := slices.IndexFunc(action.Payload, func(value platform.Field) bool { return value.Name == mapping.Parameter })
				if !found || parameter < 0 || !platform.ActionParameterCompatible(field, action.Payload[parameter]) {
					return fmt.Errorf("action parameter field type does not match its original signature")
				}
			}
		}
		if trigger.Operation != nil {
			info, ok := b.Entity(object)
			if !ok {
				return fmt.Errorf("page operation input object is unavailable")
			}
			if err := b.CheckOperation(trigger, info); err != nil {
				return err
			}
		}
		if trigger.Function != nil {
			asset, err := b.FunctionAsset(trigger.Function.Ref, trigger.Function.SourceVersion)
			if err != nil {
				return err
			}
			var function platform.AIFunction
			if json.Unmarshal(asset.Body, &function) != nil || function.Object != object {
				return fmt.Errorf("page function input type is unavailable")
			}
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					if err := b.checkAction(p.Name, effect.Action.Ref.Name, object); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func (b Bindings) checkAction(page, schema, object string) error {
	_, action, ok := b.Action(schema)
	if !ok || action.Target != object {
		return fmt.Errorf("page %s: no action %s about %s", page, schema, object)
	}
	return nil
}
