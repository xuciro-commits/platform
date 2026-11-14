package host

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	aigateway "platformserver/services/ai/gateway"
	trialowner "platformserver/services/devops/trials"
	controlowner "platformserver/services/security/control"
	"time"

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
	admin, submissions, err := controlowner.FixtureModelSubmissions(t.ID, model)
	if err != nil {
		return err
	}
	for _, s := range submissions {
		if _, refusal := t.Submit(admin, s, at); refusal != nil {
			return fmt.Errorf("configure fixture model: %s", refusal.Message)
		}
	}
	return nil
}

func (t *Tenant) modelAsk(effect platform.Effect) (string, bool) {
	var ask aigateway.ModelAsk
	err := json.Unmarshal([]byte(effect.Body), &ask)
	return ask.Record, err == nil && ask.Function != nil
}

func settleFunctionFixture(t *Tenant, actor platform.Member, fixture platform.FunctionFixture, now time.Time) (bool, error) {
	return trialowner.FunctionFixtureHooks{
		Outbound: t.outboundEffects,
		Ask:      t.modelAsk,
		Settle: func(call trialowner.FunctionCall, response []byte) error {
			client := t.AIClient
			defer func() { t.AIClient = client }()
			t.AIClient = func(*http.Request) (*http.Response, error) {
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(response))}, nil
			}
			outcome, usage := t.gatewayRuntime().Send(call.Effect, now)
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
