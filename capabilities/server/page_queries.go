package platformserver

import (
	"fmt"
	"platformserver/platform"
)

func pageNamedQuery(definitions []platform.Definition, binding *platform.AssetBinding) *platform.Definition {
	if binding != nil {
		for i := range definitions {
			if definitions[i].Ref == binding.Ref {
				return definitions[i].QuerySourceVersion(binding.SourceVersion)
			}
		}
	}
	return nil
}

func visiblePageInterfaceQuery(p platform.Page, q platform.PageQuery, entities map[string]platform.EntityInfo, definitions []platform.Definition) *platform.Definition {
	named := pageNamedQuery(definitions, q.Query)
	if named == nil || named.Query == nil {
		return nil
	}
	visible, ok := visibleInterfaceQuery(*named.Query, entities)
	if !ok {
		return nil
	}
	selected := *named
	selected.Query = &visible
	if p.CheckInterfaceQuery(q, &selected) != nil {
		return nil
	}
	return &selected
}

func (t *Tenant) checkPageQueries(p platform.Page) error {
	if err := p.CheckCollectionPorts(); err != nil {
		return err
	}
	for _, section := range p.Sections {
		if (section.CollectionVariable != "" && (section.Widget == "chart" || section.Widget == "metric")) || (section.Widget == "pivot" || section.Widget == "heatmap") || section.Mark != "" {
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
	for id, c := range p.ComputeResources() {
		ref := p.RecordResourceObject(c.RecordVariable)
		object, ok := t.entity(ref.Name)
		op, _, err := t.pageOperation(&c.Operation)
		if !ok || err != nil || p.CheckComputeResource(c, object, op, p.Document.Variables[id].Type) != nil {
			return fmt.Errorf("compute resource contract is unavailable")
		}
	}
	for _, v := range p.Document.Variables {
		if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
			object, ok := t.entity(v.Source.Object.Name)
			if !ok || p.CheckPropertySchema(v, object) != nil {
				return fmt.Errorf("property source schema is unavailable")
			}
		}
		if (v.Mode == "shared" || v.Scope == "application" && (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && v.Mode == "resource") && v.Source != nil && v.Source.Object != nil {
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
	for _, v := range p.Document.Variables {
		if v.Mode == "aggregate" && v.Source != nil {
			q := p.Document.Queries[v.Source.Query]
			object, ok := t.entity(q.Object.Name)
			if !ok || p.Document.CheckAggregateScalar(v, object) != nil {
				return fmt.Errorf("page aggregate field is unavailable")
			}
		}
	}
	for id, q := range p.Document.Queries {
		named := pageNamedQuery(t.definitions, q.Query)
		if q.Interface != "" {
			if err := p.CheckInterfaceQuery(q, named); err != nil {
				return fmt.Errorf("page query %s: %w", id, err)
			}
			if _, err := t.bindInterfaceQuery(*named.Query); err != nil {
				return err
			}
			continue
		}
		object, ok := t.entity(q.Object.Name)
		if !ok || object.App != q.Object.App {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		if named != nil && named.LinkType != nil {
			l := named.LinkType
			parent, pok := t.entity(l.Parent.Name)
			child, cok := t.entity(l.Child.Name)
			if !pok || !cok || l.CheckSchema(parent, child) != nil {
				return fmt.Errorf("link source schema is unavailable")
			}
		}
		if err := p.CheckRecordPickerQuery(id, named); err != nil {
			return err
		}
		if err := p.CheckObservationQuery(id, named); err != nil {
			return err
		}
		if err := p.CheckRecordWorkQuery(id, named); err != nil {
			return err
		}
		if err := p.CheckAnalysisQuery(id, named); err != nil {
			return err
		}
		if err := p.CheckResourceListQuery(id, named); err != nil {
			return err
		}
		if err := p.CheckAvatarQuery(id, named, object); err != nil {
			return err
		}
		if err := p.CheckLeaderboardQuery(id, named); err != nil {
			return err
		}
		if err := p.CheckQuerySchema(q, object, named); err != nil {
			return fmt.Errorf("page query %s: %w", id, err)
		}
		if q.Query != nil && named != nil && named.Query != nil {
			namedSources[*q.Query] = *named.Query
		}
	}
	for _, s := range p.Sections {
		if q, ok := p.InterfaceQueryForSection(s); ok {
			if err := p.CheckInterfaceSection(s, pageNamedQuery(t.definitions, q.Query)); err != nil {
				return err
			}
		}
	}
	return p.CheckQuerySetConditions(namedSources)
}
