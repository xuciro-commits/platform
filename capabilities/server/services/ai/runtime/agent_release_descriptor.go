package runtime

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func WithPublicationResolver(resolver publication.Resolver) publication.Resolver {
	resolver.CheckAgent = CheckAgentReleaseDescriptor
	resolver.AgentDependencies = AgentReleaseDependencies
	resolver.AgentDescriptor = AgentReleaseDescriptor
	return resolver
}

func AgentReleaseDescriptor(agent platform.Agent) (platform.AgentReleaseDescriptor, error) {
	cases := []map[string]any{}
	for _, test := range agent.Cases {
		cases = append(cases, map[string]any{"name": test.Name, "goal": test.Goal, "ref": test.Ref, "check": test.Check != nil})
	}
	raw, err := json.Marshal(cases)
	if err != nil {
		return platform.AgentReleaseDescriptor{}, err
	}
	descriptor := releaseDescriptor(agent, raw, agent.Guard != nil, agent.To != nil)
	descriptor.Native = true
	return descriptor, CheckAgentReleaseDescriptor(descriptor)
}

func CheckAgentReleaseDescriptor(a platform.AgentReleaseDescriptor) error {
	if !platform.ValidFunctionName(a.Name) || a.Title == "" || strings.TrimSpace(a.Instructions) == "" || len(a.Instructions) > 65536 || len(a.Tools) > 1000 || a.Budget.Steps < 0 || a.Budget.Tokens < 0 || a.Budget.Actions < 0 || a.Budget.Cost < 0 || len(a.Cases) > 262144 {
		return fmt.Errorf("agent needs a valid identity, instructions and bounded tools/budget")
	}
	if len(a.Cases) > 0 && (!json.Valid(a.Cases) || a.Cases[0] != '[') {
		return fmt.Errorf("agent evaluation cases need an array")
	}
	for tool, ref := range a.ToolBindings {
		if !slices.Contains(a.Tools, tool) || ref.Check() != nil {
			return fmt.Errorf("agent tool binding must retain an original declared tool and asset")
		}
	}
	for _, tool := range a.Checkpoints {
		if !slices.Contains(a.Tools, tool) {
			return fmt.Errorf("agent checkpoint is not a declared tool")
		}
	}
	return nil
}

func AgentReleaseDependencies(a platform.AgentReleaseDescriptor, owner string) []platform.AssetRef {
	refs := []platform.AssetRef{}
	for _, tool := range a.Tools {
		if ref, ok := a.ToolBindings[tool]; ok {
			refs = append(refs, ref)
			continue
		}
		if a.Native && !strings.Contains(tool, ".") && !strings.HasPrefix(tool, "query:") {
			continue
		}
		if name, ok := strings.CutPrefix(tool, "query:"); ok {
			refs = append(refs, platform.AssetRef{App: owner, Kind: platform.AssetQuery, Name: name})
			continue
		}
		if strings.ContainsAny(tool, "#:") {
			continue
		}
		refs = append(refs, platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: tool})
	}
	slices.SortFunc(refs, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return slices.Compact(refs)
}

func releaseDescriptor(agent platform.Agent, cases json.RawMessage, guard, handoff bool) platform.AgentReleaseDescriptor {
	return platform.AgentReleaseDescriptor{
		Model: agent.Model, ModelOptions: agent.ModelOptions, Name: agent.Name, Title: agent.Title,
		Description: agent.Description, Instructions: agent.Instructions, Tools: slices.Clone(agent.Tools),
		Budget: agent.Budget, Checkpoints: slices.Clone(agent.Checkpoints),
		Guard: guard, Handoff: handoff, Cases: cases,
	}
}
