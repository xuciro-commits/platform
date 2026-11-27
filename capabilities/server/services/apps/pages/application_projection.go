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
	roleSet := m.RolesFor(def.Ref.App, def.Ref.Name+".")
	groupAllowed := func(roles []string) bool {
		if len(roles) == 0 {
			return true
		}
		for _, role := range roleSet {
			if slices.Contains(roles, role) {
				return true
			}
		}
		return false
	}
	hiddenByGroup := map[string]bool{}
	shownByGroup := map[string]bool{}
	var markHidden func(g platform.AppGroup)
	markHidden = func(g platform.AppGroup) {
		for _, name := range g.Pages {
			hiddenByGroup[name] = true
		}
		for _, entry := range g.Entries {
			if (entry.Kind == "" || entry.Kind == "page") && entry.Page != "" {
				hiddenByGroup[entry.Page] = true
			}
		}
		for _, sub := range g.Groups {
			markHidden(sub)
		}
	}
	var projectGroup func(g platform.AppGroup) (platform.AppGroup, bool)
	projectGroup = func(g platform.AppGroup) (platform.AppGroup, bool) {
		if !groupAllowed(g.Roles) {
			markHidden(g)
			return platform.AppGroup{}, false
		}
		g.Pages = slices.DeleteFunc(slices.Clone(g.Pages), closed)
		for _, name := range g.Pages {
			shownByGroup[name] = true
		}
		entries := make([]platform.AppMenuEntry, 0, len(g.Entries))
		for _, entry := range g.Entries {
			switch entry.Kind {
			case "", "page":
				if entry.Page != "" && !closed(entry.Page) {
					shownByGroup[entry.Page] = true
					entries = append(entries, entry)
				}
			case "report":
				if _, ok := entities[entry.Object]; ok {
					entries = append(entries, entry)
				}
			case "dashboard":
				if len(application.Dashboard) > 0 {
					entries = append(entries, entry)
				}
			case "note":
				entries = append(entries, entry)
			}
		}
		g.Entries = entries
		subs := make([]platform.AppGroup, 0, len(g.Groups))
		for _, sub := range g.Groups {
			if projected, ok := projectGroup(sub); ok {
				subs = append(subs, projected)
			}
		}
		g.Groups = subs
		if len(g.Pages) == 0 && len(g.Entries) == 0 && len(g.Groups) == 0 {
			return platform.AppGroup{}, false
		}
		return g, true
	}
	groups := make([]platform.AppGroup, 0, len(application.Groups))
	for _, g := range application.Groups {
		if projected, ok := projectGroup(g); ok {
			groups = append(groups, projected)
		}
	}
	application.Groups = groups
	application.Pages = slices.DeleteFunc(application.Pages, func(name string) bool { return hiddenByGroup[name] && !shownByGroup[name] })
	if len(application.Pages) == 0 && len(application.Groups) == 0 && len(application.Resources) == 0 && registered.Source != "code" {
		return platform.Definition{}, false
	}
	application.Header = VisibleApplicationHeader(application)
	application.Resources = slices.DeleteFunc(slices.Clone(application.Resources), func(ref platform.AssetRef) bool {
		if ref.Kind == platform.AssetFlow {
			return !m.Holds("build", "builder") || !projection.PublishedFlows[ref]
		}
		return !visibleResources[ref]
	})
	if len(application.Pages) == 0 && len(application.Groups) == 0 && len(application.Resources) == 0 && registered.Source != "code" {
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
