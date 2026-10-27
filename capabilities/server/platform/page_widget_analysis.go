package platform

import (
	"fmt"
	"math"
	"slices"
	"strings"
)

// page_gauge.go

type PageGauge struct {
	Max    float64  `json:"max"`
	WarnAt *float64 `json:"warnAt,omitempty"`
	Label  string   `json:"label,omitempty"`
	Suffix string   `json:"suffix,omitempty"`
}

func (d *PageDocument) checkGauge(s Section) error {
	if s.Widget != "gauge" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	g := s.Gauge
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Gauge.RequiredUIProfile) || g == nil || g.Max <= 0 || math.IsNaN(g.Max) || math.IsInf(g.Max, 0) || g.WarnAt != nil && (math.IsNaN(*g.WarnAt) || math.IsInf(*g.WarnAt, 0)) || len(g.Label) > 1024 || len(g.Suffix) > 64 {
		return fmt.Errorf("gauge needs its number port, positive finite maximum and bounded presentation")
	}
	return nil
}

// page_sparkline.go

type PageSparkline struct {
	Field  string  `json:"field,omitempty"`
	Label  *string `json:"label,omitempty"`
	Suffix string  `json:"suffix,omitempty"`
}

func (d *PageDocument) checkSparkline(s Section) error {
	if s.Widget != "sparkline-kpi" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	c := s.Sparkline
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Sparkline.RequiredUIProfile) || c == nil || c.Label != nil && len(*c.Label) > 1024 || len(c.Suffix) > 64 {
		return fmt.Errorf("sparkline needs one original scalar and bounded presentation")
	}
	id := s.SparklineDecimalVariable
	if s.SparklineNumberVariable != "" {
		id = s.SparklineNumberVariable
	}
	v := d.Variables[id]
	if s.CollectionVariable == "" {
		if c.Field != "" {
			return fmt.Errorf("sparkline field needs its original series window")
		}
		return nil
	}
	set := d.Variables[s.CollectionVariable]
	var q PageQuery
	if set.Source != nil {
		q = d.Queries[set.Source.Query]
	}
	if c.Field == "" || set.Mode != "resource" || set.Source == nil || set.Source.Kind != "plan" || set.Scope != v.Scope || set.Owner != v.Owner || len(q.Sort) == 0 || q.Limit < 1 || q.Limit > pageWidgets.Runtime.Sparkline.MaxPoints {
		return fmt.Errorf("sparkline series needs a same-owner ordered original 30-record window")
	}
	return nil
}
func (s Section) CheckSparkline(info EntityInfo) error {
	if s.Widget != "sparkline-kpi" {
		return nil
	}
	if s.Sparkline == nil {
		return fmt.Errorf("sparkline configuration is missing")
	}
	return s.checkWidgetFieldRoles(info)
}

// page_histogram.go

type PageHistogram struct {
	Field string `json:"field"`
	Bins  int    `json:"bins"`
}

func (d *PageDocument) checkHistogram(s Section) error {
	if s.Widget != "histogram" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	h := s.Histogram
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Histogram.RequiredUIProfile) || h == nil || h.Field == "" || h.Bins < 1 || h.Bins > pageWidgets.Runtime.Histogram.MaxBins {
		return fmt.Errorf("histogram needs scoped original plan, numeric field and bounded bins")
	}
	return nil
}
func (s Section) CheckHistogram(info EntityInfo) error {
	if s.Widget != "histogram" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

// page_heatmap.go

func (d *PageDocument) checkHeatmap(s Section) error {
	if s.Widget != "heatmap" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Heatmap.RequiredUIProfile) || s.Measure != "count" || s.Group == "" || s.ColumnGroup == "" || s.Group == s.ColumnGroup || len(s.Fields) > 0 || len(s.Actions) > 0 {
		return fmt.Errorf("heatmap needs two original axes, complete count and exclusive scoped filter types")
	}

	return nil
}

func (s Section) CheckHeatmap(info EntityInfo) error {
	if s.Widget != "heatmap" {
		return nil
	}
	if s.Measure != "count" || s.Group == s.ColumnGroup || s.Group == "count" || s.ColumnGroup == "count" {
		return fmt.Errorf("heatmap needs complete count and distinct original fields")
	}
	return s.checkWidgetFieldRoles(info)
}

// page_treemap.go

// Count presentations share the declared exclusive state ports and owner.
func (d *PageDocument) checkTreemap(s Section) error {
	if s.Widget != "treemap" && s.Widget != "tag-counts" {
		return nil
	}
	return d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget))
}

// page_terms.go

func (d *PageDocument) checkTerms(s Section) error {
	if s.Widget != "term-counts" && s.Widget != "treemap" && s.Widget != "tag-counts" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Terms.RequiredUIProfile) || s.Group == "" || s.Group == "count" || strings.Contains(s.Group, ":") || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Selection != "" {
		return fmt.Errorf("term counts need original scoped plan and an unambiguous text group")
	}
	return nil
}
func (s Section) CheckTerms(info EntityInfo) error {
	if s.Widget != "term-counts" && s.Widget != "treemap" && s.Widget != "tag-counts" {
		return nil
	}
	if s.Group == "count" || strings.Contains(s.Group, ":") {
		return fmt.Errorf("term counts need an unambiguous original group")
	}
	return s.checkWidgetFieldRoles(info)
}

