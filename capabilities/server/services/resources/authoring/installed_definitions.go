package authoring

import (
	"fmt"
	"platformserver/kernel/acceptedformat"
	ruleprotocol "platformserver/kernel/protocol"
	"reflect"
	"slices"
	"strings"

	"platformserver/kernel/records"
	"platformserver/platform"
)

func PruneInstalledObject(definitions []platform.Definition, app string, info platform.RecordTypeInfo, actions []platform.Action) []platform.Definition {
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
	case ruleprotocol.FlowInstance:
		bindings = run.Applications
	case ruleprotocol.AgentRunRecord:
		bindings = run.Applications
	case ruleprotocol.FunctionRun:
		bindings = run.Applications
	}
	return acceptedformat.CheckApplicationBindings(bindings)
}

type ObjectInstall struct {
	Owners         map[string]string
	Owner          func(id string) platform.Service
	Installing     func() bool
	Properties     func(platform.RecordTypeInfo) error
	RecordSets     func(platform.RecordTypeInfo) error
	Entity         func(string) (platform.RecordTypeInfo, bool)
	Install        func(platform.RecordTypeInfo) error
	Definitions    func() []platform.Definition
	SetDefinitions func([]platform.Definition)
	Define         func(app string, info platform.RecordTypeInfo, actions []platform.Action, pages []platform.Page, protocols ...platform.Provision) error
}

func (h ObjectInstall) Run(app platform.Service, e platform.RecordType, actions []platform.Action, pages []platform.Page) error {
	id := app.Manifest().ID
	if other := h.Owner(h.Owners["action:"+actions[0].Schema]); other != nil && (other.Manifest().ID != id || other.Authority() != app.Authority()) {
		return fmt.Errorf("%s: another app owns %s", e.Type, actions[0].Schema)
	}
	info, err := records.Describe(id, e, func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	if !h.Installing() {
		if err := h.Properties(info); err != nil {
			return err
		}
		if err := h.RecordSets(info); err != nil {
			return err
		}
		if err := ruleprotocol.CheckConstraints(info, h.Entity, true); err != nil {
			return err
		}
	}
	if err := h.Install(info); err != nil {
		return err
	}
	for _, action := range actions {
		if action.Target != e.Type {
			return fmt.Errorf("%s: action %s targets %s", e.Type, action.Schema, action.Target)
		}
		h.Owners["action:"+action.Schema] = id
	}
	ReplaceProtocolReads(h.Owners, id, e.Type, e.Protocols)
	definitions := h.Definitions()
	if kept := PruneInstalledObject(definitions, id, info, actions); len(kept) != len(definitions) {
		h.SetDefinitions(kept)
	}
	return h.Define(id, info, actions, pages, e.Protocols...)
}
