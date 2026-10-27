package platform

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// page_record_card_test.go

func recordCardPage() Page {
	p := queryPlanPage()
	d := p.Document
	d.Variables["chosen"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "table"}}
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordCard: &PageRecordCard{LabelField: "name", Tone: "info"}}, ID: "card", Widget: "record-card", ConfigVersion: 1, RecordVariable: "chosen", Fields: []string{"amount"}}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	return p
}
func TestRecordCardOriginalResourceAndFields(t *testing.T) {
	p := recordCardPage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	i := len(p.Sections) - 1
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.72" }, func(p *Page) { p.Sections[i].RecordVariable = "window" }, func(p *Page) { p.Sections[i].Widget = "detail" }, func(p *Page) { p.Sections[i].RecordCard.Tone = "red" }, func(p *Page) { p.Sections[i].Fields = []string{"a", "b", "c", "d", "e"} }, func(p *Page) {
		v := p.Document.Variables["chosen"]
		v.Mode = "constant"
		v.Source = nil
		v.Initial = Raw("x")
		p.Document.Variables["chosen"] = v
	}} {
		p := recordCardPage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid record card accepted")
		}
	}
	s := p.Sections[i]
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("documentless card accepted")
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "amount", Type: "decimal"}}}
	if err := s.CheckRecordCard(info); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[1:]
	if s.CheckRecordCard(info) == nil {
		t.Fatal("hidden title accepted")
	}
	s.RecordCard.LabelField = "id"
	if err := s.CheckRecordCard(info); err != nil {
		t.Fatal(err)
	}
	s.Fields = []string{"amount", "amount"}
	if s.CheckRecordCard(info) == nil {
		t.Fatal("duplicate properties accepted")
	}
}

// page_record_list_test.go

func TestRecordGallerySharedApplicationPort(t *testing.T) {
	makePage := func() Page {
		p := queryPlanPage()
		p.Document.UIProfile = PageUIProfile()
		p.Document.Events = nil
		p.Document.Variables = map[string]PageVariable{"window": p.Document.Variables["window"], "bucket": p.Document.Variables["bucket"]}
		p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"cards"}}, "cards": {Kind: "widget", Section: "cards"}}
		p.Sections = []Section{{PageWidgetConfig: PageWidgetConfig{RecordList: &PageRecordList{Layout: "grid"}}, ID: "cards", Widget: "record-list", ConfigVersion: 1, CardLabel: "id", CollectionVariable: "window", SelectionVariable: "shared"}}
		p.Document.Variables["shared"] = PageVariable{Scope: "application", Mode: "shared", Type: "record", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "record", Object: &p.Object}}
		return p
	}
	p := makePage()
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	if err := p.CheckCollectionPorts(); err != nil {
		t.Fatal(err)
	}
	app := Application{Name: "desk", Pages: []string{p.Name}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"record": {Scope: "application", Mode: "resource", Type: "record", Source: &PageResourceSource{Kind: "record", Object: &p.Object}}}}
	if err := app.CheckPageVariables(p); err != nil {
		t.Fatal(err)
	}
	asset, err := PageReleaseAsset("sample", "cards-1", p)
	if err != nil {
		t.Fatal(err)
	}
	var body Page
	if err = json.Unmarshal(asset.Body, &body); err != nil {
		t.Fatal(err)
	}
	// The release body owns exactly the original writer declaration.
	if body.Sections[0].SelectionVariable != "shared" {
		t.Fatal("lost shared selection in release")
	}
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.93" }, func(p *Page) {
		v := p.Document.Variables["shared"]
		v.Writable = false
		p.Document.Variables["shared"] = v
	}, func(p *Page) { p.Sections[0].Selection = "local" }, func(p *Page) { p.Sections[0].RecordList.Layout = "tiles" }, func(p *Page) {
		p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows"}
		p.Document.Overlays = map[string]PageOverlay{"drawer": {Root: "overlay", Kind: "drawer", OpenVariable: "open"}}
		p.Document.Nodes["overlay"] = PageLayoutNode{Kind: "rows", Children: []string{"cards"}}
		p.Document.Variables["open"] = PageVariable{Scope: "page", Type: "boolean", Mode: "state", Initial: Raw(false)}
	}} {
		p := makePage()
		change(&p)
		if p.Document.Check(p.Sections) == nil {
			t.Fatal("invalid gallery writer accepted")
		}
	}
}

func TestRecordListProfileAndOriginalProducer(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordList: &PageRecordList{Layout: "grid"}}, ID: "cards", Widget: "record-list", ConfigVersion: 1, CardLabel: "title", Fields: []string{"qty"}, CollectionVariable: "window"}
	d.Nodes["cards"] = PageLayoutNode{Kind: "widget", Section: "cards"}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, "cards")
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	d.Variables["cardRecord"] = PageVariable{Scope: "page", Type: "record", Mode: "resource", Source: &PageResourceSource{Kind: "record", Section: "cards"}}
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Type: "sample.note", Fields: []FieldInfo{{Name: "title", Type: "text"}, {Name: "qty", Type: "integer"}}}
	if err := s.CheckRecordList(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.41"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted cards")
	}
	d.UIProfile = PageUIProfile()
	copy := s
	copy.RecordList = &PageRecordList{Layout: "script"}
	if d.checkRecordList(copy) == nil {
		t.Fatal("unknown layout accepted")
	}
	copy = s
	copy.Fields = []string{"qty", "qty"}
	if copy.CheckRecordList(info) == nil {
		t.Fatal("duplicate summary accepted")
	}
	copy = s
	copy.CardLabel = "hidden"
	if copy.CheckRecordList(info) == nil {
		t.Fatal("hidden label accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("cards without document accepted")
	}
}

