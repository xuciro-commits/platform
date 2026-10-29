package platform

import (
	"fmt"
	"maps"
	"platformserver/platform/authz"
	"slices"
	"strings"
)

// CapabilityContract names behavior, independently of a provider's release.
type CapabilityContract struct {
	ID        string `json:"id"`
	Version   string `json:"version"`
	Semantics string `json:"semantics"`
}

// CapabilityModule describes one trusted implementation, not business state.
type CapabilityModule struct {
	ID          string               `json:"id"`
	Version     string               `json:"version"`
	Title       string               `json:"title"`
	Scope       string               `json:"scope"`       // deployment, tenant, invocation
	Replacement string               `json:"replacement"` // drain or restart
	StateFormat string               `json:"stateFormat,omitempty"`
	Provides    []CapabilityContract `json:"provides"`
	Requires    []CapabilityContract `json:"requires,omitempty"`
}

type CapabilityBinding struct {
	CapabilityContract
	Provider string `json:"provider"`
}

// CompositionPlan is a pure projection of selected manifests. It owns no state.
type CompositionPlan struct {
	Modules  []CapabilityModule  `json:"modules"`
	Bindings []CapabilityBinding `json:"bindings"`
	Order    []string            `json:"order"`
}

func (c CapabilityContract) Check() error {
	if strings.TrimSpace(c.ID) == "" || c.Version == "" || c.Semantics == "" {
		return fmt.Errorf("capability needs an identity, interface version and semantic version")
	}
	return nil
}

func (m CapabilityModule) Check() error {
	if m.ID == "" || m.Version == "" || !slices.Contains([]string{"deployment", "tenant", "invocation"}, m.Scope) || !slices.Contains([]string{"drain", "restart"}, m.Replacement) {
		return fmt.Errorf("module %s needs an identity, version, scope and replacement mode", m.ID)
	}
	for _, contracts := range [][]CapabilityContract{m.Provides, m.Requires} {
		seen := map[string]bool{}
		for _, c := range contracts {
			if err := c.Check(); err != nil {
				return fmt.Errorf("module %s: %w", m.ID, err)
			}
			if seen[c.ID] {
				return fmt.Errorf("module %s repeats capability %s", m.ID, c.ID)
			}
			seen[c.ID] = true
		}
	}
	return nil
}

// ResolveComposition selects one provider per contract and orders dependencies.
// It never constructs providers, mutates manifests or interprets application assets.
func ResolveComposition(modules []CapabilityModule, selected map[string]string) (CompositionPlan, error) {
	plan := CompositionPlan{Modules: slices.Clone(modules), Bindings: []CapabilityBinding{}, Order: []string{}}
	slices.SortFunc(plan.Modules, func(a, b CapabilityModule) int { return strings.Compare(a.ID, b.ID) })
	byID := map[string]CapabilityModule{}
	providers := map[string][]CapabilityBinding{}
	for _, m := range plan.Modules {
		if err := m.Check(); err != nil {
			return CompositionPlan{}, err
		}
		if _, found := byID[m.ID]; found {
			return CompositionPlan{}, fmt.Errorf("module %s is declared twice", m.ID)
		}
		byID[m.ID] = m
		for _, c := range m.Provides {
			providers[c.ID] = append(providers[c.ID], CapabilityBinding{CapabilityContract: c, Provider: m.ID})
		}
	}
	bound := map[string]CapabilityBinding{}
	keys := make([]string, 0, len(providers))
	for id := range providers {
		keys = append(keys, id)
	}
	slices.Sort(keys)
	for _, id := range keys {
		candidates := providers[id]
		if choice := selected[id]; choice != "" {
			candidates = slices.DeleteFunc(slices.Clone(candidates), func(c CapabilityBinding) bool { return c.Provider != choice })
		}
		if len(candidates) != 1 {
			return CompositionPlan{}, fmt.Errorf("capability %s needs exactly one selected provider (found %d)", id, len(candidates))
		}
		bound[id] = candidates[0]
		plan.Bindings = append(plan.Bindings, candidates[0])
	}
	for id := range selected {
		if _, found := bound[id]; !found {
			return CompositionPlan{}, fmt.Errorf("selected capability %s is unavailable", id)
		}
	}
	active := map[string]bool{}
	for _, binding := range plan.Bindings {
		active[binding.Provider] = true
	}
	for _, m := range plan.Modules {
		if len(m.Provides) == 0 {
			active[m.ID] = true
		}
		if active[m.ID] {
			for _, contract := range m.Provides {
				if bound[contract.ID].Provider != m.ID {
					return CompositionPlan{}, fmt.Errorf("module %s cannot activate only part of its provided contracts", m.ID)
				}
			}
		}
	}
	edges := map[string][]string{}
	for _, m := range plan.Modules {
		if !active[m.ID] {
			continue
		}
		for _, need := range m.Requires {
			p, found := bound[need.ID]
			if !found || p.Version != need.Version || p.Semantics != need.Semantics {
				return CompositionPlan{}, fmt.Errorf("module %s requires %s interface %s semantics %s", m.ID, need.ID, need.Version, need.Semantics)
			}
			provider := byID[p.Provider]
			if m.Scope == "deployment" && provider.Scope != "deployment" || m.Scope == "tenant" && provider.Scope == "invocation" {
				return CompositionPlan{}, fmt.Errorf("module %s cannot retain shorter-lived provider %s", m.ID, provider.ID)
			}
			edges[m.ID] = append(edges[m.ID], p.Provider)
		}
	}
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string, []string) error
	visit = func(id string, path []string) error {
		if visiting[id] {
			return fmt.Errorf("capability dependency cycle: %s", strings.Join(append(path, id), " -> "))
		}
		if visited[id] {
			return nil
		}
		visiting[id] = true
		for _, dependency := range edges[id] {
			if err := visit(dependency, append(path, id)); err != nil {
				return err
			}
		}
		delete(visiting, id)
		visited[id] = true
		plan.Order = append(plan.Order, id)
		return nil
	}
	for _, m := range plan.Modules {
		if !active[m.ID] {
			continue
		}
		if err := visit(m.ID, nil); err != nil {
			return CompositionPlan{}, err
		}
	}
	return plan, nil
}

