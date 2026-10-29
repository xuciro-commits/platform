package aigateway

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"platformkernel/kernel"
	"platformserver/aigateway/providers"
	"platformserver/platform"
	"time"
)

type Calls struct {
	Inference  platform.AIInferencePolicy
	MostTokens func() int
	Reserve    func(platform.Member, platform.AIModel, platform.ChatRequest, int, int, time.Time) *platform.AIError
	Release    func(string, time.Time) *kernel.Error
	Available  func(string, time.Time, bool) bool
	Report     func(string, bool, time.Time)
	Provider   func(platform.AIProvider, func()) providers.Client
	Transcribe func(time.Time, string, string, string, []byte, []byte, string)
}

func Unavailable(e *platform.AIError) bool {
	return e.Status == 0 || e.Status == http.StatusTooManyRequests || e.Status >= 500
}

func (g Calls) Call(pv platform.AIProvider, model platform.AIModel, m platform.Member, req platform.ChatRequest, now time.Time) (platform.ChatAnswer, *platform.AIError) {
	var binding platform.AIModelBinding
	var configErr error
	purpose := "generation"
	if m.Agent {
		purpose = "agent"
	}
	if req.Binding != nil && req.Binding.Purpose != "" {
		purpose = req.Binding.Purpose
	}
	if req.Binding != nil {
		binding = *req.Binding
		frozen := platform.AIModel{Provider: model.Provider, Model: model.Model, Capabilities: binding.Capabilities, Defaults: binding.Options, Revision: binding.Revision}
		_, configErr = frozen.Resolve(purpose, platform.ModelOptions{}, g.MostTokens())
		if configErr == nil {
			model.Defaults = platform.ModelOptions{}
			_, configErr = model.Resolve(purpose, binding.Options, g.MostTokens())
		}
	} else {
		options := req.Options
		if req.MaxTokens > 0 {
			options.MaxTokens = req.MaxTokens
		}
		if ceiling := g.MostTokens(); ceiling > 0 && options.MaxTokens > ceiling && req.Options.MaxTokens == 0 {
			options.MaxTokens = ceiling
		}
		binding, configErr = model.Resolve(purpose, options, g.MostTokens())
	}
	if configErr == nil {
		configErr = Check(g.Inference, pv, binding)
	}
	if binding.Provider != nil && (binding.Provider.Wire != pv.Wire || binding.Provider.Vendor != pv.Vendor || binding.Provider.BaseURL != pv.BaseURL) {
		configErr = fmt.Errorf("The provider protocol or address changed; start a new request")
	}
	req.MaxTokens, req.Options = binding.Options.MaxTokens, binding.Options
	req.Binding = &binding
	input, _ := json.Marshal(struct {
		Messages []map[string]any
		Tools    []platform.Tool
	}{providers.OpenAIMessages(req.Messages), req.Tools})
	estimated := platform.EstimateModelTokens(string(input))
	if configErr == nil && binding.Capabilities != nil && binding.Capabilities.InputTokens != nil && estimated > *binding.Capabilities.InputTokens {
		configErr = fmt.Errorf("Input exceeds the model input limit; shorten the prompt or history")
	}
	if configErr == nil && req.Options.InputTokens > 0 && estimated > req.Options.InputTokens {
		configErr = fmt.Errorf("Input exceeds the configured token budget; shorten the prompt or history")
	}
	if configErr == nil && binding.Capabilities != nil && binding.Capabilities.ContextTokens != nil && estimated+req.MaxTokens > *binding.Capabilities.ContextTokens {
		configErr = fmt.Errorf("Input and generation exceed the model context window; shorten the input or generation budget")
	}
	if configErr == nil && req.MaxTokens <= 0 {
		configErr = fmt.Errorf("Set a positive generation token limit before calling this model")
	}
	if configErr != nil {
		return platform.ChatAnswer{}, &platform.AIError{Status: 400, Detail: configErr.Error()}
	}
	if !g.Available(pv.ID, now, false) {
		return platform.ChatAnswer{}, &platform.AIError{Status: 503, Detail: "the provider " + pv.ID + " failed repeatedly"}
	}
	if req.Call == "" {
		req.Call = "chat:" + rand.Text()
	}
	inputBound := len(input) + 32*(len(req.Messages)+len(req.Tools))
	protected := []json.RawMessage{}
	for _, message := range req.Messages {
		inputBound += len(message.Protocol)
		protected = append(protected, message.Protocol)
	}
	identity, _ := json.Marshal([]any{json.RawMessage(input), binding, protected})
	req.RequestHash = fmt.Sprintf("%x", sha256.Sum256(identity))
	if failure := g.Reserve(m, model, req, inputBound, req.MaxTokens, now); failure != nil {
		return platform.ChatAnswer{}, failure
	}
	if !g.Available(pv.ID, now, true) {
		if refusal := g.Release(req.Call, now); refusal != nil {
			return platform.ChatAnswer{}, &platform.AIError{Status: 503, Detail: refusal.Error()}
		}
		return platform.ChatAnswer{}, &platform.AIError{Status: 503, Detail: "the provider " + pv.ID + " failed repeatedly"}
	}
	started := time.Now()
	dispatched := false
	provider := g.Provider(pv, func() { dispatched = true })
	complete := provider.Complete
	if pv.Wire == "anthropic" {
		complete = provider.CompleteAnthropic
	}

	answer, u, failure := complete(pv, model.Model, req)
	g.Report(pv.ID, failure == nil || !Unavailable(failure), now)
	if !dispatched {
		if refusal := g.Release(req.Call, now); refusal != nil {
			return platform.ChatAnswer{}, &platform.AIError{Status: 503, Detail: refusal.Error()}
		}
		return answer, failure
	}
	if !u.TokensReported {
		request, _ := json.Marshal(req)
		said := answer.Content
		for _, c := range answer.ToolCalls {
			said += string(c.Arguments)
		}
		u.Input, u.Output, u.TokensEstimated = platform.EstimateModelTokens(string(request)), platform.EstimateModelTokens(said), true
		knownCache := 0
		for _, count := range []*int{u.CacheReadInput, u.CacheWriteInput} {
			if count != nil {
				knownCache += *count
			}
		}
		u.Input = max(u.Input, knownCache)
	}
	if u.TokensReported && (u.Input > inputBound || u.Output > req.MaxTokens) {
		failure = &platform.AIError{Status: http.StatusUnprocessableEntity, Detail: "The provider reported usage above the reserved token bound"}
		answer.ToolCalls, answer.Protocol = nil, nil
	}
	u.Call, u.Run = req.Call, req.Run
	u.At, u.Member, u.Agent, u.Model, u.Millis, u.Outcome = now, m.ID, m.Agent, model.Name(), time.Since(started).Milliseconds(), "ok"
	if failure != nil {
		u.Outcome = failure.Detail
		if len(u.Outcome) > 300 {
			u.Outcome = u.Outcome[:300]
		}
	}
	answer.Usage = u
	request, _ := json.Marshal(req)
	reply, _ := json.Marshal(answer)
	g.Transcribe(now, m.ID, model.Name(), req.Run, request, reply, u.Outcome)
	return answer, failure
}
