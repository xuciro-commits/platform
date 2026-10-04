package platform

import (
	"fmt"
	"slices"
)

func (s Section) CheckKanban(info EntityInfo) error {
	if s.Widget != "kanban" {
		return nil
	}
	l := info.Lifecycle
	if l == nil || len(l.States) < 1 || len(l.States) > 32 || s.CollectionVariable == "" {
		return fmt.Errorf("kanban needs an original bounded lifecycle and plan window")
	}
	status, ok := info.Field(l.Field)
	if !ok || !slices.Contains([]string{"text", "choice"}, status.Type) {
		return fmt.Errorf("kanban status field is unavailable")
	}
	if s.CardLabel != "id" {
		label, ok := info.Field(s.CardLabel)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference"}, label.Type) {
			return fmt.Errorf("kanban card title needs a visible scalar text field")
		}
	}
	if len(s.Fields) > 4 {
		return fmt.Errorf("kanban has at most four summary fields")
	}
	for _, name := range s.Fields {
		f, ok := info.Field(name)
		if !ok || !slices.Contains([]string{"text", "longtext", "choice", "reference", "integer", "decimal", "money", "date", "datetime", "boolean"}, f.Type) {
			return fmt.Errorf("kanban summary field is unavailable or not scalar")
		}
	}
	for _, ref := range s.Actions {
		if ref.App != info.App || ref.Kind != AssetAction || !slices.ContainsFunc(l.Transitions, func(t TransitionInfo) bool { return t.Schema == ref.Name && (len(t.To) == 1 || t.ToInput != "") }) {
			return fmt.Errorf("kanban moves need declared fixed or parameterized destination transitions")
		}
	}
	return nil
}

func (p Page) CheckKanban(s Section, info EntityInfo) error {
	if err := s.CheckKanban(info); err != nil {
		return err
	}
	if s.Widget != "kanban" {
		return nil
	}
	for _, a := range s.Actions {
		for _, move := range info.Lifecycle.Transitions {
			if move.Schema == a.Name && move.ToInput != "" && (p.Document == nil || !PageUIProfileSupports(p.Document.UIProfile, pageWidgets.Runtime.Kanban.DynamicUIProfile)) {
				return fmt.Errorf("parameterized kanban moves need their admitted profile")
			}
		}
	}
	return nil
}