// page_record_view_test.go

func TestRecordViewProfileAndTabs(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordView: &PageRecordView{Tabs: []string{"overview", "properties", "links", "history"}}}, Widget: "record-view"}
	if err := d.checkRecordView(s); err != nil {
		t.Fatal(err)
	}
	for _, tabs := range [][]string{nil, {"script"}, {"overview", "overview"}} {
		copy := s
		copy.RecordView = &PageRecordView{Tabs: tabs}
		if d.checkRecordView(copy) == nil {
			t.Fatal("invalid tabs accepted", tabs)
		}
	}
	d.UIProfile = "platform.page.v2.35"
	if d.checkRecordView(s) == nil {
		t.Fatal("old profile accepted record view")
	}
	d.UIProfile = PageUIProfile()
	s.Widget = "detail"
	if s.checkWidgetConfiguration() == nil {
		t.Fatal("another widget accepted record view config")
	}
}

// page_record_links_test.go

func TestRecordLinksProfileSchemaAndFrozenDependencies(t *testing.T) {
	parent := EntityInfo{App: "sample", Type: "sample.parent"}
	child := EntityInfo{App: "sample", Type: "sample.child", Fields: []FieldInfo{{Name: "parent", Type: "reference", Ref: parent.Type, Inverse: "children"}}}
	ref := AssetRef{App: child.App, Kind: AssetObject, Name: child.Type}
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordLinks: []PageRecordLink{{Object: ref, Field: "parent", Title: "Child records"}}}, ID: "links", Widget: "record-links", ConfigVersion: 1}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"links"}}, "links": {Kind: "widget", Section: "links"}}}
	lookup := func(typ string) (EntityInfo, bool) { return child, typ == child.Type }
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("record links without a document accepted")
	}
	if err := d.Check([]Section{s}); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckRecordLinks(parent, lookup); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Section){func(s *Section) { s.RecordLinks = nil }, func(s *Section) { s.RecordLinks = append(s.RecordLinks, s.RecordLinks[0]) }, func(s *Section) { s.RecordLinks[0].Field = "" }, func(s *Section) { s.Fields = []string{"parent"} }, func(s *Section) { s.Widget = "detail" }} {
		copy := s
		copy.RecordLinks = slices.Clone(s.RecordLinks)
		change(&copy)
		if d.Check([]Section{copy}) == nil {
			t.Fatal("invalid record links accepted")
		}
	}
	d.UIProfile = "platform.page.v2.37"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted links")
	}
	d.UIProfile = PageUIProfile()
	p := Page{Name: "parents", Title: "Parents", Object: AssetRef{App: parent.App, Kind: AssetObject, Name: parent.Type}, Document: d, Sections: []Section{s}}
	page, err := PageReleaseAsset("sample", "1", p)
	if err != nil || !slices.Contains(page.Requires, ref) {
		t.Fatalf("dependency missing: %+v %v", page, err)
	}
	body, _ := json.Marshal(parent)
	parentAsset := ReleaseAsset{Ref: p.Object, SourceVersion: "1", ContractVersion: 1, Body: body}
	body, _ = json.Marshal(child)
	childAsset, err := WithDependencies(ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, parentAsset, childAsset}); err != nil {
		t.Fatal(err)
	}
	child.Fields[0].Inverse = ""
	if s.CheckRecordLinks(parent, lookup) == nil {
		t.Fatal("undeclared inverse accepted")
	}
	body, _ = json.Marshal(child)
	childAsset.Body = body
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, parentAsset, childAsset}); err == nil {
		t.Fatal("frozen invalid incoming reference accepted")
	}
	child.Fields[0].Inverse = "children"
	child.Fields[0].Ref = "sample.other"
	if s.CheckRecordLinks(parent, lookup) == nil {
		t.Fatal("wrong parent accepted")
	}
}

// page_record_events_test.go

func TestRecordEventsRequireOriginalFieldsTonesAndOrderedPlan(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	query := d.Variables["window"].Source.Query
	q := d.Queries[query]
	q.Sort = []string{"id"}
	d.Queries[query] = q
	s := Section{PageWidgetConfig: PageWidgetConfig{RecordEvents: &PageRecordEvents{TimeField: "raised", TitleField: "title", SeverityField: "severity", Tones: []PageEventTone{{Value: "high", Tone: "danger"}}}}, ID: "events", Widget: "record-events", ConfigVersion: 1, CollectionVariable: "window"}
	d.Nodes[s.ID] = PageLayoutNode{Kind: "widget", Section: s.ID}
	root := d.Nodes[d.Root]
	root.Children = append(root.Children, s.ID)
	d.Nodes[d.Root] = root
	p.Sections = append(p.Sections, s)
	if err := d.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	info := EntityInfo{Fields: []FieldInfo{{Name: "raised", Type: "datetime"}, {Name: "title", Type: "text"}, {Name: "severity", Type: "choice", Choices: []string{"high", "low"}}}}
	if err := s.CheckRecordEvents(info); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.44"
	if d.Check(p.Sections) == nil {
		t.Fatal("old profile accepted event list")
	}
	d.UIProfile = PageUIProfile()
	q.Sort = nil
	d.Queries[query] = q
	if d.Check(p.Sections) == nil {
		t.Fatal("unordered event list accepted")
	}
	copy := s
	copy.RecordEvents = &PageRecordEvents{TimeField: "created", TitleField: "title", SeverityField: "severity"}
	if copy.CheckRecordEvents(info) == nil {
		t.Fatal("creation time substituted for business time")
	}
	copy.RecordEvents = &PageRecordEvents{TimeField: "raised", TitleField: "title", SeverityField: "severity", Tones: []PageEventTone{{Value: "unknown", Tone: "danger"}}}
	if copy.CheckRecordEvents(info) == nil {
		t.Fatal("unknown severity tone accepted")
	}
	copy.RecordEvents.Tones = []PageEventTone{{Value: "high", Tone: "danger"}, {Value: "high", Tone: "warning"}}
	q.Sort = []string{"id"}
	d.Queries[query] = q
	if d.checkRecordEvents(copy) == nil {
		t.Fatal("duplicate tone accepted")
	}
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("events without document accepted")
	}
}

