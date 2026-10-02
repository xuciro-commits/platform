package platform

import "testing"

func TestWidgetPortsRejectUndeclaredWrongTypeAndOldProfile(t *testing.T) {
	d, s := overlayDocument()
	for _, tc := range []struct {
		name   string
		change func(*PageDocument, []Section)
	}{
		{"button collection", func(d *PageDocument, s []Section) { s[3].CollectionVariable = "open" }},
		{"table record input", func(d *PageDocument, s []Section) { s[1].RecordVariable = "open" }},
		{"wrong selection type", func(d *PageDocument, s []Section) { s[1].SelectionVariable = "open" }},
		{"readonly selection", func(d *PageDocument, s []Section) {
			d.Variables["selected"] = PageVariable{Scope: "application", Type: "record", Mode: "shared", Source: &PageResourceSource{Kind: "application", Variable: "selected", Object: &AssetRef{App: "sample", Kind: AssetObject, Name: "sample.note"}}}
			s[1].SelectionVariable = "selected"
		}},
		{"unsupported event", func(d *PageDocument, s []Section) { d.Events[0].Event = "change" }},
		{"foreign event source", func(d *PageDocument, s []Section) { d.Events[0].Source = "table" }},
		{"unknown table version", func(d *PageDocument, s []Section) { s[1].ConfigVersion = 2 }},
		{"old enabled profile", func(d *PageDocument, s []Section) {
			d.UIProfile = "platform.page.v2.3"
			n := d.Nodes["trigger"]
			n.EnabledWhen = "open"
			d.Nodes["trigger"] = n
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s := overlayDocument()
			tc.change(d, s)
			if d.Check(s) == nil {
				t.Fatal("invalid widget contract accepted")
			}
		})
	}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	if widgetPort("table", "selectionVariable") == nil || widgetPort("button", "enabledWhen") == nil || widgetPort("button", "collectionVariable") != nil {
		t.Fatal("widget port discovery disagrees with validation")
	}
}
