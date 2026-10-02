package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

// registerDefinitions indexes installed code declarations at composition. The
// entity/action owners still validate and execute them. This layer checks only
// cross-descriptor identity and references, without creating a second runtime.
func (t *Tenant) registerDefinitions() error {
	classes := map[string]bool{}
	versions := map[string]string{}
	for _, app := range t.apps {
		manifest := app.Manifest()
		versions[manifest.ID] = manifest.Version
		for _, declaration := range app.Declarations() {
			classes[declaration.GetDataClass()] = true
		}
	}
	objects := map[string]platform.AssetRef{}
	objectInfo := map[string]platform.EntityInfo{}
	for _, et := range t.records.sortedTypes() {
		info := et.info
		objects[info.Type] = platform.AssetRef{App: info.App, Kind: platform.AssetObject, Name: info.Type}
		objectInfo[info.Type] = info
	}
	seen := map[platform.AssetRef]bool{}
	add := func(def platform.Definition) error {
		if err := def.Ref.Check(); err != nil {
			return err
		}
		if seen[def.Ref] {
			return fmt.Errorf("asset %s is declared twice", def.Ref)
		}
		seen[def.Ref] = true
		t.definitions = append(t.definitions, def)
		return nil
	}
	for _, et := range t.records.sortedTypes() {
		info := et.info
		var requires []platform.AssetRef
		for _, field := range info.Fields {
			if field.Property != nil {
				requires = append(requires, field.Property.Ref)
			}
			if field.Ref == "" {
				continue
			}
			if field.Type != "reference" && field.Type != "references" {
				return fmt.Errorf("asset %s field %s has reference %s with incompatible type %s", objects[info.Type], field.Name, field.Ref, field.Type)
			}
			dep, ok := objects[field.Ref]
			if !ok {
				return fmt.Errorf("asset %s field %s requires missing object %s", objects[info.Type], field.Name, field.Ref)
			}
			requires = append(requires, dep)
		}
		if err := add(platform.Definition{Ref: objects[info.Type], Source: "code", Version: versions[info.App], ContractVersion: 1,
			Requires: uniqueRefs(requires), Entity: &info}); err != nil {
			return err
		}
	}
	actionByRef := map[platform.AssetRef]platform.Action{}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, action := range manifest.Actions.All() {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetAction, Name: action.Schema}
			var requires []platform.AssetRef
			if dep, ok := objects[action.Target]; ok {
				requires = append(requires, dep)
			} else if !classes[action.Target] {
				return fmt.Errorf("asset %s targets missing data class %s", ref, action.Target)
			}
			for _, field := range action.Payload {
				if field.Ref == "" {
					continue
				}
				if field.Type != "string" {
					return fmt.Errorf("asset %s payload %s has reference %s with incompatible type %s", ref, field.Name, field.Ref, field.Type)
				}
				dep, ok := objects[field.Ref]
				if !ok {
					return fmt.Errorf("asset %s payload %s requires missing object %s", ref, field.Name, field.Ref)
				}
				requires = append(requires, dep)
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: uniqueRefs(requires), Action: &action}); err != nil {
				return err
			}
			actionByRef[ref] = action
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, page := range manifest.Pages {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetPage, Name: page.Name}
			if page.Layout != "list-detail" && page.Layout != "composed" {
				return fmt.Errorf("asset %s has unsupported layout %q", ref, page.Layout)
			}
			if page.Document != nil && page.Layout != "composed" {
				return fmt.Errorf("asset %s: a structured layout needs composed sections", ref)
			}
			if err := page.Document.Check(page.Sections); err != nil {
				return fmt.Errorf("asset %s: %w", ref, err)
			}
			if page.Object.Kind != platform.AssetObject || objects[page.Object.Name] != page.Object {
				return fmt.Errorf("asset %s requires missing object %s", ref, page.Object)
			}
			info := objectInfo[page.Object.Name]
			if len(page.Selections) != 0 && len(page.Sections) == 0 {
				return fmt.Errorf("asset %s: selections need composed sections", ref)
			}
			if len(page.Sections) != 0 {
				if err := t.checkSections(page, info); err != nil {
					return err
				}
			} else {
				for _, fields := range [][]string{page.ListFields, page.DetailFields} {
					if len(fields) == 0 {
						return fmt.Errorf("asset %s needs list and detail fields", ref)
					}
					seenFields := map[string]bool{}
					for _, field := range fields {
						if seenFields[field] {
							return fmt.Errorf("asset %s repeats field %s", ref, field)
						}
						seenFields[field] = true
						if _, ok := info.Field(field); !ok {
							return fmt.Errorf("asset %s requires missing field %s on %s", ref, field, page.Object)
						}
					}
				}
			}
			requires := []platform.AssetRef{page.Object}
			for _, actionRef := range page.Actions {
				action, ok := actionByRef[actionRef]
				if !ok {
					return fmt.Errorf("asset %s requires missing action %s", ref, actionRef)
				}
				if action.Target != page.Object.Name {
					return fmt.Errorf("asset %s action %s targets %s, not %s", ref, actionRef, action.Target, page.Object.Name)
				}
				requires = append(requires, actionRef)
			}
			if len(uniqueRefs(requires)) != len(requires) {
				return fmt.Errorf("asset %s repeats an action", ref)
			}
			asset, err := platform.PageReleaseAsset(manifest.ID, manifest.Version, page)
			if err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: asset.Requires, Page: &page}); err != nil {
				return err
			}
		}
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, p := range manifest.PropertyTypes {
			if err := p.Check(); err != nil {
				return err
			}
			p := p
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetPropertyType, Name: p.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, PropertyType: &p}); err != nil {
				return err
			}
		}
		for _, l := range manifest.LinkTypes {
			parent, ok := t.entity(l.Parent.Name)
			if !ok {
				return fmt.Errorf("link parent missing")
			}
			child, ok := t.entity(l.Child.Name)
			if !ok || l.Child.App != manifest.ID {
				return fmt.Errorf("link child owner differs")
			}
			if err := l.CheckSchema(parent, child); err != nil {
				return err
			}
			if err := t.records.installLinkConstraint(l); err != nil {
				return err
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetLinkType, Name: l.Name}, Source: "code", Version: manifest.Version, ContractVersion: l.Contract(), Requires: uniqueRefs([]platform.AssetRef{l.Parent, l.Child}), LinkType: &l}); err != nil {
				return err
			}
		}
		for _, query := range manifest.Queries {
			q := query
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetQuery, Name: q.Name}
			object, ok := objects[q.Object]
			if !ok {
				return fmt.Errorf("asset %s reads missing object %s", ref, q.Object)
			}
			if q.By != "" {
				if f, ok := objectInfo[q.Object].Field(q.By); !ok || f.Type != "reference" {
					return fmt.Errorf("asset %s is run for %s, not a reference of %s", ref, q.By, q.Object)
				}
			}
			var terms []any
			if len(q.Domain) > 0 && json.Unmarshal(q.Domain, &terms) != nil {
				return fmt.Errorf("asset %s has a domain that is not a list of conditions", ref)
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: []platform.AssetRef{object}, Query: &q}); err != nil {
				return err
			}
		}
		for _, function := range manifest.Functions {
			f := function
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetFunction, Name: f.Name}
			if err := f.Check(); err != nil {
				return err
			}
			object, ok := objects[f.Object]
			if !ok || object.App != manifest.ID {
				return fmt.Errorf("asset %s needs its owner's source object", ref)
			}
			for _, role := range f.Roles {
				if !slices.Contains(manifest.AllRoles(), role) {
					return fmt.Errorf("asset %s names an undeclared role", ref)
				}
			}
			for _, name := range f.Fields {
				field, ok := objectInfo[f.Object].Field(name)
				if !ok || !slices.Contains([]string{"text", "longtext", "choice", "integer", "decimal", "boolean"}, field.Type) ||
					slices.Contains(t.narrowable(t.records.types[f.Object]), name) {
					return fmt.Errorf("asset %s needs direct scalar source fields", ref)
				}
			}
			if err := add(platform.Definition{Ref: ref, Source: "code", Version: manifest.Version, ContractVersion: 1,
				Requires: []platform.AssetRef{object}, Function: &f}); err != nil {
				return err
			}
		}
		for _, operation := range manifest.Operations {
			o := operation
			if err := o.Check(); err != nil {
				return fmt.Errorf("operation %s: %w", o.Name, err)
			}
			for _, role := range o.Roles {
				if !slices.Contains(manifest.AllRoles(), role) {
					return fmt.Errorf("operation %s names an undeclared role", o.Name)
				}
			}
			if o.Binding.Kind == "native" {
				if _, ok := app.(platform.OperationExecutor); !ok {
					return fmt.Errorf("operation %s has no native executor", o.Name)
				}
			}
			if err := add(platform.Definition{Ref: platform.AssetRef{App: manifest.ID, Kind: platform.AssetCompute, Name: o.Name}, Source: "code", Version: manifest.Version, ContractVersion: 1, Requires: []platform.AssetRef{}, Operation: &o}); err != nil {
				return err
			}
		}
	}
	for _, et := range t.records.sortedTypes() {
		if err := t.checkObjectProperties(et.info); err != nil {
			return err
		}
	}
	slices.SortFunc(t.definitions, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	return nil
}

