package aistate

import (
	"bytes"
	"encoding/json"
	"testing"

	"platformserver/platform"
)

func TestStatePreservesSavedFormat(t *testing.T) {
	const original = `{"providers":[{"id":"local","kind":"local","baseUrl":"http://localhost:1234","wire":"openai"}],"models":[{"provider":"local","model":"model","access":"everyone"}],"limits":{"member":{"member":"member","dailyTokens":100,"perMinute":5}},"usage":null,"daily":{"2026-10-11|member":17}}`
	state, err := Decode(json.RawMessage(original))
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(state)
	if err != nil || string(encoded) != original {
		t.Fatal("pure state changed original saved field names, order or omitted values", string(encoded), err)
	}
	empty, err := json.Marshal(State{})
	if err != nil || string(empty) != `{"providers":null,"models":null,"usage":null}` {
		t.Fatal("initial owner image changed", string(empty), err)
	}
}

func TestApplyUsageStateUsesStrictAcceptedStateDecoder(t *testing.T) {
	for _, raw := range []string{`{"unknown":true}`, `{} {}`, `{"providers":[{"id":"same"},{"id":"same"}]}`, `{"usage":"invalid"}`} {
		image := json.RawMessage(raw)
		before := bytes.Clone(image)
		if next, err := ApplyUsageState(image, platform.AIUsage{}); err == nil || next != nil {
			t.Fatal("invalid saved owner state was accepted", raw, err)
		}
		if !bytes.Equal(image, before) {
			t.Fatal("rejected saved state was modified")
		}
	}
	if next, err := ApplyUsageState(json.RawMessage(`{}`), platform.AIUsage{}); err != nil || len(next) == 0 {
		t.Fatal("existing empty owner image cannot be metered", err)
	}
}
