package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// One controlled presentation family over original queries and scalar ports.
type PageAnalysisStep struct {
	Value    string `json:"value"`
	Label    string `json:"label"`
	Positive bool   `json:"positive"`
}
type PageCollectionAnalysis struct {
	Kind       string             `json:"kind"`
	GroupField string             `json:"groupField,omitempty"`
	Field      string             `json:"field,omitempty"`
	Unit       string             `json:"unit,omitempty"`
	Fields     []string           `json:"fields,omitempty"`
	Steps      []PageAnalysisStep `json:"steps,omitempty"`
}

func (s Section) AnalysisVariables() []string {
	return []string{s.CollectionVariable, s.AnalysisXVariable, s.AnalysisYVariable, s.AnalysisCountVariable, s.AnalysisMeanVariable}
}
func (d *PageDocument) checkCollectionAnalysis(s Section) error {
	if s.Widget != "collection-analysis" {
		if s.Analysis != nil || s.AnalysisXVariable != "" || s.AnalysisYVariable != "" || s.AnalysisCountVariable != "" || s.AnalysisMeanVariable != "" {
			return fmt.Errorf("analysis bindings need their widget")
		}
		return nil
	}
	c, v := s.Analysis, d.Variables[s.CollectionVariable]
	if c == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.CollectionAnalysis.RequiredUIProfile) || v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || s.Selection != "" || s.RecordVariable != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.FilterVariable != "" || s.ParentSelection != "" || s.Relation != "" || s.Query != (AssetRef{}) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Operation != nil || s.Function != nil || s.InlineEdit != nil || len(s.Inputs) > 0 {
		return fmt.Errorf("analysis needs its original plan and read-only configuration")
	}
	q, ok := d.Queries[v.Source.Query]
	if !ok || q.Owner != v.Owner || q.ItemOwner != "" || s.Object.Name != "" && q.Object != s.Object {
		return fmt.Errorf("analysis needs its original query owner and object")
	}
	port := func(id, typ, mode, kind, measure string) bool {
		p, ok := d.Variables[id]
		return ok && p.Type == typ && p.Mode == mode && p.Scope == v.Scope && p.Owner == v.Owner && (mode == "state" || p.Source != nil && p.Source.Kind == kind && p.Source.Query == v.Source.Query && p.Source.Measure == measure)
	}
	initial := func(id string) string {
		var value string
		if json.Unmarshal(d.Variables[id].Initial, &value) != nil {
			return ""
		}
		return value
	}
	switch c.Kind {
	case "status-bars", "signed-counts":
		if c.GroupField == "" || c.GroupField == "count" || strings.Contains(c.GroupField, ":") || c.Field != "" || c.Unit != "" || len(c.Fields) > 0 || s.AnalysisXVariable != "" || s.AnalysisYVariable != "" || s.AnalysisCountVariable != "" || s.AnalysisMeanVariable != "" {
			return fmt.Errorf("count analysis needs one original group field")
		}
		if c.Kind == "status-bars" && len(c.Steps) > 0 {
			return fmt.Errorf("status bars do not declare signed steps")
		}
		if c.Kind == "signed-counts" {
			if len(c.Steps) != 4 {
				return fmt.Errorf("signed counts need four explicit original steps")
			}
			seen := map[string]bool{}
			for i, step := range c.Steps {
				if step.Value == "" || len(step.Value) > 1024 || len(step.Label) > 1024 || seen[step.Value] || step.Positive != (i == 0) {
					return fmt.Errorf("signed counts need unique original values and original signs")
				}
				seen[step.Value] = true
			}
		}
	case "derived-mean":
		if c.Field == "" || len(c.Unit) > 64 || c.GroupField != "" || len(c.Fields) > 0 || len(c.Steps) > 0 || s.AnalysisXVariable != "" || s.AnalysisYVariable != "" || !port(s.AnalysisCountVariable, "decimal", "aggregate", "count", "") || !port(s.AnalysisMeanVariable, "number", "aggregate", "aggregate", "avg:"+c.Field) {
			return fmt.Errorf("derived mean needs matching original count and average ports")
		}
	case "record-axes":
		if c.GroupField != "" || c.Field != "" || c.Unit != "" || len(c.Steps) > 0 || len(c.Fields) != 4 || s.AnalysisCountVariable != "" || s.AnalysisMeanVariable != "" || s.AnalysisXVariable == s.AnalysisYVariable || !port(s.AnalysisXVariable, "string", "state", "", "") || !port(s.AnalysisYVariable, "string", "state", "", "") || !slices.Contains(c.Fields, initial(s.AnalysisXVariable)) || !slices.Contains(c.Fields, initial(s.AnalysisYVariable)) || q.Limit != pageWidgets.Runtime.CollectionAnalysis.MaxRecords || q.Offset != 0 || !slices.Equal(q.Sort, []string{"id"}) {
			return fmt.Errorf("record axes need four fields, independent same-owner state and an original 80-record ID window")
		}
		seen := map[string]bool{}
		for _, f := range c.Fields {
			if f == "" || seen[f] {
				return fmt.Errorf("record axes need four distinct original fields")
			}
			seen[f] = true
		}
		for _, axis := range []string{s.AnalysisXVariable, s.AnalysisYVariable} {
			if d.analysisAxisAffectsQuery(axis) {
				return fmt.Errorf("analysis axes change presentation and cannot feed a business query")
			}
		}
	default:
		return fmt.Errorf("analysis kind is unavailable")
	}
	return nil
}

