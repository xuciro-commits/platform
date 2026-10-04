package platform

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

const spatialProfile = "platform.page.v2.87"

type PageRecordMap struct {
	LatitudeField  string `json:"latitudeField"`
	LongitudeField string `json:"longitudeField"`
	LabelField     string `json:"labelField"`
	ColorField     string `json:"colorField,omitempty"`
	ClusterEnabled bool   `json:"clusterEnabled"`
}
type PageSceneMapping struct {
	ID        string   `json:"id"`
	Node      string   `json:"node"`
	Source    string   `json:"source"`
	Field     string   `json:"field"`
	Mode      string   `json:"mode"`
	Axis      string   `json:"axis"`
	InputMin  float64  `json:"inputMin"`
	InputMax  float64  `json:"inputMax"`
	OutputMin float64  `json:"outputMin"`
	OutputMax float64  `json:"outputMax"`
	Threshold *float64 `json:"threshold,omitempty"`
	ColorLow  string   `json:"colorLow,omitempty"`
	ColorHigh string   `json:"colorHigh,omitempty"`
	Smooth    *float64 `json:"smooth,omitempty"`
	Enabled   bool     `json:"enabled"`
}
type PageSceneLayer struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Nodes     []string `json:"nodes"`
	Visible   bool     `json:"visible"`
	Opacity   float64  `json:"opacity"`
	Wireframe bool     `json:"wireframe"`
	Color     string   `json:"color,omitempty"`
}
type PageSceneConfig struct {
	Background       string             `json:"background"`
	ShowGrid         bool               `json:"showGrid"`
	Quality          string             `json:"quality"`
	Layers           []PageSceneLayer   `json:"layers"`
	Mappings         []PageSceneMapping `json:"mappings"`
	SampleTimeField  string             `json:"sampleTimeField,omitempty"`
	SampleAssetField string             `json:"sampleAssetField,omitempty"`
}

