package platform

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// NumberValue preserves the original host's finite binary-number semantics.
// It cannot be substituted for an exact DecimalValue.
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
func (d *PageDocument) CheckAggregateScalar(v PageVariable, info EntityInfo) error {
	if v.Mode != "aggregate" || v.Source == nil || v.Source.Kind != "aggregate" {
		return nil
	}
	_, field, ok := stringsCutMeasure(v.Source.Measure)
	f, found := info.Field(field)
	if !ok || !found || (f.Type != "integer" && f.Type != "decimal") {
		return fmt.Errorf("aggregate scalar needs an original visible numeric field")
	}
	return nil
}

func stringsCutMeasure(measure string) (string, string, bool) {
	op, field, ok := strings.Cut(measure, ":")
	return op, field, ok && (op == "sum" || op == "avg" || op == "min" || op == "max") && pageNodeID.MatchString(field)
}
