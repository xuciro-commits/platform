package platform

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecimalEncodingAndProfile(t *testing.T) {
	for _, text := range []string{"-0", "1.200", "0.0001", "9007199254740993", "-12.4"} {
		value, err := ParseDecimal(text)
		if err != nil || value.Check() != nil {
			t.Fatal(text, err)
		}
	}
	for _, text := range []string{"", "1.", "01", "+1", "1e2", "NaN", strings.Repeat("9", 129)} {
		if _, err := ParseDecimal(text); err == nil {
			t.Fatal("invalid decimal accepted", text)
		}
	}
	doc := PageDocument{UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"threshold": {Scope: "page", Type: "decimal", Mode: "state", Initial: json.RawMessage(`{"kind":"decimal","value":"0.1"}`)}}}
	if err := doc.CheckVariables(); err != nil {
		t.Fatal(err)
	}
	page := Page{Name: "notes", Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}, Sections: []Section{{ID: "input", Widget: "input", ConfigVersion: 1}}, Document: &doc}
	doc.FormatVersion = 2
	doc.Root = "root"
	doc.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"input"}}, "input": {Kind: "widget", Section: "input", ValueVariable: "threshold"}}
	if err := doc.Check(page.Sections); err != nil {
		t.Fatal(err)
	}
	doc.UIProfile = "platform.page.v2.15"
	if doc.Check(page.Sections) == nil {
		t.Fatal("old profile accepted decimal")
	}
}