// page_table_presentation_test.go

func TestTablePresentationBoundsAndFieldFormats(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	search := false
	s := Section{PageWidgetConfig: PageWidgetConfig{ShowSearch: &search, TableColumns: []PageTableColumn{{Field: "id", Title: "Record", Width: 90}, {Field: "amount", Formatter: "numeric"}, {Field: "status", Formatter: "badge"}, {Field: "date", Formatter: "date"}}}, Widget: "table", Fields: []string{"amount", "status", "date"}}
	info := EntityInfo{Fields: []FieldInfo{{Name: "amount", Type: "decimal"}, {Name: "status", Type: "choice"}, {Name: "date", Type: "date"}}}
	if err := d.checkTablePresentation(s); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTablePresentation(info); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Section){func(s *Section) { s.Widget = "detail" }, func(s *Section) { s.TableColumns[0].Width = 39 }, func(s *Section) { s.TableColumns[0].Width = 1201 }, func(s *Section) { s.TableColumns[0].Formatter = "numeric" }, func(s *Section) { s.TableColumns[1].Field = "hidden" }, func(s *Section) { s.TableColumns = append(s.TableColumns, s.TableColumns[0]) }, func(s *Section) { s.TableColumns[0].Formatter = "script" }} {
		copy := s
		copy.TableColumns = append([]PageTableColumn{}, s.TableColumns...)
		change(&copy)
		if d.checkTablePresentation(copy) == nil {
			t.Fatal("invalid column accepted", copy)
		}
	}
	d.UIProfile = "platform.page.v2.33"
	if d.checkTablePresentation(s) == nil {
		t.Fatal("old profile accepted new presentation")
	}
	d.UIProfile = PageUIProfile()
	bad := s
	bad.TableColumns = []PageTableColumn{{Field: "status", Formatter: "numeric"}}
	if bad.CheckTablePresentation(info) == nil {
		t.Fatal("numeric formatter accepted choice")
	}
	bad.TableColumns = []PageTableColumn{{Field: "unknown"}}
	if bad.CheckTablePresentation(info) == nil {
		t.Fatal("unknown field accepted")
	}
}

func TestTableControlsFreezeBoundedOriginalCountTitle(t *testing.T) {
	p := collectionBuilderPage()
	p.Sections = p.Sections[1:]
	p.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"table"}}, "table": {Kind: "widget", Section: "table"}}
	p.Document.Queries = map[string]PageQuery{"result": {Object: p.Object, Limit: 2}}
	delete(p.Document.Variables, "output")
	delete(p.Document.Variables, "base")
	title := "Assets ({count})"
	p.Sections[0].TablePresentation = &PageTablePresentation{Density: "compact", ShowToolbar: true, TitleTemplate: &title}
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*Page){func(p *Page) { p.Document.UIProfile = "platform.page.v2.89" }, func(p *Page) { p.Sections[0].TablePresentation.Density = "large" }, func(p *Page) { s := "{count} {count}"; p.Sections[0].TablePresentation.TitleTemplate = &s }, func(p *Page) { s := "{execute()}"; p.Sections[0].TablePresentation.TitleTemplate = &s }, func(p *Page) { p.Sections[0].CollectionVariable = "" }, func(p *Page) { p.Sections[0].Widget = "detail" }} {
		copy := p
		section := p.Sections[0]
		style := *section.TablePresentation
		section.TablePresentation = &style
		copy.Sections = []Section{section}
		doc := *p.Document
		copy.Document = &doc
		change(&copy)
		if copy.Document.Check(copy.Sections) == nil {
			t.Fatal("invalid table controls accepted")
		}
	}
	empty := ""
	p.Sections[0].TablePresentation.TitleTemplate = &empty
	p.Sections[0].TablePresentation.ShowToolbar = false
	if err := p.Document.Check(p.Sections); err != nil {
		t.Fatal(err)
	}
}

// page_table_edit_test.go

func TestTableEditingProfileAndOriginalAction(t *testing.T) {
	p := queryPlanPage()
	d := p.Document
	s := Section{ID: "table", Widget: "table", ConfigVersion: 1, Fields: []string{"name", "locked"}, InlineEdit: &PageInlineEdit{Action: AssetRef{App: "sample", Kind: AssetAction, Name: "sample.note.edit"}, Fields: []string{"name"}}}
	info := EntityInfo{App: "sample", Type: "sample.note", Standard: []string{"sample.note.edit"}, Fields: []FieldInfo{{Name: "name", Type: "text"}, {Name: "locked", Type: "text", ReadOnly: true}}}
	action := Action{Schema: "sample.note.edit", Target: info.Type, Payload: []Field{{Name: "name", Type: "string"}}}
	if err := d.checkTableEdit(s); err != nil {
		t.Fatal(err)
	}
	if err := s.CheckTableEdit(info, action); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Section){func(s *Section) { s.InlineEdit.Fields = []string{"locked"} }, func(s *Section) { s.InlineEdit.Action.Name = "sample.note.archive" }, func(s *Section) { s.InlineEdit.Fields = []string{"id"} }} {
		changed := s
		edit := *s.InlineEdit
		changed.InlineEdit = &edit
		mutate(&changed)
		if changed.CheckTableEdit(info, action) == nil {
			t.Fatal("unsupported edit accepted")
		}
	}
	d.UIProfile = "platform.page.v2.30"
	if d.checkTableEdit(s) == nil {
		t.Fatal("old profile accepted editing")
	}
	action.NeedsApproval = true
	if s.CheckTableEdit(info, action) == nil {
		t.Fatal("approval action disguised as a field patch")
	}
}

