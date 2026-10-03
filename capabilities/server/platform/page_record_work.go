package platform

import (
	"fmt"
	"slices"
)

type PageActionParameter struct {
	Parameter string `json:"parameter"`
	Field     string `json:"field"`
}
type PageActionTable struct {
	Parameters []PageActionParameter `json:"parameters"`
}

func (d *PageDocument) checkRecordWork(s Section) error {
	if s.Widget != "action-table" && s.ActionTable != nil {
		return fmt.Errorf("action table configuration needs its widget")
	}
	if s.Widget != "notepad" && s.NotepadVariable != "" {
		return fmt.Errorf("notepad variable needs its widget")
	}
	if s.Widget == "notepad" {
		v := d.Variables[s.NotepadVariable]
		if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordWork.RequiredUIProfile) || v.Type != "string" || v.Mode != "state" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || s.CollectionVariable != "" || s.RecordVariable != "" || s.Selection != "" || len(s.Fields) > 0 || len(s.Actions) > 0 {
			return fmt.Errorf("notepad needs its original same-owner text state")
		}
		return nil
	}
	if s.Widget != "action-table" {
		return nil
	}
	c, v := s.ActionTable, d.Variables[s.CollectionVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.RecordWork.RequiredUIProfile) || c == nil || len(c.Parameters) < 1 || len(c.Parameters) > pageWidgets.Runtime.RecordWork.MaxParameters || len(s.Actions) != 1 || s.Actions[0].Check() != nil || s.Actions[0].Kind != AssetAction || v.Type != "object-set" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "plan" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || s.Selection != "" || s.RecordVariable != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.InlineEdit != nil || s.Operation != nil || s.Function != nil || len(s.Inputs) > 0 || len(s.Fields) > 0 || s.ParentSelection != "" || s.Relation != "" || s.Query != (AssetRef{}) || s.FilterVariable != "" {
		return fmt.Errorf("action table needs its original read-only target window, action and parameter map")
	}
	q, ok := d.Queries[v.Source.Query]
	if !ok || q.Owner != v.Owner || q.ItemOwner != "" || q.Limit != pageWidgets.Runtime.RecordWork.MaxActionRows || q.Offset != 0 || !slices.Equal(q.Sort, []string{"id"}) {
		return fmt.Errorf("action table needs its original 50-record ID window")
	}
	seen := map[string]bool{}
	for _, p := range c.Parameters {
		if !pageNodeID.MatchString(p.Parameter) || !pageNodeID.MatchString(p.Field) || slices.Contains([]string{"id", "revision", "created", "changed", "archived", "__proto__", "constructor", "prototype"}, p.Parameter) || seen[p.Parameter] {
			return fmt.Errorf("action parameter must not replace original record identity")
		}
		seen[p.Parameter] = true
	}
	return nil
}
func ActionParameterCompatible(f FieldInfo, p Field) bool {
	if p.From != "" {
		return false
	}
	switch p.Type {
	case "string":
		if p.Ref != "" {
			return f.Type == "reference" && f.Ref == p.Ref
		}
		return slices.Contains([]string{"text", "longtext", "choice"}, f.Type)
	case "number":
		return f.Type == "decimal" || f.Type == "integer"
	case "integer":
		return f.Type == "integer"
	case "boolean", "date", "datetime":
		return f.Type == p.Type
	}
	return false
}
func (s Section) CheckActionTable(info EntityInfo, action Action) error {
	if s.Widget != "action-table" {
		return nil
	}
	if s.ActionTable == nil || len(s.Actions) != 1 || s.Actions[0].App != info.App || s.Actions[0].Name != action.Schema || action.Target != info.Type || action.New || action.Automation || len(action.Payload) != len(s.ActionTable.Parameters) {
		return fmt.Errorf("action table needs one original non-creating action and all declared parameters")
	}
	for _, m := range s.ActionTable.Parameters {
		f, ok := info.Field(m.Field)
		p := slices.IndexFunc(action.Payload, func(p Field) bool { return p.Name == m.Parameter })
		if !ok || p < 0 || !ActionParameterCompatible(f, action.Payload[p]) {
			return fmt.Errorf("action table parameter and original readable field are incompatible")
		}
	}
	return nil
}
func (p Page) CheckRecordWorkQuery(id string, named *Definition) error {
	if p.Document == nil || named == nil || named.Query == nil {
		return nil
	}
	for _, s := range p.Sections {
		v := p.Document.Variables[s.CollectionVariable]
		if v.Source == nil || v.Source.Query != id {
			continue
		}
		limit := 0
		if s.Widget == "action-table" {
			limit = pageWidgets.Runtime.RecordWork.MaxActionRows
		}
		if s.RecordList != nil && s.RecordList.Layout == "tiles" {
			limit = pageWidgets.Runtime.RecordWork.MaxTiles
		}
		q := named.Query
		if limit > 0 && (len(q.Sort) > 0 && !slices.Equal(q.Sort, []string{"id"}) || q.Limit > 0 && q.Limit < limit) {
			return fmt.Errorf("record work needs its original retained query capacity and ID order")
		}
	}
	return nil
}
