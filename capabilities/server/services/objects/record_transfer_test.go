package objects

import (
	"platformserver/platform"
	"testing"
)

func TestImportChoiceUsesStableKeysAndRejectsInvalidValues(t *testing.T) {
	field := platform.FieldInfo{Name: "stage", Type: "choice", Choices: []string{"new", "connected", "nurturing"}, ChoiceTitles: []string{"New", "Connected", "Nurturing"}}
	for _, tc := range []struct{ input, want string }{{"new", "new"}, {"New", "new"}, {"CONNECTED", "connected"}, {"Nurturing", "nurturing"}, {"Qualified", ""}, {"已联系", ""}} {
		got, err := fromCSV(field, tc.input, "USD", nil)
		if tc.want == "" {
			if err == nil {
				t.Errorf("%q unexpectedly accepted: %v", tc.input, got)
			}
		} else if err != nil || got != tc.want {
			t.Errorf("%q = %v, %v; want %s", tc.input, got, err, tc.want)
		}
	}
	field.Choices = []string{"a", "b"}
	field.ChoiceTitles = []string{"Same", "Same"}
	if _, err := fromCSV(field, "Same", "USD", nil); err == nil {
		t.Fatal("ambiguous choice accepted")
	}
}
