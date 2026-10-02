package platform

import "fmt"

// PageRecordLink selects one declared incoming reference from the authorized record view.
type PageRecordLink struct {
	Object AssetRef `json:"object"`
	Field  string   `json:"field"`
	Title  string   `json:"title,omitempty"`
}

func (d *PageDocument) checkRecordLinks(s Section) error {
	if s.Widget != "record-links" {
		if len(s.RecordLinks) != 0 {
			return fmt.Errorf("record links need their widget")
		}
		return nil
	}
	limits := pageWidgets.Runtime.RecordLinks
	if !PageUIProfileSupports(d.UIProfile, limits.RequiredUIProfile) || len(s.RecordLinks) == 0 || len(s.RecordLinks) > limits.MaxGroups || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Query.Name != "" || s.Relation != "" {
		return fmt.Errorf("record links need their profile and bounded explicit groups")
	}
	seen := map[string]bool{}
	for _, group := range s.RecordLinks {
		key := group.Object.String() + "/" + group.Field
		if group.Object.Check() != nil || group.Object.Kind != AssetObject || group.Field == "" || seen[key] || len(group.Title) > limits.MaxTitleBytes {
			return fmt.Errorf("record link needs a unique object, reference field and bounded title")
		}
		seen[key] = true
	}
	return nil
}

func (s Section) CheckRecordLinks(parent EntityInfo, lookup func(string) (EntityInfo, bool)) error {
	for _, group := range s.RecordLinks {
		target, ok := lookup(group.Object.Name)
		field, found := target.Field(group.Field)
		if !ok || target.App != group.Object.App || !found || field.Type != "reference" || field.Ref != parent.Type || field.Inverse == "" {
			return fmt.Errorf("record link needs a declared incoming reference on its original object")
		}
	}
	return nil
}
