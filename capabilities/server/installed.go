package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
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
	if err := t.checkObjectProperties(info); err != nil {
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
	var requires []platform.AssetRef
	for _, f := range info.Fields {
		if f.Property != nil {
			requires = append(requires, f.Property.Ref)
		}
	}
	put(platform.Definition{Ref: object, Source: "tenant", Version: "1", ContractVersion: 1, Requires: uniqueRefs(requires), Entity: &entity})
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
	return t.installPage(app, p, true)
}

func (t *Tenant) installPage(app platform.App, p platform.Page, targets bool) error {
	id := app.Manifest().ID
	if p.Name == "" || (p.Layout != "list-detail" && p.Layout != "composed") {
		return fmt.Errorf("page %q: a page is list-detail, or composed of sections (ADR-0035)", p.Name)
	}
	if p.Document != nil && p.Layout != "composed" {
		return fmt.Errorf("page %s: a structured layout needs composed sections", p.Name)
	}
	if err := p.Document.Check(p.Sections); err != nil {
		return fmt.Errorf("page %s: %w", p.Name, err)
	}
	if err := t.checkPageQueries(p); err != nil {
		return err
	}
	if err := p.CheckRecordPorts(); err != nil {
		return err
	}
	if p.Document != nil && p.Document.Interface != nil {
		for _, ports := range []map[string]platform.PagePort{p.Document.Interface.Inputs, p.Document.Interface.Outputs} {
			for id, port := range ports {
				if port.Object != nil {
					info, ok := t.entity(port.Object.Name)
					if !ok || info.App != port.Object.App {
						return fmt.Errorf("page port %s has an unknown object", id)
					}
				}
			}
		}
	}
	info, known := t.entity(p.Object.Name)
	if !known {
		return fmt.Errorf("page %s: no object %s", p.Name, p.Object.Name)
	}
	if len(p.Selections) != 0 && len(p.Sections) == 0 {
		return fmt.Errorf("page %s: selections need composed sections", p.Name)
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
	if targets {
		for _, d := range t.definitions {
			if d.Application != nil && d.Ref.App == id && slices.Contains(d.Application.Pages, p.Name) {
				if err := d.Application.CheckPageVariables(p); err != nil {
					return err
				}
			}
		}
		if err := t.checkPageNavigation(p, id); err != nil {
			return err
		}
	}
	t.installDefinitions(id, info, nil, []platform.Page{p})
	return nil
}

var selectionName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

// checkSections holds a composed page to what this tenant has: a widget it
// knows, an object, the fields, actions and measures that object declares
// (ADR-0035). What the registry offers must open.
func (t *Tenant) checkSections(p platform.Page, page platform.EntityInfo) error {
	if err := p.Document.Check(p.Sections); err != nil {
		return fmt.Errorf("page %s: %w", p.Name, err)
	}
	selections := map[string]string{}
	for _, selection := range p.Selections {
		if !selectionName.MatchString(selection.Name) || selections[selection.Name] != "" {
			return fmt.Errorf("page %s: selection names must be unique lowercase identifiers", p.Name)
		}
		info, known := t.entity(selection.Object.Name)
		if selection.Object.Check() != nil || selection.Object.Kind != platform.AssetObject || !known || selection.Object.App != info.App {
			return fmt.Errorf("page %s: selection %s needs a known object", p.Name, selection.Name)
		}
		selections[selection.Name] = selection.Object.Name
	}
	parentBindings := map[string]string{}
	for i, s := range p.Sections {
		where := fmt.Sprintf("page %s, section %d (%s)", p.Name, i+1, s.Widget)
		if !slices.Contains(platform.Widgets, s.Widget) {
			return fmt.Errorf("%s: no widget %q; there are %s", where, s.Widget, strings.Join(platform.Widgets, ", "))
		}
		if s.Width != "" && s.Width != "full" && s.Width != "half" {
			return fmt.Errorf("%s: width %q is neither full nor half", where, s.Width)
		}
		if s.Widget == "function" {
			if !slices.ContainsFunc(p.Sections, func(other platform.Section) bool {
				return platform.WidgetWritesSelection(other.Widget) && (other.Object.Name == "" || other.Object == p.Object) && other.Selection == s.Selection
			}) {
				return fmt.Errorf("%s: add a table that selects a source record", where)
			}
			if s.Function == nil || s.Function.Ref.Kind != platform.AssetFunction || s.Function.Ref.App != p.Object.App ||
				s.Object.Name != "" && s.Object != p.Object || len(s.Fields) != 0 || len(s.Actions) != 0 || s.Query.Name != "" || s.Relation != "" {
				return fmt.Errorf("%s: a function must bind one retained version on this page's object", where)
			}
			owner, ok := t.app(s.Function.Ref.App).(interface {
				FunctionReleaseAsset(string, string) (platform.ReleaseAsset, error)
			})
			if !ok {
				return fmt.Errorf("%s: the function has no published call owner", where)
			}
			asset, err := owner.FunctionReleaseAsset(s.Function.Ref.Name, s.Function.SourceVersion)
			if err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			var function platform.AIFunction
			if asset.Ref != s.Function.Ref || json.Unmarshal(asset.Body, &function) != nil || function.Object != p.Object.Name {
				return fmt.Errorf("%s: the function does not read this page's object", where)
			}
		} else if s.Function != nil {
			return fmt.Errorf("%s: only a function widget may bind a function", where)
		}
		if s.Widget == "compute" {
			if s.Object.Name != "" || len(s.Fields) != 0 || len(s.Actions) != 0 || s.Query.Name != "" || s.Relation != "" {
				return fmt.Errorf("%s: compute binds its typed inputs, not another widget's configuration", where)
			}
			if err := t.checkPageOperation(s, page); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
		} else if s.Operation != nil || s.Widget != "form" && len(s.Inputs) != 0 {
			return fmt.Errorf("%s: only a compute or form widget may bind inputs", where)
		}
		info := page
		if s.Object.Name != "" && s.Object.Name != p.Object.Name {
			shown, known := t.entity(s.Object.Name)
			if !known {
				return fmt.Errorf("%s: no object %s", where, s.Object.Name)
			}
			info = shown
		}
		if s.RecordVariable != "" {
			producer := p.Document.LoopRecordSource(s.RecordVariable)
			sourceType := p.RecordVariableObject(s.RecordVariable)
			for _, candidate := range p.Sections {
				if candidate.ID == producer {
					sourceType = candidate.Object.Name
					if sourceType == "" {
						sourceType = p.Object.Name
					}
				}
			}
			if sourceType != info.Type {
				return fmt.Errorf("%s: item record type does not match the widget object", where)
			}
		}

		if err := s.CheckMetricPresentation(info); err != nil {
			return err
		}
		if s.DetailPresentation != nil && p.Document == nil {
			return fmt.Errorf("detail presentation requires a document")
		}
		if s.Widget == "button-group" && p.Document == nil {
			return fmt.Errorf("button group requires a document")
		}
		if len(s.TableColumns) > 0 || s.ShowSearch != nil {
			if p.Document == nil {
				return fmt.Errorf("table presentation requires a document")
			}
			if err := s.CheckTablePresentation(info); err != nil {
				return err
			}
		}
		if s.InlineEdit != nil {
			if p.Document == nil {
				return fmt.Errorf("table editing requires a document")
			}
			owner := t.owner["action:"+s.InlineEdit.Action.Name]
			if owner == nil {
				return fmt.Errorf("table edit action is unavailable")
			}
			action, _ := owner.Manifest().Actions.Action(s.InlineEdit.Action.Name)
			if err := s.CheckTableEdit(info, action); err != nil {
				return err
			}
		}
		parentType := p.Object.Name
		if s.ParentSelection != "" {
			parentType = selections[s.ParentSelection]
			if parentType == "" {
				return fmt.Errorf("%s: parent selection %q is not declared", where, s.ParentSelection)
			}
		}
		if s.Selection != "" {
			if selections[s.Selection] != info.Type {
				return fmt.Errorf("%s: selection %q does not hold %s records", where, s.Selection, info.Type)
			}
			if !platform.WidgetReadsSelection(s.Widget) {
				return fmt.Errorf("%s: this widget does not use a record selection", where)
			}
			if !slices.ContainsFunc(p.Sections, func(other platform.Section) bool {
				return platform.WidgetWritesSelection(other.Widget) && other.Selection == s.Selection
			}) {
				return fmt.Errorf("%s: add a table that supplies selection %q", where, s.Selection)
			}
		}
		if s.ParentSelection != "" {
			if !slices.Contains([]string{"table", "chart", "metric", "form"}, s.Widget) ||
				!slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Type == "reference" && f.Ref == parentType }) || s.Widget == "form" && s.Relation == "" {
				return fmt.Errorf("%s: parent selection needs a related table, chart, metric or bound form", where)
			}
			if !slices.ContainsFunc(p.Sections, func(other platform.Section) bool {
				return other.Widget == "table" && other.Selection == s.ParentSelection && (other.Object.Name == parentType || other.Object.Name == "" && parentType == p.Object.Name)
			}) {
				return fmt.Errorf("%s: add a table that supplies parent selection %q", where, s.ParentSelection)
			}
		}
		if s.Widget == "table" && (info.Type != p.Object.Name || s.ParentSelection != "") && slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool { return f.Type == "reference" && f.Ref == parentType }) {
			key := "object:" + info.Type
			if s.Selection != "" {
				key = "selection:" + s.Selection
			}
			parent := "object:" + parentType
			if s.ParentSelection != "" {
				parent = "selection:" + s.ParentSelection
			}
			if previous := parentBindings[key]; previous != "" && previous != parent {
				return fmt.Errorf("%s: record selection has conflicting parent bindings", where)
			}
			parentBindings[key] = parent
		}
		boundParent := ""
		if s.Relation != "" {
			if s.Widget != "table" && s.Widget != "chart" && s.Widget != "metric" && s.Widget != "form" {
				return fmt.Errorf("%s: only a table, chart, metric or form follows a relation", where)
			}
			at := slices.IndexFunc(info.Fields, func(f platform.FieldInfo) bool {
				return f.Type == "reference" && f.Ref == parentType && f.Inverse == s.Relation
			})
			if at < 0 {
				return fmt.Errorf("%s: %s declares no relation %q from %s", where, info.Type, s.Relation, parentType)
			}
			if s.Widget == "form" {
				if info.Fields[at].ReadOnly {
					return fmt.Errorf("%s: parent reference %s is read only", where, info.Fields[at].Name)
				}
				if !slices.ContainsFunc(p.Sections, func(other platform.Section) bool {
					return other.Widget == "table" && (other.Object.Name == parentType || other.Object.Name == "" && parentType == p.Object.Name) && other.Selection == s.ParentSelection
				}) {
					return fmt.Errorf("%s: add a table that selects a parent record", where)
				}
				boundParent = info.Fields[at].Name
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
			case q.By != "" && (!slices.ContainsFunc(info.Fields, func(f platform.FieldInfo) bool {
				return f.Name == q.By && f.Ref == parentType
			})):
				return fmt.Errorf("%s: query %s is run for a %s record, not this page's %s", where, s.Query, q.By, parentType)
			}
		}
		field := func(name string) error {
			if _, ok := info.Field(strings.Split(name, ":")[0]); !ok {
				return fmt.Errorf("%s: %s has no field %s", where, info.Type, name)
			}
			return nil
		}
		switch s.Widget {
		case "inline-action":
			if p.Document == nil || len(s.Actions) != 1 {
				return fmt.Errorf("inline action needs a V2 page and one action")
			}
			owner := t.owner["action:"+s.Actions[0].Name]
			if owner == nil {
				return fmt.Errorf("inline action is unavailable")
			}
			action, _ := owner.Manifest().Actions.Action(s.Actions[0].Name)
			if err := s.CheckInlineAction(info, action); err != nil {
				return err
			}
		case "kanban":
			if p.Document == nil {
				return fmt.Errorf("kanban needs a V2 document")
			}
			if err := s.CheckKanban(info); err != nil {
				return err
			}
			for _, ref := range s.Actions {
				if err := t.checkAction(p.Name, ref.Name, info.Type); err != nil {
					return err
				}
			}
		case "record-chart":
			if err := s.CheckRecordChart(info); err != nil {
				return err
			}
		case "record-list":
			if err := s.CheckRecordList(info); err != nil {
				return err
			}
		case "record-timeline":
			if p.Document == nil {
				return fmt.Errorf("record timeline needs a V2 document")
			}
			if err := s.CheckTimeline(info); err != nil {
				return err
			}
		case "status-tracker":
			if err := s.CheckStatusTracker(info); err != nil {
				return err
			}
		case "record-links":
			if p.Document == nil {
				return fmt.Errorf("record links require a document")
			}
			if err := s.CheckRecordLinks(info, t.entity); err != nil {
				return err
			}
		case "record-view":
			if p.Document == nil {
				return fmt.Errorf("record view requires a document")
			}
			for _, name := range s.Fields {
				if err := field(name); err != nil {
					return err
				}
			}
			for _, ref := range s.Actions {
				if err := t.checkAction(p.Name, ref.Name, info.Type); err != nil {
					return err
				}
				owner := t.owner["action:"+ref.Name]
				if owner == nil {
					return fmt.Errorf("record view action owner is unavailable")
				}
				action, _ := owner.Manifest().Actions.Action(ref.Name)
				if action.New {
					return fmt.Errorf("record view only offers existing-record actions")
				}
			}
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
		case "chart", "metric", "pivot":
			if s.Mark != "" && (p.Document == nil || !platform.PageUIProfileSupports(p.Document.UIProfile, "platform.page.v2.24")) {
				return fmt.Errorf("%s: chart mark requires v2.24", where)
			}
			if s.Widget == "pivot" && p.Document == nil {
				return fmt.Errorf("%s: pivot needs a V2 document", where)
			}
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
			if s.Widget == "chart" || s.Widget == "pivot" {
				if s.Group == "" {
					return fmt.Errorf("%s: nothing to group by", where)
				}
				if err := field(s.Group); err != nil {
					return err
				}
			}
			if (s.Widget == "pivot" || s.Mark != "") && !checkAggregateSection(s, info) {
				return fmt.Errorf("%s: invalid pivot group or measure", where)
			}
		case "button", "input":
			if p.Document == nil || s.Object.Name != "" || s.Query.Name != "" || s.Selection != "" || s.ParentSelection != "" || s.Relation != "" || len(s.Fields) > 0 || len(s.Actions) > 0 || s.Text != "" || s.Function != nil || s.Operation != nil || len(s.Inputs) > 0 {
				return fmt.Errorf("%s: an interactive presentation widget requires a document and no business binding", where)
			}
		case "text":
			if strings.TrimSpace(s.Text) == "" {
				return fmt.Errorf("%s: no words to show", where)
			}
		case "filter":
			if len(s.Facets) > 0 || s.FilterSearchVariable != "" {
				if err := s.CheckFacetSchema(info); err != nil {
					return err
				}
				break
			}
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
			// so it asks for everything that action needs, except a parent
			// reference supplied by its explicitly declared relation.
			if err := t.checkAction(p.Name, info.Type+".create", info.Type); err != nil {
				return fmt.Errorf("%s: %s cannot be created here", where, info.Type)
			}
			if len(s.Fields) == 0 && boundParent == "" && len(s.Inputs) == 0 {
				return fmt.Errorf("%s: no fields to fill in", where)
			}
			for _, name := range s.Fields {
				if err := field(name); err != nil {
					return err
				}
			}
			if err := t.checkFormInputs(s, info, parentType, boundParent); err != nil {
				return fmt.Errorf("%s: %w", where, err)
			}
			for _, f := range info.Fields {
				_, bound := s.Inputs[f.Name]
				if f.Required && !f.ReadOnly && f.Name != boundParent && !bound && !slices.Contains(s.Fields, f.Name) {
					return fmt.Errorf("%s: %s needs %s, which the form does not ask for", where, info.Type, f.Name)
				}
			}
		}
	}
	for key := range parentBindings {
		seen := map[string]bool{}
		for at := key; at != ""; at = parentBindings[at] {
			if seen[at] {
				return fmt.Errorf("page %s: record selections have a parent cycle", p.Name)
			}
			seen[at] = true
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
		pageDefinition := slices.IndexFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
		if pageDefinition >= 0 {
			if err := a.CheckPageVariables(*t.definitions[pageDefinition].Page); err != nil {
				return err
			}
		}
		if pageDefinition < 0 {
			return fmt.Errorf("application %s: no page %s", a.Name, page)
		}
	}
	if err := a.CheckVariables(); err != nil {
		return err
	}
	if err := t.checkPageQueries(a.QueryPage()); err != nil {
		return err
	}
	if err := a.CheckResources(); err != nil {
		return fmt.Errorf("application %s: %w", a.Name, err)
	}
	for _, ref := range a.Resources {
		known := slices.ContainsFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref })
		if ref.Kind == platform.AssetFlow {
			known = t.procs != nil && strings.HasPrefix(ref.Name, ref.App+".") && t.procs.HasPublishedFlow(ref.Name)
		}
		if !known {
			return fmt.Errorf("application %s: no published resource %s", a.Name, ref)
		}
	}
	if err := a.CheckGroups(); err != nil {
		return fmt.Errorf("application %s: %v", a.Name, err)
	}
	installed := a
	def := platform.Definition{Ref: platform.AssetRef{App: id, Kind: platform.AssetApp, Name: a.Name}, Source: "tenant", Version: "1",
		ContractVersion: 1, Application: &installed}
	def.Requires = a.Dependencies(id)
	if i := slices.IndexFunc(t.definitions, func(x platform.Definition) bool { return x.Ref == def.Ref }); i >= 0 {
		t.definitions[i] = def
	} else {
		t.definitions = append(t.definitions, def)
	}
	slices.SortFunc(t.definitions, func(x, y platform.Definition) int { return strings.Compare(x.Ref.String(), y.Ref.String()) })
	return nil
}
