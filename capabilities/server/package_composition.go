package platformserver

import transport "platformserver/kernel/transport"

import (
	"net/http"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"strings"
)

func (t *Tenant) baseModules() []platform.CapabilityModule {
	modules := []platform.CapabilityModule{t.services.RuleModule, t.services.AuthModule, t.services.KernelModule, t.services.StorageModule}
	for _, app := range t.apps {
		m := app.Manifest()
		module := platform.CapabilityModule{ID: "platform.owner." + m.ID, Title: m.Title, Version: m.Version, Scope: "tenant", Replacement: "restart", Provides: []platform.CapabilityContract{{ID: "platform.owner." + m.ID, Version: "1", Semantics: "1"}}}
		if owner, ok := app.(platform.SnapshotFormatOwner); ok {
			module.StateFormat = owner.SnapshotFormat()
		}
		for _, provider := range modules[:4] {
			module.Requires = append(module.Requires, provider.Provides...)
		}
		needs := map[string]bool{}
		for _, need := range m.Consumes {
			if binding, found := t.bindings[need.Protocol]; found && !need.Optional {
				needs[binding.provider.Manifest().ID] = true
			}
		}
		for _, entity := range m.Entities {
			if declared := t.records.Type(entity.Type); declared != nil {
				for _, field := range declared.Info().Fields {
					if referenced := t.records.Type(field.Ref); referenced != nil {
						needs[referenced.Info().App] = true
					}
				}
			}
		}
		if m.ID == controlowner.PlatformApp {
			for _, app := range t.apps {
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

func (h *Host) routesComposition(rt *routes) {
	rt.authorized(transport.Route{Pattern: "GET /v1/composition", Summary: "Selected runtime contracts, implementations and immutable dependency order", Answer: platform.CompositionPlan{}}, controlowner.PlatformApp, []string{controlowner.Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		modules := slices.DeleteFunc(t.baseModules(), func(m platform.CapabilityModule) bool {
			id, owner := strings.CutPrefix(m.ID, "platform.owner.")
			return owner && !t.ownerActive(id)
		})
		plan, err := platform.ResolveComposition(modules, nil)
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, plan)
	})
}
