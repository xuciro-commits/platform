package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/acceptance"
	"slices"
	"strconv"
	"strings"

	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// ApplicationAI is a projection of the original function, action, evaluation
// and usage owners. It carries no prompts, grants, quotas or second AI state.
type ApplicationAI struct {
	Resource    platform.AssetBinding     `json:"resource"`
	Placement   string                    `json:"placement"`
	Model       string                    `json:"model,omitempty"`
	HandoffRole string                    `json:"handoffRole,omitempty"`
	Adoptions   []string                  `json:"adoptions"`
	Checkpoints []string                  `json:"checkpoints"`
	Evaluations []ApplicationAIEvaluation `json:"evaluations"`
	Calls       int                       `json:"calls"`
	SharedCalls int                       `json:"sharedCalls"`
	Input       int                       `json:"input"`
	Output      int                       `json:"output"`
	Cost        float64                   `json:"cost"`
	CostUnknown int                       `json:"costUnknown"`
}
type ApplicationAIEvaluation struct {
	ID        string  `json:"id"`
	Candidate string  `json:"candidate"`
	State     string  `json:"state"`
	Model     string  `json:"model"`
	Quality   float64 `json:"quality"`
	CostUSD   float64 `json:"costUsd"`
}

func (t *Tenant) applicationAILocked(m platform.Member, assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]ApplicationAI, *kernel.Error) {
	out := []ApplicationAI{}
	for _, asset := range assets {
		if !visible[asset.Ref] {
			continue
		}
		if asset.Ref.Kind == platform.AssetAgent {
			var definition platform.AgentReleaseDescriptor
			if json.Unmarshal(asset.Body, &definition) != nil || definition.Check() != nil {
				return nil, migrationRefusal(fmt.Errorf("invalid agent declaration"))
			}
			item := ApplicationAI{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Placement: "assist", Model: t.setting(t.automation(AgentApp, false), SettingAgentModel), HandoffRole: definition.HandoffRole, Adoptions: []string{}, Checkpoints: slices.Clone(definition.Checkpoints), Evaluations: []ApplicationAIEvaluation{}}
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
			if t.agents != nil && (m.Holds(construction.ID, construction.Builder) || m.Holds(construction.ID, construction.Publisher)) {
				digest, _ := acceptance.Digest(asset)
				reports, _, problem := platform.Find[Evaluation](t.automation(AgentApp, false), platform.Query{Domain: platform.Raw([]any{[]any{"agent", "=", asset.Ref.App + "." + asset.Ref.Name}, []any{"definitionDigest", "=", digest}})})
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
					item.Evaluations = append(item.Evaluations, ApplicationAIEvaluation{ID: report.ID, Candidate: report.Candidate, State: state, Model: report.Model, Quality: report.Score, CostUSD: report.Cost})
				}
			}
			out = append(out, item)
			continue
		}
		if asset.Ref.Kind != platform.AssetFunction {
			continue
		}
		definition, _, err := functionAssetDefinition(asset)
		if err != nil {
			return nil, migrationRefusal(err)
		}
		item := ApplicationAI{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Placement: "assist", Model: definition.Model, Adoptions: []string{}, Checkpoints: []string{}, Evaluations: []ApplicationAIEvaluation{}}
		if definition.Proposal != nil {
			item.Placement = "propose"
			item.HandoffRole = definition.Proposal.HandoffRole
		}
		for _, object := range assets {
			if object.Ref.Kind != platform.AssetObject || object.Ref.App != asset.Ref.App {
				continue
			}
			var declaration construction.Object
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

		out = append(out, item)
	}
	return out, nil
}

func (t *Tenant) applicationAIUsage(m platform.Member, application platform.AssetRef, items []ApplicationAI) []ApplicationAI {
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
	slices.SortFunc(items, func(a, b ApplicationAI) int { return strings.Compare(a.Resource.Ref.String(), b.Resource.Ref.String()) })
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
