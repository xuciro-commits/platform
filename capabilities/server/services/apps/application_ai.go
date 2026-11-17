package applications

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/ontology"
)

type ApplicationAI struct {
	Model               func(kind, name string, options platform.ModelOptions) (string, string, bool)
	AgentEvaluations    func(string, string, string) ([]platform.ApplicationAIEvaluation, *platform.Error)
	FunctionDefinition  func(platform.ReleaseAsset) (platform.AIFunction, int, error)
	FunctionEvaluations func(string, int, string) ([]platform.ApplicationAIEvaluation, *platform.Error)
	Roles               []string
}

func conflict(err error) *platform.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}

func (a ApplicationAI) List(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, *platform.Error) {
	out := []platform.ApplicationAI{}
	for _, asset := range assets {
		if !visible[asset.Ref] {
			continue
		}
		authorized := slices.ContainsFunc(a.Roles, func(role string) bool { return m.Holds(asset.Ref.App, role) })
		if asset.Ref.Kind == platform.AssetAgent {
			var definition platform.AgentReleaseDescriptor
			if json.Unmarshal(asset.Body, &definition) != nil || definition.Check() != nil {
				return nil, conflict(fmt.Errorf("invalid agent declaration"))
			}
			item := platform.ApplicationAI{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Placement: "assist", Model: definition.Model, HandoffRole: definition.HandoffRole, Adoptions: []string{}, Checkpoints: slices.Clone(definition.Checkpoints), Evaluations: []platform.ApplicationAIEvaluation{}}
			currentConfig := ""
			if bound, config, ok := a.Model("agent", definition.Model, definition.ModelOptions); ok {
				item.Model, currentConfig = bound, config
			}
			for _, tool := range definition.Tools {
				if strings.HasPrefix(tool, "query:") || strings.HasPrefix(tool, "read:") {
					continue
				}
				if item.Placement == "assist" {
					item.Placement = "act"
				}
				if !slices.Contains(definition.Checkpoints, tool) {
					item.Placement = "autonomous"
				}
			}
			if authorized && a.AgentEvaluations != nil {
				digest, _ := platform.CanonicalDigest(asset)
				reports, problem := a.AgentEvaluations(asset.Ref.App+"."+asset.Ref.Name, digest, currentConfig)
				if problem != nil {
					return nil, problem
				}
				item.Evaluations = reports
			}
			out = append(out, item)
			continue
		}
		if asset.Ref.Kind != platform.AssetFunction {
			continue
		}
		if a.FunctionDefinition == nil {
			return nil, conflict(fmt.Errorf("function definition owner is unavailable"))
		}
		definition, version, err := a.FunctionDefinition(asset)
		if err != nil {
			return nil, conflict(err)
		}
		item := platform.ApplicationAI{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Placement: "assist", Model: definition.Model, Adoptions: []string{}, Checkpoints: []string{}, Evaluations: []platform.ApplicationAIEvaluation{}}
		currentConfig := ""
		if bound, config, ok := a.Model("generation", definition.Model, definition.ModelOptions.Override(platform.ModelOptions{MaxTokens: definition.MaxTokens})); ok {
			item.Model, currentConfig = bound, config
		}
		if definition.Proposal != nil {
			item.Placement = "propose"
			item.HandoffRole = definition.Proposal.HandoffRole
		}
		for _, object := range assets {
			if object.Ref.Kind != platform.AssetObject || object.Ref.App != asset.Ref.App {
				continue
			}
			var declaration ontology.Object
			if json.Unmarshal(object.Body, &declaration) != nil {
				continue
			}
			for _, action := range declaration.Actions {
				if action.Adoption == nil || action.Adoption.Function.Name != asset.Ref.Name {
					continue
				}
				ref := platform.AssetRef{App: object.Ref.App, Kind: platform.AssetAction, Name: object.Ref.Name + "." + action.Name}
				if !visible[ref] {
					continue
				}
				item.Adoptions = append(item.Adoptions, ref.Name)
				if action.Approval != nil {
					for _, level := range action.Approval.Levels {
						item.Checkpoints = append(item.Checkpoints, level.Role)
					}
				}
			}
		}
		if authorized && a.FunctionEvaluations != nil {
			reports, problem := a.FunctionEvaluations(asset.Ref.Name, version, currentConfig)
			if problem != nil {
				return nil, problem
			}
			item.Evaluations = reports
		}
		out = append(out, item)
	}
	return out, nil
}

func ApplicationAIUsage(report platform.AIUsageReport, application platform.AssetRef, items []platform.ApplicationAI) []platform.ApplicationAI {
	for _, total := range report.Applications {
		if total.Resource == nil || total.Application.Ref != application {
			continue
		}
		for i := range items {
			item := &items[i]
			if item.Resource.Ref != *total.Resource {
				continue
			}
			item.Calls += total.Calls
			item.SharedCalls += total.SharedCalls
			item.Input += total.Input
			item.Output += total.Output
			item.Cost += total.Cost
			item.CostUnknown += total.CostUnknown
		}
	}
	slices.SortFunc(items, func(a, b platform.ApplicationAI) int {
		return strings.Compare(a.Resource.Ref.String(), b.Resource.Ref.String())
	})
	return items
}

func ApplicationAIUsageOf(value any, application platform.AssetRef, items []platform.ApplicationAI) []platform.ApplicationAI {
	if report, ok := value.(platform.AIUsageReport); ok {
		return ApplicationAIUsage(report, application, items)
	}
	return items
}
