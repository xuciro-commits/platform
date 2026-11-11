package host

import (
	"context"
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/services/ai/gateway"

	"platformserver/platform"
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

func (t *Tenant) ModelSelection(ctx context.Context, m platform.Member, purpose string) (ModelSelection, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (ModelSelection, *kernel.Error) {
		out := ModelSelection{Purpose: purpose, Models: []ModelChoice{}}
		if purpose != "generation" && purpose != "agent" && purpose != "embedding" {
			return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a model purpose")
		}
		models, err := t.read(m, "ai-models")
		if err != nil {
			return out, err
		}
		app, key := aigateway.PurposeSetting(purpose)
		out.Default = t.setting(t.automation(app, false), key)
		if resolved, _, err := t.modelResolver().Resolve(purpose, "", platform.ModelOptions{}); err == nil {
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
			binding, _, failure := t.modelResolver().Resolve(purpose, model.Name(), platform.ModelOptions{})
			if failure != nil {
				choice.Problem = failure.Message
			} else {
				choice.Effective = &binding
			}
			if m.Roles[platform.AIApp] == platform.AIAdmin {
				for _, use := range []string{"generation", "agent", "embedding"} {
					owner, setting := aigateway.PurposeSetting(use)
					if actual, _, _ := t.modelResolver().Resolve(use, "", platform.ModelOptions{}); actual.Model == model.Name() {
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

func (t *Tenant) modelResolver() aigateway.Resolver {
	r := aigateway.Resolver{Setting: func(app, key string) string { return t.setting(t.automation(app, false), key) }}
	if t.services.AIInference != nil {
		r.Policy = *t.services.AIInference
	}
	if t.ai != nil {
		r.Registry = t.ai
	}
	return r
}
