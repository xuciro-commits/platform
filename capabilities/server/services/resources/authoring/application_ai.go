package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ApplicationAI struct {
	Model       func(kind, name string, options platform.ModelOptions) (string, string, bool)
	Agents      *platform.Caller
	Evaluations platform.Caller
}

func conflict(err error) *kernel.Error {
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
}

func (a ApplicationAI) List(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, *kernel.Error) {
	out := []platform.ApplicationAI{}
	for _, asset := range assets {
		if !visible[asset.Ref] {
			continue
		}
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
			if a.Agents != nil && (m.Holds(ID, Builder) || m.Holds(ID, Publisher)) {
				digest, _ := platform.CanonicalDigest(asset)
				reports, _, problem := platform.Find[platform.Evaluation](*a.Agents, platform.Query{Domain: platform.Raw([]any{[]any{"agent", "=", asset.Ref.App + "." + asset.Ref.Name}, []any{"definitionDigest", "=", digest}})})
				if problem != nil {
					return nil, problem
				}
				for _, report := range reports {
					if report.Archived {
						continue
					}
					state := report.State
					if state == "done" {
						state = "failed"
						if report.Score == 1 && report.Fails == 0 && report.Differs == 0 {
							state = "passed"
						}
					}
					if report.State == "done" && (currentConfig == "" || report.ModelConfig != currentConfig) {
						state = "Configuration changed; evaluate again"
					}
					item.Evaluations = append(item.Evaluations, platform.ApplicationAIEvaluation{ID: report.ID, Candidate: report.Candidate, State: state, Model: report.Model, Quality: report.Score, CostUSD: report.Cost})
				}
			}
			out = append(out, item)
			continue
		}
		if asset.Ref.Kind != platform.AssetFunction {
			continue
		}
		definition, version, err := FunctionAssetDefinition(asset)
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
			var declaration Object
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
		if m.Holds(ID, Builder) || m.Holds(ID, Publisher) {
			offset := 0
			for {
				rows, total, problem := platform.Find[Evaluation](a.Evaluations, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}})
				if problem != nil {
					return nil, problem
				}
				for _, report := range rows {
					if report.Function == asset.Ref.Name && report.Version == version && !report.Archived {
						state := report.State
						if (state == "passed" || state == "failed") && (currentConfig == "" || report.ModelConfig != currentConfig) {
							state = "Configuration changed; evaluate again"
						}
						item.Evaluations = append(item.Evaluations, platform.ApplicationAIEvaluation{ID: report.ID, Candidate: report.Candidate, State: state, Model: report.Model, Quality: report.Quality, CostUSD: report.CostUSD})
					}
				}
				offset += len(rows)
				if offset >= total || len(rows) == 0 {
					break
				}
			}
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

func FunctionAssetDefinition(asset platform.ReleaseAsset) (platform.AIFunction, int, error) {
	var definition platform.AIFunction
	if err := json.Unmarshal(asset.Body, &definition); err != nil {
		return definition, 0, err
	}
	version := 1
	if _, ordinal, found := strings.Cut(asset.SourceVersion, ".function-"); found {
		parsed, err := strconv.Atoi(ordinal)
		if err != nil || parsed < 1 {
			return definition, 0, fmt.Errorf("Invalid function asset version")
		}
		version = parsed
	}
	return definition, version, definition.Check()
}

func CheckProposalFunctions(candidate platform.ReleaseCandidate) error {
	functions := map[string]platform.ReleaseAsset{}
	for _, asset := range candidate.Assets {
		if asset.Ref.App == ID && asset.Ref.Kind == platform.AssetFunction {
			functions[asset.Ref.Name] = asset
		}
	}
	for _, asset := range candidate.Assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject {
			continue
		}
		var object Object
		if json.Unmarshal(asset.Body, &object) != nil {
			return fmt.Errorf("Proposal object declaration is invalid")
		}
		for _, action := range object.Actions {
			if action.Adoption == nil {
				continue
			}
			functionAsset, ok := functions[action.Adoption.Function.Name]
			if !ok {
				return fmt.Errorf("Application adoption needs its declared function in the candidate")
			}
			definition, version, err := FunctionAssetDefinition(functionAsset)
			if err != nil {
				return err
			}
			if action.Adoption.Function.Version != 0 && action.Adoption.Function.Version != version {
				return fmt.Errorf("Application adoption function version differs from the candidate")
			}
			inputs := []platform.Field{}
			for _, input := range action.Inputs {
				inputs = append(inputs, input.Field())
			}
			if err := action.Adoption.CheckMapping(definition, inputs); err != nil {
				return err
			}
		}
	}
	return nil
}
