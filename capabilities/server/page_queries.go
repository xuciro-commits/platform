package platformserver

import (
	"fmt"
	"platformserver/platform"
)

func (t *Tenant) checkPageQueries(p platform.Page) error {
	if err := p.CheckCollectionPorts(); err != nil {
		return err
	}
	for _, section := range p.Sections {
		if section.CollectionVariable != "" && (section.Widget == "chart" || section.Widget == "metric") {
			object := section.Object.Name
			if object == "" {
				object = p.Object.Name
			}
			info, ok := t.entity(object)
			if !ok || !checkAggregateSection(section, info) {
				return fmt.Errorf("aggregate collection fields are unavailable or incompatible")
			}
		}
	}
	if p.Document == nil {
		return nil
	}
	for _, v := range p.Document.Variables {
		if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
			object, ok := t.entity(v.Source.Object.Name)
			if !ok || p.CheckPropertySchema(v, object) != nil {
				return fmt.Errorf("property source schema is unavailable")
			}
		}
		if (v.Mode == "shared" || v.Scope == "application" && (v.Type == "record" || v.Type == "filter") && v.Mode == "resource") && v.Source != nil && v.Source.Object != nil {
			info, ok := t.entity(v.Source.Object.Name)
			if v.Type == "filter" && v.Mode == "resource" && platform.CheckFilterSchema(*v.Source, info) != nil {
				return fmt.Errorf("application filter fields are unavailable")
			}
			if !ok || info.App != v.Source.Object.App {
				return fmt.Errorf("shared window object is unavailable")
			}
		}
	}
	namedSources := map[platform.AssetBinding]platform.NamedQuery{}
	for id, q := range p.Document.Queries {
		object, ok := t.entity(q.Object.Name)
		if !ok || object.App != q.Object.App {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		var named *platform.Definition
		if q.Query != nil {
			for i := range t.definitions {
				if t.definitions[i].Ref == q.Query.Ref {
					named = t.definitions[i].QueryVersion(q.Query.SourceVersion)
					break
				}
			}
		}
		if err := p.CheckQuerySchema(q, object, named); err != nil {
			return fmt.Errorf("page query %s: %w", id, err)
		}
		if q.Query != nil && named != nil && named.Query != nil {
			namedSources[*q.Query] = *named.Query
		}
	}
	return p.CheckQuerySetConditions(namedSources)
}
