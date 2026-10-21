package platformserver

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

// A page projection receives already authorized entities and actions. Only
// retained function/operation reads remain with their original host owners;
// the projection cannot submit, query arbitrary records or reach Tenant.
type pageDefinitionProjection struct {
	member      platform.Member
	entities    map[string]platform.EntityInfo
	actions     map[string]platform.Action
	definitions []platform.Definition
	sources     pageDefinitionSources
}

type pageDefinitionSources interface {
	function(platform.AssetBinding) (platform.AIFunction, bool)
	operation(*platform.AssetBinding) (platform.Operation, bool)
}

type retainedPageDefinitionSources struct{ tenant *Tenant }

func (s retainedPageDefinitionSources) function(binding platform.AssetBinding) (platform.AIFunction, bool) {
	app := s.tenant.app(binding.Ref.App)
	owner, ok := app.(platform.FunctionDefinitions)
	if !ok {
		return platform.AIFunction{}, false
	}
	text, ok := strings.CutPrefix(binding.SourceVersion, app.Manifest().Version+".function-")
	version, err := strconv.Atoi(text)
	if !ok || err != nil {
		return platform.AIFunction{}, false
	}
	value, _, exists := owner.FunctionDefinition(binding.Ref.Name, version)
	return value, exists
}
func (s retainedPageDefinitionSources) operation(binding *platform.AssetBinding) (platform.Operation, bool) {
	value, _, err := s.tenant.pageOperation(binding)
	return value, err == nil
}

