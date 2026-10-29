package build

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
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

func (b *Build) pageEntity() platform.Entity {
	return platform.Entity{Type: PageType, Title: "Page", Plural: "Pages", Model: Page{}, Display: "title",
		Description: "A list and detail page this organisation composed over an object it may read: the fields it shows and the actions it offers.",
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if c.Replaying {
				return nil
			}
			if err := b.validatePageDocument(record.(*Page), b.conditionLookup); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return nil
		},
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "pages"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being composed; nobody can open it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Open to the members who may read its object."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{Builder}, Capability: "pages", Payload: []platform.Field{},
				Description: "Put the page in the workspace as it stands. Published again, it takes its new fields and actions.",
				Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
					page, ok := record.(*Page)
					if !ok {
						return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
					}
					if !c.Replaying {
						if owner, ok := b.host.(platform.ModelPageHost); ok {
							if err := owner.CheckPageModel(c, descriptor(*page)); err != nil {
								return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
							}
						}
					}
					if !c.Replaying && page.Document != nil {
						if err := descriptor(*page).BindRuleTypes(b.conditionLookup); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
						if err := page.Document.NormalizePageRules(c.Rules()); err != nil {
							return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
						}
					}
					next := *page
					if err := retainPagePublication(&next); err != nil {
						return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
					}
					if c.Staging() {
						if err := b.checkPage(*page); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
						if err := b.host.ValidateInstallPage(descriptor(*page)); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
					} else if err := b.release(c, *page); err != nil {
						return err
					}
					*page = next
					return nil
				}}}}}
}

func (b *Build) pageDraftEntity() platform.Entity {
	entity := b.pageEntity()
	entity.Validate = func(c platform.Caller, record any) *kernel.Error {
		page := record.(*Page)
		if c.Replaying {
			return nil
		}
		objects, err := readDefinitionInventory[Object](c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		models := map[string]platform.EntityInfo{}
		for _, object := range objects {
			if object.Archived {
				continue
			}
			info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
			if err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			models[info.Type] = info
		}
		lookup := func(typ string) (platform.EntityInfo, bool) {
			if info, saved := models[typ]; saved {
				return info, true
			}
			return b.conditionLookup(typ)
		}
		if err := b.validatePageDocument(page, lookup, c.Rules()); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		return nil
	}
	return entity
}

func (b *Build) validatePageDocument(page *Page, lookup func(string) (platform.EntityInfo, bool), engines ...platform.RuleEngine) error {
	if err := b.normalizePageModel(page, lookup); err != nil {
		return err
	}
	d := descriptor(*page)
	if err := d.BindRuleTypes(lookup); err != nil {
		return err
	}
	if err := d.InputSchema.NormalizePageRules(engines...); err != nil {
		return err
	}
	page.InputSchema = d.InputSchema
	return nil
}

func (b *Build) release(c platform.Caller, p Page) *kernel.Error {
	if err := b.checkPage(p); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.host.InstallPage(c, descriptor(p)); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return nil
}

func descriptor(p Page) platform.Page {
	if p.Format == 3 {
		owner, _, _ := strings.Cut(p.Object, ".")
		value := platform.Page{Format: 3, Name: p.Name, Title: p.Title, Description: p.Description, Object: platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: p.Object}, DataBindings: p.DataBindings, ActionTriggers: p.ActionTriggers, InputSchema: p.InputSchema, ScopeVisibility: p.ScopeVisibility, WidgetSpec: p.WidgetSpec}
		raw, _ := json.Marshal(value)
		_ = json.Unmarshal(raw, &value)
		return value
	}
	app, _, _ := strings.Cut(p.Object, ".")
	actions := []platform.AssetRef{}
	for _, schema := range p.Actions {
		owner, _, _ := strings.Cut(schema, ".")
		actions = append(actions, platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: schema})
	}
	out := platform.Page{Name: p.Name, Title: p.Title, Description: p.Description, Layout: "list-detail",
		Object:     platform.AssetRef{App: app, Kind: platform.AssetObject, Name: p.Object},
		ListFields: slices.Clone(p.List), DetailFields: slices.Clone(p.Detail), Actions: actions}
	out.Selections = slices.Clone(p.Selections)
	out.Document = p.Document
	if len(p.Sections) == 0 {
		return out
	}
	out.Layout, out.ListFields, out.DetailFields, out.Sections = "composed", nil, nil, []platform.Section{}
	for _, s := range p.Sections {
		values := map[string]json.RawMessage{}
		raw, _ := json.Marshal(s)
		_ = json.Unmarshal(raw, &values)
		for _, key := range []string{"object", "query", "actions", "function", "inlineEdit"} {
			delete(values, key)
		}
		raw, _ = json.Marshal(values)
		var section platform.Section
		_ = json.Unmarshal(raw, &section)

		if s.InlineEdit != nil {
			section.InlineEdit = &platform.PageInlineEdit{Action: platform.AssetRef{App: strings.SplitN(s.InlineEdit.Action, ".", 2)[0], Kind: platform.AssetAction, Name: s.InlineEdit.Action}, Fields: slices.Clone(s.InlineEdit.Fields)}
		}
		if s.Function != nil {
			section.Function = &platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: s.Function.Name},
				SourceVersion: ValueDefinitionVersion + ".function-" + fmt.Sprint(s.Function.Version)}
		}
		if s.Object != "" {
			owner, _, _ := strings.Cut(s.Object, ".")
			section.Object = platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: s.Object}
		}
		if s.Query != "" {
			owner, name, _ := strings.Cut(s.Query, ".")
			section.Query = platform.AssetRef{App: owner, Kind: platform.AssetQuery, Name: name}
		}
		if s.Actions != nil {
			section.Actions = make([]platform.AssetRef, 0, len(s.Actions))
		}
		for _, schema := range s.Actions {
			owner, _, _ := strings.Cut(schema, ".")
			section.Actions = append(section.Actions, platform.AssetRef{App: owner, Kind: platform.AssetAction, Name: schema})
		}
		out.Sections = append(out.Sections, section)
	}
	return out
}

