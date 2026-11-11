package aigateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	knowledgeindex "platformserver/services/ai/retrieval"
	"time"

	"platformserver/platform"
)

func (g Calls) Embed(member platform.Member, name string, model platform.AIModel, pv platform.AIProvider, input []string, now time.Time) ([][]float32, error) {
	if pv.Wire == "anthropic" {
		return nil, fmt.Errorf("the provider %s does not embed", pv.ID)
	}
	if !g.Available(pv.ID, now, false) {
		return nil, fmt.Errorf("the provider %s failed repeatedly; embedding waits", pv.ID)
	}
	inputTokens := 0
	for _, text := range input {
		inputTokens += len(text) + 32
	}
	call := "embedding:" + rand.Text()
	identity, _ := json.Marshal(input)
	digest := fmt.Sprintf("%x", sha256.Sum256(identity))
	if why := g.Reserve(member, model, platform.ChatRequest{Call: call, RequestHash: digest}, inputTokens, 0, now); why != nil {
		return nil, why
	}
	if !g.Available(pv.ID, now, true) {
		if refusal := g.Release(call, now); refusal != nil {
			return nil, refusal
		}
		return nil, fmt.Errorf("the provider %s failed repeatedly; embedding waits", pv.ID)
	}
	started := time.Now()
	dispatched := false
	status, answer, failure := g.Provider(pv, func() { dispatched = true }).Embeddings(pv, model.Model, input)
	g.Report(pv.ID, failure == nil && status < 500 && status != http.StatusTooManyRequests, now)
	if !dispatched {
		if why := g.Release(call, now); why != nil {
			return nil, why
		}
		if failure != nil {
			return nil, failure
		}
		return nil, fmt.Errorf("The embedding call was not dispatched")
	}
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Usage struct {
			Prompt *int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	u := platform.AIUsage{Call: call, At: now, Member: member.ID, Model: name, Millis: time.Since(started).Milliseconds(), Outcome: "ok"}
	switch {
	case failure != nil:
		u.Outcome = failure.Detail
	case status/100 != 2 || json.Unmarshal(answer, &out) != nil || len(out.Data) != len(input):
		u.Outcome = fmt.Sprintf("HTTP %d: no embeddings", status)
	}
	if out.Usage.Prompt != nil {
		u.Input, u.TokensReported = *out.Usage.Prompt, true
	} else {
		for _, text := range input {
			u.Input += platform.EstimateModelTokens(text)
		}
		u.TokensEstimated = u.Input > 0
	}
	if err := g.Meter(member, u); err != nil {
		return nil, fmt.Errorf("the embedding usage was not accepted: %v", err)
	}
	if u.Outcome != "ok" {
		return nil, fmt.Errorf("%s", u.Outcome)
	}
	if u.TokensReported && u.Input > inputTokens {
		return nil, fmt.Errorf("The provider reported usage above the reserved token bound")
	}
	vs := make([][]float32, len(input))
	for _, d := range out.Data {
		if d.Index >= 0 && d.Index < len(vs) {
			vs[d.Index] = d.Embedding
		}
	}
	if err := knowledgeindex.ConsistentVectors(vs, 0); err != nil {
		return nil, err
	}
	return vs, nil
}
