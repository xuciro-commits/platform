package platform

import "fmt"

func (d *PageDocument) checkTreemap(s Section) error {
	if s.Widget != "treemap" && s.Widget != "tag-counts" {
		if s.GroupValueVariable != "" || s.GroupSetVariable != "" {
			return fmt.Errorf("group output bindings need their count presentation")
		}
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidget(s.Widget).RequiredUIProfile) || s.GroupValueVariable != "" && s.GroupSetVariable != "" || s.Widget == "tag-counts" && s.GroupSetVariable != "" {
		return fmt.Errorf("count presentation needs its profile and an exclusive supported group state type")
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
