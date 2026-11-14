package trials

import (
	"encoding/json"
	"fmt"
	"strings"

	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type FunctionCall struct {
	Effect platform.Effect
	Record string
}

type FunctionFixtureHooks struct {
	Outbound func() []platform.Effect
	Ask      func(platform.Effect) (record string, function bool)
	Settle   func(call FunctionCall, response []byte) error
	Run      func(actor platform.Member, kind, id string) (state string, output json.RawMessage, found bool)
}

func (h FunctionFixtureHooks) Apply(actor platform.Member, fixture platform.FunctionFixture) (bool, error) {
	pending := PendingFunctionCalls(h.Outbound(), h.Ask)
	if len(pending) == 0 {
		return false, nil
	}
	if len(pending) != 1 {
		return false, fmt.Errorf("one fixed answer needs exactly one pending function call")
	}
	response, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]string{"content": fixture.Output}}},
		"usage":   map[string]int{"prompt_tokens": fixture.InputTokens, "completion_tokens": fixture.OutputTokens},
	})
	if err != nil {
		return false, err
	}
	if err := h.Settle(pending[0], response); err != nil {
		return false, err
	}
	kind, id, ok := strings.Cut(pending[0].Record, "/")
	if !ok || kind != build.FunctionCallType {
		return false, fmt.Errorf("fixture reply is not a builder function call")
	}
	state, output, found := h.Run(actor, kind, id)
	if !found || state != fixture.ExpectState {
		return false, nil
	}
	if fixture.ExpectOutput != "" {
		actual, err := platform.CanonicalDigest(output)
		if err != nil {
			return false, nil
		}
		expected, err := platform.CanonicalDigest(json.RawMessage(fixture.ExpectOutput))
		return err == nil && actual == expected, nil
	}
	return true, nil
}

type ComputeFixtureHooks struct {
	Outbound func() []platform.Effect
	Binding  func(effect platform.Effect) (platform.Operation, bool)
	Claim    func(effect string) (platform.Effect, bool)
	Settle   func(id string, outcome platform.Outcome) bool
	Result   func(id string) (state string, output json.RawMessage, err error)
}

func (h ComputeFixtureHooks) Apply(fixture platform.ComputeFixture) (bool, error) {
	var matched []platform.Effect
	for _, effect := range PendingCompute(h.Outbound()) {
		if definition, ok := h.Binding(effect); ok && definition.Name == fixture.Name && (fixture.App == "" || fixture.App == effect.App) {
			matched = append(matched, effect)
		}
	}
	if len(matched) == 0 {
		return false, nil
	}
	if len(matched) != 1 {
		return false, fmt.Errorf("one fixture needs exactly one matching pending compute call")
	}
	effect := matched[0]
	definition, _ := h.Binding(effect)
	outcome := platform.Outcome{Effect: effect.ID, Result: "rejected", Detail: fixture.Error}
	if fixture.ExpectState == "completed" {
		if err := definition.Output.Validate([]byte(fixture.Output), definition.Limits.MaxOutputBytes); err != nil {
			return false, err
		}
		outcome.Result = "delivered"
		outcome.Answer = json.RawMessage(fixture.Output)
	}
	claimed, ok := h.Claim(effect.ID)
	if !ok {
		return false, fmt.Errorf("compute fixture could not claim original work")
	}
	outcome.Generation = claimed.Generation
	if !h.Settle(claimed.ID, outcome) {
		return false, fmt.Errorf("compute fixture settlement failed")
	}
	state, output, err := h.Result(claimed.ID)
	if err != nil {
		return false, err
	}
	if state != fixture.ExpectState {
		return false, nil
	}
	if fixture.ExpectOutput != "" {
		actual, err := platform.CanonicalDigest(output)
		if err != nil {
			return false, err
		}
		expected, err := platform.CanonicalDigest(json.RawMessage(fixture.ExpectOutput))
		return err == nil && actual == expected, err
	}
	return true, nil
}

func PendingCompute(effects []platform.Effect) []platform.Effect {
	var out []platform.Effect
	for _, effect := range effects {
		if effect.Endpoint == acceptedformat.OperationEndpoint && !platform.EffectSettled(effect.State) {
			out = append(out, effect)
		}
	}
	return out
}

func PendingFunctionCalls(effects []platform.Effect, ask func(platform.Effect) (record string, function bool)) []FunctionCall {
	var pending []FunctionCall
	for _, effect := range effects {
		if effect.Endpoint != acceptedformat.ModelEndpoint || platform.EffectSettled(effect.State) {
			continue
		}
		if record, function := ask(effect); function {
			pending = append(pending, FunctionCall{Effect: effect, Record: record})
		}
	}
	return pending
}
