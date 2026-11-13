package host

import (
	"encoding/json"
	"fmt"
	acceptedformat "platformserver/kernel/acceptedformat"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"

	"time"

	"platformserver/platform"
)

func pendingComputeEffects(t *Tenant) []platform.Effect {
	var out []platform.Effect
	for _, effect := range t.outbound.Select(nil) {
		if effect.Endpoint == acceptedformat.OperationEndpoint && !platform.EffectSettled(effect.State) {
			out = append(out, effect.Effect)
		}
	}
	return out
}
func settleComputeFixture(t *Tenant, actor platform.Member, fixture platform.ComputeFixture, now time.Time) (bool, error) {
	var matched []platform.Effect
	for _, effect := range pendingComputeEffects(t) {
		var binding compute.OperationBinding
		if json.Unmarshal([]byte(effect.Body), &binding) == nil && binding.Definition.Name == fixture.Name && (fixture.App == "" || fixture.App == effect.App) {
			matched = append(matched, effect)
		}
	}
	return trialowner.ComputeFixtureResult(matched, fixture, func(effect platform.Effect) (string, json.RawMessage, error) {
		var binding compute.OperationBinding
		json.Unmarshal([]byte(effect.Body), &binding)
		outcome := platform.Outcome{Effect: effect.ID, Result: "rejected", Detail: fixture.Error}
		if fixture.ExpectState == "completed" {
			if err := binding.Definition.Output.Validate([]byte(fixture.Output), binding.Definition.Limits.MaxOutputBytes); err != nil {
				return "", nil, err
			}
			outcome.Result = "delivered"
			outcome.Answer = json.RawMessage(fixture.Output)
		}
		claimed, ok := t.claimOperation(effect.ID, now)
		if !ok {
			return "", nil, fmt.Errorf("compute fixture could not claim original work")
		}
		outcome.Generation = claimed.Generation
		t.settleWithUsage(claimed.ID, outcome, nil, now)
		if t.quarantined() {
			return "", nil, fmt.Errorf("compute fixture settlement failed")
		}
		result, refusal := t.operationResult(platform.NewCaller(runtime{t: t, scoped: true}, actor, controlowner.PlatformApp, false, false), claimed.ID)
		if refusal != nil {
			return "", nil, refusal
		}
		return result.State, json.RawMessage(result.Output), nil
	})
}
