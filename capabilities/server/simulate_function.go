package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/build"
	"platformserver/platform"
)

// Candidate tests never inherit live transports or bindings. A fixture replaces
// only the provider response; authorization, parsing, metering and replies use
// the same model effect path as a live call.
func configureFunctionFixtureModel(t *Tenant, model string, at time.Time) error {
	t.AIClient = func(*http.Request) (*http.Response, error) {
		return nil, fmt.Errorf("candidate model call has no fixed answer")
	}
	if model == "" {
		return nil
	}
	provider, name, ok := strings.Cut(model, "/")
	if !ok || provider == "" || name == "" {
		return fmt.Errorf("fixture model must identify provider/model")
	}
	admin := platform.Member{ID: "app:candidate-test", Tenant: t.ID, Roles: map[string]string{PlatformApp: Admin, ai.ID: ai.Admin}}
	settings := []struct {
		app, schema, kind, id string
		payload               any
	}{
		{ai.ID, ai.SchemaProviderAdd, ai.ProviderType, provider, map[string]string{"kind": "local", "baseUrl": "http://candidate.invalid", "wire": "openai"}},
		{ai.ID, ai.SchemaModelEnable, ai.ModelType, model, map[string]string{"access": "users"}},
		{PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": model}},
	}
	for i, setting := range settings {
		payload, _ := json.Marshal(setting.payload)
		_, err := t.Submit(admin, &pb.Submission{TenantId: t.ID, PrincipalId: admin.ID, Authority: setting.app,
			IdempotencyKey: fmt.Sprintf("fixture-model-%d", i), Target: &pb.EntityRef{Type: setting.kind, Id: setting.id},
			Schema: &pb.SchemaRef{Name: setting.schema, Version: 1}, Payload: payload}, at)
		if err != nil {
			return fmt.Errorf("configure fixture model: %s", err.Message)
		}
	}
	return nil
}

func pendingFunctionEffects(t *Tenant) []platform.Effect {
	var pending []platform.Effect
	for _, effect := range t.outbound {
		var ask modelAsk
		if effect.Endpoint == modelEndpoint && !settled(effect.State) && json.Unmarshal([]byte(effect.Body), &ask) == nil && ask.Function != nil {
			pending = append(pending, effect.Effect)
		}
	}
	return pending
}

func settleFunctionFixture(t *Tenant, actor platform.Member, fixture build.FunctionFixture, now time.Time) (bool, error) {
	pending := pendingFunctionEffects(t)
	if len(pending) == 0 {
		return false, nil
	}
	if len(pending) != 1 {
		return false, fmt.Errorf("one fixed answer needs exactly one pending function call")
	}
	effect := pending[0]
	var ask modelAsk
	if err := json.Unmarshal([]byte(effect.Body), &ask); err != nil {
		return false, err
	}
	response, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]string{"content": fixture.Output}}},
		"usage":   map[string]int{"prompt_tokens": fixture.InputTokens, "completion_tokens": fixture.OutputTokens},
	})
	if err != nil {
		return false, err
	}
	client := t.AIClient
	defer func() { t.AIClient = client }()
	t.AIClient = func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(response))}, nil
	}
	outcome, usage := t.sendModel(effect, now)
	// Fixed fixtures replay the same logical input; wall-clock transport time
	// from this in-process stand-in must not change their deterministic result.
	if usage != nil {
		usage.Millis = 0
	}
	t.settleWithUsage(effect.ID, outcome, usage, now)
	if t.quarantined() {
		return false, fmt.Errorf("fixture settlement failed")
	}
	kind, id, ok := strings.Cut(ask.Record, "/")
	if !ok || kind != build.FunctionCallType {
		return false, fmt.Errorf("fixture reply is not a builder function call")
	}
	view, refusal := t.RecordOf(actor, kind, id, now)
	if refusal != nil {
		return false, nil
	}
	run, ok := view.Record.(build.FunctionRun)
	if !ok || run.State != fixture.ExpectState {
		return false, nil
	}
	if fixture.ExpectOutput != "" {
		actual, err := canonicalDigest(json.RawMessage(run.Output))
		if err != nil {
			return false, nil
		}
		expected, err := canonicalDigest(json.RawMessage(fixture.ExpectOutput))
		return err == nil && actual == expected, nil
	}
	return true, nil
}
