package platform

import (
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

// DecimalValue carries exact presentation numbers without a binary floating
// conversion. The canonical text is also usable by the original read boundary.
type DecimalValue struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

var decimalText = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func ParseDecimal(text string) (DecimalValue, error) {
	if len(text) > pageWidgets.Runtime.Decimal.MaxBytes || !decimalText.MatchString(text) {
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
