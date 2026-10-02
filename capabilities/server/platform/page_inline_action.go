package platform

import "fmt"

func (s Section) CheckInlineAction(info EntityInfo, action Action) error {
	if s.Widget != "inline-action" {
		return nil
	}
	if len(s.Actions) != 1 || s.Actions[0].App != info.App || s.Actions[0].Kind != AssetAction || s.Actions[0].Name != action.Schema || action.Target != info.Type || action.New {
		return fmt.Errorf("inline action needs one original non-creating action on its bound object")
	}
	return nil
}
