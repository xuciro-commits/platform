package platform

import (
	"fmt"
	"slices"
	"strings"
)

// page_record_card.go

type PageRecordCard struct {
	LabelField string `json:"labelField"`
	Tone       string `json:"tone"`
}

func (d *PageDocument) checkRecordCard(s Section) error {
	if s.Widget != "record-card" {
		return nil
	}
	c := s.RecordCard
	v := d.Variables[s.RecordVariable]
	limits := pageWidgets.Runtime.RecordCard
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.LabelField == "" || !slices.Contains(limits.Tones, c.Tone) || v.Type != "record" || !(v.Mode == "resource" && v.Source != nil && v.Source.Kind == "record" && slices.Contains([]string{"page", "overlay"}, v.Scope) || d.sharedContextRecord(s.RecordVariable)) || len(s.Fields) > limits.MaxFields {
		return fmt.Errorf("record card needs its original record resource, title, tone and bounded properties")
	}
	return nil
}
func (s Section) CheckRecordCard(info EntityInfo) error {
	if s.Widget != "record-card" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

// page_record_list.go

type PageRecordList struct {
	Layout string `json:"layout"`
}

func (d *PageDocument) checkRecordList(s Section) error {
	if s.Widget != "record-list" {
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordList.RequiredUIProfile) || s.RecordList == nil || !slices.Contains(pageWidgets.Runtime.RecordList.Layouts, s.RecordList.Layout) || s.CollectionVariable == "" || len(s.Fields) > pageWidgets.Runtime.RecordList.MaxFields {
		return fmt.Errorf("record list needs its profile, bounded fields, layout and original window")
	}
	if err := d.checkSharedRecordOutput(s); err != nil {
		return err
	}
	if s.RecordList.Layout == "tiles" {
		v := d.Variables[s.CollectionVariable]
		if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordWork.RequiredUIProfile) || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(s.Fields) > 0 {
			return fmt.Errorf("tiles need their original bounded plan")
		}
		q := d.Queries[v.Source.Query]
		if q.Limit != pageWidgets.Runtime.RecordWork.MaxTiles || q.Offset != 0 || !slices.Equal(q.Sort, []string{"id"}) {
			return fmt.Errorf("tiles need an eight-record ID window")
		}
	}
	return nil
}
func (s Section) CheckRecordList(info EntityInfo) error {
	if s.Widget != "record-list" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

// page_record_view.go

type PageRecordView struct {
	Tabs []string `json:"tabs"`
}

func (d *PageDocument) checkRecordView(s Section) error {
	if s.Widget != "record-view" {
		return nil
	}
	limits := pageWidgets.Runtime.RecordView
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) {
		return fmt.Errorf("record view needs its supported profile")
	}
	if s.RecordView == nil {
		return nil
	}
	if len(s.RecordView.Tabs) == 0 || len(s.RecordView.Tabs) > len(limits.Tabs) {
		return fmt.Errorf("record view needs bounded tabs")
	}
	seen := map[string]bool{}
	for _, tab := range s.RecordView.Tabs {
		if seen[tab] || !slices.Contains(limits.Tabs, tab) {
			return fmt.Errorf("record view has an invalid tab")
		}
		seen[tab] = true
	}
	return nil
}

// page_record_links.go

// PageRecordLink selects one declared incoming reference from the authorized record view.
type PageRecordLink struct {
	Object AssetRef `json:"object"`
	Field  string   `json:"field"`
	Title  string   `json:"title,omitempty"`
}

func (d *PageDocument) checkRecordLinks(s Section) error {
	if s.Widget != "record-links" {
		return nil
	}
	limits := pageWidgets.Runtime.RecordLinks
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || len(s.RecordLinks) == 0 || len(s.RecordLinks) > limits.MaxGroups {
		return fmt.Errorf("record links need their profile and bounded explicit groups")
	}
	seen := map[string]bool{}
	for _, group := range s.RecordLinks {
		key := group.Object.String() + "/" + group.Field
		if group.Object.Check() != nil || group.Object.Kind != AssetObject || group.Field == "" || seen[key] || len(group.Title) > limits.MaxTitleBytes {
			return fmt.Errorf("record link needs a unique object, reference field and bounded title")
		}
		seen[key] = true
	}
	return nil
}

