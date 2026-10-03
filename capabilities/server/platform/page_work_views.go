package platform

import (
	"fmt"
	"slices"
)

func workFeed(widget string) bool { return widget == "approval-inbox" || widget == "notification-feed" }
func (d *PageDocument) checkWorkViews(s Section) error {
	if s.HistoryLimit < 0 || s.HistoryLimit > 0 && s.Widget != "timeline" {
		return fmt.Errorf("history range needs the original timeline")
	}
	if !workFeed(s.Widget) && s.HistoryLimit == 0 {
		return nil
	}
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.WorkViews.RequiredUIProfile) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.RecordSetVariable != "" || s.Selection != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.FilterVariable != "" || s.Relation != "" || s.ParentSelection != "" || s.Query.Name != "" || s.InlineEdit != nil || s.Function != nil || s.Operation != nil || len(s.Inputs) > 0 {
		return fmt.Errorf("work views need their finite profile without independent business bindings")
	}
	if workFeed(s.Widget) {
		if s.Object != (AssetRef{}) || s.RecordVariable != "" || s.HistoryLimit != 0 {
			return fmt.Errorf("caller work feeds cannot bind an independent object or record")
		}
		return nil
	}
	v := d.Variables[s.RecordVariable]
	if s.HistoryLimit > pageWidgets.Runtime.WorkViews.MaxHistoryWindow || v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope) {
		return fmt.Errorf("bounded history needs its original scoped record resource and range")
	}
	return nil
}

func (s Section) WorkViewDependencies() []AssetRef {
	switch s.Widget {
	case "approval-inbox":
		return []AssetRef{{App: "work", Kind: AssetObject, Name: "work.task"}, {App: "work", Kind: AssetObject, Name: "work.approval"}, {App: "work", Kind: AssetAction, Name: "work.approval.approve"}, {App: "work", Kind: AssetAction, Name: "work.approval.reject"}}
	case "notification-feed":
		return []AssetRef{{App: "platform", Kind: AssetAction, Name: "platform.notification.read"}}
	}
	return nil
}
func (s Section) WorkViewRead() (app, name string) {
	switch s.Widget {
	case "approval-inbox":
		return "work", "inbox"
	case "notification-feed":
		return "platform", "notifications"
	}
	return "", ""
}
func (s Section) ServiceDependencies() []AssetRef {
	return append(s.CollaborationDependencies(), s.WorkViewDependencies()...)
}
func (s Section) CheckServices(object func(AssetRef) (EntityInfo, bool), action func(AssetRef) (Action, bool)) error {
	if err := s.CheckCollaborationServices(object, action); err != nil {
		return err
	}
	for _, ref := range s.WorkViewDependencies() {
		if ref.Kind == AssetObject {
			info, ok := object(ref)
			if !ok || info.App != ref.App || info.Type != ref.Name {
				return fmt.Errorf("work service object %s is unavailable", ref)
			}
		} else {
			target := "work.approval"
			if s.Widget == "notification-feed" {
				target = "platform.notification"
			}
			a, ok := action(ref)
			if !ok || a.Schema != ref.Name || a.Target != target {
				return fmt.Errorf("work service action %s is unavailable", ref)
			}
		}
	}
	return nil
}
func (p Page) CheckHistoryBinding(s Section) error {
	if s.HistoryLimit <= 0 {
		return nil
	}
	object := s.Object
	if object.Name == "" {
		object = p.Object
	}
	if original := p.RecordResourceObject(s.RecordVariable); original.Name == "" || original != object {
		return fmt.Errorf("bounded history object differs from its original record producer")
	}
	return nil
}
