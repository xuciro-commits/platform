package platform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

// PageDocument is the presentation tree of a composed page. Sections remain
// the page's authoritative, server-checked business bindings; leaves name
// those sections by stable ID. The UI profile versions layout and finite
// presentation-state semantics; business bindings keep their original owners.
type PageUnusedWidget struct {
	Node   string `json:"node"`
	Parent string `json:"parent"`
}

type PageDocument struct {
	UnusedWidgets []PageUnusedWidget        `json:"unusedWidgets,omitempty"`
	FormatVersion int                       `json:"formatVersion"`
	UIProfile     string                    `json:"uiProfile"`
	Root          string                    `json:"root"`
	Nodes         map[string]PageLayoutNode `json:"nodes"`
	Variables     map[string]PageVariable   `json:"variables,omitempty"`
	Overlays      map[string]PageOverlay    `json:"overlays,omitempty"`
	Events        []PageEventBinding        `json:"events,omitempty"`
	Queries       map[string]PageQuery      `json:"queries,omitempty"`
	Interface     *PageInterface            `json:"interface,omitempty"`
}

// PageLayoutSize contains controlled presentation dimensions in CSS pixels.
type PageLayoutSize struct {
	Weight    *int   `json:"weight,omitempty"`
	Width     *int   `json:"width,omitempty"`
	Height    *int   `json:"height,omitempty"`
	MinWidth  *int   `json:"minWidth,omitempty"`
	MaxWidth  *int   `json:"maxWidth,omitempty"`
	MinHeight *int   `json:"minHeight,omitempty"`
	MaxHeight *int   `json:"maxHeight,omitempty"`
	Scroll    string `json:"scroll,omitempty"`
}

type PageLayoutNode struct {
	Presentation   *PageRegionPresentation `json:"presentation,omitempty"`
	Size           *PageLayoutSize         `json:"size,omitempty"`
	Gap            *int                    `json:"gap,omitempty"`
	Kind           string                  `json:"kind"` // rows, columns, tabs, flow, toolbar, or widget
	Children       []string                `json:"children,omitempty"`
	Section        string                  `json:"section,omitempty"`
	Title          string                  `json:"title,omitempty"`
	ValueVariable  string                  `json:"valueVariable,omitempty"`
	ActiveVariable string                  `json:"activeVariable,omitempty"`
	VisibleWhen    string                  `json:"visibleWhen,omitempty"`
	EnabledWhen    string                  `json:"enabledWhen,omitempty"`
	Align          string                  `json:"align,omitempty"`
	Loop           *PageLoop               `json:"loop,omitempty"`
}