func (s Section) CheckRecordLinks(parent EntityInfo, lookup func(string) (EntityInfo, bool)) error {
	for _, group := range s.RecordLinks {
		target, ok := lookup(group.Object.Name)
		field, found := target.Field(group.Field)
		if !ok || target.App != group.Object.App || !found || field.Type != "reference" || field.Ref != parent.Type || field.Inverse == "" {
			return fmt.Errorf("record link needs a declared incoming reference on its original object")
		}
	}
	return nil
}

// page_record_events.go

type PageEventTone struct {
	Value string `json:"value"`
	Tone  string `json:"tone"`
}
type PageRecordEvents struct {
	TimeField     string          `json:"timeField"`
	TitleField    string          `json:"titleField"`
	SeverityField string          `json:"severityField"`
	Tones         []PageEventTone `json:"tones"`
}

func (d *PageDocument) checkRecordEvents(s Section) error {
	if s.Widget != "record-events" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordEvents.RequiredUIProfile) || s.RecordEvents == nil {
		return fmt.Errorf("record events need their profile and ordered original plan window")
	}
	seen := map[string]bool{}
	if len(s.RecordEvents.Tones) > pageWidgets.Runtime.RecordEvents.MaxTones {
		return fmt.Errorf("event tones exceed their bound")
	}
	for _, tone := range s.RecordEvents.Tones {
		if tone.Value == "" || seen[tone.Value] || !slices.Contains(pageWidgets.Runtime.RecordEvents.Tones, tone.Tone) {
			return fmt.Errorf("event tones need unique values and supported tones")
		}
		seen[tone.Value] = true
	}
	return nil
}
func (s Section) CheckRecordEvents(info EntityInfo) error {
	if s.Widget != "record-events" {
		return nil
	}
	if s.RecordEvents == nil {
		return fmt.Errorf("event configuration is missing")
	}
	if err := s.checkWidgetFieldRoles(info); err != nil {
		return err
	}
	e := s.RecordEvents
	severity, _ := info.Field(e.SeverityField)
	for _, tone := range e.Tones {
		if !slices.Contains(severity.Choices, tone.Value) {
			return fmt.Errorf("event tone does not name an original severity value")
		}
	}
	return nil
}

// page_table_presentation.go

// Column presentation never replaces the object's field or edit contract.
type PageTableColumn struct {
	Field     string `json:"field"`
	Title     string `json:"title,omitempty"`
	Width     int    `json:"width,omitempty"`
	Formatter string `json:"formatter,omitempty"`
}

type PageTablePresentation struct {
	Density       string  `json:"density"`
	ShowToolbar   bool    `json:"showToolbar"`
	TitleTemplate *string `json:"titleTemplate,omitempty"`
}

func (d *PageDocument) checkTablePresentation(s Section) error {
	if s.TablePresentation != nil {
		c := s.TablePresentation
		if s.Widget != "table" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.90") || !slices.Contains([]string{"compact", "normal"}, c.Density) {
			return fmt.Errorf("table controls need their renderer profile and density")
		}
		if c.TitleTemplate != nil {
			text := *c.TitleTemplate
			plain := strings.Replace(text, "{count}", "", 1)
			if len(text) > 256 || strings.ContainsAny(plain, "{}") {
				return fmt.Errorf("table title needs bounded literal text and one count placeholder")
			}
			if strings.Contains(text, "{count}") {
				v := d.Variables[s.CollectionVariable]
				if v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || v.Type != "object-set" {
					return fmt.Errorf("table count title needs its original plan collection")
				}
			}
		}
	}
	if len(s.TableColumns) == 0 && s.ShowSearch == nil {
		return nil
	}
	limits := pageWidgets.Runtime.TablePresentation
	if s.Widget != "table" || !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || len(s.TableColumns) > limits.MaxColumns {
		return fmt.Errorf("table presentation needs its supported profile and bounded columns")
	}
	seen := map[string]bool{}
	for _, c := range s.TableColumns {
		if !pageNodeID.MatchString(c.Field) || seen[c.Field] || c.Field != "id" && !slices.Contains(s.Fields, c.Field) || len(c.Title) > limits.MaxTitleBytes || c.Width != 0 && (c.Width < limits.MinWidth || c.Width > limits.MaxWidth) {
			return fmt.Errorf("table column needs a unique displayed field and bounded title/width")
		}
		if c.Formatter != "" && !slices.ContainsFunc(limits.Formatters, func(f struct {
			ID         string   `json:"id"`
			FieldTypes []string `json:"fieldTypes"`
		}) bool {
			return f.ID == c.Formatter
		}) {
			return fmt.Errorf("unknown table column formatter")
		}
		if c.Field == "id" && c.Formatter != "" && c.Formatter != "text" {
			return fmt.Errorf("record identity only supports text presentation")
		}
		seen[c.Field] = true
	}
	return nil
}

