package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

// Objects a tenant defines are installed into the running host (ADR-0034):
// the record store keeps their records, the host routes their generated
// actions to the app that owns them, and the asset registry offers the object,
// its actions and its page. From then on they are ordinary entity types — the
// same reads, forms, search, aggregates, journal and replay as an app's own.

// Install declares an entity an app composed at runtime, with the actions
// generated for it and the page people open it through. Installed again, the
// type takes its new fields and its records come with them (D3). The app has
// already taught its own ledger the classes and schemas.
func (t *Tenant) Install(app platform.App, e platform.Entity, actions []platform.Action, pages ...platform.Page) error {
	id := app.Manifest().ID
	if t.owner["action:"+actions[0].Schema] != nil && t.owner["action:"+actions[0].Schema] != app {
		return fmt.Errorf("%s: another app owns %s", e.Type, actions[0].Schema)
	}
	info, err := platform.Describe(id, e, func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	if err := t.records.install(info); err != nil {
		return err
	}
	for _, action := range actions {
		if action.Target != e.Type {
			return fmt.Errorf("%s: action %s targets %s", e.Type, action.Schema, action.Target)
		}
		t.owner["action:"+action.Schema] = app
	}
	t.installDefinitions(id, info, actions, pages)
	return nil
}

// install declares a type, or declares it again: the rows are carried into the
// new Go type through their JSON, so a record keeps the value of every field
// that is still there (ADR-0034 D3).
func (s *recordStore) install(info platform.EntityInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if other := s.byGo[info.Go]; other != nil && other.info.Type != info.Type {
		return fmt.Errorf("%s: another type is already this Go type", info.Type)
	}
	et := &entityType{info: info, rows: map[string]*row{}, knowledge: knowledgeBearing(info)}
	if before := s.types[info.Type]; before != nil {
		if before.info.App != info.App {
			return fmt.Errorf("%s is declared by %s", info.Type, before.info.App)
		}
		for id, r := range before.rows {
			raw, err := json.Marshal(r.value.Interface())
			if err != nil {
				return err
			}
			v := reflect.New(info.Go).Elem()
			if err := json.Unmarshal(raw, v.Addr().Interface()); err != nil {
				return err
			}
			emptyLists(v)
			et.rows[id] = &row{value: v, history: r.history}
		}
		delete(s.byGo, before.info.Go)
	}
	s.types[info.Type], s.byGo[info.Go] = et, et
	s.generation++
	return nil
}

// installDefinitions puts the installed object, its actions and its pages in
// the asset registry, so code and builder discover one set of assets (ADR-0032).
func (t *Tenant) installDefinitions(app string, info platform.EntityInfo, actions []platform.Action, pages []platform.Page) {
	put := func(def platform.Definition) {
		if i := slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }); i >= 0 {
			t.definitions[i] = def
			return
		}
		t.definitions = append(t.definitions, def)
	}
	object := platform.AssetRef{App: app, Kind: platform.AssetObject, Name: info.Type}
	entity := info
	put(platform.Definition{Ref: object, Source: "tenant", Version: "1", ContractVersion: 1, Entity: &entity})
	for _, action := range actions {
		a := action
		put(platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetAction, Name: a.Schema}, Source: "tenant", Version: "1",
			ContractVersion: 1, Requires: []platform.AssetRef{object}, Action: &a})
	}
	for _, page := range pages {
		p := page
		requires := append([]platform.AssetRef{object}, p.Actions...)
		put(platform.Definition{Ref: platform.AssetRef{App: app, Kind: platform.AssetPage, Name: p.Name}, Source: "tenant", Version: "1",
			ContractVersion: 1, Requires: requires, Page: &p})
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
}

// reinstall asks every app that installs its own definitions to install them
// again: after a restore, before the records of the types they define (D4).
func (t *Tenant) reinstall() error {
	for _, a := range t.apps {
		x, ok := a.(interface{ Reinstall() error })
		if !ok {
			continue
		}
		if err := x.Reinstall(); err != nil {
			return fmt.Errorf("tenant %s: %s: %v", t.ID, a.Manifest().ID, err)
		}
	}
	return nil
}

