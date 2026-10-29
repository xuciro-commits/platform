package platform

import (
	"fmt"
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