func (s Section) CheckTablePresentation(info EntityInfo) error {
	for _, c := range s.TableColumns {
		if c.Field == "id" {
			continue
		}
		field, ok := info.Field(c.Field)
		if !ok || field.Aside || field.Type == "lines" {
			return fmt.Errorf("table column field is unavailable")
		}
		if c.Formatter == "" {
			continue
		}
		allowed := false
		for _, f := range pageWidgets.Runtime.TablePresentation.Formatters {
			if f.ID == c.Formatter && slices.Contains(f.FieldTypes, field.Type) {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("table formatter does not support its field type")
		}
	}
	return nil
}

// page_table_edit.go

func (d *PageDocument) checkTableEdit(s Section) error {
	if s.InlineEdit == nil {
		return nil
	}
	edit := s.InlineEdit
	if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.31") || edit.Action.Check() != nil || edit.Action.Kind != AssetAction || len(edit.Fields) == 0 || len(edit.Fields) > pageWidgets.Runtime.TableEditing.MaxFields {
		return fmt.Errorf("inline table editing needs v2.31, a table, original action and bounded fields")
	}
	seen := map[string]bool{}
	for _, name := range edit.Fields {
		if !pageNodeID.MatchString(name) || seen[name] || !slices.Contains(s.Fields, name) {
			return fmt.Errorf("inline edit fields must be unique displayed object fields")
		}
		seen[name] = true
	}
	return nil
}

func (s Section) CheckTableEdit(info EntityInfo, action Action) error {
	if s.InlineEdit == nil {
		return nil
	}
	edit := s.InlineEdit
	if edit.Action.App != info.App || edit.Action.Name != info.Type+".edit" || action.Schema != edit.Action.Name || action.Target != info.Type || action.New || action.NeedsApproval || action.Approval != nil || !slices.Contains(info.Standard, action.Schema) || slices.ContainsFunc(action.Payload, func(f Field) bool { return f.Required }) {
		return fmt.Errorf("table editing requires its original standard field patch action without extra required inputs or approval")
	}
	for _, name := range edit.Fields {
		f, ok := info.Field(name)
		if !ok || f.ReadOnly || f.Aside || info.Lifecycle != nil && info.Lifecycle.Field == name || !slices.Contains(pageWidgets.Runtime.TableEditing.FieldTypes, f.Type) || !slices.ContainsFunc(action.Payload, func(p Field) bool { return p.Name == name }) {
			return fmt.Errorf("table edit field is unavailable or not accepted by its original action")
		}
	}
	return nil
}

// page_detail_presentation.go

// Detail layout does not change fields, reads or record actions.
type PageDetailPresentation struct {
	Columns  int  `json:"columns"`
	HideNull bool `json:"hideNull,omitempty"`
	// Barcode names a text field also drawn as a Code 128 barcode, and Print
	// offers the detail as a printable label.
	Barcode string `json:"barcode,omitempty"`
	Print   bool   `json:"print,omitempty"`
}

func (d *PageDocument) checkDetailPresentation(s Section) error {
	if s.DetailPresentation == nil {
		return nil
	}
	limits := pageWidgets.Runtime.DetailPresentation
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || s.DetailPresentation.Columns < 1 || s.DetailPresentation.Columns > limits.MaxColumns {
		return fmt.Errorf("detail presentation needs its supported profile and bounded columns")
	}
	if (s.DetailPresentation.Barcode != "" || s.DetailPresentation.Print) && !PageUIProfileSupports(d.UIProfile, limits.BarcodeRequiredUIProfile) {
		return fmt.Errorf("barcode and print presentation need their profile")
	}
	return nil
}

