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
	if p.Name == "" || p.Layout != "list-detail" {
		return fmt.Errorf("page %q: only a list-detail page", p.Name)
	}
	info, known := t.entity(p.Object.Name)
	if !known {
		return fmt.Errorf("page %s: no object %s", p.Name, p.Object.Name)
	}
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
	for _, ref := range p.Actions {
		owner := t.owner["action:"+ref.Name]
		action, ok := platform.Action{}, false
		if owner != nil {
			action, ok = owner.Manifest().Actions.Action(ref.Name)
		}
		if !ok || action.Target != p.Object.Name {
			return fmt.Errorf("page %s: no action %s about %s", p.Name, ref.Name, p.Object.Name)
		}
	}
	t.installDefinitions(id, info, nil, []platform.Page{p})
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
