package platformserver

import (
	"encoding/json"
	"fmt"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

func pendingComputeEffects(t *Tenant) []platform.Effect {
	var out []platform.Effect
	for _, effect := range t.outbound {
		if effect.Endpoint == operationEndpoint && !settled(effect.State) {
			out = append(out, effect.Effect)
		}
	}
	return out
}
func settleComputeFixture(t *Tenant, actor platform.Member, fixture build.ComputeFixture, now time.Time) (bool, error) {
	var matched []platform.Effect
	for _, effect := range pendingComputeEffects(t) {
		var binding operationBinding
		if json.Unmarshal([]byte(effect.Body), &binding) == nil && binding.Definition.Name == fixture.Name && (fixture.App == "" || fixture.App == effect.App) {
			matched = append(matched, effect)
		}
	}
	if len(matched) == 0 {
		return false, nil
	}
	if len(matched) != 1 {
		return false, fmt.Errorf("one fixture needs exactly one matching pending compute call")
	}
	var binding operationBinding
	json.Unmarshal([]byte(matched[0].Body), &binding)
	outcome := platform.Outcome{Effect: matched[0].ID, Result: "rejected", Detail: fixture.Error}
	if fixture.ExpectState == "completed" {
		if err := binding.Definition.Output.Validate([]byte(fixture.Output), binding.Definition.Limits.MaxOutputBytes); err != nil {
			return false, err
		}
		outcome.Result = "delivered"
		outcome.Answer = json.RawMessage(fixture.Output)
	}
	claimed, ok := t.claimOperation(matched[0].ID, now)
	if !ok {
		return false, fmt.Errorf("compute fixture could not claim original work")
	}
	outcome.Generation = claimed.Generation
	t.settleWithUsage(claimed.ID, outcome, nil, now)
	if t.quarantined() {
		return false, fmt.Errorf("compute fixture settlement failed")
	}
	result, refusal := t.operationResult(platform.NewCaller(runtime{t}, actor, PlatformApp, false, false), claimed.ID)
	if refusal != nil {
		return false, refusal
	}
	if result.State != fixture.ExpectState {
		return false, nil
	}
	if fixture.ExpectOutput != "" {
		actual, err := canonicalDigest(result.Output)
		if err != nil {
			return false, err
		}
		expected, err := canonicalDigest(json.RawMessage(fixture.ExpectOutput))
		return err == nil && actual == expected, err
	}
	return true, nil
}
