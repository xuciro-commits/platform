package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

type PageObservationSignal struct {
	Field string `json:"field"`
	Unit  string `json:"unit"`
	Group string `json:"group,omitempty"`
}
type PageObservationMetadata struct {
	Plant  string `json:"plant,omitempty"`
	Device string `json:"device,omitempty"`
	Asset  string `json:"asset,omitempty"`
	Status string `json:"status,omitempty"`
}
type PageObservation struct {
	Kind         string                   `json:"kind"`
	TimeField    string                   `json:"timeField"`
	Signals      []PageObservationSignal  `json:"signals"`
	Metadata     *PageObservationMetadata `json:"metadata,omitempty"`
	RowHeight    int                      `json:"rowHeight,omitempty"`
	AssetObject  *AssetRef                `json:"assetObject,omitempty"`
	AssetField   string                   `json:"assetField,omitempty"`
	RowOutput    string                   `json:"rowOutput,omitempty"`
	AssetOutput  string                   `json:"assetOutput,omitempty"`
	AverageField string                   `json:"averageField,omitempty"`
}

func (s Section) hasObservationConfiguration() bool {
	return s.Observation != nil || s.ObservationHistoryVariable != "" || s.ObservationContextVariable != "" || s.ObservationSignalVariable != "" || s.ObservationThresholdVariable != "" || s.ObservationRowsVariable != "" || s.ObservationCountVariable != "" || s.ObservationMeanVariable != ""
}

