package objects

import (
	"encoding/json"
	"slices"

	"platformserver/platform"
	"platformserver/services/resources/publication"
)

type Query struct {
	platform.Record
	Name            string              `json:"name" field:"required,search"`
	Title           string              `json:"title" field:"required,search"`
	Description     string              `json:"description" field:"required" type:"longtext"`
	Object          string              `json:"object,omitempty" title:"Source object"`
	Interface       string              `json:"interface,omitempty" title:"Source interface"`
	InterfaceShape  *platform.Interface `json:"interfaceShape,omitempty" field:"readonly,aside" type:"json" title:"Published interface shape"`
	Implementations []string            `json:"implementations,omitempty" field:"readonly,aside" title:"Published implementations"`
	By              string              `json:"by,omitempty" title:"Parent reference"`
	Domain          json.RawMessage     `json:"domain,omitempty" field:"aside" type:"json"`
	Filter          *platform.Expr      `json:"filter,omitempty" field:"aside" type:"json"`
	Sort            []string            `json:"sort,omitempty" field:"aside"`
	Limit           int                 `json:"limit" field:"required"`
	State           string              `json:"state" field:"readonly" choices:"draft,published"`
	Version         int                 `json:"version,omitempty" field:"readonly"`
	Published       string              `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions        []string            `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

func (q Query) Definition() platform.NamedQuery {
	return platform.NamedQuery{Name: q.Name, Title: q.Title, Description: q.Description, Object: q.Object, Interface: q.Interface, InterfaceShape: q.InterfaceShape, Implementations: q.Implementations, By: q.By, Domain: q.Domain, Filter: q.Filter, Sort: q.Sort, Limit: q.Limit}
}

func QueryFromDeclaration(query platform.NamedQuery) Query {
	return Query{Name: query.Name, Title: query.Title, Description: query.Description, Object: query.Object, Interface: query.Interface, InterfaceShape: query.InterfaceShape, Implementations: query.Implementations, By: query.By, Domain: query.Domain, Filter: query.Filter, Sort: query.Sort, Limit: query.Limit}
}

func (q Query) family() publication.Family {
	return publication.Family{ID: q.ID, Name: q.Name, State: q.State, Version: q.Version, Published: q.Published, Versions: slices.Clone(q.Versions), Archived: q.Archived}
}

func setQueryFamily(q *Query, family publication.Family) {
	q.ID, q.Name, q.State = family.ID, family.Name, family.State
	q.Version, q.Published, q.Versions, q.Archived = family.Version, family.Published, slices.Clone(family.Versions), family.Archived
}

func (q Query) image() string {
	q.Published = ""
	q.Versions = nil
	raw, _ := json.Marshal(q)
	return string(raw)
}

func (q Query) keepsPublishedIdentity(next Query) bool {
	return q.Name == next.Name && q.Object == next.Object && q.Interface == next.Interface
}
