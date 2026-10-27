package platform

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

// page_spacer_test.go

func TestSpacerFiniteSizesZeroAndOriginalLayoutBudget(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	size := 16.0
	s := Section{PageWidgetConfig: PageWidgetConfig{Spacer: &PageSpacer{Size: &size}}, ID: "blank", Widget: "spacer", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	for _, value := range []float64{0, 0.5, 16, 16.25, float64(pageWidgets.Layout.MaxSize)} {
		size = value
		if err := d.Check(p.Sections); err != nil {
			t.Fatal("valid spacer refused", value, err)
		}
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("spacer without document accepted")
	}
	d.UIProfile = "platform.page.v2.62"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted spacer")
	}
	d.UIProfile = PageUIProfile()
	for _, value := range []float64{-1, float64(pageWidgets.Layout.MaxSize) + 0.5, math.Inf(1), math.Inf(-1), math.NaN()} {
		size = value
		if d.checkSpacer(s) == nil {
			t.Fatal("invalid spacer size accepted", value)
		}
	}
	size = 16
	for _, change := range []func(*Section){func(s *Section) { s.Spacer = nil }, func(s *Section) { s.Spacer.Size = nil }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Fields = []string{"secret"} }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		c := *s.Spacer
		copy.Spacer = &c
		change(&copy)
		if copy.checkWidgetConfiguration() == nil && d.checkSpacer(copy) == nil {
			t.Fatal("invalid spacer configuration accepted", copy)
		}
	}
}

// page_separator_test.go

func TestSeparatorStaticLabelsProfilesAndBudgets(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	label := "<img src=x> {value}"
	s := Section{PageWidgetConfig: PageWidgetConfig{Separator: &PageSeparator{Label: &label}}, ID: "line", Widget: "separator", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("separator without document accepted")
	}
	d.UIProfile = "platform.page.v2.61"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted separator")
	}
	d.UIProfile = PageUIProfile()
	empty := ""
	for _, value := range []*string{nil, &empty} {
		s.Separator.Label = value
		if err := d.checkSeparator(s); err != nil {
			t.Fatal("absent or empty label refused", err)
		}
	}
	for _, change := range []func(*Section){func(s *Section) { s.Separator = nil }, func(s *Section) { v := strings.Repeat("中", 342); s.Separator.Label = &v }, func(s *Section) { s.Widget = "text" }, func(s *Section) { s.CollectionVariable = "window" }, func(s *Section) { s.Fields = []string{"secret"} }, func(s *Section) { s.Actions = []AssetRef{{App: "sample", Kind: AssetAction, Name: "execute"}} }} {
		copy := s
		c := *s.Separator
		copy.Separator = &c
		change(&copy)
		if copy.checkWidgetConfiguration() == nil && d.checkSeparator(copy) == nil {
			t.Fatal("invalid separator accepted", copy)
		}
	}
}

// page_button_group_test.go

func TestButtonGroupControlsAndBindings(t *testing.T) {
	d, sections := overlayDocument()
	d.UIProfile = PageUIProfile()
	sections[len(sections)-2].Widget = "button-group"
	sections[len(sections)-2].Buttons = []PageButton{{ID: "open", Title: "Open", Variant: "primary", Icon: "arrow"}, {ID: "close", Title: "Close", Variant: "danger", Icon: "trash"}}
	d.Events = []PageEventBinding{{Source: "trigger", Control: "open", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: json.RawMessage(`true`)}}}, {Source: "trigger", Control: "close", Event: "click", Effects: []PageEffect{{Kind: "set", Target: "open", Value: json.RawMessage(`false`)}}}}
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.36"
	if d.Check(sections) == nil {
		t.Fatal("old profile accepted controls")
	}
	d.UIProfile = PageUIProfile()
	original := append([]PageEventBinding{}, d.Events...)
	d.Events = d.Events[:1]
	if d.Check(sections) == nil {
		t.Fatal("missing control binding accepted")
	}
	d.Events = append(original, original[0])
	if d.Check(sections) == nil {
		t.Fatal("duplicate control binding accepted")
	}
	d.Events = original
	d.Events[0].Control = "unknown"
	if d.Check(sections) == nil {
		t.Fatal("unknown control accepted")
	}
	d.Events[0].Control = "open"
	sections[len(sections)-2].Buttons[0].Icon = "script"
	if d.Check(sections) == nil {
		t.Fatal("unsupported icon accepted")
	}
	sections[len(sections)-2].Buttons[0].Icon = "arrow"
	projected := d.Visible(sections[:len(sections)-1])
	if len(projected.Events) != 0 {
		t.Fatal("hidden overlay retained group events")
	}
	if _, ok := projected.Nodes["trigger"]; ok {
		t.Fatal("group without visible controls retained")
	}
}

