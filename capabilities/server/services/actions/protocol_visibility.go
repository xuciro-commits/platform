package actions

import (
	"maps"
	"slices"

	"platformserver/platform"
)

func ProjectProtocol(protocol platform.ProtocolReleaseDescriptor, actions map[string]platform.Action, readable func(name string) bool) (*platform.ProtocolReleaseDescriptor, bool) {
	projected := protocol
	projected.ActionBindings = maps.Clone(projected.ActionBindings)
	projected.ActionRoles = maps.Clone(projected.ActionRoles)
	projected.Actions = slices.DeleteFunc(slices.Clone(projected.Actions), func(action platform.Action) bool {
		binding := projected.ActionBindings[action.Schema]
		_, ok := actions[binding.Name]
		if !ok {
			delete(projected.ActionBindings, action.Schema)
			delete(projected.ActionRoles, action.Schema)
		}
		return !ok
	})
	projected.Reads = slices.DeleteFunc(slices.Clone(projected.Reads), func(name string) bool { return !readable(name) })
	return &projected, len(projected.Actions) > 0 || len(projected.Reads) > 0
}
