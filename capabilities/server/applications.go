package platformserver

// An application is one thing whether Go or the builder made it: an
// application definition in the tenant's asset registry, whose pages are page
// definitions in the same registry. This file reads both through one path, so
// the workspace opens a code app and a model app with the same call.

import (
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// ApplicationPage is one page of an application, as its navigation lists it.
type ApplicationPage struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Object      string `json:"object,omitempty"`
}

// ApplicationView is one application the caller may open: its identity, how it
// came to the tenant, and the pages it offers them.
type ApplicationView struct {
	App         string            `json:"app"`  // the owner whose registry holds it
	Name        string            `json:"name"` // the application's own name
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Icon        string            `json:"icon,omitempty"`
	Source      string            `json:"source"` // code | tenant
	Pages       []ApplicationPage `json:"pages"`
}

// PageView is one page of an application resolved for the caller: the
// declaration, the object it runs on with the caller's own field projection,
// and the actions of that page the caller may take.
type PageView struct {
	Application ApplicationView      `json:"application"`
	Page        platform.Page        `json:"page"`
	Entity      *platform.EntityInfo `json:"entity,omitempty"`
	Actions     []platform.Action    `json:"actions"`
}

// Applications lists every application the member may open. It reads the same
// member-filtered definitions the workspace already discovers: an application
// is offered when at least one of its pages is, and a page is offered when the
// member may read what it shows.
func (t *Tenant) Applications(m platform.Member) []ApplicationView {
	definitions := t.Definitions(m)
	pages := map[string]*platform.Page{}
	for i := range definitions {
		if definition := definitions[i]; definition.Ref.Kind == platform.AssetPage && definition.Page != nil {
			pages[pageKey(definition.Ref.App, definition.Ref.Name)] = definition.Page
		}
	}
	out := []ApplicationView{}
	for _, definition := range definitions {
		if definition.Ref.Kind != platform.AssetApp || definition.Application == nil {
			continue
		}
		application := *definition.Application
		view := ApplicationView{App: definition.Ref.App, Name: definition.Ref.Name, Title: application.Title,
			Description: application.Description, Icon: application.Icon, Source: definition.Source, Pages: []ApplicationPage{}}
		if view.Title == "" {
			view.Title = view.Name
		}
		for _, name := range application.Pages {
			page := pages[pageKey(definition.Ref.App, name)]
			if page == nil {
				continue
			}
			view.Pages = append(view.Pages, ApplicationPage{Name: page.Name, Title: page.Title,
				Description: page.Description, Object: page.Object.Name})
		}
		if len(view.Pages) == 0 {
			continue
		}
		out = append(out, view)
	}
	slices.SortFunc(out, func(a, b ApplicationView) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// pageKey is how an application finds its pages in the same owner's registry.
func pageKey(app, name string) string { return app + "\x00" + name }

// PageView resolves one page for the caller: application by name (an owner may
// be named when two applications share a name), page by name. The page's object
// comes back as the caller sees it, and only the page actions the caller's
// catalog carries are offered.
func (t *Tenant) PageView(m platform.Member, app, owner, page string) (PageView, *kernel.Error) {
	definitions := t.Definitions(m)
	var application *platform.Definition
	for i := range definitions {
		definition := &definitions[i]
		if definition.Ref.Kind != platform.AssetApp || definition.Application == nil || definition.Ref.Name != app {
			continue
		}
		if owner != "" && definition.Ref.App != owner {
			continue
		}
		if application == nil || definition.Source == "code" {
			application = definition
		}
	}
	if application == nil {
		return PageView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This workspace offers no such application")
	}
	wanted := false
	for _, name := range application.Application.Pages {
		wanted = wanted || name == page
	}
	var declared *platform.Page
	if wanted {
		for i := range definitions {
			definition := &definitions[i]
			if definition.Ref.Kind == platform.AssetPage && definition.Page != nil &&
				definition.Ref.App == application.Ref.App && definition.Ref.Name == page {
				declared = definition.Page
				break
			}
		}
	}
	if declared == nil {
		return PageView{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "This application has no such page")
	}
	view := PageView{Page: *declared, Actions: []platform.Action{}, Application: ApplicationView{
		App: application.Ref.App, Name: application.Ref.Name, Title: application.Application.Title,
		Description: application.Application.Description, Icon: application.Application.Icon, Source: application.Source}}
	for _, other := range definitions {
		if other.Ref.Kind == platform.AssetPage && other.Ref.App == application.Ref.App && other.Page != nil {
			view.Application.Pages = append(view.Application.Pages, ApplicationPage{Name: other.Page.Name, Title: other.Page.Title,
				Description: other.Page.Description, Object: other.Page.Object.Name})
		}
	}
	slices.SortFunc(view.Application.Pages, func(a, b ApplicationPage) int {
		return indexIn(application.Application.Pages, a.Name) - indexIn(application.Application.Pages, b.Name)
	})
	if declared.Object.Name != "" {
		for _, entity := range t.Entities(m) {
			if entity.Type == declared.Object.Name {
				info := entity
				view.Entity = &info
				break
			}
		}
	}
	catalog := t.Catalog(m)
	for _, ref := range declared.Actions {
		for _, action := range catalog {
			if action.Schema == ref.Name {
				view.Actions = append(view.Actions, action)
				break
			}
		}
	}
	return view, nil
}

// indexIn orders pages as the application declares them; a page the list does
// not name comes after those it does.
func indexIn(names []string, name string) int {
	if i := slices.Index(names, name); i >= 0 {
		return i
	}
	return len(names)
}
