package platform

import "fmt"

func (d *PageDocument) checkTreemap(s Section) error {
	if s.Widget != "treemap" {
		if s.GroupValueVariable != "" || s.GroupSetVariable != "" {
			return fmt.Errorf("treemap group bindings need their widget")
		}
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Treemap.RequiredUIProfile) || s.GroupValueVariable != "" && s.GroupSetVariable != "" {
		return fmt.Errorf("treemap needs its profile and an exclusive group state type")
	}
	collection := d.Variables[s.CollectionVariable]
	for _, binding := range []struct{ id, typ string }{{s.GroupValueVariable, "string"}, {s.GroupSetVariable, "string-set"}} {
		if binding.id == "" {
			continue
		}
		v := d.Variables[binding.id]
		if v.Mode != "state" || v.Type != binding.typ || v.Scope != collection.Scope || v.Owner != collection.Owner {
			return fmt.Errorf("treemap group output needs the original collection state owner")
		}
	}
	return nil
}