func (b *Build) checkPage(p Page) error {
	if !Named(p.Name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", p.Name)
	}
	if err := descriptor(p).CheckSafety(); err != nil {
		return err
	}
	if b.host != nil {
		if _, ok := b.lookupEntity(p.Object); !ok {
			return fmt.Errorf("this tenant has no object %q", p.Object)
		}
	}
	return nil
}

func (b *Build) normalizePageModel(p *Page, lookup func(string) (platform.EntityInfo, bool)) error {
	d := descriptor(*p)
	if err := d.NormalizeModelBindings(lookup); err != nil {
		return err
	}
	for i, binding := range d.DataBindings {
		if binding.Query != nil && binding.Query.SourceVersion == "" {
			if owner, ok := b.host.(platform.ModelPageHost); ok {
				if source, known := owner.ModelQueryBinding(binding.Query.Ref); known {
					binding.Query = &source
				}
			}
		}
		for _, model := range binding.ModelBindings() {
			if model.Set != nil && model.Set.Query != nil && model.Set.Query.SourceVersion == "" {
				if owner, ok := b.host.(platform.ModelPageHost); ok {
					if source, known := owner.ModelQueryBinding(model.Set.Query.Ref); known {
						*model.Set.Query = source
					}
				}
			}
		}
		d.DataBindings[i] = binding
	}
	p.Format = 3
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

func retainPagePublication(p *Page) error {
	snapshots := append(slices.Clone(p.Versions), p.Published, published(*p))
	kept := []string{}
	seen := map[string]bool{}
	for _, raw := range snapshots {
		if raw == "" {
			continue
		}
		saved, ok := ReadPublication[Page](raw)
		if !ok || saved.ID != p.ID || saved.Name == "" || saved.Published != "" || len(saved.Versions) != 0 {
			return fmt.Errorf("invalid original page publication history")
		}
		view := descriptor(saved)
		if _, err := platform.PageReleaseAsset(ID, "", view); err != nil {
			return err
		}
		version, err := platform.PageContentVersion(view)
		if err != nil {
			return err
		}
		if !seen[version] {
			seen[version] = true
			kept = append(kept, raw)
		}
	}
	if len(kept) > 64 {
		return fmt.Errorf("a page may retain at most 64 different published contents")
	}
	p.Versions = kept
	p.Published = published(*p)
	return nil
}

func (b *Build) PageContent(name, version string) (platform.Page, bool) {
	if platform.CheckPageContentVersion(version) != nil || b.host == nil {
		return platform.Page{}, false
	}
	rows, err := readDefinitionInventory[Page](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return platform.Page{}, false
	}
	for _, p := range rows {
		if p.Archived || p.Name != name || p.Published == "" {
			continue
		}
		for _, raw := range append(slices.Clone(p.Versions), p.Published) {
			saved, ok := ReadPublication[Page](raw)
			if !ok || saved.ID != p.ID || saved.Name == "" || saved.Published != "" || len(saved.Versions) != 0 {
				return platform.Page{}, false
			}
			if saved.Name != name {
				continue
			}
			view := descriptor(saved)
			digest, err := platform.PageContentVersion(view)
			if err == nil && digest == version {
				return view, true
			}
		}
	}
	return platform.Page{}, false
}

const (
	PageType      = "build.page"
	SchemaRelease = PageType + ".publish"
)

func (s *Section) UnmarshalJSON(raw []byte) error {
	type value Section
	return platform.RetainPageSection(raw, (*value)(s), &s.LegacyVisual)
}
func (s Section) MarshalJSON() ([]byte, error) {
	type value Section
	return platform.MarshalPageSection(value(s), s.LegacyVisual)
}