// page_detail_presentation_test.go

func TestDetailPresentationProfileAndBounds(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	s := Section{PageWidgetConfig: PageWidgetConfig{DetailPresentation: &PageDetailPresentation{Columns: 2, HideNull: true}}, Widget: "detail"}
	if err := d.checkDetailPresentation(s); err != nil {
		t.Fatal(err)
	}
	for _, columns := range []int{0, -1, 5} {
		copy := s
		copy.DetailPresentation = &PageDetailPresentation{Columns: columns}
		if d.checkDetailPresentation(copy) == nil {
			t.Fatal("invalid detail columns accepted", columns)
		}
	}
	s.Widget = "table"
	if s.checkWidgetConfiguration() == nil {
		t.Fatal("table accepted detail presentation")
	}
	s.Widget = "detail"
	d.UIProfile = "platform.page.v2.34"
	if d.checkDetailPresentation(s) == nil {
		t.Fatal("old profile accepted detail presentation")
	}
	s.DetailPresentation = nil
	if err := d.checkDetailPresentation(s); err != nil {
		t.Fatal("legacy detail changed", err)
	}
}

// page_metric_presentation_test.go

func TestMetricPresentationAndFrozenMeasure(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.item", Fields: []FieldInfo{{Name: "qty", Type: "decimal"}, {Name: "amount", Type: "money"}}}
	s := Section{PageWidgetConfig: PageWidgetConfig{MetricPresentation: &PageMetricPresentation{Suffix: "%", Formatter: "number", Variant: "card", Tone: "success"}}, ID: "metric", Widget: "metric", ConfigVersion: 1, Measure: "avg:qty"}
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"metric"}}, "metric": {Kind: "widget", Section: "metric"}}}
	if d.Check([]Section{s}) != nil || s.CheckMetricPresentation(info) != nil {
		t.Fatal("valid metric presentation refused")
	}
	for _, change := range []func(*Section){func(s *Section) { s.MetricPresentation.Prefix = strings.Repeat("x", 65) }, func(s *Section) { s.MetricPresentation.Formatter = "script" }, func(s *Section) { s.Widget = "text" }} {
		copy := s
		value := *s.MetricPresentation
		copy.MetricPresentation = &value
		change(&copy)
		if d.Check([]Section{copy}) == nil {
			t.Fatal("invalid metric presentation accepted")
		}
	}
	d.UIProfile = "platform.page.v2.39"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted display")
	}
	d.UIProfile = PageUIProfile()
	if (*PageDocument)(nil).Check([]Section{s}) == nil {
		t.Fatal("display without document accepted")
	}
	ref := AssetRef{App: info.App, Kind: AssetObject, Name: info.Type}
	page, err := PageReleaseAsset("sample", "1", Page{Name: "metrics", Title: "Metrics", Object: ref, Document: d, Sections: []Section{s}})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(info)
	object := ReleaseAsset{Ref: ref, SourceVersion: "1", ContractVersion: 1, Body: body}
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err != nil {
		t.Fatal(err)
	}
	info.Fields = info.Fields[1:]
	body, _ = json.Marshal(info)
	object.Body = body
	if _, err := Candidate([]AssetRef{page.Ref}, []ReleaseAsset{page, object}); err == nil {
		t.Fatal("frozen missing measure field accepted")
	}
	s.Measure = "sum:amount"
	s.MetricPresentation.Formatter = "short"
	if s.CheckMetricPresentation(info) == nil {
		t.Fatal("money lost currency semantics")
	}
}

