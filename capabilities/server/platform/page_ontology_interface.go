package platform

import (
	"encoding/json"
	"fmt"
	"slices"
)

func frozenPageInterfaceQuery(q PageQuery, lookup map[AssetRef]ReleaseAsset) (*Definition, error) {
	if q.Query == nil {
		return nil, fmt.Errorf("interface page query needs a published binding")
	}
	asset, ok := lookup[q.Query.Ref]
	var named NamedQuery
	if !ok || asset.SourceVersion != q.Query.SourceVersion || json.Unmarshal(asset.Body, &named) != nil || named.InterfaceShape == nil || named.Check() != nil {
		return nil, fmt.Errorf("frozen interface query version or shape is unavailable")
	}
	for _, ref := range named.Dependencies() {
		implementation, exists := lookup[ref]
		info, err := queryObjectDescriptor(implementation.Body)
		if !exists || err != nil || info.Type != ref.Name || info.App != ref.App || !slices.Contains(info.Implements, named.Interface) || named.InterfaceShape.Implements(info) != nil {
			return nil, fmt.Errorf("frozen interface query implementation %s is unavailable", ref.Name)
		}
	}
	return &Definition{Ref: asset.Ref, Version: asset.SourceVersion, Query: &named}, nil
}

// Interface windows are published read contracts. They never supply an object
// identity: the selected record keeps the implementation's own type and ID.
func (p Page) WindowVariableInterface(variable string) string {
	v := p.DocumentVariables()[variable]
	if p.Document == nil || v.Mode != "resource" || v.Type != "object-set" || v.Source == nil || v.Source.Kind != "plan" {
		return ""
	}
	return p.Document.Queries[v.Source.Query].Interface
}

func (p Page) RecordVariableInterface(variable string) string {
	v := p.DocumentVariables()[variable]
	if v.Type != "record" || v.Mode != "resource" || v.Source == nil || v.Source.Kind != "record" || v.Source.Port != "" {
		return ""
	}
	for _, s := range p.Sections {
		if s.ID == v.Source.Section && s.Widget == "record-picker" {
			return p.WindowVariableInterface(s.CollectionVariable)
		}
	}
	return ""
}

func (p Page) SectionInterface(s Section) string {
	if name := p.WindowVariableInterface(s.CollectionVariable); name != "" {
		return name
	}
	return p.RecordVariableInterface(s.RecordVariable)
}

// InterfaceQueryForSection resolves through the original resource producer,
// rather than looking up the latest query or inventing an entity descriptor.
func (p Page) InterfaceQueryForSection(s Section) (PageQuery, bool) {
	if p.Document == nil {
		return PageQuery{}, false
	}
	variable := s.CollectionVariable
	if variable == "" {
		v := p.Document.Variables[s.RecordVariable]
		if v.Source != nil {
			for _, producer := range p.Sections {
				if producer.ID == v.Source.Section && producer.Widget == "record-picker" {
					variable = producer.CollectionVariable
				}
			}
		}
	}
	v := p.Document.Variables[variable]
	if v.Source == nil || v.Source.Kind != "plan" {
		return PageQuery{}, false
	}
	q, ok := p.Document.Queries[v.Source.Query]
	return q, ok && q.Interface != ""
}

func (p Page) CheckInterfaceQuery(q PageQuery, named *Definition) error {
	if q.Query == nil || named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.Query == nil || named.Query.Interface != q.Interface || named.Query.InterfaceShape == nil || named.Query.Check() != nil {
		return fmt.Errorf("interface page query needs its exact published query and frozen shape")
	}
	if q.Object != (AssetRef{}) || q.For != nil || q.Input != "" || q.Set != nil || q.Direction != "" || q.ItemOwner != "" || len(q.Conditions) != 0 {
		return fmt.Errorf("interface page query cannot replace the published source or conditions")
	}
	for _, sort := range q.Sort {
		field := sort
		if len(field) > 0 && field[0] == '-' {
			field = field[1:]
		}
		if field != "id" && field != "created" && field != "changed" && !slices.ContainsFunc(named.Query.InterfaceShape.Fields, func(f InterfaceField) bool { return f.Name == field }) {
			return fmt.Errorf("interface query sort needs a common field")
		}
	}
	ordering := named.Query.Sort
	if len(ordering) == 0 {
		ordering = []string{"id"}
	}
	if len(q.Sort) > 0 && !slices.Equal(ordering, q.Sort) {
		return fmt.Errorf("the published interface query owns its ordering")
	}
	if named.Query.Limit > 0 && q.Limit > named.Query.Limit {
		return fmt.Errorf("interface page window exceeds its published query limit")
	}
	if q.Search != nil {
		typ := pageLiteralType(q.Search.Literal)
		if q.Search.Variable != "" && p.Document != nil {
			typ = p.Document.Variables[q.Search.Variable].Type
		}
		if typ != "string" {
			return fmt.Errorf("interface query search requires text")
		}
	}
	return nil
}

// CheckInterfaceSection checks only common fields. The record card can use
// the original implementation's renderer after a separate authorized get.
func (p Page) CheckInterfaceSection(s Section, named *Definition) error {
	q, ok := p.InterfaceQueryForSection(s)
	if !ok || p.CheckInterfaceQuery(q, named) != nil {
		return fmt.Errorf("interface widget source is unavailable")
	}
	if err := p.CheckCollectionPorts(); err != nil {
		return err
	}
	fields := EntityInfo{} // schema-only; no type, owner, storage, or action identity
	for _, f := range named.Query.InterfaceShape.Fields {
		fields.Fields = append(fields.Fields, FieldInfo{Name: f.Name, Type: f.Type, Title: f.Title})
	}
	switch s.Widget {
	case "record-picker":
		if err := s.CheckRecordPicker(fields); err != nil {
			return err
		}
		if len(named.Query.Sort) > 0 && !slices.Equal(named.Query.Sort, []string{"id"}) {
			return fmt.Errorf("named query owns an incompatible record picker ordering")
		}
	case "record-card":
		if p.RecordVariableInterface(s.RecordVariable) != q.Interface {
			return fmt.Errorf("interface card differs from its original record producer")
		}
		if err := s.CheckRecordCard(fields); err != nil {
			return err
		}
	default:
		return fmt.Errorf("widget %s cannot bind an interface record window", s.Widget)
	}
	return nil
}
