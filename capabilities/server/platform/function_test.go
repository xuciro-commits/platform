package platform

import (
	"strings"
	"testing"
)

func TestFunctionOutputIsStrictAndBounded(t *testing.T) {
	f := RecordAdviceFunction("probe.record", []string{"title"}, []string{"reader"})
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"summary":"Ready","category":"routine","review":false}`, true},
		{`{"summary":"Review","category":"review","review":true}`, true},
		{`{"summary":"Ready","category":"routine","review":false,"extra":1}`, false},
		{`{"summary":"Ready","category":"routine","review":false,"review":true}`, false},
		{`{"summary":"Ready","category":"routine","review":"false"}`, false},
		{`{"summary":"Ready","category":"unknown","review":false}`, false},
		{`{"summary":"Ready","category":"routine"}`, false},
		{`{"summary":null,"category":"routine","review":false}`, false},
		{`{"summary":"Ready","category":"routine","review":false} {}`, false},
		{"```json\n{}\n```", false},
		{`[]`, false},
		{`{"summary":"` + strings.Repeat("x", 1024) + `","category":"routine","review":false}`, false},
		{string([]byte{'{', '"', 's', 'u', 'm', 'm', 'a', 'r', 'y', '"', ':', '"', 0xff, '"', '}'}), false},
	} {
		if err := f.ValidateOutput([]byte(tc.raw)); (err == nil) != tc.valid {
			t.Errorf("valid=%t, err=%v for %q", tc.valid, err, tc.raw)
		}
	}
}

func TestFunctionScalarNumbersDoNotCoerce(t *testing.T) {
	f := RecordAdviceFunction("probe.record", []string{"title"}, []string{"reader"})
	f.Output = []Field{{Name: "count", Type: "integer", Description: "Count", Required: true}, {Name: "ratio", Type: "decimal", Description: "Ratio", Required: true}}
	for _, tc := range []struct {
		raw   string
		valid bool
	}{
		{`{"count":9223372036854775807,"ratio":0.5}`, true},
		{`{"count":1.0,"ratio":0.5}`, false},
		{`{"count":9223372036854775808,"ratio":0.5}`, false},
		{`{"count":1,"ratio":"0.5"}`, false},
		{`{"count":1,"ratio":1e309}`, false},
	} {
		if err := f.ValidateOutput([]byte(tc.raw)); (err == nil) != tc.valid {
			t.Errorf("valid=%t err=%v for %s", tc.valid, err, tc.raw)
		}
	}
}
