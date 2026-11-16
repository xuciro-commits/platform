package pages

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

const maxPageEffects = 8

func checkEvents(p platform.Page) error {
	d := p.InputSchema
	events := []platform.PageEventBinding{}
	overlays := map[string]platform.PageScope{}
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
	if (len(overlays) > 0 || len(events) > 0) && !pageUIProfileSupports(profileVersion(d), "platform.page.v2.4") {
		return fmt.Errorf("page overlays and events require UI profile v2.4")
	}
	open := map[string]bool{}
	for id, overlay := range overlays {
		variable := d.Variables[overlay.Bindings["openVariable"]]
		var initial bool
		if !platform.PageNodeID.MatchString(id) || !platform.PageNodeID.MatchString(overlay.Bindings["root"]) || variable.Mode != "state" || variable.Type != "boolean" || json.Unmarshal(variable.Initial, &initial) != nil || initial || open[overlay.Bindings["openVariable"]] {
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
		if (len(event.Effects) > 1 || slices.ContainsFunc(event.Effects, func(e platform.PageEffect) bool {
			return e.Kind == "action"
		})) && !pageUIProfileSupports(profileVersion(d), "platform.page.v2.106") {
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
				if event.Event == "select" && (!(variable.Mode == "state" && slices.Contains([]string{"page", "overlay"}, variable.Scope) || pageUIProfileSupports(profileVersion(d), "platform.page.v2.89") && variable.Mode == "shared" && variable.Scope == "application" && variable.Writable && rootPresentationSource(p, event.Source)) || !slices.Contains([]string{"string", "boolean"}, variable.Type) || open[effect.Target]) {
					return fmt.Errorf("selection event needs writable presentation scalar state without navigation or overlay control")
				}
				if effect.From != "" {
					source := d.Variables[effect.From]
					if !pageUIProfileSupports(profileVersion(d), "platform.page.v2.108") || len(effect.Value) != 0 || effect.From == effect.Target || source.Type == "" || source.Type != variable.Type || !variableIsWritable(variable) {
						return fmt.Errorf("page event %s needs a readable source variable of the target's type", event.Source)
					}
				} else if !variableIsWritable(variable) || literalType(effect.Value) != variable.Type {
					return fmt.Errorf("page event %s needs a matching state value", event.Source)
				}

			case "action":
				a := effect.Action
				if event.Event == "select" || effect.Target != "" || len(effect.Value) != 0 || effect.Navigate != nil || a == nil || a.Ref.Check() != nil || a.Ref.Kind != platform.AssetAction {
					return fmt.Errorf("page action effect %s needs one original action on a click", event.Source)
				}
				if v := d.Variables[a.RecordVariable]; a.RecordVariable != "" && v.Type != "record" {
					return fmt.Errorf("page action effect %s needs a record variable", event.Source)
				}
				if len(a.Inputs) > 0 && !pageUIProfileSupports(profileVersion(d), "platform.page.v2.108") || len(a.Inputs) > platform.PageWidgets.Runtime.Interface.MaxPorts {
					return fmt.Errorf("page action effect %s inputs need UI profile v2.108 and a bounded count", event.Source)
				}
				for parameter, arg := range a.Inputs {
					if !platform.PageNodeID.MatchString(parameter) || (arg.Variable != "") == (len(arg.Literal) != 0) || arg.Variable != "" && d.Variables[arg.Variable].Type == "" || arg.Variable == "" && literalType(arg.Literal) == "" {
						return fmt.Errorf("page action effect %s input %s is invalid", event.Source, parameter)
					}
				}
			case "navigate", "return":
				if event.Event == "select" || !last {
					return fmt.Errorf("page event %s navigation must end a click chain", event.Source)
				}
				if err := checkNavigationEffect(d, effect); err != nil {
					return err
				}
			default:
				return fmt.Errorf("page effect %s has an unknown kind", event.Source)
			}
		}
	}

	return nil
}

func checkPageInterface(d platform.PageInputSchema) error {
	if d.Interface == nil {
		for id, v := range d.Variables {
			if v.Mode == "input" {
				return fmt.Errorf("page input %s needs an interface", id)
			}
		}
		return nil
	}
	i, c := d.Interface, platform.PageWidgets.Runtime.Interface
	if !pageUIProfileSupports(profileVersion(d), "platform.page.v2.6") || i.Version < 1 || i.Version > c.MaxVersion || len(i.Inputs) > c.MaxPorts || len(i.Outputs) > c.MaxPorts {
		return fmt.Errorf("page interface needs v2.6, a version and bounded ports")
	}
	inputs := map[string]bool{}
	for _, ports := range []map[string]platform.PagePort{i.Inputs, i.Outputs} {
		for id, p := range ports {
			v, ok := d.Variables[p.Variable]
			if !platform.PageNodeID.MatchString(id) || !ok || v.Scope != "page" || v.Type != p.Type || !slices.Contains(c.ValueTypes, p.Type) || (p.Type == "record" || p.Type == "object-set") && (p.Object == nil || p.Object.Check() != nil || p.Object.Kind != platform.AssetObject) || p.Type != "record" && p.Type != "object-set" && p.Object != nil {
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
		if p.Type == "object-set" && !pageUIProfileSupports(profileVersion(d), "platform.page.v2.85") {
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

func checkNavigationEffect(d platform.PageInputSchema, effect platform.PageEffect) error {
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
	c := platform.PageWidgets.Runtime.Interface
	if n == nil || n.Page.Check() != nil || n.Page.Kind != platform.AssetPage || n.InterfaceVersion < 0 || n.InterfaceVersion > c.MaxVersion || len(n.Inputs) > c.MaxPorts || len(n.Results) > c.MaxPorts {
		return fmt.Errorf("page navigation needs a page and bounded interface bindings")
	}
	for id, arg := range n.Inputs {
		if !platform.PageNodeID.MatchString(id) || (arg.Variable != "") == (len(arg.Literal) != 0) || arg.Variable != "" && d.Variables[arg.Variable].Type == "" || arg.Variable == "" && literalType(arg.Literal) == "" {
			return fmt.Errorf("page navigation input %s is invalid", id)
		}
	}
	return nil
}

func rootPresentationSource(p platform.Page, source string) bool {
	owner := p.ScopeVisibility.Owners[source]
	if owner == "" {
		owner = source
	}
	return scopeOwner(p, owner, "overlay") == "" && scopeOwner(p, owner, "loop") == ""
}
