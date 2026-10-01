package platform

import (
	"fmt"
	"slices"
)

func (v PageVariable) IsWritable() bool { return v.Mode == "state" || v.Mode == "shared" && v.Writable }

func (a Application) CheckVariables() error {
	if a.UIProfile != "" && !SupportsPageUIProfile(a.UIProfile) {
		return fmt.Errorf("unsupported application UI profile %s", a.UIProfile)
	}
	if len(a.Variables) == 0 {
		return nil
	}
	if !PageUIProfileSupports(a.UIProfile, "platform.page.v2.8") {
		return fmt.Errorf("application variables require UI profile v2.8")
	}
	for id, v := range a.Variables {
		if v.Scope != "application" || !slices.Contains(pageWidgets.Runtime.Application.Modes, v.Mode) {
			return fmt.Errorf("application variable %s needs its own pure scalar declaration", id)
		}
	}
	return (&PageDocument{Variables: a.Variables}).CheckVariables()
}

// Shared bindings are typed requirements on the application which holds the
// page. They introduce no reverse membership edge or permission grant.
func (a Application) CheckPageVariables(p Page) error {
	if p.Document == nil {
		return nil
	}
	for id, v := range p.Document.Variables {
		if v.Scope != "application" {
			continue
		}
		if v.Source == nil {
			return fmt.Errorf("page %s shared binding %s has no source", p.Name, id)
		}
		source, ok := a.Variables[v.Source.Variable]
		if !ok || source.Type != v.Type || v.Writable && source.Mode != "state" {
			return fmt.Errorf("application %s does not satisfy page %s shared binding %s", a.Name, p.Name, id)
		}
	}
	return nil
}
