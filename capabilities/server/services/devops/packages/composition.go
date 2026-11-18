package packages

import (
	"fmt"
	"slices"
	"strings"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

type Composition struct {
	Base        []runtimeprotocol.CapabilityModule
	Apps        []platform.Service
	Bindings    func() map[string]string
	Entity      func(string) (platform.RecordTypeInfo, bool)
	PlatformApp string
	Directory   string
}

func (c Composition) Modules() []runtimeprotocol.CapabilityModule {
	modules := slices.Clone(c.Base)
	for _, app := range c.Apps {
		m := app.Manifest()
		module := runtimeprotocol.CapabilityModule{ID: "platform.owner." + m.ID, Title: m.Title, Version: m.Version, Scope: "tenant", Replacement: "restart", Provides: []runtimeprotocol.CapabilityContract{{ID: "platform.owner." + m.ID, Version: "1", Semantics: "1"}}}
		module.StateFormat = app.SnapshotFormat()
		for _, provider := range c.Base {
			module.Requires = append(module.Requires, provider.Provides...)
		}
		needs := map[string]bool{}
		for _, need := range m.Consumes {
			if provider, found := c.Bindings()[need.Protocol]; found && !need.Optional {
				needs[provider] = true
			}
		}
		for _, entity := range m.Entities {
			if declared, ok := c.Entity(entity.Type); ok {
				for _, field := range declared.Fields {
					if referenced, ok := c.Entity(field.Ref); ok {
						needs[referenced.App] = true
					}
				}
			}
		}
		if m.ID == c.PlatformApp && c.Directory != "" && slices.ContainsFunc(c.Apps, func(app platform.Service) bool { return app.Manifest().ID == c.Directory }) {
			needs[c.Directory] = true
		}
		for id := range needs {
			if id != m.ID {
				module.Requires = append(module.Requires, runtimeprotocol.CapabilityContract{ID: "platform.owner." + id, Version: "1", Semantics: "1"})
			}
		}
		slices.SortFunc(module.Requires, func(a, b runtimeprotocol.CapabilityContract) int { return strings.Compare(a.ID, b.ID) })
		modules = append(modules, module)
	}
	slices.SortFunc(modules, func(a, b runtimeprotocol.CapabilityModule) int { return strings.Compare(a.ID, b.ID) })
	return modules
}

func ActiveComposition(modules []runtimeprotocol.CapabilityModule, active func(string) bool) (runtimeprotocol.CompositionPlan, error) {
	modules = slices.DeleteFunc(modules, func(module runtimeprotocol.CapabilityModule) bool {
		id, owner := strings.CutPrefix(module.ID, "platform.owner.")
		return owner && !active(id)
	})
	return runtimeprotocol.ResolveComposition(modules, nil)
}

func CheckOwnerRemoval(disabled []string, known func(string) bool, modules []runtimeprotocol.CapabilityModule, open func(removed map[string]bool) error) error {
	removed := map[string]bool{}
	for _, id := range disabled {
		if removed[id] || !known(id) {
			return fmt.Errorf("Unknown or repeated platform module %s", id)
		}
		removed[id] = true
	}
	var kept []runtimeprotocol.CapabilityModule
	for _, module := range modules {
		if id, owner := strings.CutPrefix(module.ID, "platform.owner."); owner && removed[id] {
			continue
		}
		kept = append(kept, module)
	}
	if _, err := runtimeprotocol.ResolveComposition(kept, nil); err != nil {
		return err
	}
	return open(removed)
}
