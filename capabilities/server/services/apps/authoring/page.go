package authoring

import (
	"encoding/json"
	"fmt"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	ontology "platformserver/services/ontology"
)

const (
	PageType      = "build.page"
	SchemaRelease = PageType + ".publish"
)

type Page struct {
	platform.Record
	Format          int                          `json:"format,omitempty"`
	DataBindings    []platform.PageDataBinding   `json:"dataBindings,omitempty" type:"json"`
	ActionTriggers  []platform.PageActionTrigger `json:"actionTriggers,omitempty" type:"json"`
	InputSchema     platform.PageInputSchema     `json:"inputSchema,omitzero" type:"json"`
	ScopeVisibility platform.PageScopeVisibility `json:"scopeVisibility,omitzero" type:"json"`
	WidgetSpec      platform.PageWidgetSpec      `json:"widgetSpec,omitzero" type:"json"`
	Name            string                       `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visits"`
	Title           string                       `json:"title" field:"required,search" title:"What people call it" example:"Visits"`
	Description     string                       `json:"description,omitempty" type:"longtext" help:"What people do on this page"`
	Object          string                       `json:"object" field:"required" title:"Object it shows" help:"An object this tenant has: one it defined, or an app's own" example:"build.visits"`
	State           string                       `json:"state" field:"readonly" choices:"draft,published"`
	Published       string                       `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions        []string                     `json:"versions,omitempty" field:"readonly" type:"lines" title:"Published page contents"`
}

func (o *Owner) PageEntity() platform.Entity {
	return platform.Entity{Type: PageType, Title: "Page", Plural: "Pages", Model: Page{}, Display: "title",
		Description: "A list and detail page this organisation composed over an object it may read: the fields it shows and the actions it offers.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			if err := o.validatePageDocument(record.(*Page), o.env.Entity); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{ontology.BuilderRole}, Capability: "pages"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being composed; nobody can open it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Open to the members who may read its object."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{ontology.BuilderRole}, Capability: "pages", Payload: []platform.Field{},
				Description: "Put the page in the workspace as it stands. Published again, it takes its new fields and actions.",
				Do:          o.publishPage}}}}
}

func (o *Owner) PageDraftEntity() platform.Entity {
	entity := o.PageEntity()
	entity.Validate = func(c platform.Caller, record any) *kernel.Error {
		page := record.(*Page)
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

func (o *Owner) publishPage(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	page, ok := record.(*Page)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if !c.Replaying {
		if owner, ok := o.env.Host().(platform.ModelPageHost); ok {
			if err := owner.CheckPageModel(c, Descriptor(*page, o.env.AppID(), o.env.Version())); err != nil {
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
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
		if err := o.env.Host().ValidateInstallPage(Descriptor(*page, o.env.AppID(), o.env.Version())); err != nil {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
		}
	} else if err := o.installPage(c, *page); err != nil {
		return err
	}
	*page = next
	return nil
}

func (o *Owner) installPage(c platform.Caller, p Page) *kernel.Error {
	if err := o.checkPage(p); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := o.env.Host().InstallPage(c, Descriptor(p, o.env.AppID(), o.env.Version())); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return nil
}

func (o *Owner) validatePageDocument(page *Page, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	if err := o.normalizePageModel(page, lookup); err != nil {
		return err
	}
	d := Descriptor(*page, o.env.AppID(), o.env.Version())
	if err := d.BindRuleTypes(lookup); err != nil {
		return err
	}
	if err := d.InputSchema.CheckVariables(engines...); err != nil {
		return err
	}
	return Descriptor(*page, o.env.AppID(), o.env.Version()).CheckSafety()
}

func (o *Owner) checkPage(p Page) error {
	if !ontology.Named(p.Name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", p.Name)
	}
	if err := Descriptor(p, o.env.AppID(), o.env.Version()).CheckSafety(); err != nil {
		return err
	}
	if o.env.Host() != nil {
		if _, ok := o.env.Entity(p.Object); !ok {
			return fmt.Errorf("this tenant has no object %q", p.Object)
		}
	}
	return nil
}

func (o *Owner) normalizePageModel(p *Page, lookup func(string) (platform.EntityInfo, bool)) error {
	d := Descriptor(*p, o.env.AppID(), o.env.Version())
	if err := d.NormalizeModelBindings(lookup); err != nil {
		return err
	}
	if err := d.CheckQueryFilters(lookup); err != nil {
		return err
	}
	for i, binding := range d.DataBindings {
		if binding.Query != nil && binding.Query.SourceVersion == "" {
			if owner, ok := o.env.Host().(platform.ModelPageHost); ok {
				if source, known := owner.ModelQueryBinding(binding.Query.Ref); known {
					binding.Query = &source
				}
			}
		}
		for _, model := range binding.ModelBindings() {
			if model.Set != nil && model.Set.Query != nil && model.Set.Query.SourceVersion == "" {
				if owner, ok := o.env.Host().(platform.ModelPageHost); ok {
					if source, known := owner.ModelQueryBinding(model.Set.Query.Ref); known {
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
