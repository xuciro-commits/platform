package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/ai/gateway"
	trialowner "platformserver/services/devops/trials"
	controlowner "platformserver/services/security/control"

	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

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
	admin := platform.Member{ID: "app:candidate-test", Tenant: t.ID, Roles: map[string]string{controlowner.PlatformApp: controlowner.Admin, platform.AIApp: platform.AIAdmin}}
	settings := []struct {
		app, schema, kind, id string
		payload               any
	}{
		{platform.AIApp, platform.AISchemaProviderAdd, platform.AIProviderType, provider, map[string]string{"kind": "local", "baseUrl": "http://candidate.invalid", "wire": "openai"}},
		{platform.AIApp, platform.AISchemaModelEnable, platform.AIModelType, model, map[string]string{"access": "users"}},
		{controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "ai/app-model", map[string]string{"value": model}},
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

func pendingFunctionCalls(t *Tenant) []trialowner.FunctionCall {
	var pending []trialowner.FunctionCall
	for _, effect := range t.outbound.Select(nil) {
		var ask aigateway.ModelAsk
		if effect.Endpoint == acceptedformat.ModelEndpoint && !platform.EffectSettled(effect.State) && json.Unmarshal([]byte(effect.Body), &ask) == nil && ask.Function != nil {
			pending = append(pending, trialowner.FunctionCall{Effect: effect.Effect, Record: ask.Record})
		}
	}
	return pending
}

func settleFunctionFixture(t *Tenant, actor platform.Member, fixture platform.FunctionFixture, now time.Time) (bool, error) {
	return trialowner.FunctionFixtureHooks{
		Pending: func() []trialowner.FunctionCall { return pendingFunctionCalls(t) },
		Settle: func(call trialowner.FunctionCall, response []byte) error {
			client := t.AIClient
			defer func() { t.AIClient = client }()
			t.AIClient = func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(response))}, nil
			}
			outcome, usage := t.sendModel(call.Effect, now)
			if usage != nil {
				usage.Millis = 0
			}
			t.settleWithUsage(call.Effect.ID, outcome, usage, now)
			if t.quarantined() {
				return fmt.Errorf("fixture settlement failed")
			}
			return nil
		},
		Run: func(actor platform.Member, kind, id string) (string, json.RawMessage, bool) {
			view, refusal := t.recordOf(actor, kind, id, now)
			if refusal != nil {
				return "", nil, false
			}
			run, ok := view.Record.(build.FunctionRun)
			return run.State, json.RawMessage(run.Output), ok
		},
	}.Apply(actor, fixture)
}
