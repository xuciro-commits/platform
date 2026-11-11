package data

import "platformserver/platform"

func ProjectObservation(declaration *platform.ObservationDefinition, entities map[string]platform.EntityInfo, actions map[string]platform.Action) (*platform.ObservationDefinition, bool) {
	info, found := entities[declaration.Object.Name]
	if !found {
		return nil, false
	}
	if _, found := info.Field(declaration.DeviceField); !found {
		return nil, false
	}
	visible := *declaration
	if visible.Decision != nil {
		readable := true
		for _, name := range ObservationDecisionFields(*visible.Decision) {
			if _, found := info.Field(name); !found {
				readable = false
			}
		}
		if _, found := actions[visible.Decision.Action.Name]; !found || !readable {
			visible.Decision = nil
		}
	}
	return &visible, true
}
