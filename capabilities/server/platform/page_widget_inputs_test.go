package platform

import (
	"strings"
	"testing"
)

// page_boolean_input_test.go

func TestBooleanInputOriginalStateProfileAndEmptyLabel(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["flag"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	empty := ""
	s := Section{PageWidgetConfig: PageWidgetConfig{BooleanVariable: "flag", BooleanLabel: &empty}, ID: "switch", Widget: "boolean-input", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.53"
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("legacy switch rejected", err)
	}
	s.BooleanVariant = "checkbox"
	p.Sections[len(p.Sections)-1] = s
	if d.Check(p.Sections) == nil {
		t.Fatal("checkbox accepted in legacy switch profile")
	}
	d.UIProfile = PageUIProfile()
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	s.BooleanVariant = "unknown"
	if d.checkBooleanInput(s) == nil {
		t.Fatal("unknown boolean presentation accepted")
	}
	s.BooleanVariant = ""
	p.Sections[len(p.Sections)-1] = s
	if !strings.Contains(string(Raw(s)), `"booleanLabel":""`) {
		t.Fatal("intentional empty label lost")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("boolean input without document accepted")
	}
	d.UIProfile = "platform.page.v2.52"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted switch")
	}
	d.UIProfile = PageUIProfile()
	for _, v := range []PageVariable{{Scope: "page", Type: "boolean", Mode: "constant", Initial: Raw(false)}, {Scope: "page", Type: "string", Mode: "state", Initial: Raw("false")}, {Scope: "application", Type: "boolean", Mode: "shared", Writable: true}} {
		d.Variables["flag"] = v
		if d.checkBooleanInput(s) == nil {
			t.Fatal("coercion or different state owner accepted")
		}
	}
	d.Variables["flag"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	long := strings.Repeat("a", 1025)
	s.BooleanLabel = &long
	if d.checkBooleanInput(s) == nil {
		t.Fatal("unbounded label accepted")
	}
	s.BooleanLabel = nil
	s.Widget = "text"
	if s.checkWidgetConfiguration() == nil {
		t.Fatal("binding escaped widget")
	}
}

func TestBooleanInputOriginalOverlayAndEnablePort(t *testing.T) {
	d, sections := overlayScopeDocument()
	d.Variables["flag"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "boolean", Mode: "state", Initial: Raw(false)}
	d.Variables["enabled"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "boolean", Mode: "state", Initial: Raw(true)}
	s := Section{PageWidgetConfig: PageWidgetConfig{BooleanVariable: "flag"}, ID: "switch", Widget: "boolean-input", ConfigVersion: 1}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID, EnabledWhen: "enabled"}
	o := d.Overlays["panel"]
	root := d.Nodes[o.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[o.Root] = root
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	root.Children = root.Children[:len(root.Children)-1]
	d.Nodes[o.Root] = root
	main := d.Nodes[d.Root]
	main.Children = append(main.Children, s.ID)
	d.Nodes[d.Root] = main
	if d.Check(sections) == nil {
		t.Fatal("overlay switch and enabled state escaped into page")
	}
}

// page_choice_input_test.go

func TestChoiceInputOriginalStateAndStaticOptions(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["choice"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("retired")}
	s := Section{PageWidgetConfig: PageWidgetConfig{ChoiceVariable: "choice", ChoiceInput: &PageChoiceInput{Variant: "select", Options: []string{"Open", "Closed"}}}, ID: "choice", Widget: "choice-input", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal("unmatched draft must remain valid state", err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("choice without document accepted")
	}
	d.UIProfile = "platform.page.v2.54"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted choice")
	}
	d.UIProfile = PageUIProfile()
	for _, c := range []PageChoiceInput{{Variant: "other", Options: []string{}}, {Variant: "select", Options: nil}, {Variant: "radio", Options: []string{"A", "A"}}, {Variant: "segments", Options: []string{""}}, {Variant: "select", Options: []string{strings.Repeat("a", 257)}}} {
		s.ChoiceInput = &c
		if d.checkChoiceInput(s) == nil {
			t.Fatal("invalid static choices accepted", c)
		}
	}
	s.ChoiceInput = &PageChoiceInput{Variant: "select", Options: []string{}}
	if err := d.checkChoiceInput(s); err != nil {
		t.Fatal("empty source options rejected", err)
	}
	for _, v := range []PageVariable{{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("")}, {Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}, {Scope: "application", Type: "string", Mode: "shared", Writable: true}} {
		d.Variables["choice"] = v
		if d.checkChoiceInput(s) == nil {
			t.Fatal("invalid state owner or coercion accepted")
		}
	}
}

func TestChoiceInputOverlayCannotEscape(t *testing.T) {
	d, sections := overlayScopeDocument()
	s := Section{PageWidgetConfig: PageWidgetConfig{ChoiceVariable: "local", ChoiceInput: &PageChoiceInput{Variant: "radio", Options: []string{"A", "B"}}}, ID: "choice", Widget: "choice-input", ConfigVersion: 1}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID, EnabledWhen: "open"}
	o := d.Overlays["panel"]
	root := d.Nodes[o.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[o.Root] = root
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	root.Children = root.Children[:len(root.Children)-1]
	d.Nodes[o.Root] = root
	main := d.Nodes[d.Root]
	main.Children = append(main.Children, s.ID)
	d.Nodes[d.Root] = main
	if d.Check(sections) == nil {
		t.Fatal("overlay choice escaped")
	}
}

func TestMultipleChoiceOriginalSetProfileAndPorts(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["pick"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: Raw(map[string]any{"kind": "string-set", "values": []string{"retired"}})}
	s := Section{PageWidgetConfig: PageWidgetConfig{ChoiceSetVariable: "pick", ChoiceInput: &PageChoiceInput{Variant: "multiple", Options: []string{"A", "B"}}}, ID: "multi", Widget: "choice-input", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.55"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted multiple")
	}
	d.UIProfile = PageUIProfile()
	s.ChoiceVariable = "pick"
	if d.checkChoiceInput(s) == nil {
		t.Fatal("dual or coerced ports accepted")
	}
	s.ChoiceVariable = ""
	s.ChoiceInput.Variant = "select"
	if d.checkChoiceInput(s) == nil {
		t.Fatal("single presentation accepted set port")
	}
	s.ChoiceInput.Variant = "multiple"
	v := d.Variables["pick"]
	v.Type = "record-set"
	d.Variables["pick"] = v
	if d.checkChoiceInput(s) == nil {
		t.Fatal("record references coerced to choices")
	}
	v.Type = "string-set"
	v.Mode = "constant"
	d.Variables["pick"] = v
	if d.checkChoiceInput(s) == nil {
		t.Fatal("readonly set accepted")
	}
}

func TestChoiceSetClearRequiresMultipleProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["pick"] = PageVariable{Scope: "page", Type: "string-set", Mode: "state", Initial: Raw(map[string]any{"kind": "string-set", "values": []string{"retired", "A"}})}
	s := Section{PageWidgetConfig: PageWidgetConfig{ChoiceSetVariable: "pick", ChoiceInput: &PageChoiceInput{Variant: "multiple", Options: []string{"A", "B"}, Clearable: true}}, ID: "clear", Widget: "choice-input", ConfigVersion: 1}
	if err := d.checkChoiceInput(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.64"
	if d.checkChoiceInput(s) == nil {
		t.Fatal("old profile accepted full clear")
	}
	s.ChoiceInput.Clearable = false
	if err := d.checkChoiceInput(s); err != nil {
		t.Fatal("legacy multiple failed", err)
	}
	d.UIProfile = PageUIProfile()
	s.ChoiceInput.Clearable = true
	s.ChoiceInput.Variant = "select"
	if d.checkChoiceInput(s) == nil {
		t.Fatal("single choice accepted full clear")
	}
}

// page_input_test.go

func TestOriginalSearchInputBindingAndProfile(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	q := d.Queries["read"]
	q.Search = &PageValue{Variable: "bucket"}
	d.Queries["read"] = q
	s := Section{PageWidgetConfig: PageWidgetConfig{InputKind: "search"}, ID: "search", Widget: "input", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID, ValueVariable: "bucket"}
	r := d.Nodes[d.Root]
	r.Children = append(r.Children, s.ID)
	d.Nodes[d.Root] = r
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.65"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted search input")
	}
	d.UIProfile = PageUIProfile()
	q.Search = nil
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unbound search input accepted")
	}
	q.Search = &PageValue{Variable: "bucket"}
	q.Owner = "other"
	d.Queries["read"] = q
	if d.checkInputPresentation(s) == nil {
		t.Fatal("foreign-owner query accepted")
	}
	q.Owner = ""
	d.Queries["read"] = q
	v := d.Variables["bucket"]
	v.Type = "decimal"
	d.Variables["bucket"] = v
	if d.checkInputPresentation(s) == nil {
		t.Fatal("decimal search coercion accepted")
	}
	v.Type = "string"
	d.Variables["bucket"] = v
	s.Widget = "text"
	if d.checkInputPresentation(s) == nil {
		t.Fatal("search kind on other widget accepted")
	}
}

// page_range_test.go

