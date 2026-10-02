package platform

import (
	"fmt"
	"slices"
)

func (d *PageDocument) checkTableEdit(s Section) error {
	if s.InlineEdit == nil {
		return nil
	}
	edit := s.InlineEdit
	if s.Widget != "table" || !PageUIProfileSupports(d.UIProfile, "platform.page.v2.31") || edit.Action.Check() != nil || edit.Action.Kind != AssetAction || len(edit.Fields) == 0 || len(edit.Fields) > pageWidgets.Runtime.TableEditing.MaxFields {
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
