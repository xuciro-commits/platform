package platformserver

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/aigateway"
	"platformserver/internal/acceptance"
	"platformserver/platform"
	"strconv"
)

type ModelChoice struct {
	Model          platform.AIModel         `json:"model"`
	Effective      *platform.AIModelBinding `json:"effective,omitempty"`
	Problem        string                   `json:"problem,omitempty"`
	PurposeUnknown bool                     `json:"purposeUnknown,omitempty"`
	References     []string                 `json:"references,omitempty"`
}

type ModelSelection struct {
	Purpose         string                `json:"purpose"`
	Default         string                `json:"default"`
	DefaultProblem  string                `json:"defaultProblem,omitempty"`
	ResolvedDefault string                `json:"resolvedDefault,omitempty"`
	Options         platform.ModelOptions `json:"options"`
	Models          []ModelChoice         `json:"models"`
}

func (t *Tenant) ModelSelection(m platform.Member, purpose string) (ModelSelection, *kernel.Error) {
	return recordRead(t, context.Background(), func() (ModelSelection, *kernel.Error) {
		out := ModelSelection{Purpose: purpose, Models: []ModelChoice{}}
		if purpose != "generation" && purpose != "agent" && purpose != "embedding" {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a model purpose")
		}
		models, err := t.read(m, "ai-models")
		if err != nil {
			return out, err
		}
		app, key := modelPurposeSetting(purpose)
		out.Default = t.setting(t.automation(app, false), key)
		if resolved, _, err := t.resolveModel(purpose, "", platform.ModelOptions{}); err == nil {
			out.ResolvedDefault = resolved.Model
		} else {
			out.DefaultProblem = err.Message
			if out.DefaultProblem == "" {
				out.DefaultProblem = "The selected model is disabled or missing"
			}
		}
		_ = json.Unmarshal([]byte(t.setting(t.automation(app, false), key+"-options")), &out.Options)
		for _, model := range models.([]platform.AIModel) {
			choice := ModelChoice{Model: model, PurposeUnknown: model.Capabilities == nil || len(model.Capabilities.Purposes) == 0}
			binding, _, failure := t.resolveModel(purpose, model.Name(), platform.ModelOptions{})
			if failure != nil {
				choice.Problem = failure.Message
			} else {
				choice.Effective = &binding
			}
			if m.Roles[platform.AIApp] == platform.AIAdmin {
				for _, use := range []string{"generation", "agent", "embedding"} {
					owner, setting := modelPurposeSetting(use)
					if actual, _, _ := t.resolveModel(use, "", platform.ModelOptions{}); actual.Model == model.Name() {
						choice.References = append(choice.References, owner+"/"+setting)
					}
				}
			}
			for _, owner := range t.apps {
				manifest := owner.Manifest()
				if m.Roles[manifest.ID] == "" {
					continue
				}
				for _, f := range manifest.Functions {
					if f.Model == model.Name() {
						choice.References = append(choice.References, manifest.ID+"/function/"+f.Name)
					}
				}
				for _, agent := range manifest.Agents {
					if agent.Model == model.Name() {
						choice.References = append(choice.References, manifest.ID+"/agent/"+agent.Name)
					}
				}
			}
			out.Models = append(out.Models, choice)
		}
		return out, nil
	})
}

func modelPurposeSetting(purpose string) (string, string) {
	switch purpose {
	case "agent":
		return AgentApp, SettingAgentModel
	case "embedding":
		return platform.KnowledgeApp, platform.SettingKnowledgeEmbeddingModel
	default:
		return platform.AIApp, platform.AISettingAppModel
	}
}

// Purpose defaults remain settings of the original app. The model owner owns
// facts/defaults; consumers retain the resolved binding in their accepted work.
func (t *Tenant) resolveModel(purpose, name string, override platform.ModelOptions) (platform.AIModelBinding, platform.AIProvider, *kernel.Error) {
	app, key := modelPurposeSetting(purpose)
	if name == "" {
		name = t.setting(t.automation(app, false), key)
	}
	if t.ai == nil || name == "" {
		return platform.AIModelBinding{}, platform.AIProvider{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Choose a default model in AI configuration")
	}
	if purpose == "agent" && name == "@generation" {
		name = t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel)
	}
	model, pv, err := t.ai.Model(name)
	if err != nil {
		return platform.AIModelBinding{}, pv, err
	}
	var defaults platform.ModelOptions
	if raw := t.setting(t.automation(app, false), key+"-options"); raw != "" {
		if json.Unmarshal([]byte(raw), &defaults) != nil {
			return platform.AIModelBinding{}, pv, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The purpose model options are invalid")
		}
	}
	b, problem := model.Resolve(purpose, defaults.Override(override), t.mostTokens())
	b.Provider = &pv
	if problem != nil {
		return b, pv, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
	}
	return b, pv, nil
}

func (t *Tenant) checkModelSetting(id, value string) *kernel.Error {
	if id == platform.AIApp+"/"+platform.AISettingMaxTokens || id == platform.AIApp+"/"+platform.AISettingDailyTokens || id == platform.AIApp+"/"+platform.AISettingPerMinute || id == AgentApp+"/"+SettingAgentDaily {
		n, err := strconv.Atoi(value)
		if err != nil || n < 0 {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Token and call limits cannot be negative")
		}
	}
	for _, purpose := range []string{"generation", "agent", "embedding"} {
		app, key := modelPurposeSetting(purpose)
		if id != app+"/"+key && id != app+"/"+key+"-options" {
			continue
		}
		name := value
		var options platform.ModelOptions
		if id == app+"/"+key+"-options" {
			if value == "" {
				return nil
			}
			if value != "" && json.Unmarshal([]byte(value), &options) != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The model options are invalid")
			}
			name = t.setting(t.automation(app, false), key)
		}
		if name == "" {
			return nil
		}
		if id == app+"/"+key+"-options" {
			if purpose == "agent" && name == "@generation" {
				name = t.setting(t.automation(platform.AIApp, false), platform.AISettingAppModel)
			}
			model, _, err := t.ai.Model(name)
			if err != nil {
				return err
			}
			if _, problem := model.Resolve(purpose, options, t.mostTokens()); problem != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, problem.Error())
			}
			return nil
		}
		_, _, err := t.resolveModel(purpose, name, options)
		return err
	}
	return nil
}

// Model quality evidence uses the same effective configuration identity in
// evaluation, publication gates and their read-only readiness projections.
func modelConfigurationDigest(model platform.AIModel, provider platform.AIProvider, binding *platform.AIModelBinding) (string, error) {
	identity := []any{model, provider}
	if binding != nil {
		identity = append(identity, *binding)
	}
	return acceptance.Digest(identity)
}

func (t *Tenant) modelStage(binding platform.AIModelBinding, provider platform.AIProvider, stage string) (platform.AIModelBinding, *kernel.Error) {
	policy := platform.AIInferencePolicy{}
	if t.services.AIInference != nil {
		policy = *t.services.AIInference
	}
	pinned, err := aigateway.Bind(policy, provider, binding, stage)
	if err != nil {
		return pinned, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, err.Error())
	}
	return pinned, nil
}
