package build

import (
	"slices"
	"strings"

	"platformserver/platform"
)

func PruneInstalledObject(definitions []platform.Definition, app string, info platform.EntityInfo, actions []platform.Action) []platform.Definition {
	kept := slices.DeleteFunc(definitions, func(def platform.Definition) bool {
		return def.Ref.App == app && def.Source == "tenant" && def.Action != nil && def.Action.Target == info.Type && !slices.ContainsFunc(actions, func(action platform.Action) bool { return action.Schema == def.Ref.Name })
	})
	return slices.DeleteFunc(kept, func(def platform.Definition) bool {
		return def.Ref.App == app && def.Protocol != nil && slices.ContainsFunc(def.Requires, func(ref platform.AssetRef) bool {
			return ref.Kind == platform.AssetAction && strings.HasPrefix(ref.Name, info.Type+".")
		})
	})
}

func ReplaceProtocolReads(owners map[string]string, app, typ string, protocols []platform.Provision) {
	for name, owner := range owners {
		if owner == app && strings.HasPrefix(name, "read:protocol:"+typ+":") {
			delete(owners, name)
		}
	}
	for _, protocol := range protocols {
		for _, name := range protocol.Reads {
			owners["read:"+name] = app
		}
	}
}

func CheckRunApplicationBindings(record any) error {
	var bindings []platform.AssetBinding
	switch run := record.(type) {
	case platform.FlowInstance:
		bindings = run.Applications
	case platform.AgentRunRecord:
		bindings = run.Applications
	case FunctionRun:
		bindings = run.Applications
	}
	return platform.CheckApplicationBindings(bindings)
}