func TestMetricStaticAnnotationProfileAndBudget(t *testing.T) {
	d := &PageDocument{UIProfile: PageUIProfile()}
	s := Section{PageWidgetConfig: PageWidgetConfig{MetricPresentation: &PageMetricPresentation{Formatter: "number", Variant: "card", Tone: "neutral", Annotation: &PageMetricAnnotation{Direction: "up", Text: "+3"}}}, Widget: "metric"}
	if err := d.checkMetricPresentation(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.89"
	if d.checkMetricPresentation(s) == nil {
		t.Fatal("old profile accepted annotation")
	}
	d.UIProfile = PageUIProfile()
	for _, value := range []PageMetricAnnotation{{Direction: "script", Text: "a"}, {Direction: "flat", Text: strings.Repeat("x", 257)}} {
		s.MetricPresentation.Annotation = &value
		if d.checkMetricPresentation(s) == nil {
			t.Fatal("invalid annotation accepted")
		}
	}
}

// page_region_presentation_test.go

func TestOriginalRegionPresentation(t *testing.T) {
	makePage := func() (*PageDocument, []Section) {
		d, s := nestedDocument()
		padding := 10
		n := d.Nodes["columns"]
		n.Title = "Detail"
		n.Presentation = &PageRegionPresentation{Padding: &padding, Background: "panel", Border: true, ShowHeader: true, Collapsible: true}
		d.Nodes["columns"] = n
		return d, s
	}
	d, s := makePage()
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var restored PageDocument
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err = restored.Check(s); err != nil {
		t.Fatal(err)
	}
	if !restored.Nodes["columns"].Presentation.Collapsible || *restored.Nodes["columns"].Presentation.Padding != 10 {
		t.Fatal("lost original presentation")
	}
	for _, change := range []func(*PageDocument){func(d *PageDocument) { d.UIProfile = "platform.page.v2.92" }, func(d *PageDocument) { *d.Nodes["columns"].Presentation.Padding = 65 }, func(d *PageDocument) { *d.Nodes["columns"].Presentation.Padding = -1 }, func(d *PageDocument) { d.Nodes["columns"].Presentation.Background = "script" }, func(d *PageDocument) { d.Nodes["columns"].Presentation.ShowHeader = false }, func(d *PageDocument) { n := d.Nodes["columns"]; n.Title = ""; d.Nodes["columns"] = n }, func(d *PageDocument) {
		p := d.Nodes["columns"].Presentation
		p.Collapsible = false
		p.DefaultCollapsed = true
	}, func(d *PageDocument) {
		n := d.Nodes["table"]
		n.Presentation = d.Nodes["columns"].Presentation
		d.Nodes["table"] = n
	}} {
		d, s := makePage()
		change(d)
		if err := d.Check(s); err == nil {
			t.Fatal("accepted invalid region")
		}
	}
	d, s = makePage()
	*d.Nodes["columns"].Presentation.Padding = 0
	if err := d.Check(s); err != nil {
		t.Fatal("lost explicit zero", err)
	}
}

// page_timeline_test.go

func TestRecordTimelineMappingsRequireTypedWindowAndVisibleFields(t *testing.T) {
	info := EntityInfo{Type: "sample.job", Fields: []FieldInfo{{Name: "start", Type: "date"}, {Name: "end", Type: "date"}, {Name: "instant", Type: "datetime"}, {Name: "title", Type: "text"}, {Name: "team", Type: "choice"}, {Name: "qty", Type: "integer"}}}
	s := Section{ID: "schedule", Widget: "record-timeline", ConfigVersion: 1, CollectionVariable: "window", TimeStart: "start", TimeEnd: "end", TimeLabel: "title", TimeGroup: "team"}
	if err := s.CheckTimeline(info); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []func(*Section){func(s *Section) { s.TimeStart = "qty" }, func(s *Section) { s.TimeEnd = "instant" }, func(s *Section) { s.TimeLabel = "missing" }, func(s *Section) { s.TimeGroup = "qty" }, func(s *Section) { s.CollectionVariable = "" }} {
		bad := s
		patch(&bad)
		if bad.CheckTimeline(info) == nil {
			t.Fatal("accepted incompatible timeline", bad)
		}
	}
	visible := info
	visible.Fields = visible.Fields[1:]
	if s.CheckTimeline(visible) == nil {
		t.Fatal("hidden start silently fell back")
	}
	if !WidgetWritesSelection(s.Widget) || WidgetWritesSelection("timeline") {
		t.Fatal("history and record-time selection identities were conflated")
	}
}
func TestRecordTimelineRequiresNewProfileAndPreservesHistory(t *testing.T) {
	d, _ := nestedDocument()
	d.UIProfile = PageUIProfile()
	d.Root = "root"
	d.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"schedule"}}, "schedule": {Kind: "widget", Section: "schedule"}}
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.job"}
	d.Queries = map[string]PageQuery{"read": {Object: object, Limit: 10}}
	d.Variables = map[string]PageVariable{"window": {Scope: "page", Type: "object-set", Mode: "resource", Source: &PageResourceSource{Kind: "plan", Query: "read"}}}
	s := []Section{{ID: "schedule", Widget: "record-timeline", ConfigVersion: 1, CollectionVariable: "window", TimeStart: "start", TimeLabel: "id"}}
	if err := d.Check(s); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.24"
	if d.Check(s) == nil {
		t.Fatal("old profile accepted new timeline")
	}
	d.UIProfile = PageUIProfile()
	s[0].ConfigVersion = 2
	if d.Check(s) == nil {
		t.Fatal("unknown config version accepted")
	}
}

// page_kanban_test.go

