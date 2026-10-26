package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strings"
	"unicode/utf8"
)

// EvaluationPolicy is the original AI owner’s measured synthetic-case gate.
// Reports and model execution stay with that owner; releases freeze this data.
type EvaluationPolicy struct {
	CompareFields    []string         `json:"compareFields,omitempty"`
	MinQuality       float64          `json:"minQuality"`
	MaxCostUSD       float64          `json:"maxCostUsd"`
	MaxLatencyMillis int64            `json:"maxLatencyMillis"`
	Cases            []EvaluationCase `json:"cases"`
}
type EvaluationCase struct {
	Name     string          `json:"name"`
	Input    json.RawMessage `json:"input"`
	Expected json.RawMessage `json:"expected"`
}

func (p EvaluationPolicy) Check() error {
	if math.IsNaN(p.MinQuality) || p.MinQuality <= 0 || p.MinQuality > 1 || math.IsNaN(p.MaxCostUSD) || p.MaxCostUSD <= 0 || p.MaxCostUSD > 1000 || p.MaxLatencyMillis < 1 || p.MaxLatencyMillis > 300000 || len(p.Cases) < 1 || len(p.Cases) > 5 {
		return fmt.Errorf("function evaluation needs bounded quality, cost, latency and cases")
	}
	if len(p.CompareFields) > 16 {
		return fmt.Errorf("evaluation compares at most 16 output fields")
	}
	for i, name := range p.CompareFields {
		if !functionFieldName.MatchString(name) || slices.Contains(p.CompareFields[:i], name) {
			return fmt.Errorf("evaluation comparison fields need unique output names")
		}
	}
	seen := map[string]bool{}
	for _, test := range p.Cases {
		var input, expected map[string]json.RawMessage
		if !functionName.MatchString(test.Name) || seen[test.Name] || len(test.Input) > 32<<10 || len(test.Expected) > 32<<10 || json.Unmarshal(test.Input, &input) != nil || input == nil || json.Unmarshal(test.Expected, &expected) != nil || expected == nil {
			return fmt.Errorf("evaluation cases need unique names and bounded synthetic input/answer objects")
		}
		seen[test.Name] = true
	}
	return nil
}

// CheckEvaluationPolicy resolves the criterion against the same typed output
// declaration. It does not relax input or complete output validation.
func (f AIFunction) CheckEvaluationPolicy(p EvaluationPolicy) error {
	if err := p.Check(); err != nil {
		return err
	}
	for _, name := range p.CompareFields {
		if !slices.ContainsFunc(f.Output, func(field Field) bool { return field.Name == name }) {
			return fmt.Errorf("evaluation comparison names an undeclared output field %s", name)
		}
	}
	return nil
}

// Matches measures only the declared criterion after the original evaluator
// checks the complete output schema. Empty keeps the historical full match.
func (p EvaluationPolicy) Matches(actual, expected []byte) bool {
	var left, right any
	if json.Unmarshal(actual, &left) != nil || json.Unmarshal(expected, &right) != nil {
		return false
	}
	if len(p.CompareFields) == 0 {
		return reflect.DeepEqual(left, right)
	}
	a, aok := left.(map[string]any)
	b, bok := right.(map[string]any)
	if !aok || !bok {
		return false
	}
	for _, name := range p.CompareFields {
		av, aok := a[name]
		bv, bok := b[name]
		if !aok || !bok || !reflect.DeepEqual(av, bv) {
			return false
		}
	}
	return true
}

// EvaluationSetDescriptor is portable policy data; it contains no local
// draft record IDs, operator grants, production records or provider secrets.
type EvaluationSetDescriptor struct {
	Name     string           `json:"name"`
	Title    string           `json:"title"`
	Function AssetBinding     `json:"function"`
	Model    string           `json:"model,omitempty"`
	Policy   EvaluationPolicy `json:"policy"`
}

func (e EvaluationSetDescriptor) Check() error {
	if !functionName.MatchString(e.Name) || e.Title == "" || len(e.Title) > 256 || e.Function.Ref.Check() != nil || e.Function.Ref.Kind != AssetFunction || e.Function.SourceVersion == "" || strings.TrimSpace(e.Model) == "" || len(e.Model) > 256 {
		return fmt.Errorf("evaluation set needs a name, title and pinned function")
	}
	return e.Policy.Check()
}
func ReadEvaluationSet(raw []byte) (EvaluationSetDescriptor, error) {
	var e EvaluationSetDescriptor
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&e); err != nil {
		return e, err
	}
	if decoder.Decode(new(any)) != io.EOF {
		return e, fmt.Errorf("evaluation set has trailing data")
	}
	return e, e.Check()
}

// Synthetic evaluation uses the same prompt shape as an ordinary accepted call.
// History contains bounded synthetic typed answers, never production call IDs.
func (f AIFunction) EvaluationInput(raw []byte) bool {
	if len(raw) > f.MaxInputBytes || !utf8.Valid(raw) {
		return false
	}
	var input map[string]json.RawMessage
	if json.Unmarshal(raw, &input) != nil || input == nil {
		return false
	}
	if f.Conversation {
		var record map[string]json.RawMessage
		var question string
		var history []map[string]json.RawMessage
		if len(input) != 3 || json.Unmarshal(input["record"], &record) != nil || record == nil || json.Unmarshal(input["question"], &question) != nil || strings.TrimSpace(question) == "" || len(question) > 4096 || json.Unmarshal(input["history"], &history) != nil || history == nil || len(history) > 8 {
			return false
		}
		for _, turn := range history {
			var prior string
			if len(turn) != 2 || json.Unmarshal(turn["question"], &prior) != nil || strings.TrimSpace(prior) == "" || len(prior) > 4096 || f.ValidateOutput(turn["answer"]) != nil {
				return false
			}
		}
		input = record
	}
	if len(input) != len(f.Fields) {
		return false
	}
	for _, name := range f.Fields {
		if value, present := input[name]; !present || !scalarEvaluationInput(value) {
			return false
		}
	}
	return true
}

func scalarEvaluationInput(raw json.RawMessage) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil || value == nil {
		return false
	}
	switch value.(type) {
	case string, bool, float64:
		return true
	default:
		return false
	}
}
