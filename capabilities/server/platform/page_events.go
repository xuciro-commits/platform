package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

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
	d := p.InputSchema
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
	sourceSchema, targetSchema := source.InputSchema, target.InputSchema
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

func (d PageInputSchema) sharedContextRecord(id string) bool {
	v := d.Variables[id]
	return v.Type == "record" && v.Scope == "application" && v.Mode == "shared" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil && v.Source.Object.Kind == AssetObject && v.Source.Object.Check() == nil
}