func (d *PageDocument) analysisAxisAffectsQuery(axis string) bool {
	seen := map[string]bool{}
	var depends func(string) bool
	depends = func(id string) bool {
		if id == axis {
			return true
		}
		if id == "" || seen[id] {
			return false
		}
		seen[id] = true
		v := d.Variables[id]
		if v.Expression != nil {
			for _, arg := range v.Expression.Args {
				if depends(arg.Variable) {
					return true
				}
			}
		}
		return v.Source != nil && depends(v.Source.Variable)
	}
	for _, q := range d.Queries {
		for _, value := range q.Values() {
			if depends(value.Variable) {
				return true
			}
		}
	}
	return false
}
func (s Section) CheckCollectionAnalysis(info EntityInfo) error {
	if s.Widget != "collection-analysis" {
		return nil
	}
	c := s.Analysis
	if c == nil {
		return fmt.Errorf("analysis configuration is unavailable")
	}
	fields := c.Fields
	if c.Field != "" {
		fields = []string{c.Field}
	}
	for _, name := range fields {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("analysis needs readable original numeric fields")
		}
	}
	if c.GroupField != "" {
		f, ok := info.Field(c.GroupField)
		if !ok || !slices.Contains([]string{"text", "choice"}, f.Type) {
			return fmt.Errorf("analysis needs a readable original category")
		}
		for _, step := range c.Steps {
			if f.Type == "choice" && !slices.Contains(f.Choices, step.Value) {
				return fmt.Errorf("analysis step is outside its original choices")
			}
		}
	}
	return nil
}
func (p Page) CheckAnalysisQuery(id string, named *Definition) error {
	if p.Document == nil {
		return nil
	}
	for _, s := range p.Sections {
		v := p.Document.Variables[s.CollectionVariable]
		if s.Analysis == nil || s.Analysis.Kind != "record-axes" || v.Source == nil || v.Source.Query != id || named == nil || named.Query == nil {
			continue
		}
		q := named.Query
		if len(q.Sort) > 0 && !slices.Equal(q.Sort, []string{"id"}) || q.Limit > 0 && q.Limit < pageWidgets.Runtime.CollectionAnalysis.MaxRecords {
			return fmt.Errorf("record axes require the retained query's original ID ordering and 80-record capacity")
		}
	}
	return nil
}
