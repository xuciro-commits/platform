package knowledge

import (
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Embedder struct {
	Model func() (platform.AIModel, platform.AIProvider, *kernel.Error)
	Stage func(binding platform.AIModelBinding, provider platform.AIProvider) *kernel.Error
	Embed func(model platform.AIModel, provider platform.AIProvider, input []string) ([][]float32, error)
}

func (e Embedder) Vectors(name string, input []string) ([][]float32, error) {
	model, pv, err := e.Model()
	if err != nil {
		return nil, fmt.Errorf("the model %s is not enabled", name)
	}
	binding, problem := model.Resolve("embedding", platform.ModelOptions{}, 0)
	if problem != nil {
		return nil, problem
	}
	if refusal := e.Stage(binding, pv); refusal != nil {
		return nil, refusal
	}
	return e.Embed(model, pv, input)
}

func VectorSpace(name string, lookup func(string) (platform.AIModel, platform.AIProvider, *kernel.Error)) string {
	m, pv, err := lookup(name)
	if err != nil || m.Revision == 0 {
		return name
	}
	digest, _ := platform.CanonicalDigest([]any{name, pv.BaseURL, pv.Wire, m.Revision, m.Capabilities})
	return name + "@" + digest
}

type EmbeddingSetting struct {
	Resolve func(value string) (platform.AIProvider, *kernel.Error)
	Status  func() platform.KnowledgeIndexStatus
}

func (s EmbeddingSetting) Check(id, value string) *kernel.Error {
	if id != platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingNext && id != platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel {
		return nil
	}
	if value == "" {
		return nil
	}
	pv, err := s.Resolve(value)
	if err != nil {
		return err
	}
	if pv.Wire != "openai" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This provider adapter does not support embeddings")
	}
	if id == platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel {
		status := s.Status()
		if status.Preparing != value || !status.Ready {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Build this model's index in AI configuration before activating it")
		}
	}
	return nil
}