// page_layout_size_test.go

func TestPageLayoutSizing(t *testing.T) {
	ptr := func(v int) *int { return &v }
	makePage := func() (*PageDocument, []Section) {
		d, sections := nestedDocument()
		n := d.Nodes["root"]
		n.Size = &PageLayoutSize{Height: ptr(640)}
		d.Nodes["root"] = n
		n = d.Nodes["columns"]
		n.Size = &PageLayoutSize{Weight: ptr(1)}
		n.Gap = ptr(0)
		d.Nodes["columns"] = n
		n = d.Nodes["table"]
		n.Size = &PageLayoutSize{Weight: ptr(2), MinWidth: ptr(96), MaxWidth: ptr(640), Scroll: "auto"}
		d.Nodes["table"] = n
		n = d.Nodes["detail"]
		n.Size = &PageLayoutSize{Width: ptr(240)}
		d.Nodes["detail"] = n
		return d, sections
	}
	d, sections := makePage()
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	// Wire bytes and member pruning retain sizing on original stable IDs.
	raw, _ := json.Marshal(d)
	var roundtrip PageDocument
	if err := json.Unmarshal(raw, &roundtrip); err != nil {
		t.Fatal(err)
	}
	visible := roundtrip.Visible(sections[:2])
	if *visible.Nodes["table"].Size.Weight != 2 || len(visible.Nodes["columns"].Children) != 1 || *visible.Nodes["columns"].Gap != 0 {
		t.Fatal("projection lost sizing or retained a hidden track")
	}
	for _, tc := range []struct {
		name  string
		patch func(*PageDocument)
	}{
		{"old profile", func(d *PageDocument) { d.UIProfile = "platform.page.v2.26" }},
		{"gap budget", func(d *PageDocument) { n := d.Nodes["columns"]; n.Gap = ptr(65); d.Nodes["columns"] = n }},
		{"widget gap", func(d *PageDocument) { n := d.Nodes["table"]; n.Gap = ptr(1); d.Nodes["table"] = n }},
		{"size budget", func(d *PageDocument) { d.Nodes["table"].Size.MinWidth = ptr(4097) }},
		{"negative size", func(d *PageDocument) { d.Nodes["table"].Size.MinWidth = ptr(-1) }},
		{"inverted range", func(d *PageDocument) { d.Nodes["table"].Size.MinWidth = ptr(700) }},
		{"fixed outside range", func(d *PageDocument) { d.Nodes["detail"].Size.MaxWidth = ptr(200) }},
		{"fixed and weighted", func(d *PageDocument) { d.Nodes["detail"].Size.Weight = ptr(1) }},
		{"root weight", func(d *PageDocument) { d.Nodes["root"].Size.Weight = ptr(1) }},
		{"excess weight", func(d *PageDocument) { d.Nodes["table"].Size.Weight = ptr(25) }},
		{"natural rows", func(d *PageDocument) { d.Nodes["root"].Size.Height = nil }},
		{"unbounded scroll", func(d *PageDocument) {
			n := d.Nodes["root"]
			n.Size = &PageLayoutSize{Scroll: "auto"}
			d.Nodes["root"] = n
		}},
		{"unsupported scroll", func(d *PageDocument) { d.Nodes["table"].Size.Scroll = "hidden" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, s := makePage()
			tc.patch(d)
			if d.Check(s) == nil {
				t.Fatal("accepted incompatible sizing")
			}
		})
	}
	// A column fills a bounded cross axis, so nested weighted rows have space.
	d, sections = makePage()
	n := d.Nodes["table"]
	n.Kind = "rows"
	n.Section = ""
	n.Children = []string{"row"}
	d.Nodes["table"] = n
	d.Nodes["row"] = PageLayoutNode{Kind: "widget", Section: "table", Size: &PageLayoutSize{Weight: ptr(3), Scroll: "auto"}}
	if err := d.Check(sections); err != nil {
		t.Fatal("lost inherited height", err)
	}
}

// page_titles_test.go

