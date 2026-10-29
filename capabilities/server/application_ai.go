package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/aigateway"

	"slices"
	"strconv"
	"strings"

	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/studio/build"
)

func (t *Tenant) applicationAILocked(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, *kernel.Error) {
	out := []platform.ApplicationAI{}
	for _, asset := range assets {
		if !visible[asset.Ref] {
			continue
		}
		if asset.Ref.Kind == platform.AssetAgent {
			var definition platform.AgentReleaseDescriptor
			if json.Unmarshal(asset.Body, &definition) != nil || definition.Check() != nil {
				return nil, conflictRefusal(fmt.Errorf("invalid agent declaration"))
			}
			item := platform.ApplicationAI{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Placement: "assist", Model: definition.Model, HandoffRole: definition.HandoffRole, Adoptions: []string{}, Checkpoints: slices.Clone(definition.Checkpoints), Evaluations: []platform.ApplicationAIEvaluation{}}
			currentConfig := ""
			if binding, pv, err := t.resolveModel("agent", definition.Model, definition.ModelOptions); err == nil {
				item.Model = binding.Model
				model, _, _ := t.ai.Model(binding.Model)
				if pinned, refusal := t.modelStage(binding, pv, platform.AIStageDesign); refusal == nil {
					currentConfig, _ = aigateway.ConfigurationDigest(model, pv, &pinned)
				}
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
			if t.agents != nil && (m.Holds(build.ID, build.Builder) || m.Holds(build.ID, build.Publisher)) {
				digest, _ := platform.CanonicalDigest(asset)
				reports, _, problem := platform.Find[platform.Evaluation](t.automation(platform.AgentApp, false), platform.Query{Domain: platform.Raw([]any{[]any{"agent", "=", asset.Ref.App + "." + asset.Ref.Name}, []any{"definitionDigest", "=", digest}})})
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
		definition, version, err := functionAssetDefinition(asset)
		if err != nil {
			return nil, conflictRefusal(err)
		}
		item := platform.ApplicationAI{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Placement: "assist", Model: definition.Model, Adoptions: []string{}, Checkpoints: []string{}, Evaluations: []platform.ApplicationAIEvaluation{}}
		currentConfig := ""
		if binding, pv, err := t.resolveModel("generation", definition.Model, definition.ModelOptions.Override(platform.ModelOptions{MaxTokens: definition.MaxTokens})); err == nil {
			item.Model = binding.Model
			model, _, _ := t.ai.Model(binding.Model)
			if pinned, refusal := t.modelStage(binding, pv, platform.AIStageDesign); refusal == nil {
				currentConfig, _ = aigateway.ConfigurationDigest(model, pv, &pinned)
			}
		}
		if definition.Proposal != nil {
			item.Placement = "propose"
			item.HandoffRole = definition.Proposal.HandoffRole
		}
		for _, object := range assets {
			if object.Ref.Kind != platform.AssetObject || object.Ref.App != asset.Ref.App {
				continue
			}
			var declaration build.Object
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
		if m.Holds(build.ID, build.Builder) || m.Holds(build.ID, build.Publisher) {
			offset := 0
			for {
				rows, total, problem := platform.Find[build.Evaluation](t.automation(build.ID, false), platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}})
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

func (t *Tenant) applicationAIUsage(m platform.Member, application platform.AssetRef, items []platform.ApplicationAI) []platform.ApplicationAI {
	value, problem := t.Read(m, "ai-usage")
	if problem != nil {
		return items
	}
	report, ok := value.(platform.AIUsageReport)
	if !ok {
		return items
	}
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

func functionAssetDefinition(asset platform.ReleaseAsset) (platform.AIFunction, int, error) {
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
