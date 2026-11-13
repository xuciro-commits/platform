package aigateway

import (
	"encoding/json"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

func (r Resolver) Selection(m platform.Member, purpose string, apps []platform.App, read func() ([]platform.AIModel, *kernel.Error)) (ModelSelection, *kernel.Error) {
	out := ModelSelection{Purpose: purpose, Models: []ModelChoice{}}
	if purpose != "generation" && purpose != "agent" && purpose != "embedding" {
		return out, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Choose a model purpose")
	}
	models, err := read()
	if err != nil {
		return out, err
	}
	app, key := PurposeSetting(purpose)
	out.Default = r.Setting(app, key)
	if resolved, _, err := r.Resolve(purpose, "", platform.ModelOptions{}); err == nil {
		out.ResolvedDefault = resolved.Model
	} else {
		out.DefaultProblem = err.Message
		if out.DefaultProblem == "" {
			out.DefaultProblem = "The selected model is disabled or missing"
		}
	}
	_ = json.Unmarshal([]byte(r.Setting(app, key+"-options")), &out.Options)
	for _, model := range models {
		choice := ModelChoice{Model: model, PurposeUnknown: model.Capabilities == nil || len(model.Capabilities.Purposes) == 0}
		binding, _, failure := r.Resolve(purpose, model.Name(), platform.ModelOptions{})
		if failure != nil {
			choice.Problem = failure.Message
		} else {
			choice.Effective = &binding
		}
		if m.Roles[platform.AIApp] == platform.AIAdmin {
			for _, use := range []string{"generation", "agent", "embedding"} {
				owner, setting := PurposeSetting(use)
				if actual, _, _ := r.Resolve(use, "", platform.ModelOptions{}); actual.Model == model.Name() {
					choice.References = append(choice.References, owner+"/"+setting)
				}
			}
		}
		for _, owner := range apps {
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
}

func (s *ModelSelection) Localize(say func(string) string) {
	s.DefaultProblem = say(s.DefaultProblem)
	for i := range s.Models {
		s.Models[i].Problem = say(s.Models[i].Problem)
	}
}
