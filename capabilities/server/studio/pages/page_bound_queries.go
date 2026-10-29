package pages

import (
	"fmt"
	"platformserver/platform"
	"slices"
)

func QuerySourceDefinition(definitions []platform.Definition, binding *platform.AssetBinding) *platform.Definition {
	if binding != nil {
		for i := range definitions {
			if definitions[i].Ref == binding.Ref {
				return definitions[i].QuerySourceVersion(binding.SourceVersion)
			}
		}
	}
	return nil
}

func VisibleInterfaceQuery(p platform.Page, q platform.PageQuery, entities map[string]platform.EntityInfo, definitions []platform.Definition) *platform.Definition {
	named := QuerySourceDefinition(definitions, q.Query)
	if named == nil || named.Query == nil {
		return nil
	}
	visible, ok := VisibleQuery(*named.Query, entities)
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

func (b Bindings) CheckQueries(p platform.Page) error {
	projected, err := p.Safety()
	if err != nil {
		return err
	}
	p = projected
	for _, binding := range p.DataBindings {
		if err := p.CheckDataBinding(binding, b.Entity, b.Criteria, b.Rules); err != nil {
			return err
		}
	}
	if err := p.BindRuleTypes(b.Entity); err != nil {
		return err
	}
	if err := p.InputSchema.CheckVariables(); err != nil {
		return err
	}
	for id, c := range p.ComputeResources() {
		ref := p.RecordResourceObject(c.RecordVariable)
		object, ok := b.Entity(ref.Name)
		op, _, err := b.Operation(&c.Operation)
		if !ok || err != nil || p.CheckComputeResource(c, object, op, p.InputSchema.Variables[id].Type, b.Entity) != nil {
			return fmt.Errorf("compute resource contract is unavailable")
		}
	}
	for _, v := range p.InputSchema.Variables {
		if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
			object, ok := b.Entity(v.Source.Object.Name)
			if !ok || p.CheckPropertySchema(v, object) != nil {
				return fmt.Errorf("property source schema is unavailable")
			}
		}
		if (v.Mode == "shared" || v.Scope == "application" && (v.Type == "record" || v.Type == "filter" || v.Type == "record-set") && v.Mode == "resource") && v.Source != nil && v.Source.Object != nil {
			info, ok := b.Entity(v.Source.Object.Name)
			if v.Type == "filter" && v.Mode == "resource" && platform.CheckFilterSchema(*v.Source, info) != nil {
				return fmt.Errorf("application filter fields are unavailable")
			}
			if !ok || info.App != v.Source.Object.App {
				return fmt.Errorf("shared window object is unavailable")
			}
		}
	}
	namedSources := map[platform.AssetBinding]platform.NamedQuery{}
	for _, v := range p.InputSchema.Variables {
		if v.Mode == "aggregate" && v.Source != nil {
			q := p.InputSchema.Queries[v.Source.Query]
			object, ok := b.Entity(q.Object.Name)
			if !ok || p.InputSchema.CheckAggregateScalar(v, object) != nil {
				return fmt.Errorf("page aggregate field is unavailable")
			}
		}
	}
	for id, q := range p.InputSchema.Queries {
		named := QuerySourceDefinition(b.Definitions, q.Query)
		if q.Interface != "" {
			if err := p.CheckInterfaceQuery(q, named); err != nil {
				return fmt.Errorf("page query %s: %w", id, err)
			}
			if _, err := b.Interface(*named.Query); err != nil {
				return err
			}
			continue
		}
		object, ok := b.Entity(q.Object.Name)
		if !ok || object.App != q.Object.App {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		if named != nil && named.LinkType != nil {
			l := named.LinkType
			parent, pok := b.Entity(l.Parent.Name)
			child, cok := b.Entity(l.Child.Name)
			if !pok || !cok || l.CheckSchema(parent, child) != nil {
				return fmt.Errorf("link source schema is unavailable")
			}
		}
		if err := p.CheckBoundQuery(id, named); err != nil {
			return err
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

type Bindings struct {
	Entity      func(string) (platform.EntityInfo, bool)
	Criteria    func(platform.AssetRef) (*platform.RuleCriteria, bool)
	Rules       platform.RuleEngine
	Operation   func(*platform.AssetBinding) (platform.Operation, int, error)
	Definitions []platform.Definition
	Interface   func(platform.NamedQuery, ...platform.EntityInfo) (platform.NamedQuery, error)
}

func VisibleQuery(q platform.NamedQuery, entities map[string]platform.EntityInfo) (platform.NamedQuery, bool) {
	if q.InterfaceShape == nil {
		return q, false
	}
	empty := len(q.Implementations) == 0
	q.Implementations = slices.DeleteFunc(slices.Clone(q.Implementations), func(typ string) bool {
		info, ok := entities[typ]
		return !ok || !slices.Contains(info.Implements, q.Interface) || q.InterfaceShape.Implements(info) != nil
	})
	return q, empty || len(q.Implementations) > 0
}

func CheckModelQueries(page platform.Page, definitions []platform.Definition, entity func(string) (platform.EntityInfo, bool)) error {
	for _, section := range page.DataBindings {
		for _, m := range section.ModelBindings() {
			sets := []*platform.ModelRecordSet{m.Set}
			if m.Options != nil {
				sets = append(sets, m.Options.Set)
			}
			for _, set := range sets {
				if set == nil || set.Query == nil {
					continue
				}
				named := QuerySourceDefinition(definitions, set.Query)
				if named == nil || named.Query == nil || named.Query.Object != m.Type {
					return fmt.Errorf("section %s: retained model query type or version is unavailable", section.ID)
				}

				if named.Query.By != "" {
					root := page.RecordResourceObject(set.RecordVariable)
					info, ok := entity(m.Type)
					f, found := info.Field(named.Query.By)
					if !ok || !found || f.Type != "reference" || root.Name != f.Ref {
						return fmt.Errorf("section %s: model query needs its declared source record", section.ID)
					}
				}
			}
		}
	}
	return nil
}