func (s Section) HasRecordOutput(port, variable string) bool {
	if port == "" || variable == "" {
		return false
	}
	if s.GraphExplorer != nil && s.Widget == "graph-explorer" {
		return slices.ContainsFunc(s.GraphExplorer.Outputs, func(o PageGraphOutput) bool { return o.ID == port && o.Variable == variable })
	}
	if s.Observation != nil && s.Widget == "observation" && s.Observation.Kind == "table" {
		return port == "row" && s.Observation.RowOutput == variable || port == "asset" && s.Observation.AssetOutput == variable
	}
	return false
}
func (s Section) ObservationVariables() []string {
	if s.Widget != "observation" {
		return nil
	}
	ids := []string{s.CollectionVariable, s.RecordVariable, s.ObservationHistoryVariable, s.ObservationContextVariable, s.ObservationSignalVariable, s.ObservationThresholdVariable, s.ObservationRowsVariable, s.ObservationCountVariable, s.ObservationMeanVariable}
	if s.Observation != nil {
		ids = append(ids, s.Observation.RowOutput, s.Observation.AssetOutput)
	}
	return ids
}
func (p Page) ObservationObject(s Section) AssetRef {
	if p.Document == nil {
		return AssetRef{}
	}
	id := s.CollectionVariable
	if s.Observation != nil && s.Observation.Kind == "availability" {
		id = s.ObservationHistoryVariable
	}
	v := p.Document.Variables[id]
	if v.Source == nil || v.Source.Kind != "plan" {
		return AssetRef{}
	}
	return p.Document.Queries[v.Source.Query].Object
}
func (d *PageDocument) checkObservation(s Section, sections []Section) error {
	if s.Widget != "observation" {
		if s.hasObservationConfiguration() {
			return fmt.Errorf("observation configuration needs its widget")
		}
		return nil
	}
	c, v := s.Observation, d.Variables[s.CollectionVariable]
	if c == nil || !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Observation.RequiredUIProfile) || !slices.Contains([]string{"table", "statistics", "availability", "series"}, c.Kind) || len(c.Signals) < 1 || len(c.Signals) > pageWidgets.Runtime.Observation.MaxSignals || !pageNodeID.MatchString(c.TimeField) || v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || s.Selection != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.FilterVariable != "" || s.RecordSetVariable != "" || s.Relation != "" || s.ParentSelection != "" || s.Query != (AssetRef{}) || s.InlineEdit != nil || s.Operation != nil || s.Function != nil || len(s.Fields) > 0 || len(s.Actions) > 0 || len(s.Inputs) > 0 {
		return fmt.Errorf("observation needs its scoped original plan and finite read-only configuration")
	}
	same := func(id, typ, mode string) bool {
		p := d.Variables[id]
		return p.Type == typ && p.Mode == mode && p.Scope == v.Scope && p.Owner == v.Owner
	}
	plan := func(id string, context bool) bool {
		p := d.Variables[id]
		if !same(id, "object-set", "resource") || p.Source == nil || p.Source.Kind != "plan" {
			return false
		}
		q, ok := d.Queries[p.Source.Query]
		return ok && q.Owner == v.Owner && q.ItemOwner == "" && q.Limit == pageWidgets.Runtime.Observation.MaxRecords && q.Offset == 0 && slices.Equal(q.Sort, []string{"-" + c.TimeField, "id"}) && (context || q.For == nil)
	}
	primary := d.Queries[v.Source.Query]
	if primary.Owner != v.Owner || primary.ItemOwner != "" {
		return fmt.Errorf("observation query owner differs")
	}
	// A paged table cannot move a view promised as the latest fixed history.
	fixed := []string{s.ObservationHistoryVariable, s.ObservationContextVariable}
	if c.Kind == "series" || c.Kind == "statistics" && s.RecordVariable == "" {
		fixed = append(fixed, s.CollectionVariable)
	}
	for _, id := range fixed {
		window := d.Variables[id]
		if window.Source == nil || window.Source.Kind != "plan" {
			continue
		}
		for _, other := range sections {
			input := d.Variables[other.CollectionVariable]
			if other.Widget == "observation" && other.Observation != nil && other.Observation.Kind == "table" && input.Source != nil && input.Source.Query == window.Source.Query {
				return fmt.Errorf("fixed observation history cannot share a paged table plan")
			}
		}
	}
	seen := map[string]bool{c.TimeField: true}
	for _, signal := range c.Signals {
		if !pageNodeID.MatchString(signal.Field) || seen[signal.Field] || len(signal.Unit) > 64 || len(signal.Group) > 128 {
			return fmt.Errorf("observation signals need unique original fields and bounded units")
		}
		seen[signal.Field] = true
	}
	if c.Metadata != nil {
		for _, field := range []string{c.Metadata.Plant, c.Metadata.Device, c.Metadata.Asset, c.Metadata.Status} {
			if field != "" {
				if !pageNodeID.MatchString(field) || seen[field] {
					return fmt.Errorf("observation metadata fields must be distinct")
				}
				seen[field] = true
			}
		}
	}
	if c.Kind != "table" && (c.Metadata != nil || c.RowHeight != 0 || c.RowOutput != "" || c.AssetOutput != "") {
		return fmt.Errorf("only observation table declares metadata and record outputs")
	}
	if c.AssetObject != nil && c.AssetField == "" {
		return fmt.Errorf("observation asset needs its original reference field")
	}
	if c.AssetObject == nil && c.AssetField != "" {
		return fmt.Errorf("observation asset field needs its object")
	}
	if c.Kind == "table" && c.AssetObject != nil && (c.AssetOutput == "" || c.Metadata == nil || c.Metadata.Asset != c.AssetField) {
		return fmt.Errorf("table asset metadata and output must retain the same original reference")
	}
	if c.AssetObject != nil && (c.AssetObject.Check() != nil || c.AssetObject.Kind != AssetObject) {
		return fmt.Errorf("observation asset needs its complete original object")
	}
	if c.Kind == "table" {
		if !plan(s.CollectionVariable, false) || c.RowHeight < 26 || c.RowHeight > 44 || s.RecordVariable != "" || c.AverageField != "" {
			return fmt.Errorf("observation table needs its original window and row height")
		}
		for port, id := range map[string]string{"row": c.RowOutput, "asset": c.AssetOutput} {
			if id == "" {
				continue
			}
			p := d.Variables[id]
			if port == "asset" && d.sharedTelemetryRecord(id) {
				if !p.Writable || c.AssetObject == nil || *p.Source.Object != *c.AssetObject {
					return fmt.Errorf("observation asset output needs its writable original application object")
				}
				overlays, loops := d.overlayOwners(), d.loopOwners()
				for node, n := range d.Nodes {
					if n.Section == s.ID && (overlays[node] != "" || loops[node] != "") {
						return fmt.Errorf("shared observation asset output needs a main-page producer")
					}
				}
				continue
			}
			if !same(id, "record", "resource") || p.Writable || p.Source == nil || p.Source.Kind != "record" || p.Source.Section != s.ID || p.Source.Port != port {
				return fmt.Errorf("observation output needs one same-owner original record port")
			}
		}
		if c.RowOutput != "" && c.RowOutput == c.AssetOutput || c.AssetOutput != "" && (c.RowOutput == "" || c.AssetObject == nil || c.Metadata == nil || c.Metadata.Asset == "") {
			return fmt.Errorf("asset output needs its original row and typed reference")
		}
	}
	if c.Kind == "series" {
		if len(c.Signals) != 3 || !plan(s.CollectionVariable, false) || s.RecordVariable != "" || c.AssetObject != nil || c.AverageField != "" {
			return fmt.Errorf("series needs three original fields and actual time window")
		}
	}
	if c.Kind == "statistics" {
		if !plan(s.CollectionVariable, false) || !same(s.ObservationSignalVariable, "string", "state") || !same(s.ObservationThresholdVariable, "string", "state") || !same(s.ObservationRowsVariable, "string", "state") || s.ObservationSignalVariable == s.ObservationThresholdVariable || s.ObservationSignalVariable == s.ObservationRowsVariable || s.ObservationRowsVariable == s.ObservationThresholdVariable || c.AverageField != "" {
			return fmt.Errorf("statistics need distinct same-owner signal, threshold and window state")
		}
		var signal, threshold, rows string
		if json.Unmarshal(d.Variables[s.ObservationSignalVariable].Initial, &signal) != nil || !slices.ContainsFunc(c.Signals, func(s PageObservationSignal) bool { return s.Field == signal }) || json.Unmarshal(d.Variables[s.ObservationThresholdVariable].Initial, &threshold) != nil || json.Unmarshal(d.Variables[s.ObservationRowsVariable].Initial, &rows) != nil || !slices.Contains([]string{"1000", "10000", "100000"}, rows) {
			return fmt.Errorf("statistics initial state differs from its original declared choices")
		}
		if s.RecordVariable != "" {
			if _, _, ok := d.recordInputOwner(s); !ok {
				return fmt.Errorf("shared statistics cannot enter a loop")
			}
			if !(d.sharedTelemetryRecord(s.RecordVariable) || same(s.RecordVariable, "record", "resource") || d.Variables[s.RecordVariable].Scope == "page" && d.Variables[s.RecordVariable].Type == "record" && d.Variables[s.RecordVariable].Mode == "resource") || c.AssetObject == nil || s.ObservationContextVariable == "" || !plan(s.ObservationContextVariable, true) {
				return fmt.Errorf("statistics context needs the original asset and retained history")
			}
			q := d.Queries[d.Variables[s.ObservationContextVariable].Source.Query]
			if q.Query == nil || q.Query.Ref.Kind != AssetQuery || q.Set != nil || q.For == nil || q.For.Variable != s.RecordVariable {
				return fmt.Errorf("statistics history must retain its asset parent query")
			}
		} else if s.ObservationContextVariable != "" || c.AssetObject != nil {
			return fmt.Errorf("statistics context cannot exist without its original asset")
		}
	}
	if c.Kind == "availability" {
		count, mean := d.Variables[s.ObservationCountVariable], d.Variables[s.ObservationMeanVariable]
		if len(c.Signals) != 1 || !plan(s.ObservationHistoryVariable, false) || c.AverageField == "" || s.RecordVariable != "" || c.AssetObject != nil || !same(s.ObservationCountVariable, "decimal", "aggregate") || !same(s.ObservationMeanVariable, "number", "aggregate") || count.Source == nil || count.Source.Kind != "count" || count.Source.Query != v.Source.Query || mean.Source == nil || mean.Source.Kind != "aggregate" || mean.Source.Query != v.Source.Query || mean.Source.Measure != "avg:"+c.AverageField {
			return fmt.Errorf("availability needs original asset average/count and an independent actual history")
		}
	}
	if c.Kind != "statistics" && (s.ObservationSignalVariable != "" || s.ObservationThresholdVariable != "" || s.ObservationRowsVariable != "" || s.ObservationContextVariable != "") || c.Kind != "availability" && (s.ObservationCountVariable != "" || s.ObservationMeanVariable != "" || s.ObservationHistoryVariable != "") {
		return fmt.Errorf("observation bindings belong to their kind")
	}
	return nil
}
func (p Page) CheckObservationBinding(s Section) error {
	if s.Widget != "observation" || s.Observation == nil || s.Observation.Kind != "statistics" || s.RecordVariable == "" {
		return nil
	}
	if s.Observation.AssetObject == nil || p.RecordResourceObject(s.RecordVariable) != *s.Observation.AssetObject {
		return fmt.Errorf("statistics context differs from its original full asset identity")
	}
	return nil
}

