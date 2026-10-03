package platform

import (
	"fmt"
	"slices"
)

func collaborationWidget(widget string) bool {
	return slices.Contains([]string{"record-comments", "record-uploader", "media-preview", "pdf-viewer"}, widget)
}

func (d *PageDocument) checkCollaboration(s Section) error {
	if !collaborationWidget(s.Widget) {
		if s.CommentDraftVariable != "" || s.FileVariable != "" || s.PdfPageVariable != "" {
			return fmt.Errorf("collaboration bindings need their widget")
		}
		return nil
	}
	v := d.Variables[s.RecordVariable]
	if !PageUIProfileSupports(d.UIProfile, pageWidgets.Runtime.Collaboration.RequiredUIProfile) || v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || !slices.Contains([]string{"page", "overlay"}, v.Scope) || len(s.Fields) > 0 || len(s.Actions) > 0 || s.CollectionVariable != "" || s.Selection != "" || s.SelectionVariable != "" || s.SelectionSetVariable != "" || s.RecordSetVariable != "" || s.Relation != "" || s.ParentSelection != "" || s.Query.Name != "" || s.InlineEdit != nil {
		return fmt.Errorf("collaboration needs its original scoped record resource without independent business bindings")
	}
	var scalar, mode string
	switch s.Widget {
	case "record-comments":
		if s.FileVariable != "" || s.PdfPageVariable != "" {
			return fmt.Errorf("comments cannot bind media state")
		}
		scalar, mode = s.CommentDraftVariable, "state"
	case "record-uploader":
		if s.CommentDraftVariable != "" || s.PdfPageVariable != "" {
			return fmt.Errorf("uploader cannot bind comment or PDF state")
		}
		scalar, mode = s.FileVariable, "state"
	case "media-preview", "pdf-viewer":
		if s.CommentDraftVariable != "" || s.Widget == "media-preview" && s.PdfPageVariable != "" {
			return fmt.Errorf("media preview bindings are incompatible")
		}
		scalar, mode = s.FileVariable, "read"
	}
	binding := d.Variables[scalar]
	if scalar == "" || binding.Type != "string" || binding.Scope != v.Scope || binding.Owner != v.Owner || binding.Mode != "state" && !(mode == "read" && binding.Mode == "constant") {
		return fmt.Errorf("collaboration scalar needs its original record owner and supported string binding")
	}
	if s.Widget == "pdf-viewer" {
		page := d.Variables[s.PdfPageVariable]
		if s.PdfPageVariable == "" || s.PdfPageVariable == s.FileVariable || page.Type != "string" || page.Mode != "state" || page.Scope != v.Scope || page.Owner != v.Owner {
			return fmt.Errorf("PDF page needs independent original same-owner string state")
		}
	}
	return nil
}

func (d *PageDocument) checkCollaborationOwners(sections []Section) error {
	owners := map[string]string{}
	for _, s := range sections {
		if !collaborationWidget(s.Widget) {
			continue
		}
		record := d.Variables[s.RecordVariable]
		if record.Source == nil {
			continue // the section's original-resource check supplies the diagnostic
		}
		for _, scalar := range []string{s.CommentDraftVariable, s.FileVariable, s.PdfPageVariable} {
			if scalar == "" {
				continue
			}
			if producer, used := owners[scalar]; used && producer != record.Source.Section {
				return fmt.Errorf("shared collaboration scalar %s needs one original record producer", scalar)
			}
			owners[scalar] = record.Source.Section
		}
	}
	return nil
}

// CollaborationDependencies names existing platform service assets. A page
// binds those fixed services without granting their actions to a reader.
func (s Section) CollaborationDependencies() []AssetRef {
	switch s.Widget {
	case "record-comments":
		return []AssetRef{{App: "relations", Kind: AssetObject, Name: "platform.comment"}, {App: "relations", Kind: AssetAction, Name: "platform.comment.add"}}
	case "record-uploader":
		return []AssetRef{{App: "files", Kind: AssetObject, Name: "files.file"}, {App: "files", Kind: AssetAction, Name: "files.file.attach"}}
	case "media-preview", "pdf-viewer":
		return []AssetRef{{App: "files", Kind: AssetObject, Name: "files.file"}}
	}
	return nil
}

func (s Section) CheckCollaborationServices(object func(AssetRef) (EntityInfo, bool), action func(AssetRef) (Action, bool)) error {
	dependencies := s.CollaborationDependencies()
	for _, ref := range dependencies {
		if ref.Kind == AssetObject {
			info, ok := object(ref)
			if !ok || info.Type != ref.Name || info.App != ref.App {
				return fmt.Errorf("collaboration service object %s is unavailable", ref)
			}
		} else {
			a, ok := action(ref)
			if !ok || a.Schema != ref.Name || a.Target != dependencies[0].Name {
				return fmt.Errorf("collaboration service action %s is unavailable", ref)
			}
		}
	}
	return nil
}

// RecordResourceObject resolves a local record resource through its compatible
// original producer; it retains the complete object identity, including owner.
func (p Page) RecordResourceObject(variable string) AssetRef {
	if p.Document == nil {
		return AssetRef{}
	}
	v := p.Document.Variables[variable]
	if v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" {
		return AssetRef{}
	}
	for _, s := range p.Sections {
		if s.ID != v.Source.Section {
			continue
		}
		compatible := false
		for _, resource := range pageWidgets.Runtime.Resources {
			if resource.Kind == "record" && (resource.Widget == s.Widget || slices.Contains(resource.Widgets, s.Widget)) {
				compatible = true
				break
			}
		}
		if !compatible {
			return AssetRef{}
		}
		if s.Object.Name != "" {
			return s.Object
		}
		return p.Object
	}
	return AssetRef{}
}

func (p Page) CheckCollaborationBinding(s Section) error {
	if !collaborationWidget(s.Widget) {
		return nil
	}
	object := s.Object
	if object.Name == "" {
		object = p.Object
	}
	if original := p.RecordResourceObject(s.RecordVariable); original.Name == "" || original != object {
		return fmt.Errorf("collaboration object differs from its original record producer")
	}
	return nil
}
