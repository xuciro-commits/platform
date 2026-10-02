package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// A page someone composes here (ADR-0034, #132): which object it shows, the
// fields its list and its detail carry, and the actions it offers. It is the
// same descriptor a code page declares, so the workspace renders it with the
// same component, and every read and action on it is still the member's own.

const (
	PageType      = "build.page"
	SchemaRelease = PageType + ".publish"
)

// Page is a list and detail page this organisation composed.
type Page struct {
	platform.Record
	Name        string   `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visits"`
	Title       string   `json:"title" field:"required,search" title:"What people call it" example:"Visits"`
	Description string   `json:"description,omitempty" type:"longtext" help:"What people do on this page"`
	Object      string   `json:"object" field:"required" title:"Object it shows" help:"An object this tenant has: one it defined, or an app's own" example:"crm.opportunity"`
	List        []string `json:"list" title:"Fields in the list" help:"The object's field names, in the order the list shows them"`
	Detail      []string `json:"detail" title:"Fields in the detail" help:"The object's field names, in the order one record shows them"`
	Actions     []string `json:"actions,omitempty" title:"Actions it offers" help:"Actions of that object, by their schema" example:"crm.opportunity.close"`
	// Sections are what the page is laid out from when someone composes it in
	// the editor (ADR-0035); with none, the page is the list and detail above.
	Sections   []Section                    `json:"sections,omitempty" field:"aside" title:"What is on the page"`
	Document   *platform.PageDocument       `json:"document,omitempty" field:"aside" type:"json" title:"Page layout"`
	Selections []platform.SelectionVariable `json:"selections,omitempty" field:"aside" title:"Record selections"`
	State      string                       `json:"state" field:"readonly" choices:"draft,published"`
	// Published is the page as it was last published: what people open, and
	// what a restore puts back — not the draft beside it.
	Published string `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

// Section is one widget on a composed page, as someone lays it out.
type Section struct {
	ID                 string `json:"id,omitempty"`
	ConfigVersion      int    `json:"configVersion,omitempty"`
	Widget             string `json:"widget" field:"required" help:"What it shows; the shared page widget registry defines supported kinds"`
	Title              string `json:"title,omitempty"`
	Width              string `json:"width,omitempty" choices:"full,half"`
	Object             string `json:"object,omitempty" title:"Object" help:"Another object it shows; empty: the page's own"`
	Selection          string `json:"selection,omitempty"`
	CollectionVariable string `json:"collectionVariable,omitempty"`
	RecordVariable     string `json:"recordVariable,omitempty"`
	SelectionVariable  string `json:"selectionVariable,omitempty"`
	FilterVariable     string `json:"filterVariable,omitempty"`
	ParentSelection    string `json:"parentSelection,omitempty"`
	// Relation follows the typed parent selection, or the page's shared record.
	Relation string `json:"relation,omitempty" title:"Through" help:"For another object's table, chart, metric or form: the relation to the selected parent record"`
	// Query is a named query "<app>.<name>" the section lists (ADR-0040 21c).
	Query       string                      `json:"query,omitempty" title:"Query" help:"For a table: a named query of its object, like crm.open-opportunities"`
	Fields      []string                    `json:"fields,omitempty" help:"For a table or a detail: the fields it shows; a filter: the fields it filters by; a form: the fields it asks for"`
	Actions     []string                    `json:"actions,omitempty" help:"For actions: the schemas it offers"`
	Group       string                      `json:"group,omitempty" title:"Grouped by" help:"For a chart: the field, or <field>:month"`
	Mark        string                      `json:"mark,omitempty"`
	ColumnGroup string                      `json:"columnGroup,omitempty"`
	TimeStart   string                      `json:"timeStart,omitempty" title:"Start time field"`
	TimeEnd     string                      `json:"timeEnd,omitempty" title:"End time field"`
	TimeLabel   string                      `json:"timeLabel,omitempty" title:"Timeline label field"`
	TimeGroup   string                      `json:"timeGroup,omitempty" title:"Timeline resource field"`
	Measure     string                      `json:"measure,omitempty" help:"For a chart or a metric: count, sum:<field>, avg:<field>"`
	Text        string                      `json:"text,omitempty" type:"longtext" help:"For text: the words to show"`
	Function    *platform.FunctionRef       `json:"function,omitempty" title:"Published AI function"`
	Operation   *platform.AssetBinding      `json:"operation,omitempty" title:"Published code function" type:"json"`
	Inputs      map[string]platform.Binding `json:"inputs,omitempty" type:"json"`
}

func (b *Build) pageEntity() platform.Entity {
	return platform.Entity{Type: PageType, Title: "Page", Plural: "Pages", Model: Page{}, Display: "title",
		Description: "A list and detail page this organisation composed over an object it may read: the fields it shows and the actions it offers.",
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "pages"},
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
					page.Published = published(*page)
					return nil
				}}}}}
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
		section := platform.Section{ID: s.ID, ConfigVersion: s.ConfigVersion, Widget: s.Widget, Title: s.Title, Width: s.Width, Relation: s.Relation, Selection: s.Selection, RecordVariable: s.RecordVariable, SelectionVariable: s.SelectionVariable, FilterVariable: s.FilterVariable, CollectionVariable: s.CollectionVariable, ParentSelection: s.ParentSelection, Fields: slices.Clone(s.Fields),
			Group: s.Group, Mark: s.Mark, ColumnGroup: s.ColumnGroup, TimeStart: s.TimeStart, TimeEnd: s.TimeEnd, TimeLabel: s.TimeLabel, TimeGroup: s.TimeGroup, Measure: s.Measure, Text: s.Text, Actions: []platform.AssetRef{}, Operation: s.Operation, Inputs: s.Inputs}
		if s.Function != nil {
			section.Function = &platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: s.Function.Name},
				SourceVersion: definitionVersion + ".function-" + fmt.Sprint(s.Function.Version)}
		}
		if s.Object != "" {
			owner, _, _ := strings.Cut(s.Object, ".")
			section.Object = platform.AssetRef{App: owner, Kind: platform.AssetObject, Name: s.Object}
		}
		if s.Query != "" {
			owner, name, _ := strings.Cut(s.Query, ".")
			section.Query = platform.AssetRef{App: owner, Kind: platform.AssetQuery, Name: name}
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
	if !named(p.Name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", p.Name)
	}
	if p.Document != nil && len(p.Sections) == 0 {
		return fmt.Errorf("page %s: a structured layout needs sections", p.Name)
	}
	if err := p.Document.Check(descriptor(p).Sections); err != nil {
		return fmt.Errorf("page %s: %w", p.Name, err)
	}
	if b.host == nil {
		return nil
	}
	info, known := b.host.Entity(p.Object)
	if !known {
		return fmt.Errorf("this tenant has no object %q", p.Object)
	}
	if len(p.Sections) > 0 {
		return nil // the host checks every section when the page is published (ADR-0035)
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
		_, action, ok := b.host.Action(schema)
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