// CheckDetailBarcode refuses a barcode over a field the record has not, or
// one that is not text: a barcode encodes a code, not a number or a date.
func (s Section) CheckDetailBarcode(info EntityInfo) error {
	if s.DetailPresentation == nil || s.DetailPresentation.Barcode == "" {
		return nil
	}
	if s.DetailPresentation.Barcode != "id" {
		if f, ok := info.Field(s.DetailPresentation.Barcode); !ok || f.Type != "text" {
			return fmt.Errorf("barcode needs an original text field")
		}
	}
	return nil
}

// page_metric_presentation.go

type PageMetricAnnotation struct {
	Direction string `json:"direction"`
	Text      string `json:"text"`
}

type PageMetricPresentation struct {
	Annotation *PageMetricAnnotation `json:"annotation,omitempty"`
	Prefix     string                `json:"prefix,omitempty"`
	Suffix     string                `json:"suffix,omitempty"`
	Formatter  string                `json:"formatter"`
	Variant    string                `json:"variant"`
	Tone       string                `json:"tone"`
}

func (d *PageDocument) checkMetricPresentation(s Section) error {
	if s.MetricPresentation == nil {
		return nil
	}
	v := s.MetricPresentation
	if v.Annotation != nil && (!PageUIProfileSupports(d.UIProfile, "platform.page.v2.90") || !slices.Contains([]string{"up", "down", "flat"}, v.Annotation.Direction) || len(v.Annotation.Text) > 256) {
		return fmt.Errorf("metric annotation needs a bounded static note and renderer profile")
	}
	c := pageWidgets.Runtime.MetricPresentation
	if !PageUIProfileSupports(d.UIProfile, c.RequiredUIProfile) || len(v.Prefix) > c.MaxUnitBytes || len(v.Suffix) > c.MaxUnitBytes || !slices.Contains(c.Formatters, v.Formatter) || !slices.Contains(c.Variants, v.Variant) || !slices.Contains(c.Tones, v.Tone) {
		return fmt.Errorf("metric presentation needs its widget, profile and bounded supported display")
	}
	return nil
}

func (s Section) CheckMetricPresentation(info EntityInfo) error {
	if s.MetricPresentation == nil {
		return nil
	}
	if s.Measure == "count" {
		return nil
	}
	op, name, ok := strings.Cut(s.Measure, ":")
	field, known := info.Field(name)
	if !ok || !slices.Contains([]string{"sum", "avg", "min", "max"}, op) || !known || !slices.Contains([]string{"integer", "decimal", "money"}, field.Type) || field.Type == "money" && s.MetricPresentation.Formatter == "short" {
		return fmt.Errorf("metric presentation needs an original numeric measure; money keeps its original currency display")
	}
	return nil
}

// page_region_presentation.go

// PageRegionPresentation is bounded container chrome; it owns no business state.
type PageRegionPresentation struct {
	Padding          *int   `json:"padding,omitempty"`
	Background       string `json:"background,omitempty"`
	Border           bool   `json:"border,omitempty"`
	ShowHeader       bool   `json:"showHeader,omitempty"`
	Collapsible      bool   `json:"collapsible,omitempty"`
	DefaultCollapsed bool   `json:"defaultCollapsed,omitempty"`
}

func (d *PageDocument) checkRegionPresentation(id string, n PageLayoutNode) error {
	p := n.Presentation
	if p == nil {
		return nil
	}
	l := pageWidgets.Layout
	if !PageUIProfileSupports(d.UIProfile, l.PresentationProfile) || n.Kind != "rows" && n.Kind != "columns" || p.Padding != nil && (*p.Padding < 0 || *p.Padding > l.MaxPadding) || p.Background != "" && p.Background != "default" && p.Background != "panel" || p.ShowHeader && n.Title == "" || p.Collapsible && (!p.ShowHeader || n.Title == "") || p.DefaultCollapsed && !p.Collapsible {
		return fmt.Errorf("page node %s: invalid region presentation", id)
	}
	return nil
}

// page_timeline.go

// CheckTimeline keeps presentation mappings in the original object schema.
// Window ownership and record authorization remain their existing contracts.
func (s Section) CheckTimeline(info EntityInfo) error {
	if s.Widget != "record-timeline" {
		return nil
	}
	start, ok := info.Field(s.TimeStart)
	if s.CollectionVariable == "" || !ok || !slices.Contains([]string{"date", "datetime"}, start.Type) {
		return fmt.Errorf("record timeline needs a plan window and date or datetime start field")
	}
	if s.TimeEnd != "" {
		end, ok := info.Field(s.TimeEnd)
		if !ok || end.Type != start.Type {
			return fmt.Errorf("timeline end must have the start field's time type")
		}
	}
	if !visibleContextField(info, s.TimeLabel, true) || s.TimeGroup != "" && !visibleContextField(info, s.TimeGroup, true) {
		return fmt.Errorf("timeline label and resource need visible scalar text fields")
	}
	return nil
}

