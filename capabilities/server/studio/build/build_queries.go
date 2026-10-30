package build

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
	"reflect"
)

const SchemaQuery = QueryType + ".publish"

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
	Sort            []string            `json:"sort,omitempty" field:"aside"`
	Limit           int                 `json:"limit" field:"required"`
	State           string              `json:"state" field:"readonly" choices:"draft,published"`
	Version         int                 `json:"version,omitempty" field:"readonly"`
	Published       string              `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
	Versions        []string            `json:"versions,omitempty" field:"readonly" title:"Published versions"`
}

func (f Query) Definition() platform.NamedQuery {
	return platform.NamedQuery{Name: f.Name, Title: f.Title, Description: f.Description, Object: f.Object, Interface: f.Interface, InterfaceShape: f.InterfaceShape, Implementations: f.Implementations, By: f.By, Domain: f.Domain, Sort: f.Sort, Limit: f.Limit}
}

func (b *Build) queryDraftObjects() []platform.EntityInfo {
	var out []platform.EntityInfo
	for _, info := range b.jointEntities {
		out = append(out, info)
	}
	return out
}

func validateQuery(b *Build, f Query) (Query, error) {
	if f.Interface != "" {
		q := f.Definition()
		q.InterfaceShape, q.Implementations = nil, nil
		bound, err := b.host.BindInterfaceQuery(q, b.queryDraftObjects()...)
		if err != nil {
			return f, err
		}
		f.InterfaceShape, f.Implementations = bound.InterfaceShape, bound.Implementations
	}
	if !Named(f.Name) {
		return f, fmt.Errorf("The query name %s must be lower-case letters and digits", f.Name)
	}
	shape, selected := b.jointEntities[f.Object]
	declared, native := b.conditionLookup(f.Object)
	if _, ok := b.installed[f.Object]; f.Interface == "" && !ok && (!selected || shape.App != ID) && (!native || declared.App != ID) {
		return f, fmt.Errorf("Query %s needs a published object of this builder", f.Name)
	}
	var validation error
	if f.Interface == "" && selected {
		validation = f.Definition().Check()
	} else if f.Interface != "" && b.jointMode {
		_, validation = b.host.BindInterfaceQuery(f.Definition(), b.queryDraftObjects()...)
	} else {
		validation = b.host.ValidateInstallQuery(f.Definition())
	}
	return f, validation
}

var queryKind = resourceKind[Query, platform.NamedQuery]{
	asset: platform.AssetQuery, typ: QueryType, prefix: "query", noun: "query", title: "Query", plural: "Queries", capability: "queries", joint: true,
	description: "A reusable record query over a published object or interface; draft edits do not change published bindings.",
	transition:  "Install this record query as its next retained version.",
	immutable:   "A published query keeps its name and source object",
	records:     func(b *Build) map[string]Query { return b.queries },
	validate:    validateQuery,
	identity: func(a, b Query) bool {
		return a.Name == b.Name && a.Object == b.Object && a.Interface == b.Interface
	},
	fromDeclaration: func(q platform.NamedQuery) Query {
		return Query{Name: q.Name, Title: q.Title, Description: q.Description, Object: q.Object, Interface: q.Interface, InterfaceShape: q.InterfaceShape, Implementations: q.Implementations, By: q.By, Domain: q.Domain, Sort: q.Sort, Limit: q.Limit}
	},
	install: func(h platform.BuildHost, c platform.Caller, d platform.NamedQuery, version int) error {
		return h.InstallQuery(c, d, version)
	},
	validateInstall: func(h platform.BuildHost, d platform.NamedQuery) error { return h.ValidateInstallQuery(d) },
}

func (b *Build) QueryDefinition(name string, version int) (platform.NamedQuery, int, bool) {
	return queryKind.definition(b, name, version)
}

func (b *Build) checkRecordSets(o Object, engines ...platform.RuleEngine) error {
	if len(o.Sets) == 0 {
		return nil
	}
	info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return b.conditionLookup(typ)
	}
	if err := platform.CheckRecordSets(info, lookup, engines...); err != nil {
		return err
	}
	for _, set := range o.Sets {
		if set.Binding != nil {
			reader, ok := b.host.(platform.RecordSetHost)
			if !ok {
				return fmt.Errorf("set %s needs a typed host binding", set.Name)
			}
			if err := reader.ValidateRecordSet(info, set); err != nil {
				return fmt.Errorf("set %s: %w", set.Name, err)
			}
		}
	}
	return nil
}

const QueryType = "build.query"
