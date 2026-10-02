package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

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

func (d *PageDocument) checkInterface() error {
	if d.Interface == nil {
		for id, v := range d.Variables {
			if v.Mode == "input" {
				return fmt.Errorf("page input %s needs an interface", id)
			}
		}
		return nil
	}
	i, c := d.Interface, pageWidgets.Runtime.Interface
	if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.6") || i.Version < 1 || i.Version > c.MaxVersion || len(i.Inputs) > c.MaxPorts || len(i.Outputs) > c.MaxPorts {
		return fmt.Errorf("page interface needs v2.6, a version and bounded ports")
	}
	inputs := map[string]bool{}
	for _, ports := range []map[string]PagePort{i.Inputs, i.Outputs} {
		for id, p := range ports {
			v, ok := d.Variables[p.Variable]
			if !pageNodeID.MatchString(id) || !ok || v.Scope != "page" || v.Type != p.Type || !slices.Contains(c.ValueTypes, p.Type) || p.Type == "record" && (p.Object == nil || p.Object.Check() != nil || p.Object.Kind != AssetObject) || p.Type != "record" && p.Object != nil {
				return fmt.Errorf("page port %s needs a typed page variable and record object", id)
			}
		}
	}
	for id, p := range i.Inputs {
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
	d := p.Document
	if d == nil {
		return ""
	}
	if d.Interface != nil {
		for _, port := range d.Interface.Inputs {
			if port.Variable == variable && port.Object != nil {
				return port.Object.Name
			}
		}
	}
	v := d.Variables[variable]
	if v.Mode == "shared" && v.Type == "record" && v.Source != nil && v.Source.Object != nil {
		return v.Source.Object.Name
	}
	section := ""
	if v.Source != nil {
		if v.Source.Kind == "record" {
			section = v.Source.Section
		} else if v.Source.Kind == "item" {
			loop := d.Nodes[v.Owner].Loop
			if loop != nil {
				if object := p.WindowVariableObject(loop.Collection); object != "" {
					return object
				}
			}
			section = d.LoopRecordSource(variable)
		}
	}
	for _, s := range p.Sections {
		if s.ID == section {
			if s.Object.Name != "" {
				return s.Object.Name
			}
			return p.Object.Name
		}
	}
	return ""
}

// CheckPageNavigation compares the source and target frozen interfaces. It
// reads no records and grants no access to either page or their business data.
func CheckPageNavigation(source Page, target Page, nav PageNavigation) error {
	version := 0
	inputs, outputs := map[string]PagePort{}, map[string]PagePort{}
	if target.Document != nil && target.Document.Interface != nil {
		version = target.Document.Interface.Version
		inputs = target.Document.Interface.Inputs
		outputs = target.Document.Interface.Outputs
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
			typ = source.Document.Variables[value.Variable].Type
		}
		if !ok || typ != port.Type || port.Type == "record" && (port.Object == nil || source.RecordVariableObject(value.Variable) != port.Object.Name) {
			return fmt.Errorf("page navigation input %s type or object differs", id)
		}
	}
	for id, variable := range nav.Results {
		port, ok := outputs[id]
		v := source.Document.Variables[variable]
		if !ok || !v.IsWritable() || v.Type != port.Type || v.Type == "record" {
			return fmt.Errorf("page navigation output %s type differs", id)
		}
	}
	return nil
}

func (p Page) NavigationTargets() []AssetRef {
	var refs []AssetRef
	if p.Document != nil {
		for _, event := range p.Document.Events {
			if event.Navigate != nil {
				refs = append(refs, event.Navigate.Page)
			}
		}
		if p.Document.Interface != nil {
			for _, ports := range []map[string]PagePort{p.Document.Interface.Inputs, p.Document.Interface.Outputs} {
				for _, port := range ports {
					if port.Object != nil {
						refs = append(refs, *port.Object)
					}
				}
			}
		}
	}
	return refs
}

func (p Page) CheckRecordPorts() error {
	if p.Document == nil || p.Document.Interface == nil {
		return nil
	}
	for id, port := range p.Document.Interface.Outputs {
		if port.Type == "record" && (port.Object == nil || p.RecordVariableObject(port.Variable) != port.Object.Name) {
			return fmt.Errorf("page output %s record object differs", id)
		}
	}
	return nil
}

func (d *PageDocument) checkNavigationEvent(event PageEventBinding) error {
	if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.6") || event.Target != "" || len(event.Value) != 0 || event.Return && event.Navigate != nil {
		return fmt.Errorf("page navigation needs one v2.6 handler")
	}
	if event.Return {
		if d.Interface == nil {
			return fmt.Errorf("page return needs an interface")
		}
		return nil
	}
	n := event.Navigate
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

// A cycle exception applies only to page edges declared by navigation.
func pageNavigationEdge(asset ReleaseAsset, target AssetRef) bool {
	if asset.Ref.Kind != AssetPage || target.Kind != AssetPage {
		return false
	}
	var p Page
	if json.Unmarshal(asset.Body, &p) != nil || p.Document == nil {
		return false
	}
	for _, e := range p.Document.Events {
		if e.Navigate != nil && e.Navigate.Page == target {
			return true
		}
	}
	return false
}
