package platform

import (
	"fmt"
	"slices"
)

func (s Section) CheckInlineAction(info EntityInfo, action Action) error {
	if s.Widget != "inline-action" {
		return nil
	}
	if len(s.Actions) != 1 || s.Actions[0].App != info.App || s.Actions[0].Kind != AssetAction || s.Actions[0].Name != action.Schema || action.Target != info.Type || action.New {
		return fmt.Errorf("inline action needs one original non-creating action on its bound object")
	}
	for _, m := range s.ActionDefaults {
		f, ok := info.Field(m.Field)
		p := slices.IndexFunc(action.Payload, func(p Field) bool { return p.Name == m.Parameter })
		if !ok || p < 0 || !ActionParameterCompatible(f, action.Payload[p]) {
			return fmt.Errorf("action default parameter and original readable field are incompatible")
		}
	}
	return nil
}
