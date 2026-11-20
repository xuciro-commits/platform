package pages

import (
	"maps"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

func ProjectApplication(registered platform.Definition, projection publication.ApplicationProjection) (platform.Definition, bool) {
	m, visible, entities := projection.Member, projection.Definitions, projection.Entities
	opens, visibleResources := projection.Pages, projection.Resources
	if registered.Ref.Kind != platform.AssetApp || registered.Application == nil || len(m.RolesFor(registered.Ref.App, registered.Ref.Name+".")) == 0 {
		return platform.Definition{}, false
	}
	def, application := registered, *registered.Application
	closed := func(name string) bool {
		return !opens[platform.AssetRef{App: def.Ref.App, Kind: platform.AssetPage, Name: name}]
	}
	application.Pages = slices.DeleteFunc(slices.Clone(application.Pages), closed)
	if len(application.Pages) == 0 && len(application.Resources) == 0 && registered.Source != "code" {
		return platform.Definition{}, false
	}
	groups := make([]platform.AppGroup, 0, len(application.Groups))
	for _, g := range application.Groups {
		if g.Pages = slices.DeleteFunc(slices.Clone(g.Pages), closed); len(g.Pages) > 0 {
			groups = append(groups, g)
		}
	}
	application.Groups = groups
	application.Header = VisibleApplicationHeader(application)
	application.Resources = slices.DeleteFunc(slices.Clone(application.Resources), func(ref platform.AssetRef) bool {
		if ref.Kind == platform.AssetFlow {
			return !m.Holds("build", "builder") || !projection.PublishedFlows[ref]
		}
		return !visibleResources[ref]
	})
	if len(application.Pages) == 0 && len(application.Resources) == 0 && registered.Source != "code" {
		return platform.Definition{}, false
	}
	application.Replacements = slices.DeleteFunc(slices.Clone(application.Replacements), func(replacement platform.ApplicationPageReplacement) bool {
		return !slices.Contains(application.Pages, replacement.Page)
	})
	application.Queries = maps.Clone(application.Queries)
	application.Variables = maps.Clone(application.Variables)
	for id, q := range application.Queries {
		info, ok := entities[q.Object.Name]
		var named *platform.Definition
		if q.Query != nil {
			for _, d := range visible {
				if d.Ref == q.Query.Ref {
					named = projection.Resolver.QuerySourceVersion(d, q.Query.SourceVersion)
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
		if !ok || CheckQuerySchema(ApplicationPage(application), q, info, named) != nil {
			delete(application.Queries, id)
		}
	}
	for changed := true; changed; {
		changed = false
		for id, q := range application.Queries {
			missing := MissingQuerySetInput(q, application.Queries)
			for _, param := range QueryVariables(q) {
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
				missing = !ok || !parent || CheckPropertySchema(ApplicationPage(application), v, info) != nil
			}
			if v.Mode == "aggregate" && v.Source != nil {
				_, ok := application.Queries[v.Source.Query]
				missing = !ok
			}
			if v.Mode == "resource" && v.Source != nil {
				if (v.Source.Kind == "record" || v.Source.Kind == "filter" || v.Source.Kind == "record-set") && v.Source.Object != nil {
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
			if missing {
				delete(application.Variables, id)
				changed = true
			}
		}
	}
	def.Application = &application
	return def, true
}
