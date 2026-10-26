package platform

import (
	"encoding/json"
	"testing"
)

func TestPredicateRejectsUnboundedNumericExponent(t *testing.T) {
	p := Predicate{Op: "gt", Left: &Binding{Source: "input", Path: []string{"value"}}, Right: &Binding{Source: "literal", Value: json.RawMessage(`0`)}}
	if _, err := p.Test(&Run{Data: json.RawMessage(`{"value":1e99999999}`)}, nil); err == nil {
		t.Fatal("unbounded exponent reached exact arithmetic")
	}
	good, err := p.Test(&Run{Data: json.RawMessage(`{"value":9007199254740993}`)}, nil)
	if err != nil || !good {
		t.Fatalf("bounded precise integer comparison failed: %v %v", good, err)
	}
}