// InstallPage offers a page an app composed at runtime: it must name an object
// this tenant has, fields that object declares and actions about it, so the
// workspace can always open what the registry offers (ADR-0034, #132). The
// member's own reads and catalog still decide what they see on it.
func (t *Tenant) InstallPage(app platform.App, p platform.Page) error {
	id := app.Manifest().ID
	if p.Name == "" || (p.Layout != "list-detail" && p.Layout != "composed") {
		return fmt.Errorf("page %q: a page is list-detail, or composed of sections (ADR-0035)", p.Name)
	}
	info, known := t.entity(p.Object.Name)
	if !known {
		return fmt.Errorf("page %s: no object %s", p.Name, p.Object.Name)
	}
	if len(p.Sections) > 0 {
		if err := t.checkSections(p, info); err != nil {
			return err
		}
	} else {
		for _, fields := range [][]string{p.ListFields, p.DetailFields} {
			if len(fields) == 0 {
				return fmt.Errorf("page %s: no fields in its list or its detail", p.Name)
			}
			for _, name := range fields {
				if _, ok := info.Field(name); !ok {
					return fmt.Errorf("page %s: %s has no field %s", p.Name, p.Object.Name, name)
				}
			}
		}
	}
	for _, ref := range p.Actions {
		if err := t.checkAction(p.Name, ref.Name, p.Object.Name); err != nil {
			return err
		}
	}
	t.installDefinitions(id, info, nil, []platform.Page{p})
	return nil
}

// checkSections holds a composed page to what this tenant has: a widget it
// knows, an object, the fields, actions and measures that object declares
// (ADR-0035). What the registry offers must open.
func (t *Tenant) checkSections(p platform.Page, page platform.EntityInfo) error {
	for i, s := range p.Sections {
		where := fmt.Sprintf("page %s, section %d (%s)", p.Name, i+1, s.Widget)
		if !slices.Contains(platform.Widgets, s.Widget) {
			return fmt.Errorf("%s: no widget %q; there are %s", where, s.Widget, strings.Join(platform.Widgets, ", "))
		}
		if s.Width != "" && s.Width != "full" && s.Width != "half" {
			return fmt.Errorf("%s: width %q is neither full nor half", where, s.Width)
		}
		info := page
		if s.Object.Name != "" && s.Object.Name != p.Object.Name {
			shown, known := t.entity(s.Object.Name)
			if !known {
				return fmt.Errorf("%s: no object %s", where, s.Object.Name)
			}
			info = shown
		}
		if s.Relation != "" {
			if s.Widget != "table" && s.Widget != "chart" && s.Widget != "metric" {
				return fmt.Errorf("%s: only a table, chart or metric follows a relation", where)
			}
			if info.Type == p.Object.Name || !slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool {
				return f.Type == "reference" && f.Ref == p.Object.Name && f.Inverse == s.Relation
			}) {
				return fmt.Errorf("%s: %s declares no relation %q from %s", where, info.Type, s.Relation, p.Object.Name)
			}
		}
		if s.Query.Name != "" {
			q, ok := t.namedQuery(s.Query.App, s.Query.Name)
			switch {
			case s.Widget != "table":
				return fmt.Errorf("%s: only a table lists a query", where)
			case !ok:
				return fmt.Errorf("%s: no query %s", where, s.Query)
			case q.Object != info.Type:
				return fmt.Errorf("%s: query %s reads %s, not %s", where, s.Query, q.Object, info.Type)
			case q.By != "" && (info.Type == p.Object.Name || !slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool {
				return f.Name == q.By && f.Ref == p.Object.Name
			})):
				return fmt.Errorf("%s: query %s is run for a %s record, not this page's %s", where, s.Query, q.By, p.Object.Name)
			}
		}
		field := func(name string) error {
			if _, ok := info.Field(strings.Split(name, ":")[0]); !ok {
				return fmt.Errorf("%s: %s has no field %s", where, info.Type, name)
			}
			return nil
		}
		switch s.Widget {
		case "table", "detail":
			if len(s.Fields) == 0 {
				return fmt.Errorf("%s: no fields to show", where)
			}
			for _, name := range s.Fields {
				if err := field(name); err != nil {
					return err
				}
			}
		case "actions":
			if len(s.Actions) == 0 {
				return fmt.Errorf("%s: no actions to offer", where)
			}
			for _, ref := range s.Actions {
				if err := t.checkAction(p.Name, ref.Name, info.Type); err != nil {
					return err
				}
			}
		case "chart", "metric":
			if s.Measure == "" {
				return fmt.Errorf("%s: nothing measured; count, sum:<field>, avg:<field>, min:<field> or max:<field>", where)
			}
			kind, name, some := strings.Cut(s.Measure, ":")
			if !slices.Contains([]string{"count", "sum", "avg", "min", "max"}, kind) || (kind != "count") != some {
				return fmt.Errorf("%s: %q is not count, sum:<field>, avg:<field>, min:<field> or max:<field>", where, s.Measure)
			}
			if some {
				if err := field(name); err != nil {
					return err
				}
			}
			if s.Widget == "chart" {
				if s.Group == "" {
					return fmt.Errorf("%s: nothing to group by", where)
				}
				if err := field(s.Group); err != nil {
					return err
				}
			}
		case "text":
			if strings.TrimSpace(s.Text) == "" {
				return fmt.Errorf("%s: no words to show", where)
			}
		case "filter":
			if len(s.Fields) == 0 {
				return fmt.Errorf("%s: no fields to filter by", where)
			}
			for _, name := range s.Fields {
				f, ok := info.Field(name)
				if !ok {
					return fmt.Errorf("%s: %s has no field %s", where, info.Type, name)
				}
				if !slices.Contains(platform.Filterable, f.Type) {
					return fmt.Errorf("%s: %s is a %s field; a filter takes %s fields", where, name, f.Type, strings.Join(platform.Filterable, ", "))
				}
			}
		case "form":
			// A form makes a new record through the object's own create action,
			// so it asks for everything that action needs.
			if err := t.checkAction(p.Name, info.Type+".create", info.Type); err != nil {
				return fmt.Errorf("%s: %s cannot be created here", where, info.Type)
			}
			if len(s.Fields) == 0 {
				return fmt.Errorf("%s: no fields to fill in", where)
			}
			for _, name := range s.Fields {
				if err := field(name); err != nil {
					return err
				}
			}
			for _, f := range info.Fields {
				if f.Required && !f.ReadOnly && !slices.Contains(s.Fields, f.Name) {
					return fmt.Errorf("%s: %s needs %s, which the form does not ask for", where, info.Type, f.Name)
				}
			}
		}
	}
	return nil
}

