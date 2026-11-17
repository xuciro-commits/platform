package expr

import (
	"encoding/json"
	"fmt"
	"math"
)

type DecimalValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func ParseDecimal(text string) (DecimalValue, error) {
	canonical, err := ParseDecimalText(text)
	if err != nil {
		return DecimalValue{}, fmt.Errorf("invalid bounded decimal")
	}
	return DecimalValue{Kind: "decimal", Value: canonical}, nil
}

func (v DecimalValue) Check() error {
	parsed, err := ParseDecimal(v.Value)
	if err != nil || v.Kind != "decimal" || parsed != v {
		return fmt.Errorf("invalid canonical decimal")
	}
	return nil
}

func DecimalLiteral(raw json.RawMessage) (DecimalValue, bool) {
	var shape map[string]json.RawMessage
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 2 || shape["kind"] == nil || shape["value"] == nil {
		return DecimalValue{}, false
	}
	var value DecimalValue
	if json.Unmarshal(raw, &value) != nil || value.Check() != nil {
		return DecimalValue{}, false
	}
	return value, true
}

type NumberValue struct {
	Kind  string  `json:"kind"`
	Value float64 `json:"value"`
}

func NumberLiteral(raw json.RawMessage) (NumberValue, bool) {
	var shape map[string]json.RawMessage
	var v NumberValue
	if json.Unmarshal(raw, &shape) != nil || len(shape) != 2 || shape["kind"] == nil || shape["value"] == nil || string(shape["value"]) == "null" || json.Unmarshal(raw, &v) != nil || v.Kind != "number" || math.IsNaN(v.Value) || math.IsInf(v.Value, 0) {
		return NumberValue{}, false
	}
	return v, true
}
