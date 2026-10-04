package platform

import (
	"fmt"
	"slices"
)

type PageRecordList struct {
	Layout string `json:"layout"`
}

func (d *PageDocument) checkRecordList(s Section) error {
	if s.Widget != "record-list" {
		if s.RecordList != nil {
			return fmt.Errorf("record list configuration needs its widget")
		}
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordList.RequiredUIProfile) || s.RecordList == nil || !slices.Contains(pageWidgets.Runtime.RecordList.Layouts, s.RecordList.Layout) || s.CollectionVariable == "" || len(s.Fields) > pageWidgets.Runtime.RecordList.MaxFields || len(s.Actions) > 0 {
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
	if s.CardLabel != "id" {
		f, ok := info.Field(s.CardLabel)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, f.Type) {
			return fmt.Errorf("record list title needs a visible text field")
		}
	}
	seen := map[string]bool{}
	for _, name := range s.Fields {
		f, ok := info.Field(name)
		if !ok || seen[name] || !slices.Contains([]string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"}, f.Type) {
			return fmt.Errorf("record list summary needs unique visible scalar fields")
		}
		seen[name] = true
	}
	return nil
}
