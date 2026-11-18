package protocol

import (
	"fmt"
	"maps"
	"slices"

	"platformserver/capability"
	"platformserver/platform"
)

type CapabilityContract = capability.CapabilityContract
type CapabilityModule = capability.CapabilityModule
type CapabilityBinding = capability.CapabilityBinding
type CompositionPlan = capability.CompositionPlan

func ResolveComposition(modules []CapabilityModule, selected map[string]string) (CompositionPlan, error) {
	return capability.ResolveComposition(modules, selected)
}

type OwnerBindings struct {
	Training    AITrainingHost
	Agent       AgentHost
	Console     platform.ConsoleHost
	Build       BuildHost
	Transaction platform.TransactionHost
	Files       MediaFileHost
	Knowledge   platform.KnowledgeHost
	Relations   platform.RelationsHost
	Flow        FlowHost
	Work        WorkHost
}

type MediaFileHost interface {
	platform.FileHost
	platform.MediaFiles
}

type OwnerAttacher interface {
	AttachOwner(OwnerBindings)
}

func BindOwner(app platform.Service, bindings OwnerBindings) {
	switch owner := app.(type) {
	case OwnerAttacher:
		owner.AttachOwner(bindings)
	}
}

type RuntimeCapabilities struct {
	NewTransactionExecutor func(BusinessRuntimeContext) TransactionExecutor
	NewAgentEvaluator      func() AgentEvaluator
	BindOwner              func(platform.Service, OwnerBindings)
	AIInference            *AIInferencePolicy
	Rules                  platform.RuleEngine
	Authorization          platform.AuthorizationEngine
	RuleModule             CapabilityModule
	AuthModule             CapabilityModule
	KernelModule           CapabilityModule
	StorageModule          CapabilityModule
}

func DefaultRuntimeCapabilities(authorization platform.AuthorizationEngine) RuntimeCapabilities {
	return RuntimeCapabilities{
		Rules: platform.NativeRules{}, Authorization: authorization, StorageModule: MemoryStorageModule(),
		KernelModule: CapabilityModule{ID: "platform.kernel.native", Version: "1", Title: "Kernel", Scope: "tenant", Replacement: "restart", StateFormat: "platform.kernel.v1", Provides: []CapabilityContract{{ID: "platform.kernel", Version: "1", Semantics: "1"}}},
		RuleModule:   CapabilityModule{ID: "platform.rules.native", Version: "1", Title: "Business rules", Scope: "tenant", Replacement: "restart", Provides: []CapabilityContract{{ID: "platform.rules", Version: "1", Semantics: fmt.Sprint(platform.ExprVersion)}}},
		AuthModule:   CapabilityModule{ID: "platform.authorization.standard", Version: "1", Title: "Authorization", Scope: "tenant", Replacement: "restart", Provides: []CapabilityContract{{ID: "platform.authorization", Version: "1", Semantics: "1"}}},
	}
}

func (c RuntimeCapabilities) Check() error {
	if c.Rules == nil || c.Authorization == nil {
		return fmt.Errorf("runtime needs explicit rule and authorization providers")
	}
	plan, err := ResolveComposition([]CapabilityModule{c.RuleModule, c.AuthModule, c.KernelModule}, nil)
	if err != nil {
		return err
	}
	for _, expected := range []CapabilityContract{{ID: "platform.rules", Version: "1", Semantics: fmt.Sprint(platform.ExprVersion)}, {ID: "platform.authorization", Version: "1", Semantics: "1"}, {ID: "platform.kernel", Version: "1", Semantics: "1"}} {
		found := false
		for _, binding := range plan.Bindings {
			found = found || binding.CapabilityContract == expected
		}
		if !found {
			return fmt.Errorf("runtime provider does not implement %s", expected.ID)
		}
	}
	return nil
}

type RuleProvider struct {
	Module CapabilityModule
	Engine platform.RuleEngine
}
type AuthorizationProvider struct {
	Module CapabilityModule
	Engine platform.AuthorizationEngine
}

type RuntimeProviders struct {
	Rules         []RuleProvider
	Authorization []AuthorizationProvider
	Kernel        []CapabilityModule
}

func DefaultRuntimeProviders(authorization platform.AuthorizationEngine) RuntimeProviders {
	c := DefaultRuntimeCapabilities(authorization)
	return RuntimeProviders{Rules: []RuleProvider{{c.RuleModule, c.Rules}}, Authorization: []AuthorizationProvider{{c.AuthModule, c.Authorization}}, Kernel: []CapabilityModule{c.KernelModule}}
}

func (c RuntimeCapabilities) Selection() map[string]string {
	return map[string]string{"platform.rules": c.RuleModule.ID, "platform.authorization": c.AuthModule.ID, "platform.kernel": c.KernelModule.ID}
}

func (p RuntimeProviders) Modules() []CapabilityModule {
	var modules []CapabilityModule
	for _, provider := range p.Rules {
		modules = append(modules, provider.Module)
	}
	for _, provider := range p.Authorization {
		modules = append(modules, provider.Module)
	}
	return append(modules, p.Kernel...)
}

func (p RuntimeProviders) Resolve(selection map[string]string) (RuntimeCapabilities, error) {
	plan, err := ResolveComposition(p.Modules(), selection)
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
	for _, module := range p.Kernel {
		if slices.Contains(plan.Order, module.ID) {
			runtime.KernelModule = module
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

func MemoryStorageModule() CapabilityModule {
	return CapabilityModule{ID: "platform.storage.memory", Title: "In-memory development storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.memory.v1", Provides: []CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}
}
