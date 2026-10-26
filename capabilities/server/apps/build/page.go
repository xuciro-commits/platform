package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/host"
	"reflect"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A page someone composes here (#132): which object it shows, the
// fields its list and its detail carry, and the actions it offers. It is the
// same descriptor a code page declares, so the workspace renders it with the
// same component, and every read and action on it is still the member's own.

// Page is a list and detail page this organisation composed.
type Page struct {
	platform.Record
	Name        string   `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visits"`
	Title       string   `json:"title" field:"required,search" title:"What people call it" example:"Visits"`
	Description string   `json:"description,omitempty" type:"longtext" help:"What people do on this page"`
	Object      string   `json:"object" field:"required" title:"Object it shows" help:"An object this tenant has: one it defined, or an app's own" example:"build.visits"`
	List        []string `json:"list" title:"Fields in the list" help:"The object's field names, in the order the list shows them"`
	Detail      []string `json:"detail" title:"Fields in the detail" help:"The object's field names, in the order one record shows them"`
	Actions     []string `json:"actions,omitempty" title:"Actions it offers" help:"Actions of that object, by their schema" example:"build.visits.close"`
	// Sections are what the page is laid out from when someone composes it in
	// the editor; with none, the page is the list and detail above.
	Sections   []Section                    `json:"sections,omitempty" field:"aside" title:"What is on the page"`
	Document   *platform.PageDocument       `json:"document,omitempty" field:"aside" type:"json" title:"Page layout"`
	Selections []platform.SelectionVariable `json:"selections,omitempty" field:"aside" title:"Record selections"`
	State      string                       `json:"state" field:"readonly" choices:"draft,published"`
	// Published is the page as it was last published: what people open, and
	// what a restore puts back — not the draft beside it.
	Published string   `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions  []string `json:"versions,omitempty" field:"readonly" type:"lines" title:"Published page contents"`
}

// Section is one widget on a composed page, as someone lays it out.
type InlineEdit struct {
	Action string   `json:"action"`
	Fields []string `json:"fields"`
}

type Section struct {
	platform.PageWidgetConfig
	InlineEdit           *InlineEdit          `json:"inlineEdit,omitempty"`
	Facets               []platform.PageFacet `json:"facets,omitempty"`
	FilterSearchVariable string               `json:"filterSearchVariable,omitempty"`
	ID                   string               `json:"id,omitempty"`
	ConfigVersion        int                  `json:"configVersion,omitempty"`
	Widget               string               `json:"widget" field:"required" help:"What it shows; the shared page widget registry defines supported kinds"`
	Title                string               `json:"title,omitempty"`
	Width                string               `json:"width,omitempty" choices:"full,half"`
	Object               string               `json:"object,omitempty" title:"Object" help:"Another object it shows; empty: the page's own"`
	Selection            string               `json:"selection,omitempty"`
	CollectionVariable   string               `json:"collectionVariable,omitempty"`
	RecordVariable       string               `json:"recordVariable,omitempty"`
	SelectionVariable    string               `json:"selectionVariable,omitempty"`
	FilterVariable       string               `json:"filterVariable,omitempty"`
	ParentSelection      string               `json:"parentSelection,omitempty"`
	// Relation follows the typed parent selection, or the page's shared record.
	Relation string `json:"relation,omitempty" title:"Through" help:"For another object's table, chart, metric or form: the relation to the selected parent record"`
	// Query is a named query "<app>.<name>" the section lists.
	Query        string                      `json:"query,omitempty" title:"Query" help:"For a table: a named query of its object, like build.open-visits"`
	Fields       []string                    `json:"fields,omitempty" help:"For a table or a detail: the fields it shows; a filter: the fields it filters by; a form: the fields it asks for"`
	Actions      []string                    `json:"actions,omitzero" help:"For actions: the schemas it offers; omitted inherits the page, empty forbids section writes"`
	Group        string                      `json:"group,omitempty" title:"Grouped by" help:"For a chart: the field, or <field>:month"`
	ChartVariant string                      `json:"chartVariant,omitempty"`
	Mark         string                      `json:"mark,omitempty"`
	ColumnGroup  string                      `json:"columnGroup,omitempty"`
	TimeStart    string                      `json:"timeStart,omitempty" title:"Start time field"`
	TimeEnd      string                      `json:"timeEnd,omitempty" title:"End time field"`
	TimeLabel    string                      `json:"timeLabel,omitempty" title:"Timeline label field"`
	TimeGroup    string                      `json:"timeGroup,omitempty" title:"Timeline resource field"`
	CardLabel    string                      `json:"cardLabel,omitempty" title:"Card title field"`
	Measure      string                      `json:"measure,omitempty" help:"For a chart or a metric: count, sum:<field>, avg:<field>"`
	Text         string                      `json:"text,omitempty" type:"longtext" help:"For text: the words to show"`
	Function     *platform.FunctionRef       `json:"function,omitempty" title:"Published AI function"`
	Operation    *platform.AssetBinding      `json:"operation,omitempty" title:"Published code function" type:"json"`
	Inputs       map[string]platform.Binding `json:"inputs,omitempty" type:"json"`
}

func (b *Build) pageEntity() platform.Entity {
	return platform.Entity{Type: construction.PageType, Title: "Page", Plural: "Pages", Model: Page{}, Display: "title",
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
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{construction.Builder}, Capability: "pages"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being composed; nobody can open it yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Open to the members who may read its object."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{construction.Builder}, Capability: "pages", Payload: []platform.Field{},
				Description: "Put the page in the workspace as it stands. Published again, it takes its new fields and actions.",
				Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
					page, ok := record.(*Page)
					if !ok {
						return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
					}
					if !c.Replaying {
						if owner, ok := b.host.(host.ModelPageHost); ok {
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

// Only page create/edit use saved object drafts. Publication and candidate
// validation still resolve their original installed or precisely selected model.
func (b *Build) pageDraftEntity() platform.Entity {
	entity := b.pageEntity()
	entity.Validate = func(c platform.Caller, record any) *kernel.Error {
		page := record.(*Page)
		if c.Replaying || page.Document == nil {
			return nil
		}
		objects, err := readDefinitionInventory[construction.Object](c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
		}
		models := map[string]platform.EntityInfo{}
		for _, object := range objects {
			if object.Archived {
				continue
			}
			info, err := platform.Describe(construction.ID, Entity(object), func(reflect.Type) string { return "" })
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
	if page.Document == nil {
		return nil
	}
	if err := descriptor(*page).BindRuleTypes(lookup); err != nil {
		return err
	}
	return page.Document.NormalizePageRules(engines...)
}

// release installs the page: the host checks it against what is installed now
// and offers it to the members who may read its object.
func (b *Build) release(c platform.Caller, p Page) *kernel.Error {
	if err := b.checkPage(p); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.host.InstallPage(c, descriptor(p)); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	return nil
}

// descriptor is the page as the platform's registry holds it — the same shape a
// code page declares in its manifest.
func descriptor(p Page) platform.Page {
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
		section := platform.Section{PageWidgetConfig: s.PageWidgetConfig, Facets: slices.Clone(s.Facets), FilterSearchVariable: s.FilterSearchVariable, ID: s.ID, ConfigVersion: s.ConfigVersion, Widget: s.Widget, Title: s.Title, Width: s.Width, Relation: s.Relation, Selection: s.Selection, RecordVariable: s.RecordVariable, SelectionVariable: s.SelectionVariable, FilterVariable: s.FilterVariable, CollectionVariable: s.CollectionVariable, ParentSelection: s.ParentSelection, Fields: slices.Clone(s.Fields),
			Group: s.Group, Mark: s.Mark, ChartVariant: s.ChartVariant, ColumnGroup: s.ColumnGroup, TimeStart: s.TimeStart, TimeEnd: s.TimeEnd, TimeLabel: s.TimeLabel, TimeGroup: s.TimeGroup, CardLabel: s.CardLabel, Measure: s.Measure, Text: s.Text, Operation: s.Operation, Inputs: s.Inputs}
		section.RecordLinks, section.Buttons = slices.Clone(s.RecordLinks), slices.Clone(s.Buttons)
		section.TableColumns = slices.Clone(s.TableColumns)
		if s.InlineEdit != nil {
			section.InlineEdit = &platform.PageInlineEdit{Action: platform.AssetRef{App: strings.SplitN(s.InlineEdit.Action, ".", 2)[0], Kind: platform.AssetAction, Name: s.InlineEdit.Action}, Fields: slices.Clone(s.InlineEdit.Fields)}
		}
		if s.Function != nil {
			section.Function = &platform.AssetBinding{Ref: platform.AssetRef{App: construction.ID, Kind: platform.AssetFunction, Name: s.Function.Name},
				SourceVersion: construction.ValueDefinitionVersion + ".function-" + fmt.Sprint(s.Function.Version)}
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

// checkPage refuses a page the workspace could not open, with the reason: an
// object this tenant has not, a field it has not, an action of something else.
func (b *Build) checkPage(p Page) error {
	if !construction.Named(p.Name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", p.Name)
	}
	if p.Document != nil && len(p.Sections) == 0 {
		return fmt.Errorf("page %s: a structured layout needs sections", p.Name)
	}
	if err := p.Document.Check(descriptor(p).EffectiveModelPage(b.conditionLookup).Sections); err != nil {
		return fmt.Errorf("page %s: %w", p.Name, err)
	}
	if b.host == nil {
		return nil
	}
	info, known := b.lookupEntity(p.Object)
	if !known {
		return fmt.Errorf("this tenant has no object %q", p.Object)
	}
	if len(p.Sections) > 0 {
		return nil // the host checks every section when the page is published
	}
	if len(p.List) == 0 || len(p.Detail) == 0 {
		return fmt.Errorf("a page needs fields in its list and in its detail, or sections to lay out; %s has %s", p.Object, names(info))
	}
	for _, fields := range [][]string{p.List, p.Detail} {
		seen := map[string]bool{}
		for _, name := range fields {
			if seen[name] {
				return fmt.Errorf("the field %q is on the page twice", name)
			}
			seen[name] = true
			if _, ok := info.Field(name); !ok {
				return fmt.Errorf("%s has no field %q; it has %s", p.Object, name, names(info))
			}
		}
	}
	for _, schema := range p.Actions {
		var action platform.Action
		var ok bool
		if model, selected := b.jointEntities[p.Object]; selected && strings.HasPrefix(schema, p.Object+".") {
			ok = slices.Contains(model.Standard, schema)
			if model.Lifecycle != nil {
				ok = ok || slices.ContainsFunc(model.Lifecycle.Transitions, func(transition platform.TransitionInfo) bool { return transition.Schema == schema })
			}
			action.Target = model.Type
		} else {
			_, action, ok = b.host.Action(schema)
		}
		if !ok {
			return fmt.Errorf("this tenant has no action %q", schema)
		}
		if action.Target != p.Object {
			return fmt.Errorf("the action %q is about %s, not %s", schema, action.Target, p.Object)
		}
	}
	return nil
}

// names are an object's field names, to tell someone what they may choose.
func names(info platform.EntityInfo) string {
	var out []string
	for _, f := range info.Fields {
		out = append(out, f.Name)
	}
	return strings.Join(out, ", ")
}

// Canonicalize the author's original page in place. Only presentation bindings
// migrate; action identities, saved publication bytes and layout stay intact.
func (b *Build) normalizePageModel(p *Page, lookup func(string) (platform.EntityInfo, bool)) error {
	if p.Document == nil {
		return nil
	}
	d := descriptor(*p)
	if err := d.NormalizeModelBindings(lookup); err != nil {
		return err
	}
	for _, section := range d.Sections {
		for _, model := range section.ModelBindings() {
			for _, set := range []*platform.ModelRecordSet{model.Set} {
				if set != nil && set.Query != nil && set.Query.SourceVersion == "" {
					if owner, ok := b.host.(host.ModelPageHost); ok {
						if binding, known := owner.ModelQueryBinding(set.Query.Ref); known {
							*set.Query = binding
						}
					}
				}
			}
		}
	}
	for i, section := range d.Sections {
		values := map[string]any{}
		_ = json.Unmarshal(platform.Raw(p.Sections[i]), &values)
		canonical := map[string]any{}
		_ = json.Unmarshal(platform.Raw(section), &canonical)
		platform.ReplaceModelPorts(values, canonical)
		var next Section
		if err := json.Unmarshal(platform.Raw(values), &next); err != nil {
			return err
		}
		p.Sections[i] = next
	}
	return nil
}