func TestRangeOriginalDraftsProfileAndOwnership(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["lower"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	d.Variables["upper"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: Raw("")}
	s := Section{PageWidgetConfig: PageWidgetConfig{RangeMinVariable: "lower", RangeMaxVariable: "upper", RangeInput: &PageRangeInput{Min: "0", Max: "45", Step: "1", Label: "Pressure", Unit: "bar"}}, ID: "range", Widget: "range-input", ConfigVersion: 1}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("range without document accepted")
	}
	d.UIProfile = "platform.page.v2.51"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted")
	}
	d.UIProfile = PageUIProfile()
	for _, r := range []PageRangeInput{{Min: "0", Max: "1", Step: "0.3"}, {Min: "0", Max: "10001", Step: "1"}, {Min: "0", Max: "1", Step: "0"}, {Min: "-0", Max: "1", Step: "1"}, {Min: "1", Max: "0", Step: "1"}, {Min: "0", Max: "1e3", Step: "1"}} {
		s.RangeInput = &r
		if d.checkRangeInput(s) == nil {
			t.Fatalf("invalid grid accepted: %+v", r)
		}
	}
	s.RangeInput = &PageRangeInput{Min: "-0.3", Max: "0.3", Step: "0.1"}
	if err := d.checkRangeInput(s); err != nil {
		t.Fatal(err)
	}
	s.RangeInput = &PageRangeInput{Min: "1" + strings.Repeat("0", 126), Max: "1" + strings.Repeat("0", 125) + "1", Step: "0.01"}
	if d.checkRangeInput(s) == nil {
		t.Fatal("generated draft exceeds decimal budget")
	}
	s.RangeInput = &PageRangeInput{Min: "-0.3", Max: "0.3", Step: "0.1"}
	s.RangeMinVariable = "upper"
	if d.checkRangeInput(s) == nil {
		t.Fatal("same state pair accepted")
	}
	s.RangeMinVariable = "lower"
	for _, v := range []PageVariable{{Scope: "page", Type: "string", Mode: "constant", Initial: Raw("")}, {Scope: "page", Type: "decimal", Mode: "state", Initial: Raw(DecimalValue{Kind: "decimal", Value: "0"})}, {Scope: "overlay", Owner: "other", Type: "string", Mode: "state", Initial: Raw("")}} {
		d.Variables["upper"] = v
		if d.checkRangeInput(s) == nil {
			t.Fatal("incompatible state owner accepted")
		}
	}
}

func TestRangeOverlayOwnershipCannotEscape(t *testing.T) {
	d, sections := overlayScopeDocument()
	d.Variables["upper"] = PageVariable{Scope: "overlay", Owner: "panel", Type: "string", Mode: "state", Initial: Raw("")}
	s := Section{PageWidgetConfig: PageWidgetConfig{RangeMinVariable: "local", RangeMaxVariable: "upper", RangeInput: &PageRangeInput{Min: "0", Max: "45", Step: "1"}}, ID: "range", Widget: "range-input", ConfigVersion: 1}
	sections = append(sections, s)
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	overlay := d.Overlays["panel"]
	root := d.Nodes[overlay.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[overlay.Root] = root
	if err := d.Check(sections); err != nil {
		t.Fatal(err)
	}
	root.Children = root.Children[:len(root.Children)-1]
	d.Nodes[overlay.Root] = root
	main := d.Nodes[d.Root]
	main.Children = append(main.Children, s.ID)
	d.Nodes[d.Root] = main
	if d.Check(sections) == nil {
		t.Fatal("overlay range escaped into page")
	}
}

// page_record_picker_test.go

func TestRecordPickerOriginalCandidatesAndProducer(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordPicker: &PageRecordPicker{LabelField: "title"}}, ID: "picker", Widget: "record-picker", ConfigVersion: 1, CollectionVariable: "window", Selection: "picker"}
	p.Selections = []SelectionVariable{{Name: "picker", Object: p.Object}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["picked"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: s.ID}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("picker without doc accepted")
	}
	d.UIProfile = "platform.page.v2.57"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted picker")
	}
	d.UIProfile = PageUIProfile()
	q := d.Queries["read"]
	q.Limit = 21
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("large candidate window accepted")
	}
	q.Limit = 20
	q.Offset = 1
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("candidate paging accepted")
	}
	q.Offset = 0
	q.Sort = []string{"title"}
	d.Queries["read"] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("non-ID order accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "title", Type: "text"}}}
	if err := s.CheckRecordPicker(info); err != nil {
		t.Fatal(err)
	}
	info.Fields[0].Type = "decimal"
	if s.CheckRecordPicker(info) == nil {
		t.Fatal("non-text label accepted")
	}
	named := &Definition{Query: &NamedQuery{Sort: []string{"title"}}}
	if p.CheckRecordPickerQuery("read", named) == nil {
		t.Fatal("named ordering discarded")
	}
}

func TestRecordPickerStableIDStateOwnership(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	d.Variables["personID"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: []byte(`"legacy name"`)}
	s := Section{PageWidgetConfig: PageWidgetConfig{PickerValueVariable: "personID", RecordPicker: &PageRecordPicker{LabelField: "title"}}, ID: "pickerID", Widget: "record-picker", ConfigVersion: 1, CollectionVariable: "window"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.63"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted stable ID output")
	}
	d.UIProfile = PageUIProfile()
	for _, v := range []PageVariable{{Scope: "page", Type: "string", Mode: "constant", Initial: []byte(`""`)}, {Scope: "page", Type: "boolean", Mode: "state", Initial: []byte(`false`)}, {Scope: "overlay", Owner: "other", Type: "string", Mode: "state", Initial: []byte(`""`)}} {
		d.Variables["personID"] = v
		if d.Check(p.Sections) == nil {
			t.Fatal("incompatible ID state accepted")
		}
	}
	d.Variables["personID"] = PageVariable{Scope: "page", Type: "string", Mode: "state", Initial: []byte(`""`)}
	p.Sections[len(p.Sections)-1].Widget = "table"
	if d.Check(p.Sections) == nil {
		t.Fatal("ID output on wrong widget accepted")
	}
}
