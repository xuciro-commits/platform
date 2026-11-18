package authoring

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	ontology "platformserver/services/ontology"
	"platformserver/services/resources/publication"
)

func (o *Owner) PageEntity() platform.RecordType {
	return platform.RecordType{Type: protocol.PageType, Title: "Page", Plural: "Pages", Model: protocol.Page{}, Display: "title",
		Description: "A list and detail page this organisation composed over an object it may read: the fields it shows and the actions it offers.",
		Validate: func(c platform.Caller, record any) *platform.Error {
			if c.Replaying {
				return nil
			}
			if err := o.validatePageDocument(record.(*protocol.Page), o.env.Entity); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{ontology.BuilderRole}, Capability: "pages"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being composed; nobody can open it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Open to the members who may read its object."}},
			Actions: []platform.ActionType{{
				Name:        "publish",
				Title:       "Publish",
				Roles:       []string{ontology.BuilderRole},
				Capability:  "pages",
				Parameters:  []platform.Field{},
				Description: "Put the page in the workspace as it stands. Published again, it takes its new fields and actions.",
				Criteria:    platform.ActionCriteria{State: &platform.FieldCondition{Field: "state", In: []string{"draft", "published"}}},
				Rules:       []platform.ActionRule{{Function: o.publishPage}, {Edit: &platform.PropertyRule{Field: "state", Allowed: []string{"published"}}}},
			}}}}
}

func (o *Owner) PageDraftEntity() platform.RecordType {
	entity := o.PageEntity()
	entity.Validate = func(c platform.Caller, record any) *platform.Error {
		page := record.(*protocol.Page)
		if c.Replaying {
			return nil
		}
		lookup, err := o.env.DraftObjectLookup(c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		if err := o.validatePageDocument(page, lookup, c.Rules()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		return nil
	}
	return entity
}

func (o *Owner) CheckPageObject(c platform.Caller, object string) (bool, error) {
	if o.env.Host() != nil && o.env.Host().Declares(object) {
		return true, nil
	}
	lookup, err := o.env.DraftObjectLookup(c)
	if err != nil {
		return false, err
	}
	_, known := lookup(object)
	return known, nil
}

func (o *Owner) publishPage(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *platform.Error {
	var page *protocol.Page
	switch value := record.(type) {
	case *protocol.Page:
		page = value
	default:
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if host := o.env.Host(); host != nil {
			if err := host.CheckPageModel(c, Descriptor(*page, o.env.AppID(), o.env.Version())); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
		}
	}
	next := *page
	if err := o.RetainPagePublication(&next); err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if c.Staging() {
		if err := o.checkPage(*page); err != nil {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
		if err := o.env.Host().ValidateInstallPage(Descriptor(*page, o.env.AppID(), o.env.Version())); err != nil {
			return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	} else if err := o.installPage(c, *page); err != nil {
		return err
	}
	*page = next
	return nil
}

func (o *Owner) installPage(c platform.Caller, p protocol.Page) *platform.Error {
	if err := o.checkPage(p); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := o.env.Host().InstallPage(c, Descriptor(p, o.env.AppID(), o.env.Version())); err != nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return nil
}

func (o *Owner) validatePageDocument(page *protocol.Page, lookup func(string) (platform.RecordTypeInfo, bool), engines ...platform.RuleEngine) error {
	if err := o.normalizePageModel(page, lookup); err != nil {
		return err
	}
	d := Descriptor(*page, o.env.AppID(), o.env.Version())
	if err := pageowner.BindRuleTypes(d, lookup); err != nil {
		return err
	}
	if err := pageowner.CheckVariables(d.InputSchema, engines...); err != nil {
		return err
	}
	return pageowner.CheckSafety(Descriptor(*page, o.env.AppID(), o.env.Version()))
}

func (o *Owner) checkPage(p protocol.Page) error {
	if !publication.ValidName(p.Name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", p.Name)
	}
	if err := pageowner.CheckSafety(Descriptor(p, o.env.AppID(), o.env.Version())); err != nil {
		return err
	}
	if o.env.Host() != nil {
		if _, ok := o.env.Entity(p.Object); !ok {
			return fmt.Errorf("this tenant has no object %q", p.Object)
		}
	}
	return nil
}

func (o *Owner) normalizePageModel(p *protocol.Page, lookup func(string) (platform.RecordTypeInfo, bool)) error {
	d := Descriptor(*p, o.env.AppID(), o.env.Version())
	if err := pageowner.NormalizeModelBindings(&d, lookup); err != nil {
		return err
	}
	if err := pageowner.CheckQueryFilters(d, lookup); err != nil {
		return err
	}
	for i, binding := range d.DataBindings {
		if binding.Query != nil && binding.Query.SourceVersion == "" {
			if host := o.env.Host(); host != nil {
				if source, known := host.ModelQueryBinding(binding.Query.Ref); known {
					binding.Query = &source
				}
			}
		}
		for _, model := range pageowner.ModelBindings(binding) {
			if model.Set != nil && model.Set.Query != nil && model.Set.Query.SourceVersion == "" {
				if host := o.env.Host(); host != nil {
					if source, known := host.ModelQueryBinding(model.Set.Query.Ref); known {
						*model.Set.Query = source
					}
				}
			}
		}
		d.DataBindings[i] = binding
	}
	p.Format = 4
	p.DataBindings = d.DataBindings
	p.ActionTriggers = d.ActionTriggers
	p.InputSchema = d.InputSchema
	p.ScopeVisibility = d.ScopeVisibility
	p.WidgetSpec = d.WidgetSpec
	return nil
}