// checkAction holds a page's action to one about the object it shows.
func (t *Tenant) checkAction(page, schema, object string) error {
	owner := t.owner["action:"+schema]
	action, ok := platform.Action{}, false
	if owner != nil {
		action, ok = owner.Manifest().Actions.Action(schema)
	}
	if !ok || action.Target != object {
		return fmt.Errorf("page %s: no action %s about %s", page, schema, object)
	}
	return nil
}

// entity is an entity type's declaration, as the app that composes over it sees it.
func (t *Tenant) entity(typ string) (platform.EntityInfo, bool) {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	et := t.records.types[typ]
	if et == nil {
		return platform.EntityInfo{}, false
	}
	return et.info, true
}

// InstallApplication offers an application a tenant handed to its people: a
// name over pages that are already installed (ADR-0036). It grants nothing —
// the registry offers each page to whoever may read what it shows, and an
// application to whoever may open one of its pages.
func (t *Tenant) InstallApplication(app platform.App, a platform.Application) error {
	id := app.Manifest().ID
	if a.Name == "" || a.Title == "" {
		return fmt.Errorf("application %q: it needs a name and a title", a.Name)
	}
	if len(a.Pages) == 0 {
		return fmt.Errorf("application %s: it holds no page", a.Name)
	}
	if a.Icon != "" && !slices.Contains(platform.Icons, a.Icon) {
		return fmt.Errorf("application %s: no icon %s", a.Name, a.Icon)
	}
	for _, page := range a.Pages {
		ref := platform.AssetRef{App: id, Kind: platform.AssetPage, Name: page}
		if !slices.ContainsFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref }) {
			return fmt.Errorf("application %s: no page %s", a.Name, page)
		}
	}
	if err := a.CheckGroups(); err != nil {
		return fmt.Errorf("application %s: %v", a.Name, err)
	}
	installed := a
	def := platform.Definition{Ref: platform.AssetRef{App: id, Kind: platform.AssetApp, Name: a.Name}, Source: "tenant", Version: "1",
		ContractVersion: 1, Application: &installed}
	for _, page := range a.Pages {
		def.Requires = append(def.Requires, platform.AssetRef{App: id, Kind: platform.AssetPage, Name: page})
	}
	if i := slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }); i >= 0 {
		t.definitions[i] = def
	} else {
		t.definitions = append(t.definitions, def)
	}
	slices.SortFunc(t.definitions, func(x, y platform.Definition) int { return strings.Compare(x.Ref.String(), y.Ref.String()) })
	return nil
}
