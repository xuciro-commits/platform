package platform

import (
	"encoding/json"
	"slices"
	"testing"
)

func TestManifestWidgetFieldsPreserveFlatSections(t *testing.T) {
	for _, id := range []string{"heading", "notice", "separator", "spacer"} {
		widget := pageWidget(id)
		section := Section{Widget: widget.ComponentID, ConfigVersion: widget.ConfigVersion}
		raw, _ := json.Marshal(widget.Defaults)
		if err := json.Unmarshal(raw, &section); err != nil {
			t.Fatal(err)
		}
		if err := checkPageWidget(section); err != nil {
			t.Fatalf("%s defaults: %v", widget.ComponentID, err)
		}
	}
	section := Section{PageWidgetConfig: PageWidgetConfig{Avatar: &PageAvatarStack{ContextVariable: "person", ContextCollectionVariable: "team"}, CollectionOutputVariable: "result"}}
	ports := section.variablePorts()
	if ports["avatar.contextVariable"] != "person" || ports["avatar.contextCollectionVariable"] != "team" || ports["collectionOutputVariable"] != "result" {
		t.Fatal("manifest paths did not read the original flat section", ports)
	}
	section = Section{Widget: "heading", ConfigVersion: 1, Text: "Instructions", PageWidgetConfig: PageWidgetConfig{Notice: &PageNotice{Tone: "info", Message: "foreign configuration"}}}
	if checkPageWidget(section) == nil {
		t.Fatal("a property belonging to another widget was accepted")
	}
	if err := (*PageDocument)(nil).Check([]Section{{Widget: "text", Text: "Retained page"}}); err != nil {
		t.Fatal("legacy flat sections require no current authoring configuration", err)
	}
}

func TestFrozenDocumentlessSectionsPreserveRetainedFields(t *testing.T) {
	for _, section := range []Section{
		{Widget: "text", Text: "Retained page"},
		{Widget: "table", SelectionVariable: "record", PageWidgetConfig: PageWidgetConfig{SelectionSetVariable: "records", TableColumns: []PageTableColumn{{Field: "title"}}}},
		{Widget: "button-group", PageWidgetConfig: PageWidgetConfig{Buttons: []PageButton{{ID: "next", Title: "Next"}}}},
		{Widget: "record-view", PageWidgetConfig: PageWidgetConfig{RecordView: &PageRecordView{Tabs: []string{"overview"}}}},
		{Widget: "external-frame", PageWidgetConfig: PageWidgetConfig{ExternalFrame: &PageExternalFrame{}}},
		{Widget: "table", PageWidgetConfig: PageWidgetConfig{Model: &ModelBinding{Type: "sample.item"}}},
		{Widget: "layered-graph", PageWidgetConfig: PageWidgetConfig{Composite: &PageComposite{Bindings: map[string]ModelViewBinding{"nodes": {Model: ModelBinding{Type: "sample.item"}}}}}},
	} {
		if err := (*PageDocument)(nil).Check([]Section{section}); err != nil {
			t.Fatalf("retained %s section was rejected: %v", section.Widget, err)
		}
	}
	for _, section := range []Section{
		{Widget: "notice"},
		{Widget: "record-card"},
		{Widget: "text", PageWidgetConfig: PageWidgetConfig{InputKind: "search"}},
		{Widget: "text", PageWidgetConfig: PageWidgetConfig{Notice: &PageNotice{Tone: "info", Message: "Needs a document"}}},
		{Widget: "table", PageWidgetConfig: PageWidgetConfig{HistoryLimit: 1}},
		{Widget: "chart", ChartVariant: "pie"},
		{Widget: "table", PageWidgetConfig: PageWidgetConfig{ObservationRowsVariable: "observations"}},
	} {
		if err := (*PageDocument)(nil).Check([]Section{section}); err == nil {
			t.Fatalf("document-owned %s section was accepted", section.Widget)
		}
	}
}

func TestWidgetAuthorFieldsUseOriginalComponentDeclarations(t *testing.T) {
	var manifest struct {
		Widgets []struct {
			ID     string   `json:"componentID"`
			Fields []string `json:"authoringFields"`
		}
	}
	if err := json.Unmarshal([]byte(PageUIManifest()), &manifest); err != nil {
		t.Fatal(err)
	}
	fields := map[string][]string{}
	for _, widget := range manifest.Widgets {
		fields[widget.ID] = widget.Fields
	}
	if !slices.Contains(fields["heading"], "headingLevel") || slices.Contains(fields["heading"], "collectionVariable") ||
		!slices.Contains(fields["table"], "collectionVariable") || !slices.Contains(fields["table"], "inlineEdit") ||
		!slices.Contains(fields["avatar-stack"], "avatar") || !slices.Contains(fields["layered-graph"], "composite") {
		t.Fatalf("component author fields lost their original owner: %+v", fields)
	}
	if pageFeatureSupported("platform.page.v2.1", "variables") || !pageFeatureSupported("platform.page.v2.2", "variables") ||
		pageFeatureSupported("platform.page.v2.6", "overlayVariables") || !pageFeatureSupported("platform.page.v2.7", "overlayVariables") ||
		pageFeatureSupported("platform.page.v2.1", "unknown") {
		t.Fatal("feature versions no longer follow the declared runtime profiles")
	}
}

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