// page removes unreadable fields, actions and dependent widgets from a copy.
// Retained values are requested only after the original visibility filters,
// preserving the permission boundary and lookup order for hidden sections.
func (projection pageDefinitionProjection) page(def platform.Definition) (platform.Definition, bool) {
	m, entities, actions, registeredDefinitions := projection.member, projection.entities, projection.actions, projection.definitions
	if m.Roles[def.Ref.App] == "" || def.Page == nil {
		return def, false
	}
	page := *def.Page
	digest, err := platform.PageContentVersion(page)
	if err != nil {
		return def, false
	}
	def.ContentVersion = digest
	info, ok := entities[page.Object.Name]
	if !ok {
		return def, false
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
	// action this member may not have is not on the page.
	if len(page.Sections) > 0 {
		sections := make([]platform.Section, 0, len(page.Sections))
		for _, section := range page.Sections {
			section = page.EffectiveModelSection(section, func(typ string) (platform.EntityInfo, bool) { info, ok := entities[typ]; return info, ok })
			if q, ok := page.InterfaceQueryForSection(section); ok {
				named := visiblePageInterfaceQuery(page, q, entities, registeredDefinitions)
				if named != nil && page.CheckInterfaceSection(section, named) == nil {
					sections = append(sections, section)
				}
				continue
			}
			if slices.ContainsFunc(section.ServiceDependencies(), func(ref platform.AssetRef) bool {
				if ref.Kind != platform.AssetObject {
					return false // write permission does not control discovery of a readable service
				}
				service, visible := entities[ref.Name]
				return !visible || service.App != ref.App
			}) {
				continue
			}
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
			shown = platform.ModelFields(shown, section.Model, func(typ string) (platform.EntityInfo, bool) {
				value, ok := entities[typ]
				return platform.ConstraintEntity(value), ok
			})
			section.Fields = slices.DeleteFunc(slices.Clone(section.Fields), func(name string) bool {
				field, visible := shown.Field(name)
				return !visible || section.Widget == "form" && (field.ReadOnly || !field.WritesAny(m.RolesIn(shown.App)))
			})
			section.Actions = slices.DeleteFunc(slices.Clone(section.Actions), func(ref platform.AssetRef) bool { _, ok := actions[ref.Name]; return !ok })
			if (section.Widget == "chart" || section.Widget == "metric" || (section.Widget == "pivot" || section.Widget == "heatmap")) && !checkAggregateSection(section, shown) {
				continue
			}
			if section.Avatar != nil {
				avatar := *section.Avatar
				avatar.DetailFields = slices.DeleteFunc(slices.Clone(avatar.DetailFields), func(name string) bool { _, ok := shown.Field(name); return name != "id" && !ok })
				section.Avatar = &avatar
			}
			if section.CheckResourceList(shown) != nil {
				continue
			}
			projected, ok := page.ProjectExploration(section, func(ref platform.AssetRef) (platform.EntityInfo, bool) {
				e, ok := entities[ref.Name]
				return e, ok && e.App == ref.App
			}, func(b platform.AssetBinding) (platform.LinkType, bool) {
				for _, d := range registeredDefinitions {
					if d.Ref == b.Ref {
						if selected := d.LinkVersion(b.SourceVersion); selected != nil && selected.LinkType != nil {
							return *selected.LinkType, true
						}
					}
				}
				return platform.LinkType{}, false
			})
			if !ok {
				continue
			}
			section = projected
			if section.CheckContextViews(shown) != nil {
				continue
			}
			if section.Widget == "action-table" {
				if len(section.Actions) != 1 {
					continue
				}
				a, offered := actions[section.Actions[0].Name]
				if !offered || section.CheckActionTable(shown, a) != nil {
					continue
				}
			}
			if page.CheckObservation(section, func(ref platform.AssetRef) (platform.EntityInfo, bool) {
				e, ok := entities[ref.Name]
				return e, ok && e.App == ref.App
			}) != nil {
				continue
			}
			if page.CheckSectionFields(section, shown) != nil {
				if section.Analysis != nil && section.Analysis.Kind == "record-axes" && page.Document != nil {
					doc := *page.Document
					doc.Variables = maps.Clone(doc.Variables)
					delete(doc.Variables, section.AnalysisXVariable)
					delete(doc.Variables, section.AnalysisYVariable)
					page.Document = &doc
				}
				continue
			}
			if section.CheckCollectionBuilderFields(shown) != nil || page.CheckSceneBinding(section, func(ref platform.AssetRef) (platform.EntityInfo, bool) {
				actual, ok := entities[ref.Name]
				return actual, ok && actual.App == ref.App
			}) != nil {
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
				if !writable || !field.WritesAny(m.RolesIn(shown.App)) {
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
			if section.Widget == "record-links" {
				section.RecordLinks = slices.DeleteFunc(slices.Clone(section.RecordLinks), func(group platform.PageRecordLink) bool {
					probe := section
					probe.RecordLinks = []platform.PageRecordLink{group}
					return probe.CheckRecordLinks(shown, func(typ string) (platform.EntityInfo, bool) { info, ok := entities[typ]; return info, ok }) != nil
				})
				if len(section.RecordLinks) == 0 {
					continue
				}
			}
			section.TableColumns = slices.DeleteFunc(slices.Clone(section.TableColumns), func(c platform.PageTableColumn) bool {
				return c.Field != "id" && !slices.Contains(section.Fields, c.Field)
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
					return !visible || f.ReadOnly || !f.WritesAny(m.RolesIn(shown.App))
				})
				if _, offered := actions[edit.Action.Name]; !offered || len(edit.Fields) == 0 {
					section.InlineEdit = nil
				} else {
					section.InlineEdit = &edit
				}
			}
			if section.Widget == "inline-action" && len(section.Actions) == 1 && len(section.ActionDefaults) > 0 {
				a, ok := actions[section.Actions[0].Name]
				if !ok || section.CheckInlineAction(shown, a) != nil {
					continue
				}
			}
			if section.Widget == "inline-action" && len(section.Actions) != 1 {
				continue
			}
			if section.Function != nil {
				function, exists := projection.sources.function(*section.Function)
				if !exists || !m.May(section.Function.Ref.App, function.Name, function.Roles).Allow || slices.ContainsFunc(function.Fields, func(name string) bool {
					field, found := shown.Field(name)
					return !found || !field.ReadsAny(m.RolesIn(section.Function.Ref.App))
				}) {
					continue
				}
			}
			if section.Operation != nil {
				op, available := projection.sources.operation(section.Operation)
				if !available || !m.May(section.Operation.Ref.App, op.Name, op.Roles).Allow || slices.ContainsFunc(slices.Collect(maps.Values(section.Inputs)), func(binding platform.Binding) bool {
					if binding.Source != "subject" || len(binding.Path) == 0 {
						return false
					}
					_, ok := shown.Field(binding.Path[0])
					return !ok
				}) {
					continue
				}
			}
			sections = append(sections, platform.CanonicalModelSection(section))
		}
		page.Sections = sections
		if page.Document != nil {
			doc := *page.Document
			doc.Queries = map[string]platform.PageQuery{}
			doc.Variables = maps.Clone(doc.Variables)
			for id, v := range doc.Variables {
				if c := v.Source; c != nil && c.Compute != nil {
					ref := page.RecordResourceObject(c.Compute.RecordVariable)
					object, ok := entities[ref.Name]
					op, available := projection.sources.operation(&c.Compute.Operation)
					if !ok || !available || !m.May(c.Compute.Operation.Ref.App, op.Name, op.Roles).Allow || page.CheckComputeResource(*c.Compute, object, op, v.Type, func(typ string) (platform.EntityInfo, bool) { v, ok := entities[typ]; return v, ok }) != nil {
						delete(doc.Variables, id)
						continue
					}
				}
				if v.Mode == "aggregate" && v.Source != nil {
					q := doc.Queries[v.Source.Query]
					if q.Object.Name == "" {
						q = page.Document.Queries[v.Source.Query]
					}
					info, ok := entities[q.Object.Name]
					if !ok || doc.CheckAggregateScalar(v, info) != nil {
						delete(doc.Variables, id)
					}
				}
				if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
					info, ok := entities[v.Source.Object.Name]
					if !ok || page.CheckPropertySchema(v, info) != nil {
						delete(doc.Variables, id)
					}
				}
				if v.Mode == "shared" && (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && v.Source != nil && v.Source.Object != nil {
					if _, ok := entities[v.Source.Object.Name]; !ok {
						delete(doc.Variables, id)
					}
				}
			}
			for id, q := range page.Document.Queries {
				if q.Interface != "" {
					if visiblePageInterfaceQuery(page, q, entities, registeredDefinitions) != nil {
						doc.Queries[id] = q
					}
					continue
				}
				info, ok := entities[q.Object.Name]
				if !ok {
					continue
				}
				var named *platform.Definition
				if q.Query != nil {
					for i := range registeredDefinitions {
						if registeredDefinitions[i].Ref == q.Query.Ref {
							named = registeredDefinitions[i].QuerySourceVersion(q.Query.SourceVersion)
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
				if page.CheckResourceListQuery(id, named) == nil && page.CheckAvatarQuery(id, named, info) == nil && page.CheckQuerySchema(q, info, named) == nil && (named == nil || named.LinkType != nil || named.Query != nil && checkNamedQuery(*named.Query, info) == nil) {
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
	if page.Document != nil {
		for i, s := range page.Sections {
			if s.Widget == "button-group" {
				page.Sections[i].Buttons = slices.DeleteFunc(slices.Clone(s.Buttons), func(b platform.PageButton) bool {
					return !slices.ContainsFunc(page.Document.Events, func(e platform.PageEventBinding) bool { return e.Source == s.ID && e.Control == b.ID })
				})
			}
		}
	}
	def.Page = &page
	return def, true
}
