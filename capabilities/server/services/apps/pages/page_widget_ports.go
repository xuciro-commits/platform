package pages

import (
	"fmt"
	"platformserver/platform"
	"slices"
)

func checkWidgetPorts(p platform.Page, binding platform.PageDataBinding, owner string) error {
	widget := binding.Ports["widget"]
	contract := platform.PageWidget(widget)
	if contract == nil {
		return nil
	}
	ports := append(slices.Clone(contract.InputPorts), contract.OutputPorts...)
	loop, overlay := p.ScopeOwner(owner, "loop"), p.ScopeOwner(owner, "overlay")
	for field, id := range binding.Ports {
		index := -1
		for i := range ports {
			if ports[i].BindingField == field {
				index = i
				break
			}
		}
		if index < 0 {
			if pageVariablePortFields[field] {
				return fmt.Errorf("widget %s has an invalid %s port", widget, field)
			}
			continue
		}
		port := ports[index]
		if port.ScopeRule != "" && field != "enabledWhen" {
			v, ok := p.InputSchema.Variables[id]
			reachable := ok && (v.Scope == "page" || v.Scope == "application" || v.Scope == "loop-item" && loop != "" && v.Owner == loop || v.Scope == "overlay" && overlay != "" && v.Owner == overlay)
			if !reachable || port.ScopeRule == "local" && loop != "" || port.ScopeRule == "exact" && (loop != "" || v.Scope == "page" && overlay != "" || v.Scope == "overlay" && v.Owner != overlay) {
				return fmt.Errorf("widget %s %s escapes its presentation owner", widget, field)
			}
		}
		v, ok := p.InputSchema.Variables[id]
		if ok && v.Scope == "application" && v.Type == "record" && !pageUIProfileSupports(profileVersion(p.InputSchema), sharedRecordProfile(widget)) {
			return fmt.Errorf("widget %s shares an application record only from its profile", widget)
		}
		readonlyChoice := field == "choiceVariable" && v.Mode == "constant"
		if !ok || v.Type != port.Type || port.Writable && field != "selectionSetVariable" && !readonlyChoice && !(v.Writable || port.StateWritable && v.Mode == "state") {
			return fmt.Errorf("widget %s has an invalid %s port", widget, field)
		}
	}
	object, known := binding.ObjectRef(p).Name, binding.ObjectRef(p).Name != ""
	if id := binding.Ports["selectionSetVariable"]; id != "" && known {
		if v := p.InputSchema.Variables[id]; v.Mode == "shared" && (v.Source == nil || v.Source.Object == nil || v.Source.Object.Name != object) {
			return fmt.Errorf("shared record-set object does not match its table")
		}
	}
	if id := binding.Ports["collectionVariable"]; id != "" && known && p.WindowVariableObject(id) != object {
		return fmt.Errorf("table window object does not match its plan")
	}
	local := p.ScopeOwner(owner, "loop") != "" || p.ScopeOwner(owner, "overlay") != ""
	if id := binding.Ports["filterVariable"]; id != "" {
		v := p.InputSchema.Variables[id]
		if p.ScopeOwner(owner, "loop") != "" || binding.Ports["collectionVariable"] != "" || v.Type != "filter" || v.Mode != "shared" || widget == "filter" && (!v.Writable || p.ScopeOwner(owner, "overlay") != "") {
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

func sharedRecordProfile(widget string) string {
	switch widget {
	case "scene-3d", "observation":
		return platform.PageWidgets.Runtime.Telemetry.SharedUIProfile
	case "vertex-graph":
		return platform.PageWidgets.Runtime.Exploration.SharedVertexUIProfile
	case "graph-explorer", "breadcrumb", "avatar-stack":
		return platform.PageWidgets.Runtime.ContextViews.SharedUIProfile
	}
	return "platform.page.v2.14"
}

var pageVariablePortFields = func() map[string]bool {
	fields := map[string]bool{}
	for _, widget := range platform.PageWidgets.Widgets {
		for _, port := range append(slices.Clone(widget.InputPorts), widget.OutputPorts...) {
			if port.BindingField != "enabledWhen" {
				fields[port.BindingField] = true
			}
		}
	}
	return fields
}()

func pageUIProfileSupports(profile, required string) bool {
	if profile == "platform.page.v3" {
		return true
	}
	current, minimum := slices.Index(platform.PageWidgets.SupportedProfiles, profile), slices.Index(platform.PageWidgets.SupportedProfiles, required)
	return current >= 0 && minimum >= 0 && current >= minimum
}

func profileVersion(d platform.PageInputSchema) string {
	if profile := d.RawProfile(); profile != "" {
		return profile
	}
	return "platform.page.v3"
}
