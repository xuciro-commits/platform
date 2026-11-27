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
	translate := func(s string) []string {
		switch s {
		case "New":
			return []string{"新建"}
		case "Connected":
			return []string{"已联系"}
		}
		return nil
	}
	if got, err := fromCSV(field, "已联系", "USD", nil, translate); err != nil || got != "connected" {
		t.Fatalf("translated choice = %v, %v; want connected", got, err)
	}
	info := platform.RecordTypeInfo{Type: "crm.lead", Fields: []platform.FieldInfo{field}}
	parsed, refusal := ParseImport(info, []byte("stage\nPending\n已联系\n"), "USD", nil, translate)
	if refusal != nil || len(parsed.Lines) != 2 {
		t.Fatalf("ParseImport = %+v, %v", parsed, refusal)
	}
	if parsed.Lines[0].Issue == nil || parsed.Lines[0].Issue.Code != "choice" || parsed.Lines[0].Issue.Message != "Choose a listed value." {
		t.Fatalf("expected choice issue, got %+v", parsed.Lines[0])
	}
	if parsed.Lines[1].Outcome != "" || string(parsed.Lines[1].Payload) != `{"stage":"connected"}` {
		t.Fatalf("expected translated payload, got %+v", parsed.Lines[1])
	}
	field.Choices = []string{"a", "b"}
	field.ChoiceTitles = []string{"Same", "Same"}
	if _, err := fromCSV(field, "Same", "USD", nil); err == nil {
		t.Fatal("ambiguous choice accepted")
	}
	field.ChoiceTitles = []string{"First", "Second"}
	if _, err := fromCSV(field, "同词", "USD", nil, func(string) []string { return []string{"同词"} }); err == nil {
		t.Fatal("ambiguous translated choice accepted")
	}
}
