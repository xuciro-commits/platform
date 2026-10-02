package platformserver

import (
	"encoding/json"
	"platformserver/platform"
	"reflect"
	"testing"
)

func TestTypedDecimalConditionsPreserveIntegerPrecision(t *testing.T) {
	type row struct {
		Count  int64
		Amount float64
	}
	info := platform.EntityInfo{Type: "sample.note", Fields: []platform.FieldInfo{{Name: "count", Type: "integer", Index: []int{0}}, {Name: "amount", Type: "decimal", Index: []int{1}}}}
	for _, tc := range []struct {
		domain string
		want   bool
	}{
		{`[["count","=",{"kind":"decimal","value":"9007199254740993"}]]`, true},
		{`[["count","=",{"kind":"decimal","value":"9007199254740992"}]]`, false},
		{`[["count",">",{"kind":"decimal","value":"9007199254740992"}]]`, true},
		{`[["amount","=",{"kind":"decimal","value":"0.1"}]]`, true},
		{`[["amount","<",{"kind":"decimal","value":"0.10000000000000001"}]]`, true},
	} {
		p, err := compileDomain(info, json.RawMessage(tc.domain))
		if err != nil || p(reflect.ValueOf(row{9007199254740993, 0.1})) != tc.want {
			t.Fatal(tc.domain, err)
		}
	}
	for _, domain := range []string{`[["count","=",{"kind":"decimal","value":"1e99999"}]]`, `[["count","=",{"kind":"decimal","value":"1.0"}]]`, `[["count","like",{"kind":"decimal","value":"1"}]]`} {
		if _, err := compileDomain(info, json.RawMessage(domain)); err == nil {
			t.Fatal("invalid decimal condition accepted")
		}
	}
}