func TestKanbanUsesOriginalStatusFieldsAndTransitions(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.task", Fields: []FieldInfo{{Name: "title", Type: "text"}, {Name: "state", Type: "choice"}, {Name: "qty", Type: "integer"}}, Lifecycle: &LifecycleInfo{Field: "state", States: []State{{Name: "open"}, {Name: "done"}}, Transitions: []TransitionInfo{{Schema: "sample.task.close", From: []string{"open"}, To: []string{"done"}}}}}
	s := Section{Widget: "kanban", CollectionVariable: "window", CardLabel: "title", Fields: []string{"qty"}, Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.task.close"}}}
	if err := s.CheckKanban(info); err != nil {
		t.Fatal(err)
	}
	for _, patch := range []func(*Section){func(s *Section) { s.CardLabel = "qty" }, func(s *Section) { s.Fields = []string{"private"} }, func(s *Section) { s.CollectionVariable = "" }, func(s *Section) { s.Actions[0] = AssetRef{App: "sample", Kind: AssetAction, Name: "sample.task.edit"} }} {
		bad := s
		bad.Actions = append([]AssetRef(nil), s.Actions...)
		patch(&bad)
		if bad.CheckKanban(info) == nil {
			t.Fatal("accepted invalid kanban", bad)
		}
	}
	hidden := info
	hidden.Fields = hidden.Fields[:1]
	if s.CheckKanban(hidden) == nil {
		t.Fatal("hidden status produced a board")
	}
	info.Lifecycle.Transitions[0].To = []string{"open", "done"}
	if s.CheckKanban(info) == nil {
		t.Fatal("ambiguous native destination became a move")
	}
}

func TestParameterizedKanbanUsesDeclaredDestinationAndAdmittedProfile(t *testing.T) {
	type task struct {
		Record
		State string `json:"state" field:"readonly"`
	}
	transition := Transition{Name: "update", From: []string{"open", "done"}, To: []string{"open", "done"}, ToInput: "status", Payload: []Field{{Name: "status", Type: "string", Required: true, Choices: []string{"open", "done"}}}, Do: func(Caller, any, json.RawMessage, time.Time) *kernel.Error { return nil }}
	e := Entity{Type: "sample.task", Model: task{}, Lifecycle: &Lifecycle{Field: "state", Initial: "open", States: []State{{Name: "open"}, {Name: "done"}}, Transitions: []Transition{transition}}}
	info, err := Describe("sample", e, func(reflect.Type) string { return "" })
	if err != nil {
		t.Fatal(err)
	}
	if info.Lifecycle.Transitions[0].ToInput != "status" {
		t.Fatal("lost original destination input")
	}
	s := Section{Widget: "kanban", CollectionVariable: "window", CardLabel: "id", Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.task.update"}}}
	p := Page{Document: &PageDocument{UIProfile: PageUIProfile()}}
	if err := p.CheckKanban(s, info); err != nil {
		t.Fatal(err)
	}
	p.Document.UIProfile = "platform.page.v2.98"
	if p.CheckKanban(s, info) == nil {
		t.Fatal("old profile admitted dynamic gesture")
	}
	for _, change := range []func(*Transition){func(t *Transition) { t.ToInput = "missing" }, func(t *Transition) { t.Payload[0].Required = false }, func(t *Transition) { t.Payload[0].Choices = []string{"open", "fake"} }, func(t *Transition) { t.Do = nil }} {
		bad := transition
		bad.Payload = append([]Field(nil), transition.Payload...)
		change(&bad)
		e.Lifecycle.Transitions = []Transition{bad}
		if _, err := Describe("sample", e, func(reflect.Type) string { return "" }); err == nil {
			t.Fatal("accepted false destination description")
		}
	}
}

func TestFrozenParameterizedLifecycleRetainsItsOriginalDestination(t *testing.T) {
	info, err := queryObjectDescriptor([]byte(`{"type":"sample.task","states":[{"name":"open"},{"name":"done"}],"actions":[{"name":"update","from":["open","done"],"toInput":"status","inputs":[{"name":"status","type":"choice","required":true,"choices":"open,done"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	move := info.Lifecycle.Transitions[0]
	if move.ToInput != "status" || len(move.To) != 2 {
		t.Fatal("frozen descriptor lost state input", move)
	}
	if _, err := queryObjectDescriptor([]byte(`{"type":"sample.task","states":[{"name":"open"}],"actions":[{"name":"update","from":["open"],"toInput":"status","inputs":[{"name":"status","type":"choice","required":true,"choices":"fake"}]}]}`)); err == nil {
		t.Fatal("frozen destination escaped original states")
	}
}

func TestCanonicalTransactionKanbanDestinationsRemainInLiveAndFrozenDescriptions(t *testing.T) {
	type task struct {
		Record
		State string `json:"state" field:"readonly"`
	}
	condition := ExprLiteralValue("boolean", true)
	for _, tc := range []struct {
		name     string
		value    Expr
		guard    *Expr
		when     *Expr
		repeated bool
		to       []string
		input    string
	}{
		{name: "input", value: ExprReference("text", "input.status"), to: []string{"open", "done"}, input: "status"},
		{name: "literal", value: ExprLiteralValue("text", "done"), to: []string{"done"}},
		{name: "guard", value: ExprReference("text", "input.status"), guard: &condition, to: []string{"open", "done"}, input: "status"},
		{name: "conditional update", value: ExprReference("text", "input.status"), when: &condition, to: []string{"open", "done"}},
		{name: "repeated update", value: ExprReference("text", "input.status"), repeated: true, to: []string{"open", "done"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			step := TxnStep{Name: "move", Kind: "update", Target: TxnRef{Type: "sample.task", ID: "$subject"}, When: tc.when, Changes: []TxnSet{{Field: "state", Expr: &tc.value}}}
			txn := &BusinessTxn{Subject: "sample.task", Guard: tc.guard, Steps: []TxnStep{step}}
			if tc.repeated {
				step.Name = "again"
				txn.Steps = append(txn.Steps, step)
			}
			transition := Transition{Name: "update", From: []string{"open", "done"}, To: []string{"open", "done"}, Payload: []Field{{Name: "status", Type: "string", Required: true, Choices: []string{"open", "done"}}}, Txn: txn,
				ApplyTxn: func(Caller, *pb.ChangeRecord, time.Time) *kernel.Error { return nil }}
			e := Entity{Type: "sample.task", Model: task{}, Lifecycle: &Lifecycle{Field: "state", Initial: "open", States: []State{{Name: "open"}, {Name: "done"}}, Transitions: []Transition{transition}}}
			live, err := Describe("sample", e, func(reflect.Type) string { return "" })
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(map[string]any{
				"type": e.Type, "states": e.Lifecycle.States,
				"actions": []any{map[string]any{"name": transition.Name, "from": transition.From, "transaction": txn,
					"inputs": []any{map[string]any{"name": "status", "type": "choice", "required": true, "choices": "open,done"}}}},
			})
			if err != nil {
				t.Fatal(err)
			}
			frozen, err := queryObjectDescriptor(body)
			if err != nil {
				t.Fatal(err)
			}
			for _, info := range []EntityInfo{live, frozen} {
				move := info.Lifecycle.Transitions[0]
				if move.ToInput != tc.input || !reflect.DeepEqual(move.To, tc.to) {
					t.Fatalf("lost canonical destination: got %#v, want %v from %q", move, tc.to, tc.input)
				}
				s := Section{Widget: "kanban", CollectionVariable: "window", CardLabel: "id", Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.task.update"}}}
				p := Page{Document: &PageDocument{UIProfile: PageUIProfile()}}
				if err := p.CheckKanban(s, info); (err == nil) != (tc.input != "" || len(tc.to) == 1) {
					t.Fatalf("unexpected board destination admission: %v", err)
				}
			}
			if tc.input != "" {
				e.Lifecycle.Transitions[0].ApplyTxn = nil
				if _, err := Describe("sample", e, func(reflect.Type) string { return "" }); err == nil {
					t.Fatal("accepted canonical dynamic destination without its transaction executor")
				}
			}
		})
	}
}

// page_inline_action_test.go

func TestInlineActionRequiresOneOriginalRecordAction(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.task"}
	action := Action{Schema: "sample.task.close", Target: info.Type}
	s := Section{Widget: "inline-action", Actions: []AssetRef{{App: info.App, Kind: AssetAction, Name: action.Schema}}}
	if err := s.CheckInlineAction(info, action); err != nil {
		t.Fatal(err)
	}
	doc := PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"inline"}}, "inline": {Kind: "widget", Section: "inline"}}}
	s.ID, s.ConfigVersion = "inline", 1
	if err := doc.Check([]Section{s}); err != nil {
		t.Fatal(err)
	}
	doc.UIProfile = "platform.page.v2.28"
	if doc.Check([]Section{s}) == nil {
		t.Fatal("old browser profile accepted inline action")
	}
	doc.UIProfile = PageUIProfile()
	s.ConfigVersion = 2
	if doc.Check([]Section{s}) == nil {
		t.Fatal("unknown inline configuration accepted")
	}
	s.ConfigVersion = 1
	for _, patch := range []func(*Section, *Action){
		func(s *Section, a *Action) { s.Actions = nil },
		func(s *Section, a *Action) { s.Actions = append(s.Actions, s.Actions[0]) },
		func(s *Section, a *Action) { s.Actions[0].App = "other" },
		func(s *Section, a *Action) { s.Actions[0].Kind = AssetObject },
		func(s *Section, a *Action) { a.Schema = "sample.task.edit" },
		func(s *Section, a *Action) { a.Target = "sample.other" },
		func(s *Section, a *Action) { a.New = true },
	} {
		bad, declaration := s, action
		bad.Actions = append([]AssetRef(nil), s.Actions...)
		patch(&bad, &declaration)
		if bad.CheckInlineAction(info, declaration) == nil {
			t.Fatalf("accepted invalid binding: %+v %+v", bad, declaration)
		}
	}
}

func TestInlineActionDefaultsKeepOriginalParameterAndReferenceTypes(t *testing.T) {
	info := EntityInfo{App: "sample", Type: "sample.item", Fields: []FieldInfo{{Name: "state", Type: "choice"}, {Name: "owner", Type: "reference", Ref: "sample.person"}}}
	action := Action{Schema: "sample.item.update", Target: info.Type, Payload: []Field{{Name: "status", Type: "string"}, {Name: "owner", Type: "string", Ref: "sample.person"}}}
	s := Section{PageWidgetConfig: PageWidgetConfig{ActionDefaults: []PageActionParameter{{Parameter: "status", Field: "state"}, {Parameter: "owner", Field: "owner"}}}, Widget: "inline-action", Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: action.Schema}}}
	if err := s.CheckInlineAction(info, action); err != nil {
		t.Fatal(err)
	}
	private := info
	private.Fields = private.Fields[:1]
	if s.CheckInlineAction(private, action) == nil {
		t.Fatal("withheld default field accepted")
	}
	action.Payload[1].Ref = "sample.other"
	if s.CheckInlineAction(info, action) == nil {
		t.Fatal("reference default changed object identity")
	}
}

func TestActionDefaultDocumentBudgetAndProfile(t *testing.T) {
	d := &PageDocument{FormatVersion: 2, UIProfile: PageUIProfile(), Root: "root", Nodes: map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"action"}}, "action": {Kind: "widget", Section: "action"}}}
	s := Section{PageWidgetConfig: PageWidgetConfig{ActionDefaults: []PageActionParameter{{Parameter: "reason", Field: "note"}}}, ID: "action", Widget: "inline-action", ConfigVersion: 1, Actions: []AssetRef{{App: "sample", Kind: AssetAction, Name: "sample.item.update"}}}
	if err := d.Check([]Section{s}); err != nil {
		t.Fatal(err)
	}
	d.UIProfile = "platform.page.v2.91"
	if d.Check([]Section{s}) == nil {
		t.Fatal("old profile accepted defaults")
	}
	d.UIProfile = PageUIProfile()
	s.ActionDefaults = append(s.ActionDefaults, s.ActionDefaults[0])
	if d.Check([]Section{s}) == nil {
		t.Fatal("duplicate defaults accepted")
	}
	s.ActionDefaults = []PageActionParameter{{Parameter: "constructor", Field: "note"}}
	if d.Check([]Section{s}) == nil {
		t.Fatal("reserved parameter accepted")
	}
}

func TestFrozenInlineDefaultsUseNestedOriginalDeclaration(t *testing.T) {
	object := AssetRef{App: "sample", Kind: AssetObject, Name: "sample.item"}
	ref := AssetRef{App: "sample", Kind: AssetAction, Name: "sample.item.update"}
	info := EntityInfo{App: object.App, Type: object.Name, Fields: []FieldInfo{{Name: "note", Type: "text"}}}
	action := Action{Schema: ref.Name, Target: object.Name, Payload: []Field{{Name: "reason", Type: "string"}}}
	page := Page{Object: object, Document: &PageDocument{UIProfile: PageUIProfile()}, Sections: []Section{{PageWidgetConfig: PageWidgetConfig{ActionDefaults: []PageActionParameter{{Parameter: "reason", Field: "note"}}}, Widget: "inline-action", Actions: []AssetRef{ref}}}}
	lookup := map[AssetRef]ReleaseAsset{object: {Body: Raw(info)}, ref: {Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": action})}}
	if err := checkFrozenQueries(page, lookup); err != nil {
		t.Fatal(err)
	}
	action.Payload[0].Type = "boolean"
	lookup[ref] = ReleaseAsset{Body: Raw(map[string]any{"schema": action.Schema, "target": action.Target, "action": action})}
	if checkFrozenQueries(page, lookup) == nil {
		t.Fatal("nested parameter tampering passed")
	}
	action.Schema = "sample.item.other"
	lookup[ref] = ReleaseAsset{Body: Raw(map[string]any{"schema": ref.Name, "target": object.Name, "action": action})}
	if checkFrozenQueries(page, lookup) == nil {
		t.Fatal("nested identity tampering passed")
	}
}

// page_shared_record_set_test.go

func TestSharedRecordSetDeclarationsAndOriginalBindings(t *testing.T) {
	p := recordComparisonPage()
	object := p.Object
	p.Document.Variables["picked"] = PageVariable{Scope: "application", Mode: "shared", Type: "record-set", Writable: true, Source: &PageResourceSource{Kind: "application", Variable: "records", Object: &object}}
	app := Application{Name: "desk", Pages: []string{p.Name}, UIProfile: PageUIProfile(), Variables: map[string]PageVariable{"records": {Scope: "application", Type: "record-set", Mode: "resource", Source: &PageResourceSource{Kind: "record-set", Object: &object}}}}
	for _, err := range []error{app.CheckVariables(), p.Document.Check(p.Sections), app.CheckPageVariables(p), p.CheckCollectionPorts(), p.CheckRecordPorts()} {
		if err != nil {
			t.Fatal(err)
		}
	}
	if !slices.Contains(app.QueryPage().QueryReferences(), object) || p.RecordSetVariableObject("picked") != object {
		t.Fatal("lost original object requirement")
	}
	// A consumer needs no local table or additional query plan.
	consumer := p
	consumer.Sections = p.Sections[1:]
	copy := *p.Document
	consumer.Document = &copy
	consumer.Document.Nodes = map[string]PageLayoutNode{"root": {Kind: "rows", Children: []string{"comparison"}}, "comparison": {Kind: "widget", Section: "comparison"}}
	v := consumer.Document.Variables["picked"]
	v.Writable = false
	consumer.Document.Variables = map[string]PageVariable{"picked": v}
	if err := consumer.Document.Check(consumer.Sections); err != nil {
		t.Fatal(err)
	}
	if err := consumer.CheckRecordPorts(); err != nil {
		t.Fatal(err)
	}
	app.Variables["has"] = PageVariable{Scope: "application", Type: "boolean", Mode: "derived", Expression: &PageExpression{Op: "present", Args: []PageValue{{Variable: "records"}}}}
	if app.CheckVariables() == nil {
		t.Fatal("unsupported set scalar expression accepted")
	}
	delete(app.Variables, "has")
	old := app.UIProfile
	app.UIProfile = "platform.page.v2.95"
	if app.CheckVariables() == nil {
		t.Fatal("old application profile accepted")
	}
	app.UIProfile = old
	appv := app.Variables["records"]
	appv.Source.Section = "table"
	app.Variables["records"] = appv
	if app.CheckVariables() == nil {
		t.Fatal("application adopted page producer")
	}
	appv.Source.Section = ""
	app.Variables["records"] = appv
	v = p.Document.Variables["picked"]
	v.Writable = false
	p.Document.Variables["picked"] = v
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("read-only table output accepted")
	}
	v.Writable = true
	p.Document.Variables["picked"] = v
	p.Document.UIProfile = "platform.page.v2.95"
	if p.Document.Check(p.Sections) == nil {
		t.Fatal("old shared set profile accepted")
	}
	p.Document.UIProfile = PageUIProfile()
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "loop", Children: []string{"table", "comparison"}}
	if p.Document.checkSharedRecordSetOutput(p.Sections[0]) == nil {
		t.Fatal("Loop set producer accepted")
	}
	p.Document.Nodes["root"] = PageLayoutNode{Kind: "rows"}
	p.Document.Nodes["panel"] = PageLayoutNode{Kind: "rows", Children: []string{"table"}}
	p.Document.Overlays = map[string]PageOverlay{"panel": {Root: "panel", Kind: "drawer", OpenVariable: "open"}}
	if p.Document.checkSharedRecordSetOutput(p.Sections[0]) == nil {
		t.Fatal("Overlay set producer accepted")
	}
	other := object
	other.App = "other"
	v = p.Document.Variables["picked"]
	v.Source.Object = &other
	p.Document.Variables["picked"] = v
	if p.CheckCollectionPorts() == nil || app.CheckPageVariables(p) == nil || p.CheckRecordPorts() == nil {
		t.Fatal("foreign object identity accepted")
	}
}
