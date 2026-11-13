package trials

import (
	"encoding/json"
	"fmt"
	"strings"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

type FunctionCall struct {
	Effect platform.Effect
	Record string
}

type FunctionFixtureHooks struct {
	Pending func() []FunctionCall
	Settle  func(call FunctionCall, response []byte) error
	Run     func(actor platform.Member, kind, id string) (state string, output json.RawMessage, found bool)
}

func (h FunctionFixtureHooks) Apply(actor platform.Member, fixture platform.FunctionFixture) (bool, error) {
	pending := h.Pending()
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

func ComputeFixtureResult(matched []platform.Effect, fixture platform.ComputeFixture, run func(platform.Effect) (state string, output json.RawMessage, err error)) (bool, error) {
	if len(matched) == 0 {
		return false, nil
	}
	if len(matched) != 1 {
		return false, fmt.Errorf("one fixture needs exactly one matching pending compute call")
	}
	state, output, err := run(matched[0])
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
