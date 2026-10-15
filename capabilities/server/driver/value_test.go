package driver

import (
	"platformserver/platform"
	"testing"
)

func TestProtocolDriverExactPointValue(t *testing.T) {
	for _, test := range []struct {
		name    string
		point   platform.DriverPoint
		value   any
		want    any
		invalid bool
	}{
		{"decimal", platform.DriverPoint{Type: "decimal", Scale: "0.1", Offset: "0.2"}, "583", "58.5", false},
		{"integer", platform.DriverPoint{Type: "integer", Scale: "2"}, "7", "14", false},
		{"fractional integer", platform.DriverPoint{Type: "integer", Scale: "0.1"}, "7", nil, true},
		{"boolean", platform.DriverPoint{Type: "boolean"}, false, false, false},
		{"bad numeric read", platform.DriverPoint{Type: "decimal"}, "offline", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := TransformPointValue(test.point, test.value)
			if test.invalid {
				if err == nil {
					t.Fatalf("invalid value was accepted: %v", got)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("got %v, %v; want %v", got, err, test.want)
			}
		})
	}
}
