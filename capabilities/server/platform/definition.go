package platform

import (
	"fmt"
	"strings"
)

// AssetKind identifies the existing executor behind a code-defined asset.
// More kinds can be added as their canonical platform owner is built.
type AssetKind string

const (
	AssetObject AssetKind = "object"
	AssetAction AssetKind = "action"
	AssetPage   AssetKind = "page"
	// AssetApp is an application a tenant hands to its people: a name, an icon
	// and the pages it holds (ADR-0036).
	AssetApp AssetKind = "app"
)

// AssetRef is the stable identity shared by code and construction surfaces.
// Its three parts avoid collisions between apps and between kinds. A published
// revision will bind this identity to an immutable definition in a later batch.
type AssetRef struct {
	App  string    `json:"app"`
	Kind AssetKind `json:"kind"`
	Name string    `json:"name"`
}

func (r AssetRef) String() string { return r.App + "/" + string(r.Kind) + "/" + r.Name }

func (r AssetRef) Check() error {
	if r.App == "" || r.Name == "" || strings.Contains(r.App, "/") || strings.Contains(r.Name, "/") || r.Kind != AssetObject && r.Kind != AssetAction && r.Kind != AssetPage && r.Kind != AssetApp {
		return fmt.Errorf("asset reference %q needs an app, supported kind and name", r.String())
	}
	return nil
}

// Page is the first bounded page composition: one object, a list, a detail and
// an explicit action set. It binds existing records/actions; it runs no rules.
// Its field choices shape presentation, never grant access to hidden fields.
type Page struct {
	Name         string     `json:"name"`
	Title        string     `json:"title"`
	Description  string     `json:"description,omitempty"`
	Object       AssetRef   `json:"object"`
	Layout       string     `json:"layout"` // list-detail in 13b
	ListFields   []string   `json:"listFields"`
	DetailFields []string   `json:"detailFields"`
	Actions      []AssetRef `json:"actions"`
	// Sections are the widgets a composed page is laid out from (ADR-0035);
	// with sections, Layout is "composed" and the fields above are unused.
	Sections []Section `json:"sections,omitempty"`
}

// Section is one place on a composed page: a widget, what it is bound to, and
// how wide it sits (ADR-0035). A table outputs the record someone selects; a
// detail and the actions read it.
type Section struct {
	// Widget is table, detail, actions, chart, metric or text.
	Widget string `json:"widget"`
	Title  string `json:"title,omitempty"`
	// Width is full or half, in the order the sections are laid out.
	Width string `json:"width,omitempty"`
	// Object is what it shows; empty: the page's own object.
	Object  AssetRef   `json:"object,omitempty"`
	Fields  []string   `json:"fields,omitempty"`  // table, detail
	Actions []AssetRef `json:"actions,omitempty"` // actions
	Group   string     `json:"group,omitempty"`   // chart: the field it groups by, or "<field>:month"
	Measure string     `json:"measure,omitempty"` // chart, metric: count, sum:<field>, avg:<field>, min:<field>, max:<field>
	Text    string     `json:"text,omitempty"`    // text
}

// Widgets are the widget kinds a composed page may hold (ADR-0035 D2).
var Widgets = []string{"table", "detail", "actions", "chart", "metric", "text"}

// Application is what someone in this tenant hands to its people (ADR-0036): a
// name, an icon from the platform's set, and the pages it holds in order. It
// grants nothing; each page is offered to whoever may read what it shows.
type Application struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// Icon is one of Icons: the workspace draws it in the launcher and the navigation.
	Icon  string   `json:"icon,omitempty"`
	Pages []string `json:"pages"` // page names, in the order people see them
}

// Icons are the icons a tenant's application may take (ADR-0036 D3).
var Icons = []string{"boxes", "clipboard", "people", "calendar", "wrench", "map", "chart", "sparkles"}

// Definition is one installed code asset as the reader may discover it.
// Entity and Action reuse the same descriptions as the existing record/action
// APIs; this registry is an index over those owners, not another executor.
type Definition struct {
	Ref             AssetRef     `json:"ref"`
	Source          string       `json:"source"`  // code, until published definitions exist
	Version         string       `json:"version"` // installed app manifest version, not a published revision
	ContractVersion int          `json:"contractVersion"`
	Requires        []AssetRef   `json:"requires"`
	Entity          *EntityInfo  `json:"entity,omitempty"`
	Action          *Action      `json:"action,omitempty"`
	Page            *Page        `json:"page,omitempty"`
	Application     *Application `json:"application,omitempty"`
}