var pageNodeID = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9._:-]{0,79}$`)

// Check rejects cycles, shared children, unreachable nodes and references to
// missing or duplicated sections. Each published section must appear once.
func (d *PageDocument) Check(sections []Section) error {
	if d == nil {
		for _, s := range sections {
			if s.Widget == "collection-builder" || s.CollectionBuilder != nil || s.CollectionOutputVariable != "" {
				return fmt.Errorf("collection builder requires a document")
			}
			if s.Map != nil || s.Scene != nil || s.SceneSampleCollectionVariable != "" || s.SceneSampleVariable != "" || s.ScenePartVariable != "" || s.Widget == "record-map" || s.Widget == "scene-3d" || s.Widget == "image-annotation" {
				return fmt.Errorf("spatial views require a document")
			}
			if s.TablePresentation != nil {
				return fmt.Errorf("table presentation requires a document")
			}
			if s.Widget == "ai-assistant" || s.AI != nil {
				return fmt.Errorf("AI views require a document")
			}
			if s.Widget == "embedded-page" || s.Embedding != nil || s.Widget == "observation" || s.hasObservationConfiguration() || s.Widget == "action-table" || s.Widget == "notepad" || s.ActionTable != nil || s.NotepadVariable != "" {
				return fmt.Errorf("record work requires a document")
			}
			if s.Widget == "collection-analysis" || s.Analysis != nil || s.AnalysisXVariable != "" || s.AnalysisYVariable != "" || s.AnalysisCountVariable != "" || s.AnalysisMeanVariable != "" {
				return fmt.Errorf("analysis requires a page document")
			}
			if explorationWidget(s.Widget) || s.ResourceList != nil || s.AssetDirectory != nil || s.GraphExplorer != nil || s.VertexGraph != nil {
				return fmt.Errorf("exploration requires a page document")
			}
			if contextView(s.Widget) || s.Breadcrumb != nil || s.Avatar != nil || s.Image != nil {
				return fmt.Errorf("context views require a page document")
			}
			if workFeed(s.Widget) || s.HistoryLimit != 0 {
				return fmt.Errorf("work views require a page document")
			}
			if collaborationWidget(s.Widget) || s.CommentDraftVariable != "" || s.FileVariable != "" || s.PdfPageVariable != "" {
				return fmt.Errorf("collaboration requires a page document")
			}
			if s.Widget == "spacer" || s.Spacer != nil {
				return fmt.Errorf("spacer requires a document")
			}
			if s.Widget == "separator" || s.Separator != nil {
				return fmt.Errorf("separator requires a document")
			}
			if s.Widget == "notice" || s.Notice != nil {
				return fmt.Errorf("notice requires a document")
			}
			if s.Widget == "alert-banner" || s.AlertValueVariable != "" || s.AlertBanner != nil {
				return fmt.Errorf("alert banner requires a document")
			}
			if s.Widget == "record-picker" || s.RecordPicker != nil || s.PickerValueVariable != "" {
				return fmt.Errorf("record picker requires a page document")
			}
			if s.InputKind != "" {
				return fmt.Errorf("search input requires a page document")
			}
			if s.Widget == "date-input" || s.DateVariable != "" || s.DateLabel != nil || s.DateKind != "" || s.DateOffset != "" {
				return fmt.Errorf("date input requires a page document")
			}
			if s.Widget == "choice-input" || s.ChoiceInput != nil || s.ChoiceVariable != "" || s.ChoiceSetVariable != "" {
				return fmt.Errorf("choice input requires a page document")
			}
			if s.Widget == "boolean-input" || s.BooleanVariable != "" || s.BooleanLabel != nil || s.BooleanVariant != "" {
				return fmt.Errorf("boolean input requires a page document")
			}
			if s.Widget == "range-input" || s.RangeInput != nil || s.RangeMinVariable != "" || s.RangeMaxVariable != "" {
				return fmt.Errorf("range input requires a page document")
			}
			if s.Widget == "record-leaderboard" || s.Leaderboard != nil {
				return fmt.Errorf("leaderboard needs a document")
			}
			if s.Widget == "histogram" || s.Histogram != nil {
				return fmt.Errorf("histogram requires a page document")
			}
			if s.Widget == "term-counts" || s.Widget == "tag-counts" {
				return fmt.Errorf("term counts require a page document")
			}
			if s.Widget == "summary-stats" || s.SummaryField != "" || s.StatisticsVariable != "" {
				return fmt.Errorf("summary needs a document")
			}
			if s.Widget == "gauge" || s.Gauge != nil || s.GaugeValueVariable != "" {
				return fmt.Errorf("gauge needs a document")
			}
			if s.Widget == "progress" || s.ProgressLabel != "" || s.ProgressValueVariable != "" || s.ProgressTotalVariable != "" || s.ProgressTotal != "" {
				return fmt.Errorf("progress needs a document")
			}
			if s.Widget == "record-gantt" || s.RecordGantt != nil {
				return fmt.Errorf("record gantt needs a document")
			}
			if s.Widget == "record-calendar" || s.RecordCalendar != nil {
				return fmt.Errorf("record calendar requires a document")
			}
			if s.Widget == "record-events" || s.RecordEvents != nil {
				return fmt.Errorf("record events require a document")
			}
			if s.ChartVariant != "" {
				return fmt.Errorf("chart variant requires a document")
			}
			if s.Widget == "record-comparison" || s.RecordComparison != nil || s.RecordSetVariable != "" {
				return fmt.Errorf("record comparison requires a page document")
			}
			if s.Widget == "record-card" || s.RecordCard != nil {
				return fmt.Errorf("record card requires a page document")
			}
			if s.Widget == "sparkline-kpi" || s.Sparkline != nil || s.SparklineDecimalVariable != "" || s.SparklineNumberVariable != "" {
				return fmt.Errorf("sparkline KPI requires a page document")
			}
			if s.Widget == "treemap" || s.GroupValueVariable != "" || s.GroupSetVariable != "" {
				return fmt.Errorf("treemap requires a page document")
			}
			if s.Widget == "heatmap" || s.RowValueVariable != "" || s.RowSetVariable != "" || s.ColumnValueVariable != "" || s.ColumnSetVariable != "" {
				return fmt.Errorf("heatmap requires a page document")
			}
			if s.Widget == "record-scatter" || s.Scatter != nil {
				return fmt.Errorf("record scatter needs a document")
			}
			if s.Widget == "record-chart" || s.RecordChart != nil {
				return fmt.Errorf("record chart requires a document")
			}
			if s.Widget == "record-list" || s.RecordList != nil {
				return fmt.Errorf("record list requires a document")
			}
			if len(s.ActionDefaults) > 0 {
				return fmt.Errorf("action defaults require a document")
			}
			if s.Widget == "heading" || s.Widget == "collection-title" || s.HeadingLevel != "" || s.CountVariable != "" {
				return fmt.Errorf("titles require a document")
			}
			if s.MetricPresentation != nil {
				return fmt.Errorf("metric presentation requires a document")
			}
			if s.Widget == "status-tracker" || s.StatusTracker != nil {
				return fmt.Errorf("status tracker requires a document")
			}
			if s.Widget == "record-links" || len(s.RecordLinks) > 0 {
				return fmt.Errorf("record links require a document")
			}
		}
		return nil
	}
	if d.FormatVersion != 2 {
		return fmt.Errorf("page document format %d is unsupported", d.FormatVersion)
	}
	if !SupportsPageUIProfile(d.UIProfile) {
		return fmt.Errorf("page UI profile %q is unsupported", d.UIProfile)
	}
	if d.UIProfile == "platform.page.v2.1" && len(d.Variables) > 0 {
		return fmt.Errorf("page variables require UI profile v2.2")
	}
	for id, variable := range d.Variables {
		if variable.Mode == "property" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.17") {
			return fmt.Errorf("property variable %s requires v2.17", id)
		}
		if variable.Type == "decimal" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.16") {
			return fmt.Errorf("decimal variable %s requires v2.16", id)
		}
		if variable.Scope == "application" && variable.Type == "filter" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.15") {
			return fmt.Errorf("shared filter %s requires v2.15", id)
		}
		if variable.Scope == "application" && variable.Type == "record" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.14") {
			return fmt.Errorf("shared record %s requires v2.14", id)
		}
		if variable.Scope == "application" && variable.Type == "object-set" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.12") {
			return fmt.Errorf("shared window %s requires v2.12", id)
		}
		if variable.Scope == "application" && (!PageUIProfileSupports(d.UIProfile, "platform.page.v2.8") || variable.Mode != "shared") {
			return fmt.Errorf("page variable %s needs v2.8 and a shared application binding", id)
		}
		if variable.Scope == "overlay" && variable.Type == "filter" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.13") {
			return fmt.Errorf("overlay filter %s requires v2.13", id)
		}
		if variable.Scope == "overlay" && variable.Mode == "resource" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.11") {
			return fmt.Errorf("overlay resource %s requires UI profile v2.11", id)
		}
		if variable.Scope == "overlay" && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.7") {
			return fmt.Errorf("page variable %s requires UI profile v2.7", id)
		}
		if variable.Scope == pageWidgets.Runtime.Loop.Scope && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.5") {
			return fmt.Errorf("page variable %s requires UI profile v2.5", id)
		}
		if variable.Mode == "resource" && (d.UIProfile == "platform.page.v2.1" || d.UIProfile == "platform.page.v2.2") {
			return fmt.Errorf("page variable %s requires UI profile v2.3", id)
		}
	}
	if err := d.CheckQueries(sections); err != nil {
		return err
	}
	if err := d.CheckVariables(); err != nil {
		return err
	}
	if err := d.checkCollaborationOwners(sections); err != nil {
		return err
	}
	if err := d.checkInterface(); err != nil {
		return err
	}
	if err := d.checkEvents(sections); err != nil {
		return err
	}
	if !pageNodeID.MatchString(d.Root) || len(d.Nodes) == 0 || len(d.Nodes) > 256 {
		return fmt.Errorf("page document needs a valid root and 1 to 256 nodes")
	}
	if d.Nodes[d.Root].Kind == "widget" {
		return fmt.Errorf("page document root must be a layout container")
	}
	if len(sections) == 0 || len(sections) > 128 {
		return fmt.Errorf("page document needs 1 to 128 sections")
	}
	byID := make(map[string]bool, len(sections))
	for _, section := range sections {
		if !pageNodeID.MatchString(section.ID) || byID[section.ID] {
			return fmt.Errorf("page document sections need unique stable IDs")
		}
		byID[section.ID] = true
		if err := d.checkAI(section); err != nil {
			return err
		}
		if err := d.checkExternalFrame(section); err != nil {
			return err
		}
		if err := d.checkEmbedding(section); err != nil {
			return err
		}
		if err := d.checkInputPresentation(section); err != nil {
			return err
		}
		if err := d.checkRecordPicker(section); err != nil {
			return err
		}
		if err := d.checkDateInput(section); err != nil {
			return err
		}
		if err := d.checkChoiceInput(section); err != nil {
			return err
		}
		if err := d.checkBooleanInput(section); err != nil {
			return err
		}
		if err := d.checkRangeInput(section); err != nil {
			return err
		}
		if err := d.checkLeaderboard(section); err != nil {
			return err
		}
		if err := d.checkHistogram(section); err != nil {
			return err
		}
		if err := d.checkTerms(section); err != nil {
			return err
		}
		if err := d.checkObservation(section, sections); err != nil {
			return err
		}
		if err := d.checkRecordWork(section); err != nil {
			return err
		}
		if err := d.checkCollectionAnalysis(section); err != nil {
			return err
		}
		if err := d.checkSummary(section); err != nil {
			return err
		}
		if err := d.checkGauge(section); err != nil {
			return err
		}
		if err := d.checkSpacer(section); err != nil {
			return err
		}
		if err := d.checkSeparator(section); err != nil {
			return err
		}
		if err := d.checkNotice(section); err != nil {
			return err
		}
		if err := d.checkAlertBanner(section); err != nil {
			return err
		}
		if err := d.checkProgress(section); err != nil {
			return err
		}
		if err := d.checkRecordGantt(section); err != nil {
			return err
		}
		if err := d.checkRecordCalendar(section); err != nil {
			return err
		}
		if err := d.checkRecordEvents(section); err != nil {
			return err
		}
		if err := d.checkExploration(section); err != nil {
			return err
		}
		if err := d.checkContextViews(section); err != nil {
			return err
		}
		if err := d.checkWorkViews(section); err != nil {
			return err
		}
		if err := d.checkCollaboration(section); err != nil {
			return err
		}
		if err := d.checkRecordComparison(section); err != nil {
			return err
		}
		if err := d.checkRecordCard(section); err != nil {
			return err
		}
		if err := d.checkSparkline(section); err != nil {
			return err
		}
		if err := d.checkTreemap(section); err != nil {
			return err
		}
		if err := d.checkHeatmap(section); err != nil {
			return err
		}
		if err := d.checkCollectionBuilder(section, sections); err != nil {
			return err
		}
		if err := d.checkSpatial(section); err != nil {
			return err
		}
		if err := d.checkScatter(section); err != nil {
			return err
		}
		if err := d.checkRecordChart(section); err != nil {
			return err
		}
		if err := d.checkButtonGroup(section); err != nil {
			return err
		}
		if err := d.checkRecordList(section); err != nil {
			return err
		}
		if len(section.ActionDefaults) > 0 {
			if section.Widget != "inline-action" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.92") || len(section.ActionDefaults) > 16 {
				return fmt.Errorf("action defaults need their inline action profile and budget")
			}
			seen := map[string]bool{}
			for _, m := range section.ActionDefaults {
				if !pageNodeID.MatchString(m.Parameter) || !pageNodeID.MatchString(m.Field) || seen[m.Parameter] || slices.Contains([]string{"id", "revision", "created", "changed", "archived", "__proto__", "constructor", "prototype"}, m.Parameter) {
					return fmt.Errorf("action defaults need unique declared parameters and fields")
				}
				seen[m.Parameter] = true
			}
		}
		if err := d.checkTitles(section); err != nil {
			return err
		}
		if err := d.checkMetricPresentation(section); err != nil {
			return err
		}
		if err := d.checkStatusTracker(section); err != nil {
			return err
		}
		if err := d.checkRecordLinks(section); err != nil {
			return err
		}
		if err := d.checkRecordView(section); err != nil {
			return err
		}
		if err := d.checkDetailPresentation(section); err != nil {
			return err
		}
		if err := d.checkTablePresentation(section); err != nil {
			return err
		}
		if err := d.checkTableEdit(section); err != nil {
			return err
		}
		if err := d.checkFacets(section); err != nil {
			return err
		}
		if err := d.checkWidgetPorts(section); err != nil {
			return err
		}
		if section.CollectionVariable != "" {
			v, ok := d.Variables[section.CollectionVariable]
			if section.FilterVariable != "" || !ok || v.Source == nil || !(v.Mode == "resource" && (v.Scope == "page" || v.Scope == "overlay") && v.Source.Kind == "plan" || v.Mode == "shared" && v.Scope == "application" && v.Source.Kind == "application" && v.Source.Object != nil) || section.Query.Name != "" || section.ParentSelection != "" || section.Relation != "" || section.RecordVariable != "" && !(section.Widget == "observation" && section.Observation != nil && section.Observation.Kind == "statistics") {
				return fmt.Errorf("page section %s needs an exclusive plan window binding", section.ID)
			}
		}
		if err := checkPageWidget(section); err != nil {
			return fmt.Errorf("page section %s: %w", section.ID, err)
		}
	}
	for id, variable := range d.Variables {
		if variable.Source == nil || (variable.Source.Kind == pageWidgets.Runtime.Loop.Source || variable.Source.Kind == "application" || (variable.Source.Kind == "plan" || variable.Mode == "aggregate") || variable.Source.Kind == "property" || variable.Source.Kind == "compute") {
			continue
		}
		found := false
		for _, section := range sections {
			for _, resource := range pageWidgets.Runtime.Resources {
				if section.ID == variable.Source.Section && (section.Widget == resource.Widget || PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordList.RequiredUIProfile) && slices.Contains(resource.Widgets, section.Widget)) && variable.Source.Kind == resource.Kind {
					if section.Widget == "collection-builder" && (variable.Source.Kind != "query" || section.CollectionOutputVariable != id || variable.Source.Port != "") {
						return fmt.Errorf("collection resource needs its declared output")
					}
					if resource.Kind == "record" {
						if section.Widget == "graph-explorer" || section.Widget == "observation" {
							matched := section.HasRecordOutput(variable.Source.Port, id)
							if !matched {
								return fmt.Errorf("graph record source needs its declared original typed port")
							}
						} else if variable.Source.Port != "" {
							return fmt.Errorf("only a graph producer declares a record output port")
						}
					}
					if resource.Kind == "records" && section.SelectionSetVariable != id {
						return fmt.Errorf("record-set needs its explicit table producer")
					}
					if resource.Kind == "filter" && (len(section.Facets) > 0 || section.FilterSearchVariable != "") {
						return fmt.Errorf("facet filter exposes explicit state rather than a legacy filter resource")
					}
					found = true
				}
			}
		}
		if !found {
			return fmt.Errorf("page variable %s needs a compatible source section", id)
		}
	}
	for id := range d.Nodes {
		if !pageNodeID.MatchString(id) {
			return fmt.Errorf("page document has invalid node ID %q", id)
		}
	}
	unused, err := d.checkUnusedWidgets()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	used := map[string]bool{}
	var walk func(string, int, string, bool) error
	walk = func(id string, depth int, parentKind string, parentHeight bool) error {
		if depth > 24 {
			return fmt.Errorf("page document layout is too deep at %q", id)
		}
		node, ok := d.Nodes[id]
		if !ok {
			return fmt.Errorf("page document references missing node %q", id)
		}
		if seen[id] {
			return fmt.Errorf("page document node %q is shared or cyclic", id)
		}
		seen[id] = true
		if err := d.checkRegionPresentation(id, node); err != nil {
			return err
		}
		bounded, err := d.checkLayoutSize(id, node, parentKind, parentHeight, unused[id])
		if err != nil {
			return err
		}
		if len(node.Title) > 1024 {
			return fmt.Errorf("page node %s title is too long", id)
		}
		if d.UIProfile == "platform.page.v2.1" && (node.Kind == "tabs" || node.ActiveVariable != "" || node.VisibleWhen != "" || node.Title != "") {
			return fmt.Errorf("page node %s requires UI profile v2.2", id)
		}
		if node.VisibleWhen != "" && d.Variables[node.VisibleWhen].Type != "boolean" {
			return fmt.Errorf("page node %s visibility needs a boolean variable", id)
		}
		if node.Kind != "tabs" && node.ActiveVariable != "" {
			return fmt.Errorf("page node %s is not tabs", id)
		}
		if (node.Kind == "flow" || node.Kind == "toolbar" || node.Align != "" || node.EnabledWhen != "") && !PageUIProfileSupports(d.UIProfile, "platform.page.v2.4") {
			return fmt.Errorf("page node %s requires UI profile v2.4", id)
		}
		if node.Align != "" && (node.Kind != "flow" && node.Kind != "toolbar" || !slices.Contains([]string{"start", "center", "end", "between"}, node.Align)) {
			return fmt.Errorf("page node %s has unsupported alignment", id)
		}
		if node.EnabledWhen != "" && (node.Kind != "widget" || !slices.ContainsFunc(sections, func(s Section) bool {
			p := widgetPort(s.Widget, "enabledWhen")
			return s.ID == node.Section && p != nil && PageUIProfileSupports(d.UIProfile, p.RequiredUIProfile) && d.Variables[node.EnabledWhen].Type == p.Type
		})) {
			return fmt.Errorf("page node %s enable binding needs an interactive widget and boolean variable", id)
		}
		input := slices.ContainsFunc(sections, func(s Section) bool { return s.ID == node.Section && s.Widget == "input" })
		if input || node.ValueVariable != "" {
			v, ok := d.Variables[node.ValueVariable]
			if !PageUIProfileSupports(d.UIProfile, "platform.page.v2.7") || node.Kind != "widget" || !input || !ok || (v.Type != "string" && (v.Type != "decimal" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.16"))) || !v.IsWritable() {
				return fmt.Errorf("page node %s input needs v2.7 and a text state binding", id)
			}
		}
		switch node.Kind {
		case "widget":
			if len(node.Children) != 0 || !byID[node.Section] || used[node.Section] {
				return fmt.Errorf("page document widget %q needs one unique section", id)
			}
			used[node.Section] = true
		case "rows", "columns", "tabs", "flow", "toolbar", "loop":
			if node.Section != "" || len(d.ownedChildren(id)) == 0 || len(d.ownedChildren(id)) > 128 {
				return fmt.Errorf("page document container %q needs children and no section", id)
			}
			if node.Kind == "tabs" {
				variable, ok := d.Variables[node.ActiveVariable]
				var initial string
				if !ok || variable.Type != "string" || variable.Mode != "state" || json.Unmarshal(variable.Initial, &initial) != nil || !slices.Contains(d.ownedChildren(id), initial) {
					return fmt.Errorf("page tabs %s need a text state initialized to a child", id)
				}
			}
			for _, child := range d.ownedChildren(id) {
				if err := walk(child, depth+1, node.Kind, bounded); err != nil {
					return err
				}
			}
		default:
			return fmt.Errorf("page document node %q has unsupported layout %q", id, node.Kind)
		}
		return nil
	}
	if err := walk(d.Root, 0, "", false); err != nil {
		return err
	}
	for id, overlay := range d.Overlays {
		if d.Nodes[overlay.Root].Kind == "widget" {
			return fmt.Errorf("page overlay %s root must be a container", id)
		}
		if err := walk(overlay.Root, 0, "", false); err != nil {
			return err
		}
	}
	if len(seen) != len(d.Nodes) || len(used) != len(byID) {
		return fmt.Errorf("page document has unreachable nodes or sections")
	}
	if err := d.checkLoops(sections); err != nil {
		return err
	}
	// Include producer availability in the dependency graph: mutually hidden
	// tables must not deadlock even when the pure expression graph is acyclic.
	controls := map[string][]string{}
	var collect func(string, []string)
	collect = func(id string, inherited []string) {
		node := d.Nodes[id]
		conditions := slices.Clone(inherited)
		if node.VisibleWhen != "" {
			conditions = append(conditions, node.VisibleWhen)
		}
		if node.Loop != nil {
			conditions = append(conditions, node.Loop.Collection)
		}
		if node.Kind == "widget" {
			controls[node.Section] = conditions
		}
		for _, child := range d.ownedChildren(id) {
			collect(child, conditions)
		}
	}
	collect(d.Root, nil)
	for _, overlay := range d.Overlays {
		collect(overlay.Root, []string{overlay.OpenVariable})
	}
	active, complete := map[string]bool{}, map[string]bool{}
	var check func(string) error
	check = func(id string) error {
		if active[id] {
			return fmt.Errorf("page variable %s has cyclic resource visibility", id)
		}
		if complete[id] {
			return nil
		}
		active[id] = true
		variable := d.Variables[id]
		dependencies := []string{}
		if variable.Expression != nil {
			for _, arg := range variable.Expression.Args {
				if arg.Variable != "" {
					dependencies = append(dependencies, arg.Variable)
				}
			}
		}
		if variable.Source != nil {
			if variable.Source.Compute != nil {
				dependencies = append(dependencies, variable.Source.Compute.RecordVariable)
			}
			if variable.Mode == "property" {
				dependencies = append(dependencies, variable.Source.Variable)
			}
			dependencies = append(dependencies, controls[variable.Source.Section]...)
			if variable.Source.Kind == "record" {
				for _, s := range sections {
					if s.ID == variable.Source.Section && s.Widget == "graph-explorer" {
						dependencies = append(dependencies, s.RecordVariable)
					}
				}
			}
			if variable.Source.Kind == "filter" {
				for _, s := range sections {

					if s.ID == variable.Source.Section && s.FilterVariable != "" {
						dependencies = append(dependencies, s.FilterVariable)
					}
				}
			}
			if variable.Source.Kind == "query" || variable.Source.Kind == "record" || variable.Source.Kind == "records" {
				for _, s := range sections {

					if s.ID == variable.Source.Section && s.CollectionVariable != "" {
						dependencies = append(dependencies, s.CollectionVariable)
					}
				}
			}
			if variable.Source.Kind == "plan" || variable.Mode == "aggregate" {
				dependencies = append(dependencies, d.Queries[variable.Source.Query].Variables()...)
			}
		}
		for _, dependency := range dependencies {
			if err := check(dependency); err != nil {
				return err
			}
		}
		active[id] = false
		complete[id] = true
		return nil
	}
	for id := range d.Variables {
		if err := check(id); err != nil {
			return err
		}
	}
	return nil
}

// Visible keeps only the member-filtered sections and their ancestor layout.
// It never mutates the installed definition. An empty root represents a page
// whose widgets are all hidden from this reader.
func (d *PageDocument) Visible(sections []Section) *PageDocument {
	if d == nil {
		return nil
	}
	allowed := make(map[string]bool, len(sections))
	for _, section := range sections {
		allowed[section.ID] = true
	}
	queries := map[string]PageQuery{}
	for id, q := range d.Queries {
		queries[id] = q
	}
	variables := map[string]PageVariable{}
	for id, variable := range d.Variables {
		if variable.Source != nil && variable.Source.Port != "" {
			found := false
			for _, s := range sections {
				if s.ID == variable.Source.Section && s.HasRecordOutput(variable.Source.Port, id) {
					found = true
				}
			}
			if !found {
				continue
			}
		}
		if variable.Source == nil || (variable.Source.Kind == pageWidgets.Runtime.Loop.Source || variable.Source.Kind == "application" || (variable.Source.Kind == "plan" || variable.Mode == "aggregate") || variable.Source.Kind == "property") || allowed[variable.Source.Section] {
			variables[id] = variable
		}
	}
	for changed := true; changed; {
		changed = false
		for id, q := range queries {
			if q.MissingSetInput(queries) {
				delete(queries, id)
				changed = true
				continue
			}
			for _, param := range q.Variables() {
				if _, ok := variables[param]; !ok {
					delete(queries, id)
					changed = true
					break
				}
			}
		}
		for id, variable := range variables {
			if variable.Source != nil && variable.Source.Compute != nil {
				if _, ok := variables[variable.Source.Compute.RecordVariable]; !ok {
					delete(variables, id)
					changed = true
					continue
				}
			}
			if variable.Mode == "property" && variable.Source != nil {
				if _, ok := variables[variable.Source.Variable]; !ok {
					delete(variables, id)
					changed = true
					continue
				}
			}
			if variable.Source != nil && (variable.Source.Kind == "plan" || variable.Mode == "aggregate") {
				if _, ok := queries[variable.Source.Query]; !ok {
					delete(variables, id)
					changed = true
					continue
				}
			}
			if variable.Scope == pageWidgets.Runtime.Loop.Scope {
				loop := d.Nodes[variable.Owner].Loop
				if loop == nil {
					delete(variables, id)
					changed = true
					continue
				}
				if _, ok := variables[loop.Collection]; !ok {
					delete(variables, id)
					changed = true
					continue
				}
			}
			if variable.Expression != nil {
				for _, arg := range variable.Expression.Args {
					if arg.Variable != "" {
						if _, ok := variables[arg.Variable]; !ok {
							delete(variables, id)
							changed = true
							break
						}
					}
				}
			}
		}
	}
	out := &PageDocument{FormatVersion: d.FormatVersion, UIProfile: d.UIProfile, Root: d.Root, Nodes: map[string]PageLayoutNode{}, Variables: variables, Interface: d.Interface, Queries: queries}
	var copyVisible func(string) bool
	copyVisible = func(id string) bool {
		node, ok := d.Nodes[id]
		if !ok {
			return false
		}
		if node.Loop != nil {
			if _, available := variables[node.Loop.Collection]; !available {
				return false
			}
		}
		if node.ValueVariable != "" {
			if _, available := variables[node.ValueVariable]; !available {
				return false
			}
		}
		if node.EnabledWhen != "" {
			if _, available := variables[node.EnabledWhen]; !available {
				return false
			}
		}
		if node.VisibleWhen != "" {
			if _, visible := variables[node.VisibleWhen]; !visible {
				return false
			}
		}
		if node.Kind == "widget" {
			for _, s := range sections {

				if s.ID == node.Section {
					for _, id := range s.ExplorationVariables() {
						if id != "" {
							if _, ok := variables[id]; !ok {
								return false
							}
						}
					}
					for _, id := range append(append(s.ContextViewVariables(), s.ObservationVariables()...), append(append(s.EmbeddingVariables(), s.AIVariables()...), s.SpatialVariables()...)...) {
						if id != "" {
							if _, ok := variables[id]; !ok {
								return false
							}
						}
					}
					for _, id := range []string{s.NotepadVariable, s.AnalysisXVariable, s.AnalysisYVariable, s.AnalysisCountVariable, s.AnalysisMeanVariable, s.CommentDraftVariable, s.FileVariable, s.PdfPageVariable, s.RecordSetVariable, s.SparklineDecimalVariable, s.SparklineNumberVariable, s.GroupValueVariable, s.GroupSetVariable, s.RowValueVariable, s.RowSetVariable, s.ColumnValueVariable, s.ColumnSetVariable, s.PickerValueVariable, s.AlertValueVariable, s.DateVariable, s.ChoiceSetVariable, s.ChoiceVariable, s.BooleanVariable, s.RangeMinVariable, s.RangeMaxVariable, s.StatisticsVariable, s.GaugeValueVariable, s.ProgressValueVariable, s.ProgressTotalVariable} {
						if id != "" {
							if _, ok := variables[id]; !ok {
								return false
							}
						}
					}
					if s.CountVariable != "" {
						if _, ok := variables[s.CountVariable]; !ok {
							return false
						}
					}
					if s.SelectionSetVariable != "" {
						if _, ok := variables[s.SelectionSetVariable]; !ok {
							return false
						}
					}
					for _, facet := range s.Facets {
						if _, ok := variables[facet.Variable]; !ok {
							return false
						}
					}
					if s.FilterSearchVariable != "" {
						if _, ok := variables[s.FilterSearchVariable]; !ok {
							return false
						}
					}
				}
				if s.ID == node.Section && (s.CollectionVariable != "" || s.RecordVariable != "" || s.SelectionVariable != "" || s.FilterVariable != "") {
					if _, ok := variables[s.CollectionVariable]; s.CollectionVariable != "" && !ok {
						return false
					}
					if _, ok := variables[s.FilterVariable]; s.FilterVariable != "" && !ok {
						return false
					}
					if _, ok := variables[s.SelectionVariable]; s.SelectionVariable != "" && !ok {
						return false
					}
					if _, ok := variables[s.RecordVariable]; s.RecordVariable != "" && !ok {
						return false
					}
				}
			}
			if !allowed[node.Section] {
				return false
			}
			out.Nodes[id] = node
			return true
		}
		children := make([]string, 0, len(node.Children))
		for _, child := range node.Children {
			if copyVisible(child) {
				children = append(children, child)
			}
		}
		dormant := false
		for _, entry := range d.UnusedWidgets {
			if entry.Parent == id && copyVisible(entry.Node) {
				out.UnusedWidgets = append(out.UnusedWidgets, entry)
				dormant = true
			}
		}
		if len(children) == 0 && !dormant && id != d.Root {
			return false
		}
		node.Children = children
		out.Nodes[id] = node
		return true
	}
	for {
		out.UnusedWidgets = nil
		out.Nodes = map[string]PageLayoutNode{}
		out.Overlays = map[string]PageOverlay{}
		missing := map[string]bool{}
		for id, overlay := range d.Overlays {
			if copyVisible(overlay.Root) {
				out.Overlays[id] = overlay
			} else {
				missing[overlay.OpenVariable] = true
			}
		}
		out.Events = nil
		bound := map[string]bool{}
		for _, event := range d.Events {
			if allowed[event.Source] && !missing[event.Target] {
				valid := true
				if event.Navigate != nil {
					for _, arg := range event.Navigate.Inputs {
						if arg.Variable != "" {
							if _, ok := variables[arg.Variable]; !ok {
								valid = false
							}
						}
					}
					for _, id := range event.Navigate.Results {
						if _, ok := variables[id]; !ok {
							valid = false
						}
					}
				}
				if _, ok := variables[event.Target]; valid && (ok || event.Navigate != nil || event.Return) {
					out.Events = append(out.Events, event)
					bound[event.Source] = true
				}
			}
		}
		changed := false
		for _, section := range sections {
			if (section.Widget == "button" || section.Widget == "button-group") && allowed[section.ID] && !bound[section.ID] {
				delete(allowed, section.ID)
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	copyVisible(d.Root)
	if _, ok := out.Nodes[d.Root]; !ok {
		out.Nodes = map[string]PageLayoutNode{d.Root: {Kind: "rows"}}
	}
	for id, variable := range out.Variables {
		if variable.Scope == "overlay" {
			if _, ok := out.Overlays[variable.Owner]; !ok {
				delete(out.Variables, id)
			}
		}
		if variable.Scope == pageWidgets.Runtime.Loop.Scope && out.Nodes[variable.Owner].Kind != "loop" {
			delete(out.Variables, id)
		}
	}
	for id, q := range out.Queries {
		if q.Owner != "" {
			if _, ok := out.Overlays[q.Owner]; !ok {
				delete(out.Queries, id)
			}
		}
	}
	if d.Interface != nil {
		iface := *d.Interface
		iface.Inputs = map[string]PagePort{}
		iface.Outputs = map[string]PagePort{}
		for id, port := range d.Interface.Inputs {
			if _, ok := out.Variables[port.Variable]; ok {
				iface.Inputs[id] = port
			}
		}
		for id, port := range d.Interface.Outputs {
			if _, ok := out.Variables[port.Variable]; ok {
				iface.Outputs[id] = port
			}
		}
		out.Interface = &iface
	}
	return out
}
