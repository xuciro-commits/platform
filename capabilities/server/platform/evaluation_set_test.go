package platform

import (
	"encoding/json"
	"testing"
)

func TestEvaluationPolicyKeepsExactDefaultsAndDeclaredFields(t *testing.T) {
	f := RecordAdviceFunction("probe.record", []string{"title"}, []string{"reader"})
	expected := []byte(`{"summary":"Reference explanation","category":"review","review":true}`)
	actual := []byte(`{"summary":"Another useful explanation","category":"review","review":true}`)
	p := EvaluationPolicy{MinQuality: 1, MaxCostUSD: 0.1, MaxLatencyMillis: 1000, Cases: []EvaluationCase{{Name: "review", Input: json.RawMessage(`{"title":"Synthetic"}`), Expected: expected}}}
	if err := f.CheckEvaluationPolicy(p); err != nil || p.Matches(actual, expected) || !p.Matches(expected, expected) {
		t.Fatalf("default comparison stopped being an exact full answer: %v", err)
	}
	p.CompareFields = []string{"category", "review"}
	if err := f.CheckEvaluationPolicy(p); err != nil || !p.Matches(actual, expected) {
		t.Fatalf("declared structural criterion did not accept changed wording: %v", err)
	}
	for _, wrong := range []string{
		`{"summary":"Reference explanation","category":"routine","review":true}`,
		`{"summary":"Reference explanation","category":"review","review":false}`,
		`{"summary":"Reference explanation","review":true}`,
	} {
		if p.Matches([]byte(wrong), expected) {
			t.Fatalf("explanation wording concealed a wrong or missing decision: %s", wrong)
		}
	}
	for _, fields := range [][]string{{"category", "category"}, {"missing"}, {""}} {
		invalid := p
		invalid.CompareFields = fields
		if err := f.CheckEvaluationPolicy(invalid); err == nil {
			t.Fatalf("an invalid criterion was accepted: %v", fields)
		}
	}
	raw, err := json.Marshal(p)
	var restored EvaluationPolicy
	if err != nil || json.Unmarshal(raw, &restored) != nil || !restored.Matches(actual, expected) {
		t.Fatal("a saved criterion changed its result")
	}
}
