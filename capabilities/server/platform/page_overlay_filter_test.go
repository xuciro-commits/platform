package platform

import (
	"encoding/json"
	"testing"
)

func TestOverlayFilterResourceScope(t *testing.T) {
	d, s := overlayScopeDocument()
	for i := range s {
		if s[i].ID == "body" {
			s[i].Widget = "filter"
			s[i].Fields = []string{"active"}
		}
	}
	d.Variables["filter"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "filter", Mode: "resource", Source: &PageResourceSource{Kind: "filter", Section: "body"}}
	d.Variables["filtered"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "filter"}}}}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*PageDocument)
	}{
		{"old profile", func(d *PageDocument) { d.UIProfile = "platform.page.v2.12" }},
		{"page consumes local filter", func(d *PageDocument) {
			v := d.Variables["filter"]
			v.Scope, v.Owner = "page", ""
			d.Variables["filter"] = v
		}},
		{"wrong owner", func(d *PageDocument) { v := d.Variables["filter"]; v.Owner = "other"; d.Variables["filter"] = v }},
		{"page expression reads local filter", func(d *PageDocument) {
			v := d.Variables["filtered"]
			v.Scope, v.Owner = "page", ""
			d.Variables["filtered"] = v
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(d)
			var copy PageDocument
			json.Unmarshal(raw, &copy)
			tc.change(&copy)
			if err := copy.Check(s); err == nil {
				t.Fatal("invalid local filter accepted")
			}
		})
	}
	visible := d.Visible(s[:len(s)-2])
	if _, ok := visible.Variables["filter"]; ok {
		t.Fatal("hidden filter source remained")
	}
	if _, ok := visible.Variables["filtered"]; ok {
		t.Fatal("hidden filter dependency remained")
	}
}