func (c PageSceneConfig) Check() error {
	if !slices.Contains([]string{"light", "dark"}, c.Background) || !slices.Contains([]string{"performance", "balanced", "high"}, c.Quality) || c.Layers == nil || c.Mappings == nil || len(c.Layers) > 32 || len(c.Mappings) > 64 {
		return fmt.Errorf("scene needs bounded original layers and mappings")
	}
	ids := map[string]bool{}
	drives := map[string]bool{}
	name := func(s string, n int) bool { return strings.TrimSpace(s) != "" && len(s) <= n }
	id := func(s string) bool {
		if !name(s, 128) || ids[s] {
			return false
		}
		ids[s] = true
		return true
	}
	color := func(s string) bool { return s == "" || regexp.MustCompile(`^#[0-9a-fA-F]{6}$`).MatchString(s) }
	for _, l := range c.Layers {
		if !id(l.ID) || !name(l.Name, 256) || len(l.Nodes) == 0 || len(l.Nodes) > 512 || !finite(l.Opacity) || l.Opacity < 0 || l.Opacity > 1 || !color(l.Color) {
			return fmt.Errorf("scene layer is invalid")
		}
		nodes := map[string]bool{}
		for _, n := range l.Nodes {
			if !name(n, 256) || nodes[n] {
				return fmt.Errorf("scene layer node is invalid")
			}
			nodes[n] = true
		}
	}
	for _, m := range c.Mappings {
		axis := m.Axis
		if m.Mode == "color" || m.Mode == "visibility" {
			axis = ""
		}
		drive := m.Node + "\x00" + m.Mode + "\x00" + axis
		if !id(m.ID) || !name(m.Node, 256) || !name(m.Field, 256) || !slices.Contains([]string{"asset", "sample"}, m.Source) || !slices.Contains([]string{"rotation", "position", "scale", "color", "visibility"}, m.Mode) || !slices.Contains([]string{"x", "y", "z"}, m.Axis) || m.InputMin >= m.InputMax || drives[drive] || !color(m.ColorLow) || !color(m.ColorHigh) {
			return fmt.Errorf("scene mapping is invalid")
		}
		for _, v := range []float64{m.InputMin, m.InputMax, m.OutputMin, m.OutputMax} {
			if !finite(v) || math.Abs(v) > 1e9 {
				return fmt.Errorf("scene mapping range is invalid")
			}
		}
		if m.Threshold != nil && !finite(*m.Threshold) || m.Smooth != nil && (!finite(*m.Smooth) || *m.Smooth < .02 || *m.Smooth > 1.5) {
			return fmt.Errorf("scene mapping smoothing or threshold is invalid")
		}
		drives[drive] = true
	}
	return nil
}
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
func (d *PageDocument) checkSpatial(s Section) error {
	if s.Widget != "record-map" && s.Map != nil || s.Widget != "scene-3d" && (s.Scene != nil || s.SceneSampleCollectionVariable != "" || s.SceneSampleVariable != "" || s.ScenePartVariable != "") {
		return fmt.Errorf("spatial configuration needs its widget")
	}
	if !slices.Contains([]string{"record-map", "image-annotation", "scene-3d"}, s.Widget) {
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, spatialProfile) {
		return fmt.Errorf("spatial widget needs its renderer profile")
	}
	if s.Widget == "record-map" {
		v := d.Variables[s.CollectionVariable]
		var q PageQuery
		if v.Source != nil {
			q = d.Queries[v.Source.Query]
		}
		if s.Map == nil || v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(q.Sort) == 0 || q.Limit < 1 || q.Limit > 100 || len(s.Fields) > 0 || len(s.Actions) > 0 || s.RecordVariable != "" || s.SelectionVariable != "" || s.ParentSelection != "" || s.Relation != "" {
			return fmt.Errorf("map needs a bounded ordered original window")
		}
		return nil
	}
	if s.Widget == "image-annotation" {
		return nil
	} // original collaboration record/file checks also apply
	if s.Scene == nil {
		return fmt.Errorf("scene configuration is missing")
	}
	if err := s.Scene.Check(); err != nil {
		return err
	}
	record := d.Variables[s.RecordVariable]
	owned := func(id string, typ string, mode string) bool {
		v := d.Variables[id]
		return v.Type == typ && v.Mode == mode && v.Scope == record.Scope && v.Owner == record.Owner
	}
	if s.ScenePartVariable != "" && (!owned(s.ScenePartVariable, "string", "state") || s.ScenePartVariable == s.FileVariable) {
		return fmt.Errorf("scene part output needs independent same-owner string state")
	}
	sampleNeeded := slices.ContainsFunc(s.Scene.Mappings, func(m PageSceneMapping) bool { return m.Source == "sample" })
	if sampleNeeded && ((s.SceneSampleVariable == "" && s.SceneSampleCollectionVariable == "") || s.Scene.SampleAssetField == "") {
		return fmt.Errorf("sample mapping needs original sample record and asset reference")
	}
	if s.SceneSampleVariable != "" {
		v := d.Variables[s.SceneSampleVariable]
		if !owned(s.SceneSampleVariable, "record", "resource") || v.Source == nil || v.Source.Kind != "record" || s.SceneSampleVariable == s.RecordVariable {
			return fmt.Errorf("scene sample needs an independent original confirmed resource")
		}
	}
	if s.SceneSampleCollectionVariable != "" {
		v := d.Variables[s.SceneSampleCollectionVariable]
		var q PageQuery
		if v.Source != nil {
			q = d.Queries[v.Source.Query]
		}
		if s.SceneSampleVariable != "" || s.Scene.SampleTimeField == "" || !owned(s.SceneSampleCollectionVariable, "object-set", "resource") || v.Source == nil || v.Source.Kind != "plan" || q.For == nil || q.For.Variable != s.RecordVariable || q.Limit != 1 {
			return fmt.Errorf("scene latest sample needs a one-row original context query")
		}
	}
	if s.SceneSampleVariable == "" && s.SceneSampleCollectionVariable == "" && s.Scene.SampleAssetField != "" {
		return fmt.Errorf("sample reference needs its record resource")
	}
	return nil
}
func (s Section) CheckMap(info EntityInfo) error {
	if s.Widget != "record-map" {
		return nil
	}
	if s.Map == nil {
		return fmt.Errorf("map fields are missing")
	}
	for _, name := range []string{s.Map.LatitudeField, s.Map.LongitudeField} {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("map coordinates need visible original numeric fields")
		}
	}
	if s.Map.ColorField != "" {
		f, ok := info.Field(s.Map.ColorField)
		if !ok || !slices.Contains([]string{"text", "choice"}, f.Type) {
			return fmt.Errorf("map color needs visible text or choice")
		}
	}
	if s.Map.LabelField != "id" {
		f, ok := info.Field(s.Map.LabelField)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) {
			return fmt.Errorf("map title needs visible original text or ID")
		}
	}
	return nil
}
func (p Page) CheckSceneBinding(s Section, lookup func(AssetRef) (EntityInfo, bool)) error {
	if s.Widget != "scene-3d" {
		return nil
	}
	if s.Scene == nil {
		return fmt.Errorf("scene is missing")
	}
	asset := p.RecordResourceObject(s.RecordVariable)
	info, ok := lookup(asset)
	if !ok {
		return fmt.Errorf("scene asset is unavailable")
	}
	sample := p.RecordResourceObject(s.SceneSampleVariable)
	if s.SceneSampleCollectionVariable != "" {
		v := p.Document.Variables[s.SceneSampleCollectionVariable]
		if v.Source != nil {
			sample = p.Document.Queries[v.Source.Query].Object
		}
	}
	sampleInfo, sampleOK := lookup(sample)
	if s.SceneSampleVariable != "" || s.SceneSampleCollectionVariable != "" {
		field, found := sampleInfo.Field(s.Scene.SampleAssetField)
		if !sampleOK || !found || field.Type != "reference" || field.Ref != asset.Name {
			return fmt.Errorf("scene sample reference differs from the original asset")
		}
	}
	if s.SceneSampleCollectionVariable != "" {
		field, ok := sampleInfo.Field(s.Scene.SampleTimeField)
		q := p.Document.Queries[p.Document.Variables[s.SceneSampleCollectionVariable].Source.Query]
		if !ok || field.Type != "datetime" || len(q.Sort) != 2 || q.Sort[0] != "-"+field.Name || q.Sort[1] != "id" {
			return fmt.Errorf("scene latest sample needs original descending event time and stable ID")
		}
	}
	for _, m := range s.Scene.Mappings {
		source := info
		if m.Source == "sample" {
			source = sampleInfo
		}
		f, ok := source.Field(m.Field)
		if !ok || !slices.Contains([]string{"integer", "decimal"}, f.Type) {
			return fmt.Errorf("scene mapping needs visible original numeric fields")
		}
	}
	return nil
}
func (s Section) SpatialVariables() []string {
	if s.Widget == "scene-3d" {
		return []string{s.RecordVariable, s.FileVariable, s.SceneSampleCollectionVariable, s.SceneSampleVariable, s.ScenePartVariable}
	}
	return nil
}
