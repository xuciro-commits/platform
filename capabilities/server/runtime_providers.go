package platformserver

import (
	"fmt"
	"maps"
	"slices"

	"platformserver/platform"
	"platformserver/platform/authz"
)

type RuleProvider struct {
	Module platform.CapabilityModule
	Engine platform.RuleEngine
}
type AuthorizationProvider struct {
	Module platform.CapabilityModule
	Engine authz.Engine
}
type KernelProvider struct {
	Module platform.CapabilityModule
	Engine platform.KernelFactory
}

// RuntimeProviders is the deployment's trusted, typed offer. Descriptors are
// projections of these factories; importing JSON never adds executable providers.
type RuntimeProviders struct {
	Rules         []RuleProvider
	Authorization []AuthorizationProvider
	Kernel        []KernelProvider
}

func DefaultRuntimeProviders() RuntimeProviders {
	c := DefaultRuntimeCapabilities()
	return RuntimeProviders{Rules: []RuleProvider{{c.RuleModule, c.Rules}}, Authorization: []AuthorizationProvider{{c.AuthModule, c.Authorization}}, Kernel: []KernelProvider{{c.KernelModule, c.Kernel}}}
}

func (c RuntimeCapabilities) Selection() map[string]string {
	return map[string]string{"platform.rules": c.RuleModule.ID, "platform.authorization": c.AuthModule.ID, "platform.kernel": c.KernelModule.ID}
}

func (p RuntimeProviders) Modules() []platform.CapabilityModule {
	var modules []platform.CapabilityModule
	for _, provider := range p.Rules {
		modules = append(modules, provider.Module)
	}
	for _, provider := range p.Authorization {
		modules = append(modules, provider.Module)
	}
	for _, provider := range p.Kernel {
		modules = append(modules, provider.Module)
	}
	return modules
}

func (p RuntimeProviders) Resolve(selection map[string]string) (RuntimeCapabilities, error) {
	plan, err := platform.ResolveComposition(p.Modules(), selection)
	if err != nil {
		return RuntimeCapabilities{}, err
	}
	runtime := RuntimeCapabilities{StorageModule: MemoryStorageModule()}
	for _, provider := range p.Rules {
		if slices.Contains(plan.Order, provider.Module.ID) {
			runtime.RuleModule, runtime.Rules = provider.Module, provider.Engine
		}
	}
	for _, provider := range p.Authorization {
		if slices.Contains(plan.Order, provider.Module.ID) {
			runtime.AuthModule, runtime.Authorization = provider.Module, provider.Engine
		}
	}
	for _, provider := range p.Kernel {
		if slices.Contains(plan.Order, provider.Module.ID) {
			runtime.KernelModule, runtime.Kernel = provider.Module, provider.Engine
		}
	}
	return runtime, runtime.Check()
}

func (p RuntimeProviders) CheckSelection(selection map[string]string) error {
	if len(selection) != 3 {
		return fmt.Errorf("Select a provider or maintenance for each foundational capability")
	}
	complete := maps.Clone(selection)
	for _, module := range p.Modules() {
		for _, contract := range module.Provides {
			if provider, found := complete[contract.ID]; found && provider == "" {
				complete[contract.ID] = module.ID
			}
		}
	}
	_, err := p.Resolve(complete)
	return err
}

func missingRuntime(selection map[string]string) bool {
	for _, provider := range selection {
		if provider == "" {
			return true
		}
	}
	return false
}
