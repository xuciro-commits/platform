package packages

import (
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

type Composition struct {
	Base        []platform.CapabilityModule
	Apps        []platform.App
	Bindings    func() map[string]string
	Entity      func(string) (platform.EntityInfo, bool)
	PlatformApp string
}

func (c Composition) Modules() []platform.CapabilityModule {
	modules := slices.Clone(c.Base)
	for _, app := range c.Apps {
		m := app.Manifest()
		module := platform.CapabilityModule{ID: "platform.owner." + m.ID, Title: m.Title, Version: m.Version, Scope: "tenant", Replacement: "restart", Provides: []platform.CapabilityContract{{ID: "platform.owner." + m.ID, Version: "1", Semantics: "1"}}}
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
		if m.ID == c.PlatformApp {
			for _, app := range c.Apps {
				if _, ok := app.(platform.Directory); ok {
					needs[app.Manifest().ID] = true
				}
			}
		}
		for id := range needs {
			if id != m.ID {
				module.Requires = append(module.Requires, platform.CapabilityContract{ID: "platform.owner." + id, Version: "1", Semantics: "1"})
			}
		}
		slices.SortFunc(module.Requires, func(a, b platform.CapabilityContract) int { return strings.Compare(a.ID, b.ID) })
		modules = append(modules, module)
	}
	slices.SortFunc(modules, func(a, b platform.CapabilityModule) int { return strings.Compare(a.ID, b.ID) })
	return modules
}

func CheckOwnerRemoval(disabled []string, known func(string) bool, modules []platform.CapabilityModule, open func(removed map[string]bool) error) error {
	removed := map[string]bool{}
	for _, id := range disabled {
		if removed[id] || !known(id) {
			return fmt.Errorf("Unknown or repeated platform module %s", id)
		}
		removed[id] = true
	}
	var kept []platform.CapabilityModule
	for _, module := range modules {
		if id, owner := strings.CutPrefix(module.ID, "platform.owner."); owner && removed[id] {
			continue
		}
		kept = append(kept, module)
	}
	if _, err := platform.ResolveComposition(kept, nil); err != nil {
		return err
	}
	return open(removed)
}