func (p Page) CheckObservation(s Section, object func(AssetRef) (EntityInfo, bool)) error {
	if s.Widget != "observation" {
		return nil
	}
	if s.Observation == nil || p.Document == nil {
		return fmt.Errorf("observation definition is unavailable")
	}
	if err := p.CheckObservationBinding(s); err != nil {
		return err
	}
	c := s.Observation
	v := p.Document.Variables[s.CollectionVariable]
	if v.Source == nil || v.Source.Kind != "plan" {
		return fmt.Errorf("observation original plan is unavailable")
	}
	primary := p.Document.Queries[v.Source.Query].Object
	root := s.Object
	if root.Name == "" {
		root = p.Object
	}
	if primary != root {
		return fmt.Errorf("observation original primary object differs from section")
	}
	ref := p.ObservationObject(s)
	info, ok := object(ref)
	if !ok || info.App != ref.App || info.Type != ref.Name {
		return fmt.Errorf("observation original sample owner is unavailable")
	}
	field, ok := info.Field(c.TimeField)
	if !ok || field.Type != "datetime" {
		return fmt.Errorf("observation event time must be a readable original datetime")
	}
	for _, signal := range c.Signals {
		f, ok := info.Field(signal.Field)
		if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("observation signal is not an original readable number")
		}
	}
	if c.Metadata != nil {
		for _, name := range []string{c.Metadata.Plant, c.Metadata.Device, c.Metadata.Asset, c.Metadata.Status} {
			if name == "" {
				continue
			}
			f, ok := info.Field(name)
			if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) {
				return fmt.Errorf("observation metadata is not an original readable field")
			}
		}
	}
	if c.AssetObject != nil {
		asset, ok := object(*c.AssetObject)
		f, fok := info.Field(c.AssetField)
		if !ok || asset.App != c.AssetObject.App || asset.Type != c.AssetObject.Name || !fok || f.Type != "reference" || f.Ref != asset.Type {
			return fmt.Errorf("observation asset must use its actual reference target")
		}
	}
	if c.Kind == "statistics" && s.RecordVariable != "" {
		if p.RecordResourceObject(s.RecordVariable) != *c.AssetObject || p.WindowVariableObject(s.ObservationContextVariable) != info.Type {
			return fmt.Errorf("statistics context object differs from its original producer")
		}
	}
	if c.Kind == "availability" {
		v := p.Document.Variables[s.CollectionVariable]
		root := p.Document.Queries[v.Source.Query].Object
		asset, ok := object(root)
		f, fok := asset.Field(c.AverageField)
		if !ok || asset.App != root.App || asset.Type != root.Name || !fok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("availability average must retain its original readable asset field")
		}
	}
	return nil
}
func (p Page) CheckObservationQuery(id string, named *Definition) error {
	if p.Document == nil || named == nil || named.Query == nil {
		return nil
	}
	for _, s := range p.Sections {
		if s.Widget != "observation" || s.Observation == nil {
			continue
		}
		for _, variable := range []string{s.CollectionVariable, s.ObservationHistoryVariable, s.ObservationContextVariable} {
			if s.Observation.Kind == "availability" && variable == s.CollectionVariable {
				continue
			}
			v := p.Document.Variables[variable]
			if v.Source == nil || v.Source.Query != id {
				continue
			}
			q := named.Query
			if variable == s.ObservationContextVariable && q.By != s.Observation.AssetField {
				return fmt.Errorf("observation context query must retain its original asset reference")
			}
			if q.Limit > 0 && q.Limit < pageWidgets.Runtime.Observation.MaxRecords || len(q.Sort) > 0 && !slices.Equal(q.Sort, []string{"-" + s.Observation.TimeField, "id"}) {
				return fmt.Errorf("observation retained query must preserve its original time order and window capacity")
			}
		}
	}
	return nil
}