// page_kanban.go

func (s Section) CheckKanban(info EntityInfo) error {
	if s.Widget != "kanban" {
		return nil
	}
	l := info.Lifecycle
	if l == nil || len(l.States) < 1 || len(l.States) > 32 || s.CollectionVariable == "" {
		return fmt.Errorf("kanban needs an original bounded lifecycle and plan window")
	}
	status, ok := info.Field(l.Field)
	if !ok || !slices.Contains([]string{"text", "choice"}, status.Type) {
		return fmt.Errorf("kanban status field is unavailable")
	}
	if !visibleContextField(info, s.CardLabel, true) {
		return fmt.Errorf("kanban card title needs a visible scalar text field")
	}
	if len(s.Fields) > 4 {
		return fmt.Errorf("kanban has at most four summary fields")
	}
	for _, name := range s.Fields {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"}, f.Type) {
			return fmt.Errorf("kanban summary field is unavailable or not scalar")
		}
	}
	for _, ref := range s.Actions {
		if ref.App != info.App || ref.Kind != AssetAction || !slices.ContainsFunc(l.Transitions, func(t TransitionInfo) bool { return t.Schema == ref.Name && (len(t.To) == 1 || t.ToInput != "") }) {
			return fmt.Errorf("kanban moves need declared fixed or parameterized destination transitions")
		}
	}
	return nil
}

func (p Page) CheckKanban(s Section, info EntityInfo) error {
	if err := s.CheckKanban(info); err != nil {
		return err
	}
	if s.Widget != "kanban" {
		return nil
	}
	for _, a := range s.Actions {
		for _, move := range info.Lifecycle.Transitions {
			if move.Schema == a.Name && move.ToInput != "" && (p.Document == nil || !PageUIProfileSupports(p.Document.UIProfile, pageWidgets.Runtime.Kanban.DynamicUIProfile)) {
				return fmt.Errorf("parameterized kanban moves need their admitted profile")
			}
		}
	}
	return nil
}

// page_device.go

// checkDevice refuses a device the contract does not lay out for.
func (d *PageDocument) checkDevice() error {
	if d.Device == "" {
		return nil
	}
	limits := pageWidgets.Runtime.Device
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || !slices.Contains(limits.Kinds, d.Device) {
		return fmt.Errorf("page device %q needs its profile and a known kind", d.Device)
	}
	return nil
}

// page_inline_action.go

func (s Section) CheckInlineAction(info EntityInfo, action Action) error {
	if s.Widget != "inline-action" {
		return nil
	}
	if len(s.Actions) != 1 || s.Actions[0].App != info.App || s.Actions[0].Kind != AssetAction || s.Actions[0].Name != action.Schema || action.Target != info.Type || action.New {
		return fmt.Errorf("inline action needs one original non-creating action on its bound object")
	}
	for _, m := range s.ActionDefaults {
		f, ok := info.Field(m.Field)
		p := slices.IndexFunc(action.Payload, func(p Field) bool { return p.Name == m.Parameter })
		if !ok || p < 0 || !ActionParameterCompatible(f, action.Payload[p]) {
			return fmt.Errorf("action default parameter and original readable field are incompatible")
		}
	}
	return nil
}

// page_shared_record_set.go

func (d *PageDocument) checkSharedRecordSetOutput(s Section) error {
	v := d.Variables[s.SelectionSetVariable]
	if s.Widget != "table" || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordSelection.SharedUIProfile) || v.Type != "record-set" || v.Scope != "application" || v.Mode != "shared" || !v.Writable || v.Source == nil || v.Source.Kind != "application" || v.Source.Object == nil {
		return fmt.Errorf("shared record-set output needs a writable original application resource")
	}
	overlays, loops := d.overlayOwners(), d.loopOwners()
	for id, n := range d.Nodes {
		if n.Section == s.ID && (overlays[id] != "" || loops[id] != "") {
			return fmt.Errorf("shared record-set output needs a main-page table")
		}
	}
	return nil
}
