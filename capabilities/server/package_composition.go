package platformserver

import (
	"fmt"
	"net/http"
	"platformserver/internal/host"
	"slices"
	"strings"

	"platformserver/platform"
)

func packageContract(id string) platform.CapabilityContract {
	return platform.CapabilityContract{ID: "package." + id, Version: "1", Semantics: "1"}
}

func packageModule(p PackageDescriptor) platform.CapabilityModule {
	module := platform.CapabilityModule{ID: "package." + p.ID, Version: p.Version, Title: p.Title, Scope: "tenant", Replacement: "drain", Provides: []platform.CapabilityContract{packageContract(p.ID)}, Requires: slices.Clone(p.Capabilities)}
	for _, id := range p.Requires {
		module.Requires = append(module.Requires, packageContract(id))
	}
	return module
}

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
			if declared := t.records.types[entity.Type]; declared != nil {
				for _, field := range declared.info.Fields {
					if referenced := t.records.types[field.Ref]; referenced != nil {
						needs[referenced.info.App] = true
					}
				}
			}
		}
		if m.ID == PlatformApp {
			for _, app := range t.apps {
				if _, ok := app.(host.Directory); ok {
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

func (d *Console) packageComposition(candidate PackageDescriptor, index *PackageIndex) (platform.CompositionPlan, error) {
	modules := d.host.BaseModules()
	installed := d.packagesOf()
	for id, held := range installed {
		if id == candidate.ID || held.State != "active" {
			continue
		}
		var descriptor PackageDescriptor
		var err error
		if held.Artifact != "" {
			descriptor, err = d.sealedPackageDescriptor(id, held.Version, held.Artifact)
		} else {
			var found bool
			descriptor, found = index.find(id, held.Version)
			if !found {
				err = fmt.Errorf("installed package %s has no retained declaration", id)
			}
		}
		if err != nil {
			return platform.CompositionPlan{}, err
		}
		modules = append(modules, packageModule(descriptor))
	}
	modules = append(modules, packageModule(candidate))
	return platform.ResolveComposition(modules, nil)
}

func (h *Host) routesComposition(rt *routes) {
	rt.authorized(Route{Pattern: "GET /v1/composition", Summary: "Selected runtime contracts, implementations and immutable dependency order", Answer: platform.CompositionPlan{}}, PlatformApp, []string{Admin}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
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