// OwnerBindings is the composition's bundle of independent consumer roles.
// Each owner is attached only to its declared role; it cannot retain the bundle.
type OwnerBindings struct {
	Training  AITrainingHost
	System    SystemDefinitionHost
	Agent     AgentHost
	Console   ConsoleHost
	Build     BuildHost
	Files     FileHost
	Knowledge KnowledgeHost
	Relations RelationsHost
	Flow      FlowHost
	Work      WorkHost
}

// RuntimeCapabilities binds trusted pure implementations for one tenant instance.
// Rebuilding an instance carries this selection; callers never mutate it live.
type RuntimeCapabilities struct {
	NewTransactionExecutor func(BusinessRuntimeContext) TransactionExecutor
	NewAgentEvaluator      func() AgentEvaluator
	BindOwner              func(App, OwnerBindings)
	AIInference            *AIInferencePolicy
	Rules                  RuleEngine
	Authorization          authz.Engine
	Kernel                 KernelFactory
	RuleModule             CapabilityModule
	AuthModule             CapabilityModule
	KernelModule           CapabilityModule
	StorageModule          CapabilityModule
}

func DefaultRuntimeCapabilities() RuntimeCapabilities {
	return RuntimeCapabilities{
		Rules: NativeRules{}, Authorization: authz.Standard{}, Kernel: NativeKernel{}, StorageModule: MemoryStorageModule(),
		KernelModule: CapabilityModule{ID: "platform.kernel.native", Version: "1", Title: "Kernel", Scope: "tenant", Replacement: "restart", StateFormat: "platform.kernel.v1", Provides: []CapabilityContract{{ID: "platform.kernel", Version: "1", Semantics: "1"}}},
		RuleModule:   CapabilityModule{ID: "platform.rules.native", Version: "1", Title: "Business rules", Scope: "tenant", Replacement: "restart", Provides: []CapabilityContract{{ID: "platform.rules", Version: "1", Semantics: fmt.Sprint(ExprVersion)}}},
		AuthModule:   CapabilityModule{ID: "platform.authorization.standard", Version: "1", Title: "Authorization", Scope: "tenant", Replacement: "restart", Provides: []CapabilityContract{{ID: "platform.authorization", Version: "1", Semantics: "1"}}},
	}
}

func (c RuntimeCapabilities) Check() error {
	if c.Rules == nil || c.Authorization == nil || c.Kernel == nil {
		return fmt.Errorf("runtime needs explicit rule and authorization providers")
	}
	plan, err := ResolveComposition([]CapabilityModule{c.RuleModule, c.AuthModule, c.KernelModule}, nil)
	if err != nil {
		return err
	}
	for _, expected := range []CapabilityContract{{ID: "platform.rules", Version: "1", Semantics: fmt.Sprint(ExprVersion)}, {ID: "platform.authorization", Version: "1", Semantics: "1"}, {ID: "platform.kernel", Version: "1", Semantics: "1"}} {
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
	Engine RuleEngine
}
type AuthorizationProvider struct {
	Module CapabilityModule
	Engine authz.Engine
}
type KernelProvider struct {
	Module CapabilityModule
	Engine KernelFactory
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

func (p RuntimeProviders) Modules() []CapabilityModule {
	var modules []CapabilityModule
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

// MemoryStorageModule describes the existing process-local development journal.
// It does not claim durability or implement a delivery storage substitute.
func MemoryStorageModule() CapabilityModule {
	return CapabilityModule{ID: "platform.storage.memory", Title: "In-memory development storage", Version: "1", Scope: "deployment", Replacement: "restart", StateFormat: "platform.memory.v1", Provides: []CapabilityContract{{ID: "platform.storage", Version: "1", Semantics: "1"}}}
}
