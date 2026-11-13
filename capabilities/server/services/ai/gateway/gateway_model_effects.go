package aigateway

import (
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	"strconv"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	agents "platformserver/services/ai/runtime"
)

type ModelAsk struct {
	Model              string                   `json:"model"`
	Binding            *platform.AIModelBinding `json:"binding,omitempty"`
	ConfigurationError string                   `json:"configurationError,omitempty"`
	Prompt             platform.Prompt          `json:"prompt"`
	Reply              string                   `json:"reply"`
	Record             string                   `json:"record"`
	Call               string                   `json:"call"`
	Function           *agents.FunctionBinding  `json:"function,omitempty"`
	Evaluation         bool                     `json:"evaluation,omitempty"`
	EvaluationConfig   string                   `json:"evaluationConfig,omitempty"`
	EvaluationFunction *platform.AIFunction     `json:"evaluationFunction,omitempty"`
}

func (ask ModelAsk) Effect(tenant, app string, rec *pb.ChangeRecord, target string, n int, endpoint string) platform.Effect {
	body, _ := json.Marshal(ask)
	key := fmt.Sprintf("%s#%d", rec.GetChangeId(), n)
	return platform.Effect{ID: fmt.Sprintf("%s:%s:model:%s", tenant, app, key), Endpoint: endpoint,
		Event: app + "/model", App: app, Key: key, Target: target, At: rec.GetRecordedTime().AsTime(), State: "pending", Due: rec.GetRecordedTime().AsTime(), Body: string(body)}
}

func (ask ModelAsk) Request(x platform.Effect) platform.ChatRequest {
	messages := []platform.Message{{Role: "user", Content: ask.Prompt.User}}
	if ask.Prompt.System != "" {
		messages = append([]platform.Message{{Role: "system", Content: ask.Prompt.System}}, messages...)
	}
	return platform.ChatRequest{Model: ask.Model, Messages: messages, MaxTokens: ask.Prompt.MaxTokens, Binding: ask.Binding, Run: x.ID, Call: fmt.Sprintf("effect:%s:%d", x.ID, x.Attempts)}
}

func (ask ModelAsk) Outcome(out platform.Outcome, answer platform.ChatAnswer, failure *platform.AIError) (platform.Outcome, *platform.AIUsage) {
	switch {
	case failure != nil && (failure.Quota || failure.Status == 409 || strings.Contains(failure.Detail, "failed repeatedly")):
		out.Result, out.Detail = "rejected", failure.Detail
	case failure != nil && Unavailable(failure):
		out.Result, out.Detail = "retry", failure.Detail
	case failure != nil:
		out.Result, out.Detail = "rejected", failure.Detail
	default:
		out.Result, out.Answer = "delivered", json.RawMessage(strconv.Quote(answer.Content))
		definition := ask.EvaluationFunction
		if ask.Function != nil {
			definition = &ask.Function.Definition
		}
		if definition != nil && (len(answer.ToolCalls) != 0 || answer.Usage.Output > definition.MaxTokens ||
			definition.ValidateOutput([]byte(answer.Content)) != nil) {
			out.Result, out.Detail, out.Answer = "rejected", "The AI function answer failed its type or budget checks", nil
		}
	}
	if answer.Usage.Model == "" {
		return out, nil
	}
	return out, &answer.Usage
}

func (ask ModelAsk) Submission(tenant string, x platform.Effect, o platform.Outcome, usage *platform.AIUsage, authority func(string) string) *pb.Submission {
	answer := platform.Answer{Call: ask.Call, Action: "ask", Outcome: "accepted"}
	if (ask.Function != nil || ask.Evaluation) && usage != nil {
		answer.Metered, answer.TokensReported = true, usage.TokensReported
		answer.InputTokens, answer.OutputTokens = usage.Input, usage.Output
		answer.CostReported, answer.CostUSD = usage.CostReported, usage.Cost
		answer.LatencyMillis, answer.ServedModel = usage.Millis, usage.Served
	}
	if x.State == "delivered" {
		json.Unmarshal(o.Answer, &answer.Text)
	} else {
		answer.Outcome, answer.Code = "refused", o.Detail
	}
	typ, id, _ := strings.Cut(ask.Record, "/")
	payload, _ := json.Marshal(answer)
	return &pb.Submission{TenantId: tenant, PrincipalId: "app:" + x.App, Authority: authority(typ), Target: &pb.EntityRef{Type: typ, Id: id},
		Schema: &pb.SchemaRef{Name: ask.Reply, Version: 1}, IdempotencyKey: "answer:" + x.ID, Payload: payload}
}

func (ask ModelAsk) EvaluationDrift(model platform.AIModel, provider platform.AIProvider, rebind func(platform.ModelOptions) (platform.AIModelBinding, *kernel.Error)) string {
	f := ask.EvaluationFunction
	if f == nil || f.Check() != nil || ask.Prompt.System != f.SystemPrompt() || ask.Prompt.MaxTokens != f.MaxTokens || len(ask.Prompt.User) > f.MaxInputBytes {
		return "the evaluation function binding changed"
	}
	var evaluated *platform.AIModelBinding
	if ask.Binding != nil {
		current, refusal := rebind(f.ModelOptions.Override(platform.ModelOptions{MaxTokens: f.MaxTokens}))
		if refusal != nil {
			return refusal.Message
		}
		evaluated = &current
	}
	config, err := ConfigurationDigest(model, provider, evaluated)
	if err != nil || ask.EvaluationConfig == "" || ask.EvaluationConfig != config {
		return "the evaluation model configuration changed"
	}
	return ""
}

func AnswerSubmission(tenant string, x platform.Effect, o platform.Outcome, usage *platform.AIUsage, app func(string) platform.App, authority func(string) string) (platform.App, *pb.Submission) {
	var ask ModelAsk
	json.Unmarshal([]byte(x.Body), &ask)
	owner := app(x.App)
	if owner == nil || ask.Reply == "" {
		return nil, nil
	}
	return owner, ask.Submission(tenant, x, o, usage, authority)
}
