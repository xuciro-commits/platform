package host

import (
	"encoding/json"
	trialowner "platformserver/services/devops/trials"
	"platformserver/services/functions/runtime"
	controlowner "platformserver/services/security/control"

	"time"

	"platformserver/platform"
)

func (t *Tenant) outboundEffects() []platform.Effect {
	var out []platform.Effect
	for _, queued := range t.outbound.Select(nil) {
		out = append(out, queued.Effect)
	}
	return out
}

func settleComputeFixture(t *Tenant, actor platform.Member, fixture platform.ComputeFixture, now time.Time) (bool, error) {
	return trialowner.ComputeFixtureHooks{
		Outbound: t.outboundEffects,
		Binding: func(effect platform.Effect) (platform.Operation, bool) {
			var binding compute.OperationBinding
			err := json.Unmarshal([]byte(effect.Body), &binding)
			return binding.Definition, err == nil
		},
		Claim: func(effect string) (platform.Effect, bool) { return t.claimOperation(effect, now) },
		Settle: func(id string, outcome platform.Outcome) bool {
			t.settleWithUsage(id, outcome, nil, now)
			return !t.quarantined()
		},
		Result: func(id string) (string, json.RawMessage, error) {
			result, refusal := t.operationResult(platform.NewCaller(runtime{t: t, scoped: true}, actor, controlowner.PlatformApp, false, false), id)
			if refusal != nil {
				return "", nil, refusal
			}
			return result.State, json.RawMessage(result.Output), nil
		},
	}.Apply(fixture)
}