func TestTitlesRequirePlainTextAndMatchingScopedCompleteCount(t *testing.T) {
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"heading", "title"}}, "heading": {Kind: "widget", Section: "heading"}, "title": {Kind: "widget", Section: "title"}}, Queries: map[string]PageQuery{"read": {Object: AssetRef{App: "sample", Kind: AssetObject, Name: "sample.item"}, Limit: 1}}, Variables: map[string]PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}, "count": {Scope: "page", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}}}
	sections := []Section{{PageWidgetConfig: PageWidgetConfig{HeadingLevel: "h2"}, ID: "heading", Widget: "heading", ConfigVersion: 1, Text: "<b>Literal heading</b>"}, {PageWidgetConfig: PageWidgetConfig{CountVariable: "count"}, ID: "title", Widget: "collection-title", ConfigVersion: 1, CollectionVariable: "window", Title: "Permitted items"}}
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check(sections) == nil {
		t.Fatal("titles without document accepted")
	}
	for _, change := range []func([]Section){func(s []Section) { s[0].HeadingLevel = "h4" }, func(s []Section) { s[0].Text = "" }, func(s []Section) { s[0].Text = strings.Repeat("x", 4097) }, func(s []Section) { s[1].CountVariable = "" }, func(s []Section) { s[0].CountVariable = "count" }} {
		copy := append([]Section(nil), sections...)
		change(copy)
		if d.Check(copy) == nil {
			t.Fatal("invalid title configuration accepted")
		}
	}
	d.UIProfile = "platform.page.v2.40"
	if d.Check(sections) == nil {
		t.Fatal("old profile accepted titles")
	}
	d.UIProfile = PageUIProfile()
	count := d.Variables["count"]
	count.Mode = "state"
	count.Source = nil
	count.Initial = Raw(map[string]any{"kind": "decimal", "value": "999"})
	d.Variables["count"] = count
	if d.Check(sections) == nil {
		t.Fatal("writable fake count accepted")
	}
	count = PageVariable{Scope: "overlay", Owner: "other", Type: "decimal", Mode: "aggregate", Source: &PageResourceSource{Kind: "count", Query: "read"}}
	d.Variables["count"] = count
	if d.Check(sections) == nil {
		t.Fatal("count from another scope accepted")
	}
}

// page_slots_test.go

func slotDocument() (*PageDocument, []Section) {
	d, sections := nestedDocument()
	n := d.Nodes["table"]
	n.Children = []string{"toolbar", "footer"}
	d.Nodes["table"] = n
	d.Nodes["toolbar"] = PageLayoutNode{Kind: "toolbar", Slot: "toolbar"}
	d.Nodes["footer"] = PageLayoutNode{Kind: "flow", Slot: "footer", Children: []string{"control"}}
	d.Nodes["control"] = PageLayoutNode{Kind: "widget", Section: "control"}
	return d, append(sections, Section{ID: "control", Widget: "text", ConfigVersion: 1, Text: "Original footer"})
}

func TestPageSlotsUseTheOriginalOwnedTree(t *testing.T) {
	d, sections := slotDocument()
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*PageDocument){
		func(d *PageDocument) { d.UIProfile = "platform.page.v2.103" },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Slot = "unknown"; d.Nodes["footer"] = n },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Slot = "toolbar"; d.Nodes["footer"] = n },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Kind = "tabs"; d.Nodes["footer"] = n },
		func(d *PageDocument) { n := d.Nodes["footer"]; n.Slot = ""; d.Nodes["footer"] = n },
		func(d *PageDocument) {
			n := d.Nodes["root"]
			n.Children = append(n.Children, "footer")
			d.Nodes["root"] = n
		},
		func(d *PageDocument) { n := d.Nodes["control"]; n.Children = []string{"table"}; d.Nodes["control"] = n },
	} {
		next, s := slotDocument()
		change(next)
		if err := next.Check(s); err == nil {
			t.Fatal("invalid slot tree was accepted")
		}
	}
	bytes, _ := json.Marshal(d)
	var copy PageDocument
	if err := json.Unmarshal(bytes, &copy); err != nil {
		t.Fatal(err)
	}
	if copy.Nodes["footer"].Slot != "footer" {
		t.Fatal("slot identity was lost")
	}
	if err := copy.Check(sections); err != nil {
		t.Fatal(err)
	}
}

func TestPageSlotProjectionRetainsEmptySlotsAndRemovesPrivateCompositeChildren(t *testing.T) {
	d, s := slotDocument()
	visible := d.Visible(s)
	if visible.Nodes["toolbar"].Slot != "toolbar" || visible.Nodes["control"].Section != "control" {
		t.Fatal("slot projection dropped owned nodes")
	}
	withoutChild := d.Visible(s[:len(s)-1])
	if _, ok := withoutChild.Nodes["control"]; ok {
		t.Fatal("private slot child remained")
	}
	if withoutChild.Nodes["footer"].Slot != "footer" {
		t.Fatal("empty declared slot was lost")
	}
	var allowed []Section
	for _, section := range s {
		if section.ID != "table" {
			allowed = append(allowed, section)
		}
	}
	hidden := d.Visible(allowed)
	for _, id := range []string{"table", "toolbar", "footer", "control"} {
		if _, ok := hidden.Nodes[id]; ok {
			t.Fatalf("removed composite leaked %s", id)
		}
	}
}
