package build

import "platformserver/internal/construction"

import (
	"strings"
	"testing"
)

// #138/#132: "Only when" is checked as a definition is saved.
func TestFieldOnlyWhen(t *testing.T) {
	kind := construction.Field{Name: "kind", Title: "Kind", Type: "choice", Choices: "refund,return,other"}
	for _, c := range []struct {
		fields []construction.Field
		want   string
	}{
		{[]construction.Field{kind, {Name: "why", Title: "Why", Type: "text", Required: true, When: "kind=refund,return"}}, ""},
		{[]construction.Field{{Name: "urgent", Title: "Urgent", Type: "boolean"}, {Name: "why", Title: "Why", Type: "text", When: "urgent=true"}}, ""},
		{[]construction.Field{kind, {Name: "why", Title: "Why", Type: "text", When: "kind"}}, "not field=value,value"},
		{[]construction.Field{kind, {Name: "why", Title: "Why", Type: "text", When: "mood=sad"}}, "not another field"},
		{[]construction.Field{kind, {Name: "why", Title: "Why", Type: "text", When: "kind=lost"}}, "never is"},
		{[]construction.Field{{Name: "note", Title: "Note", Type: "text"}, {Name: "why", Title: "Why", Type: "text", When: "note=x"}}, "neither a choice nor a boolean"},
		{[]construction.Field{kind, {Name: "sub", Title: "Sub", Type: "choice", Choices: "a,b", When: "kind=refund"}, {Name: "why", Title: "Why", Type: "text", When: "sub=a"}}, "conditional itself"},
	} {
		err := construction.CheckFields(c.fields, nil)
		if c.want == "" && err != nil || c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)) {
			t.Fatalf("%v: got %v, want %q", c.fields[len(c.fields)-1].When, err, c.want)
		}
	}
}