// page_summary.go

func (d *PageDocument) checkSummary(s Section) error {
	if s.Widget != "summary-stats" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	set, stats := d.Variables[s.CollectionVariable], d.Variables[s.StatisticsVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Summary.RequiredUIProfile) || s.SummaryField == "" || stats.Source.Query != set.Source.Query || stats.Source.Measure != s.SummaryField {
		return fmt.Errorf("summary needs matching original statistics and query inputs")
	}
	return nil
}
func (s Section) CheckSummary(info EntityInfo) error {
	if s.Widget != "summary-stats" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

// page_record_scatter.go

// PageRecordScatter presents original records, never aggregate points.
type PageRecordScatter struct {
	XField     string `json:"xField"`
	YField     string `json:"yField"`
	ColorField string `json:"colorField"`
	LabelField string `json:"labelField"`
}

func (d *PageDocument) checkScatter(s Section) error {
	if s.Widget != "record-scatter" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if err := d.checkSharedRecordOutput(s); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordScatter.RequiredUIProfile) || s.Scatter == nil {
		return fmt.Errorf("scatter needs its profile, fields and bounded ordered original plan window")
	}
	return nil
}

func (s Section) CheckScatter(info EntityInfo) error {
	if s.Widget != "record-scatter" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

// page_record_chart.go

type PageRecordChart struct {
	Mark   string `json:"mark"`
	XField string `json:"xField"`
	YField string `json:"yField"`
}

func (d *PageDocument) checkRecordChart(s Section) error {
	if s.Widget != "record-chart" {
		return nil
	}
	if err := d.checkDeclaredWidgetPorts(s, pageWidget(s.Widget)); err != nil {
		return err
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordChart.RequiredUIProfile) || s.RecordChart == nil || !slices.Contains(pageWidgets.Runtime.RecordChart.Marks, s.RecordChart.Mark) {
		return fmt.Errorf("record chart needs its profile, axes and ordered original plan window")
	}
	return nil
}

func (s Section) CheckRecordChart(info EntityInfo) error {
	if s.Widget != "record-chart" {
		return nil
	}
	return s.checkWidgetFieldRoles(info)
}

// page_facets.go

func (d *PageDocument) checkFacets(s Section) error {
	if len(s.Facets) == 0 && s.FilterSearchVariable == "" {
		return nil
	}
	if s.Widget != "filter" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.30") || len(s.Facets) > 16 || s.FilterVariable != "" || s.CollectionVariable == "" {
		return fmt.Errorf("facets require v2.30 filter and an exclusive source plan")
	}
	owners := d.overlayOwners()
	owner := ""
	for id, n := range d.Nodes {
		if n.Section == s.ID {
			owner = owners[id]
		}
	}
	check := func(id, typ string) bool {
		v, ok := d.Variables[id]
		return ok && v.Type == typ && v.Mode == "state" && (v.Scope == "page" || v.Scope == "overlay" && v.Owner == owner && owner != "")
	}
	seen := map[string]bool{}
	for _, f := range s.Facets {
		typ := "string-set"
		if f.Kind == "search" || f.Kind == "number" {
			typ = "string"
		}
		if !pageNodeID.MatchString(f.Field) || seen[f.Field] || !slices.Contains([]string{"checkbox", "histogram", "search", "number"}, f.Kind) || !check(f.Variable, typ) {
			return fmt.Errorf("facet needs a unique field and compatible state binding")
		}
		seen[f.Field] = true
	}
	if s.FilterSearchVariable != "" && !check(s.FilterSearchVariable, "string") {
		return fmt.Errorf("facet search needs text state")
	}
	return nil
}

func (s Section) CheckFacetSchema(info EntityInfo) error {
	for _, facet := range s.Facets {
		f, ok := info.Field(facet.Field)
		if !ok || facet.Kind == "number" && !slices.Contains([]string{"integer", "decimal"}, f.Type) || facet.Kind != "number" && !slices.Contains([]string{"text", "choice"}, f.Type) {
			return fmt.Errorf("facet field type is unavailable")
		}
	}
	return nil
}

// page_shared_record_output.go

// Shared producers keep application ownership; the reference is not an access grant.
func (d *PageDocument) checkSharedRecordOutput(s Section) error {
	if s.SelectionVariable == "" {
		return nil
	}
	if s.Widget != "record-scatter" && s.Widget != "record-leaderboard" && s.Widget != "record-list" && s.Widget != "record-map" {
		return nil
	}
	v := d.Variables[s.SelectionVariable]
	if s.Selection != "" || s.Widget == "record-list" && s.RecordList != nil && s.RecordList.Layout == "tiles" || v.Type != "record" || v.Scope != "application" || v.Mode != "shared" || !v.Writable || v.Source == nil || v.Source.Kind != "application" || v.Source.Object == nil {
		return fmt.Errorf("shared record output needs its writable original application record")
	}
	overlays, loops := d.overlayOwners(), d.loopOwners()
	for id, n := range d.Nodes {
		if n.Section == s.ID && (overlays[id] != "" || loops[id] != "") {
			return fmt.Errorf("shared record output needs a main-page producer")
		}
	}
	return nil
}
