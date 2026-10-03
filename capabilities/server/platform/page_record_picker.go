package platform

import (
	"fmt"
	"slices"
)

type PageRecordPicker struct {
	LabelField string  `json:"labelField"`
	Label      *string `json:"label,omitempty"`
}

func (d *PageDocument) checkRecordPicker(s Section) error {
	if s.Widget != "record-picker" {
		if s.RecordPicker != nil || s.PickerValueVariable != "" {
			return fmt.Errorf("record picker configuration needs its widget")
		}
		return nil
	}
	v := d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordPicker.RequiredUIProfile) || s.RecordPicker == nil || s.RecordPicker.Label != nil && len(*s.RecordPicker.Label) > 1024 || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.SelectionVariable != "" {
		return fmt.Errorf("record picker needs an original scoped query window and bounded label")
	}
	if s.PickerValueVariable != "" {
		value := d.Variables[s.PickerValueVariable]
		if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordPicker.ValueRequiredUIProfile) || value.Type != "string" || value.Mode != "state" || value.Scope != v.Scope || value.Owner != v.Owner {
			return fmt.Errorf("picker ID output needs its profile and original same-owner text state")
		}
	}
	q := d.Queries[v.Source.Query]
	if q.Offset != 0 || q.Limit != pageWidgets.Runtime.RecordPicker.MaxCandidates || !slices.Equal(q.Sort, []string{"id"}) {
		return fmt.Errorf("record picker needs its fixed ID ordered candidate window")
	}
	return nil
}
func (s Section) CheckRecordPicker(info EntityInfo) error {
	if s.Widget != "record-picker" {
		return nil
	}
	if s.RecordPicker == nil {
		return fmt.Errorf("record picker fields are missing")
	}
	if s.RecordPicker.LabelField != "id" {
		f, ok := info.Field(s.RecordPicker.LabelField)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) {
			return fmt.Errorf("record picker title needs an original visible text field")
		}
	}
	return nil
}
func (p Page) CheckRecordPickerQuery(id string, named *Definition) error {
	if p.Document == nil {
		return nil
	}
	for _, s := range p.Sections {
		v := p.Document.Variables[s.CollectionVariable]
		if s.Widget == "record-picker" && v.Source != nil && v.Source.Query == id && named != nil && named.Query != nil && len(named.Query.Sort) > 0 && !slices.Equal(named.Query.Sort, []string{"id"}) {
			return fmt.Errorf("named query owns an incompatible record picker ordering")
		}
	}
	return nil
}