func uniqueRefs(refs []platform.AssetRef) []platform.AssetRef {
	out := []platform.AssetRef{}
	for _, ref := range refs {
		if !slices.Contains(out, ref) {
			out = append(out, ref)
		}
	}
	slices.SortFunc(out, func(a, b platform.AssetRef) int { return strings.Compare(a.String(), b.String()) })
	return out
}

// Definitions serves the installed assets the member can discover. Entity
// fields and actions are filtered through the existing read/catalog paths at
// request time, so role and field changes are never cached in the registry.
func (t *Tenant) Definitions(m platform.Member) []platform.Definition {
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	actions := map[string]platform.Action{}
	for _, action := range t.Catalog(m) {
		actions[action.Schema] = action
	}
	out := []platform.Definition{}
	for _, registered := range t.definitions {
		def := registered
		switch def.Ref.Kind {
		case platform.AssetObject:
			info, ok := entities[def.Ref.Name]
			if !ok {
				continue
			}
			def.Entity = &info
		case platform.AssetAction:
			action, ok := actions[def.Ref.Name]
			if !ok {
				continue
			}
			def.Action = &action
		case platform.AssetPropertyType:
			if def.PropertyType == nil {
				continue
			}
			def.PropertyVersions = maps.Clone(def.PropertyVersions)
			visible := func(version string) bool {
				if m.Roles[def.Ref.App] == "builder" {
					return true
				}
				for _, e := range entities {
					for _, f := range e.Fields {
						if f.Property != nil && f.Property.Ref == def.Ref && f.Property.SourceVersion == version {
							return true
						}
					}
				}
				return false
			}
			for version := range def.PropertyVersions {
				if !visible(version) {
					delete(def.PropertyVersions, version)
				}
			}
			if !visible(def.Version) {
				def.PropertyType = nil
				if len(def.PropertyVersions) == 0 {
					continue
				}
			}
		case platform.AssetLinkType:
			if def.LinkType == nil {
				continue
			}
			def.LinkVersions = maps.Clone(def.LinkVersions)
			visible := func(l platform.LinkType) bool {
				parent, pok := entities[l.Parent.Name]
				child, cok := entities[l.Child.Name]
				return pok && cok && l.CheckSchema(parent, child) == nil
			}
			for version, l := range def.LinkVersions {
				if !visible(l) {
					delete(def.LinkVersions, version)
				}
			}
			if !visible(*def.LinkType) {
				def.LinkType = nil
				if len(def.LinkVersions) == 0 {
					continue
				}
			}
		case platform.AssetQuery:
			if def.Query == nil {
				continue
			}
			info, ok := entities[def.Query.Object]
			if !ok {
				continue
			}
			def.QueryVersions = maps.Clone(def.QueryVersions)
			for version, query := range def.QueryVersions {
				if checkNamedQuery(query, info) != nil {
					delete(def.QueryVersions, version)
				}
			}
			if checkNamedQuery(*def.Query, info) != nil {
				def.Query = nil
				if len(def.QueryVersions) == 0 {
					continue
				}
			}
		case platform.AssetFunction:
			if def.Function == nil || !slices.Contains(def.Function.Roles, m.Roles[def.Ref.App]) {
				continue
			}
			info, ok := entities[def.Function.Object]
			if !ok || slices.ContainsFunc(def.Function.Fields, func(name string) bool {
				_, found := info.Field(name)
				return !found
			}) {
				continue
			}
		case platform.AssetCompute:
			if def.Operation == nil || !slices.Contains(def.Operation.Roles, m.Roles[def.Ref.App]) {
				continue
			}
		case platform.AssetPage:
			if m.Roles[def.Ref.App] == "" || def.Page == nil {
				continue
			}
			page := *def.Page
			info, ok := entities[page.Object.Name]
			if !ok {
				continue
			}
			visibleFields := map[string]bool{}
			page.Selections = slices.DeleteFunc(slices.Clone(page.Selections), func(selection platform.SelectionVariable) bool {
				_, visible := entities[selection.Object.Name]
				return !visible
			})
			selections := map[string]bool{}
			for _, selection := range page.Selections {
				selections[selection.Name] = true
			}
			for _, field := range info.Fields {
				visibleFields[field.Name] = true
			}
			page.ListFields = slices.DeleteFunc(slices.Clone(page.ListFields), func(name string) bool { return !visibleFields[name] })
			page.DetailFields = slices.DeleteFunc(slices.Clone(page.DetailFields), func(name string) bool { return !visibleFields[name] })
			page.Actions = slices.DeleteFunc(slices.Clone(page.Actions), func(ref platform.AssetRef) bool { _, ok := actions[ref.Name]; return !ok })
			// A composed page's widgets are trimmed the same way: a field or an
			// action this member may not have is not on the page (ADR-0035).
			if len(page.Sections) > 0 {
				sections := make([]platform.Section, 0, len(page.Sections))
				for _, section := range page.Sections {
					if section.Selection != "" && !selections[section.Selection] || section.ParentSelection != "" && !selections[section.ParentSelection] {
						continue
					}
					shown := info
					if section.Object.Name != "" && section.Object.Name != page.Object.Name {
						other, ok := entities[section.Object.Name]
						if !ok {
							continue // its object is not this member's to read
						}
						shown = other
					}
					if (section.Widget == "chart" || section.Widget == "metric" || section.Widget == "pivot") && !checkAggregateSection(section, shown) {
						continue
					}
					if section.CheckTimeline(shown) != nil {
						continue
					}
					if section.CheckKanban(shown) != nil {
						continue
					}
					parentType := page.Object.Name
					if section.ParentSelection != "" {
						for _, selection := range page.Selections {
							if selection.Name == section.ParentSelection {
								parentType = selection.Object.Name
							}
						}
					}
					if section.Relation != "" && !slices.ContainsFunc(shown.Fields, func(f platform.FieldInfo) bool {
						return f.Type == "reference" && f.Ref == parentType && f.Inverse == section.Relation
					}) {
						continue // the member cannot follow this parent reference
					}
					if section.Widget == "form" {
						if _, offered := actions[shown.Type+".create"]; !offered {
							continue
						}
					}
					if section.Widget == "form" && slices.ContainsFunc(slices.Collect(maps.Keys(section.Inputs)), func(name string) bool {
						field, writable := shown.Field(name)
						if !writable || !field.Writes(m.Roles[shown.App]) {
							return true
						}
						binding := section.Inputs[name]
						if binding.Source != "subject" {
							return false
						}
						_, err := platform.RecordPathField(parentType, binding.Path, func(typ string) (platform.EntityInfo, bool) {
							info, visible := entities[typ]
							return info, visible
						})
						return err != nil
					}) {
						continue // no manual-input fallback for a hidden bound source
					}
					section.Fields = slices.DeleteFunc(slices.Clone(section.Fields), func(name string) bool {
						field, visible := shown.Field(name)
						return !visible || section.Widget == "form" && (field.ReadOnly || !field.Writes(m.Roles[shown.App]))
					})
					if len(section.Facets) > 0 && slices.ContainsFunc(section.Facets, func(f platform.PageFacet) bool { _, ok := shown.Field(f.Field); return !ok }) {
						continue
					}
					if section.Widget == "filter" && len(section.Fields) == 0 && len(section.Facets) == 0 && section.FilterSearchVariable == "" {
						continue
					}
					if section.InlineEdit != nil {
						edit := *section.InlineEdit
						edit.Fields = slices.DeleteFunc(slices.Clone(edit.Fields), func(name string) bool {
							f, visible := shown.Field(name)
							return !visible || f.ReadOnly || !f.Writes(m.Roles[shown.App])
						})
						if _, offered := actions[edit.Action.Name]; !offered || len(edit.Fields) == 0 {
							section.InlineEdit = nil
						} else {
							section.InlineEdit = &edit
						}
					}
					section.Actions = slices.DeleteFunc(slices.Clone(section.Actions), func(ref platform.AssetRef) bool { _, ok := actions[ref.Name]; return !ok })
					if section.Widget == "inline-action" && len(section.Actions) != 1 {
						continue
					}
					if section.Function != nil {
						owner, ok := t.app(section.Function.Ref.App).(interface {
							FunctionDefinition(string, int) (platform.AIFunction, int, bool)
						})
						if !ok {
							continue
						}
						versionText, ok := strings.CutPrefix(section.Function.SourceVersion, t.app(section.Function.Ref.App).Manifest().Version+".function-")
						version, err := strconv.Atoi(versionText)
						if !ok || err != nil {
							continue
						}
						function, _, exists := owner.FunctionDefinition(section.Function.Ref.Name, version)
						if !exists || !slices.Contains(function.Roles, m.Roles[section.Function.Ref.App]) || slices.ContainsFunc(function.Fields, func(name string) bool {
							field, found := shown.Field(name)
							return !found || !field.Reads(m.Roles[section.Function.Ref.App])
						}) {
							continue
						}
					}
					if _, creates := actions[shown.Type+".create"]; section.Widget == "form" && !creates {
						continue // a form this member could not submit is not on their page
					}
					if section.Operation != nil {
						op, _, err := t.pageOperation(section.Operation)
						if err != nil || !slices.Contains(op.Roles, m.Roles[section.Operation.Ref.App]) || slices.ContainsFunc(slices.Collect(maps.Values(section.Inputs)), func(binding platform.Binding) bool {
							if binding.Source != "subject" || len(binding.Path) == 0 {
								return false
							}
							_, ok := shown.Field(binding.Path[0])
							return !ok
						}) {
							continue
						}
					}
					sections = append(sections, section)
				}
				page.Sections = sections
				if page.Document != nil {
					doc := *page.Document
					doc.Queries = map[string]platform.PageQuery{}
					doc.Variables = maps.Clone(doc.Variables)
					for id, v := range doc.Variables {
						if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
							info, ok := entities[v.Source.Object.Name]
							if !ok || page.CheckPropertySchema(v, info) != nil {
								delete(doc.Variables, id)
							}
						}
						if v.Mode == "shared" && (v.Type == "record" || v.Type == "filter") && v.Source != nil && v.Source.Object != nil {
							if _, ok := entities[v.Source.Object.Name]; !ok {
								delete(doc.Variables, id)
							}
						}
					}
					for id, q := range page.Document.Queries {
						info, ok := entities[q.Object.Name]
						if !ok {
							continue
						}
						var named *platform.Definition
						if q.Query != nil {
							for i := range t.definitions {
								if t.definitions[i].Ref == q.Query.Ref {
									named = t.definitions[i].QuerySourceVersion(q.Query.SourceVersion)
									break
								}
							}
						}
						if named != nil && named.LinkType != nil {
							l := named.LinkType
							parent, pok := entities[l.Parent.Name]
							child, cok := entities[l.Child.Name]
							if !pok || !cok || l.CheckSchema(parent, child) != nil {
								named = nil
							}
						}
						if page.CheckQuerySchema(q, info, named) == nil && (named == nil || named.LinkType != nil || named.Query != nil && checkNamedQuery(*named.Query, info) == nil) {
							doc.Queries[id] = q
						}
					}
					page.Document = &doc
				}
				page.Document = page.Document.Visible(sections)
				// Removing a resource producer also removes dependent visibility
				// branches. Repeat to closure without exposing orphan inputs.
				if page.Document != nil {
					for {
						visible := map[string]bool{}
						for _, node := range page.Document.Nodes {
							if node.Kind == "widget" {
								visible[node.Section] = true
							}
						}
						before := len(page.Sections)
						page.Sections = slices.DeleteFunc(page.Sections, func(section platform.Section) bool { return !visible[section.ID] })
						if len(page.Sections) == before {
							break
						}
						page.Document = page.Document.Visible(page.Sections)
					}
				}
			}
			def.Requires = append([]platform.AssetRef{page.Object}, page.Actions...)
			def.Page = &page
		case platform.AssetApp:
			continue // after the pages, below: an application is offered with them
		}
		out = append(out, def)
	}
	out = filterPageNavigation(out)
	// An application is offered to whoever may open one of its pages (ADR-0036).
	opens := map[platform.AssetRef]bool{}
	for _, def := range out {
		if def.Ref.Kind == platform.AssetPage {
			opens[def.Ref] = true
		}
	}
	visibleResources := map[platform.AssetRef]bool{}
	for _, def := range out {
		visibleResources[def.Ref] = true
	}
	for _, registered := range t.definitions {
		if registered.Ref.Kind != platform.AssetApp || registered.Application == nil || m.Roles[registered.Ref.App] == "" {
			continue
		}
		def, application := registered, *registered.Application
		closed := func(name string) bool {
			return !opens[platform.AssetRef{App: def.Ref.App, Kind: platform.AssetPage, Name: name}]
		}
		application.Pages = slices.DeleteFunc(slices.Clone(application.Pages), closed)
		if len(application.Pages) == 0 {
			continue
		}
		// A heading over none of this member's pages is not in their navigation.
		groups := make([]platform.AppGroup, 0, len(application.Groups))
		for _, g := range application.Groups {
			if g.Pages = slices.DeleteFunc(slices.Clone(g.Pages), closed); len(g.Pages) > 0 {
				groups = append(groups, g)
			}
		}
		application.Groups = groups
		application.Resources = slices.DeleteFunc(slices.Clone(application.Resources), func(ref platform.AssetRef) bool {
			if ref.Kind == platform.AssetFlow {
				return m.Roles["build"] != "builder" || t.procs == nil || !t.procs.HasPublishedFlow(ref.Name)
			}
			return !visibleResources[ref]
		})
		application.Queries = maps.Clone(application.Queries)
		application.Variables = maps.Clone(application.Variables)
		for id, q := range application.Queries {
			info, ok := entities[q.Object.Name]
			var named *platform.Definition
			if q.Query != nil {
				for _, d := range out {
					if d.Ref == q.Query.Ref {
						named = d.QuerySourceVersion(q.Query.SourceVersion)
						break
					}
				}
			}
			if named != nil && named.LinkType != nil {
				l := named.LinkType
				parent, pok := entities[l.Parent.Name]
				child, cok := entities[l.Child.Name]
				if !pok || !cok || l.CheckSchema(parent, child) != nil {
					named = nil
				}
			}
			if !ok || application.QueryPage().CheckQuerySchema(q, info, named) != nil {
				delete(application.Queries, id)
			}
		}
		for changed := true; changed; {
			changed = false
			for id, q := range application.Queries {
				missing := q.MissingSetInput(application.Queries)
				for _, param := range q.Variables() {
					if _, ok := application.Variables[param]; !ok {
						missing = true
					}
				}
				if missing {
					delete(application.Queries, id)
					changed = true
				}
			}
			for id, v := range application.Variables {
				missing := false
				if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
					info, ok := entities[v.Source.Object.Name]
					_, parent := application.Variables[v.Source.Variable]
					missing = !ok || !parent || application.QueryPage().CheckPropertySchema(v, info) != nil
				}
				if v.Mode == "aggregate" && v.Source != nil {
					_, ok := application.Queries[v.Source.Query]
					missing = !ok
				}
				if v.Mode == "resource" && v.Source != nil {
					if (v.Source.Kind == "record" || v.Source.Kind == "filter") && v.Source.Object != nil {
						_, ok := entities[v.Source.Object.Name]
						missing = !ok
						if ok && v.Type == "filter" {
							source := *v.Source
							info := entities[source.Object.Name]
							source.Fields = slices.DeleteFunc(slices.Clone(source.Fields), func(name string) bool {
								field, exists := info.Field(name)
								return !exists || (field.Type != "choice" && field.Type != "boolean" && field.Type != "reference")
							})
							v.Source = &source
							application.Variables[id] = v
							missing = len(source.Fields) == 0
						}
					} else {
						_, ok := application.Queries[v.Source.Query]
						missing = !ok
					}
				}
				if v.Expression != nil {
					for _, arg := range v.Expression.Args {
						if arg.Variable != "" {
							if _, ok := application.Variables[arg.Variable]; !ok {
								missing = true
							}
						}
					}
				}
				if missing {
					delete(application.Variables, id)
					changed = true
				}
			}
		}
		def.Requires = application.Dependencies(def.Ref.App)
		def.Application = &application
		out = append(out, def)
	}
	slices.SortFunc(out, func(a, b platform.Definition) int { return strings.Compare(a.Ref.String(), b.Ref.String()) })
	for i := range out {
		if out[i].Requires == nil {
			out[i].Requires = []platform.AssetRef{}
		}
	}
	return out
}
