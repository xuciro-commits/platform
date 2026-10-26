package platformserver

import (
	"context"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	"platformserver/platform"
)

const embeddingActiveSpace = "knowledge/embedding-active-space"

func (t *Tenant) embeddingNeedsRebuild(name string) bool {
	space, exists := t.settings.get(embeddingActiveSpace)
	return name != "" && exists && space != "" && space != t.vectorSpace(name)
}

type KnowledgeIndexStatus struct {
	NeedsRebuild bool   `json:"needsRebuild,omitempty"`
	Active       string `json:"active"`
	Preparing    string `json:"preparing"`
	Total        int    `json:"total"`
	Completed    int    `json:"completed"`
	Dimensions   int    `json:"dimensions,omitempty"`
	Ready        bool   `json:"ready"`
	Problem      string `json:"problem,omitempty"`
}

// Derived vectors stay in the original vector store. Model configuration
// revisions separate spaces; neither a rebuild nor a query mixes their bytes.
func (t *Tenant) vectorSpace(name string) string {
	if t.ai == nil {
		return name
	}
	m, pv, err := t.ai.Model(name)
	if err != nil || m.Revision == 0 {
		return name
	} // retained pre-configuration vectors
	digest, _ := acceptance.Digest([]any{name, pv.BaseURL, pv.Wire, m.Revision, m.Capabilities})
	return name + "@" + digest
}

func (t *Tenant) knowledgeIndexStatus() KnowledgeIndexStatus {
	t.syncRecords()
	out := KnowledgeIndexStatus{Active: t.setting(t.automation(platform.KnowledgeApp, false), platform.SettingKnowledgeEmbeddingModel), Preparing: t.setting(t.automation(platform.KnowledgeApp, false), platform.SettingKnowledgeEmbeddingNext)}
	if t.embeddingNeedsRebuild(out.Active) {
		out.NeedsRebuild = true
		out.Problem = "Model configuration changed; prepare and activate a new vector index to resume semantic search"
	}
	if out.Preparing == "" {
		return out
	}
	k := &t.index
	k.mu.Lock()
	hashes := []string{}
	seen := map[string]bool{}
	for _, chunks := range k.chunks {
		for _, c := range chunks {
			if !c.gone && !seen[c.hash] {
				seen[c.hash] = true
				hashes = append(hashes, c.hash)
			}
		}
	}
	out.Problem = k.embeddingError
	k.mu.Unlock()
	out.Total = len(hashes)
	for _, vector := range t.store().Vectors(t.ID, t.vectorSpace(out.Preparing), hashes) {
		if len(vector) == 0 {
			continue
		}
		if out.Dimensions != 0 && len(vector) != out.Dimensions {
			out.Problem = "Vector dimensions changed. Configure a new model revision and rebuild the index."
			return out
		}
		out.Dimensions = len(vector)
		out.Completed++
	}
	out.Ready = out.Completed == out.Total && out.Problem == ""
	return out
}

func (t *Tenant) KnowledgeIndexStatus(m platform.Member) (KnowledgeIndexStatus, *kernel.Error) {
	return recordRead(t, context.Background(), func() (KnowledgeIndexStatus, *kernel.Error) {
		c := platform.NewCaller(runtime{t}, m, PlatformApp, false, false)
		if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/ai/knowledge-index", "read", []string{Admin}, true)); err != nil {
			return KnowledgeIndexStatus{}, err
		}
		return t.knowledgeIndexStatus(), nil
	})
}

func (t *Tenant) checkEmbeddingSetting(id, value string) *kernel.Error {
	if id != platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingNext && id != platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel {
		return nil
	}
	if value == "" {
		return nil
	}
	_, pv, err := t.resolveModel("embedding", value, platform.ModelOptions{})
	if err != nil {
		return err
	}
	if pv.Wire != "openai" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "This provider adapter does not support embeddings")
	}
	if id == platform.KnowledgeApp+"/"+platform.SettingKnowledgeEmbeddingModel {
		status := t.knowledgeIndexStatus()
		if status.Preparing != value || !status.Ready {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Build this model's index in AI configuration before activating it")
		}
	}
	return nil
}

func consistentVectors(vectors [][]float32, expected int) error {
	for _, v := range vectors {
		if len(v) == 0 {
			return fmt.Errorf("The provider returned an empty embedding")
		}
		if expected == 0 {
			expected = len(v)
		}
		if len(v) != expected {
			return fmt.Errorf("Vector dimensions changed. Configure a new model revision and rebuild the index.")
		}
	}
	return nil
}
