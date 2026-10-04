package platform

import (
	"fmt"
	"slices"
)

type PageRecordComparison struct {
	LabelField string `json:"labelField"`
}

func (d *PageDocument) checkRecordComparison(s Section) error {
	if s.Widget != "record-comparison" {
		if s.RecordComparison != nil {
			return fmt.Errorf("record comparison configuration needs its widget")
		}
		return nil
	}
	c := s.RecordComparison
	v := d.Variables[s.RecordSetVariable]
	limits := pageWidgets.Runtime.RecordComparison
	local := v.Mode == "resource" && v.Source != nil && v.Source.Kind == "records" && slices.Contains([]string{"page", "overlay"}, v.Scope)
	shared := PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordSelection.SharedUIProfile) && v.Mode == "shared" && v.Scope == "application" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || c == nil || c.LabelField == "" || len(c.LabelField) > 256 || v.Type != "record-set" || !(local || shared) || len(s.Fields) < limits.MinFields || len(s.Fields) > limits.MaxFields || len(s.Actions) > 0 || s.Selection != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.RecordVariable != "" || s.CollectionVariable != "" || s.ParentSelection != "" || s.Relation != "" || s.Query.Name != "" || s.InlineEdit != nil {
		return fmt.Errorf("record comparison needs its original record-set resource, title and bounded properties")
	}
	seen := map[string]bool{}
	for _, name := range s.Fields {
		if name == "" || len(name) > 256 || seen[name] {
			return fmt.Errorf("record comparison properties need unique original field names")
		}
		seen[name] = true
	}
	return nil
}

func (s Section) CheckRecordComparison(info EntityInfo) error {
	if s.Widget != "record-comparison" {
		return nil
	}
	if s.RecordComparison == nil || len(s.Fields) < pageWidgets.Runtime.RecordComparison.MinFields || len(s.Fields) > pageWidgets.Runtime.RecordComparison.MaxFields {
		return fmt.Errorf("record comparison configuration is missing")
	}
	if s.RecordComparison.LabelField != "id" {
		f, ok := info.Field(s.RecordComparison.LabelField)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) {
			return fmt.Errorf("record comparison title needs a visible original text field or ID")
		}
	}
	seen := map[string]bool{}
	for _, name := range s.Fields {
		f, ok := info.Field(name)
		if !ok || seen[name] || !slices.Contains([]string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"}, f.Type) {
			return fmt.Errorf("record comparison properties need unique visible scalar fields")
		}
		seen[name] = true
	}
	return nil
}

// RecordSetVariableObject preserves the original table's complete object
// identity. A record-set does not declare an independent object requirement.
func (p Page) RecordSetVariableObject(variable string) AssetRef {
	if p.Document == nil {
		return AssetRef{}
	}
	v := p.Document.Variables[variable]
	if v.Type == "record-set" && v.Mode == "shared" && v.Scope == "application" && v.Source != nil && v.Source.Kind == "application" && v.Source.Object != nil {
		return *v.Source.Object
	}
	if v.Type != "record-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "records" {
		return AssetRef{}
	}
	for _, s := range p.Sections {
		if s.ID == v.Source.Section && s.Widget == "table" && s.SelectionSetVariable == variable {
			if s.Object.Name != "" {
				return s.Object
			}
			return p.Object
		}
	}
	return AssetRef{}
}

func (p Page) CheckRecordComparisonBinding(s Section) error {
	if s.Widget != "record-comparison" {
		return nil
	}
	object := s.Object
	if object.Name == "" {
		object = p.Object
	}
	if source := p.RecordSetVariableObject(s.RecordSetVariable); source.Name == "" || source != object {
		return fmt.Errorf("record comparison object differs from its original table producer")
	}
	return nil
}
