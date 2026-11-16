package platform

import (
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"regexp"
	"strings"
)

type DecimalValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

var decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func ParseDecimal(text string) (DecimalValue, error) {
	if len(text) > PageWidgets.Runtime.Decimal.MaxBytes || !decimalText.MatchString(text) {
		return DecimalValue{}, fmt.Errorf("invalid bounded decimal")
	}
	if strings.Contains(text, ".") {
		text = strings.TrimRight(strings.TrimRight(text, "0"), ".")
	}
	if text == "-0" {
		text = "0"
	}
	return DecimalValue{Kind: "decimal", Value: text}, nil
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

func (v DecimalValue) Rat() *big.Rat { value, _ := new(big.Rat).SetString(v.Value); return value }

func (value DecimalValue) Condition(field FieldInfo, op string, read func(reflect.Value) any) (func(reflect.Value) bool, error) {
	if field.Type != "integer" && field.Type != "decimal" {
		return nil, fmt.Errorf("decimal condition needs a numeric field")
	}
	if op != "=" && op != "!=" && op != "<" && op != "<=" && op != ">" && op != ">=" {
		return nil, fmt.Errorf("unsupported decimal condition")
	}
	right := value.Rat()
	return func(record reflect.Value) bool {
		raw, err := json.Marshal(read(record))
		if err != nil {
			return false
		}
		left, ok := new(big.Rat).SetString(string(raw))
		if !ok {
			return false
		}
		cmp := left.Cmp(right)
		switch op {
		case "=":
			return cmp == 0
		case "!=":
			return cmp != 0
		case "<":
			return cmp < 0
		case "<=":
			return cmp <= 0
		case ">":
			return cmp > 0
		case ">=":
			return cmp >= 0
		}
		return false
	}, nil
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
