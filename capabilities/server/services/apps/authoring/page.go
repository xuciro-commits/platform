package authoring

import (
	"encoding/json"
	"fmt"
	"strings"
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
	List            []string                     `json:"list" title:"Fields in the list" help:"The object's field names, in the order the list shows them" api:"retained"`
	Detail          []string                     `json:"detail" title:"Fields in the detail" help:"The object's field names, in the order one record shows them" api:"retained"`
	Actions         []string                     `json:"actions,omitempty" title:"Actions it offers" help:"Actions of that object, by their schema" example:"build.visits.close" api:"retained"`
	Sections        []Section                    `json:"sections,omitempty" field:"aside" title:"What is on the page" api:"retained"`
	Document        *platform.PageDocument       `json:"document,omitempty" field:"aside" type:"json" title:"Page layout" api:"retained"`
	Selections      []platform.SelectionVariable `json:"selections,omitempty" field:"aside" title:"Record selections" api:"retained"`
	State           string                       `json:"state" field:"readonly" choices:"draft,published"`
	Published       string                       `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions        []string                     `json:"versions,omitempty" field:"readonly" type:"lines" title:"Published page contents"`
}

type InlineEdit struct {
	Action string   `json:"action"`
	Fields []string `json:"fields"`
}

type Section struct {
	LegacyVisual         map[string]json.RawMessage     `json:"-"`
	Model                *platform.ModelBinding         `json:"model,omitempty"`
	ActionDefaults       []platform.PageActionParameter `json:"actionDefaults,omitempty"`
	InlineEdit           *InlineEdit                    `json:"inlineEdit,omitempty"`
	Facets               []platform.PageFacet           `json:"facets,omitempty"`
	FilterSearchVariable string                         `json:"filterSearchVariable,omitempty"`
	ID                   string                         `json:"id,omitempty"`
	ConfigVersion        int                            `json:"configVersion,omitempty"`
	Widget               string                         `json:"widget" field:"required" help:"What it shows; the shared page widget registry defines supported kinds"`
	Title                string                         `json:"title,omitempty"`
	Width                string                         `json:"width,omitempty" choices:"full,half"`
	Object               string                         `json:"object,omitempty" title:"Object" help:"Another object it shows; empty: the page's own"`
	Selection            string                         `json:"selection,omitempty"`
	CollectionVariable   string                         `json:"collectionVariable,omitempty"`
	RecordVariable       string                         `json:"recordVariable,omitempty"`
	SelectionVariable    string                         `json:"selectionVariable,omitempty"`
	FilterVariable       string                         `json:"filterVariable,omitempty"`
	ParentSelection      string                         `json:"parentSelection,omitempty"`
	Relation             string                         `json:"relation,omitempty" title:"Through" help:"For another object's table, chart, metric or form: the relation to the selected parent record"`
	Query                string                         `json:"query,omitempty" title:"Query" help:"For a table: a named query of its object, like build.open-visits"`
	Fields               []string                       `json:"fields,omitempty" help:"For a table or a detail: the fields it shows; a filter: the fields it filters by; a form: the fields it asks for"`
	Actions              []string                       `json:"actions,omitzero" help:"For actions: the schemas it offers; omitted inherits the page, empty forbids section writes"`
	Group                string                         `json:"group,omitempty" title:"Grouped by" help:"For a chart: the field, or <field>:month"`
	ChartVariant         string                         `json:"chartVariant,omitempty"`
	Mark                 string                         `json:"mark,omitempty"`
	ColumnGroup          string                         `json:"columnGroup,omitempty"`
	TimeStart            string                         `json:"timeStart,omitempty" title:"Start time field"`
	TimeEnd              string                         `json:"timeEnd,omitempty" title:"End time field"`
	TimeLabel            string                         `json:"timeLabel,omitempty" title:"Timeline label field"`
	TimeGroup            string                         `json:"timeGroup,omitempty" title:"Timeline resource field"`
	CardLabel            string                         `json:"cardLabel,omitempty" title:"Card title field"`
	Measure              string                         `json:"measure,omitempty" help:"For a chart or a metric: count, sum:<field>, avg:<field>"`
	Text                 string                         `json:"text,omitempty" type:"longtext" help:"For text: the words to show"`
	Function             *platform.FunctionRef          `json:"function,omitempty" title:"Published AI function"`
	Operation            *platform.AssetBinding         `json:"operation,omitempty" title:"Published code function" type:"json"`
	Inputs               map[string]platform.Binding    `json:"inputs,omitempty" type:"json"`
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
	if !c.Replaying && page.Document != nil {
		if err := Descriptor(*page, o.env.AppID(), o.env.Version()).BindRuleTypes(o.env.Entity); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		if err := page.Document.NormalizePageRules(c.Rules()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
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
	if err := d.InputSchema.NormalizePageRules(engines...); err != nil {
		return err
	}
	page.InputSchema = d.InputSchema
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
	d, err := d.UpgradeQueryConditions(lookup)
	if err != nil {
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
	p.Sections = nil
	p.Document = nil
	p.List = nil
	p.Detail = nil
	p.Actions = nil
	p.Selections = nil
	return nil
}

func (s *Section) UnmarshalJSON(raw []byte) error {
	type value Section
	return platform.RetainPageSection(raw, (*value)(s), &s.LegacyVisual)
}

func (s Section) MarshalJSON() ([]byte, error) {
	type value Section
	return platform.MarshalPageSection(value(s), s.LegacyVisual)
}

func descriptorActions(names []string) []platform.AssetRef {
	actions := make([]platform.AssetRef, 0, len(names))
	for _, schema := range names {
		owner, _, _ := strings.Cut(schema, ".")
		actions = append(actions, platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: schema})
	}
	return actions
}
