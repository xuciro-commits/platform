package pages

import (
	"fmt"
	"strings"

	"platformserver/platform"
)

func BindRuleTypes(p platform.Page, lookup func(string) (platform.EntityInfo, bool)) error {
	d := p.InputSchema
	for name, v := range d.Variables {
		v.RuleType = nil
		object := ""
		if v.Type == "record" {
			object = p.RecordVariableObject(name)
		} else if v.Type == "object-set" {
			object = p.WindowVariableObject(name)
		}
		if object != "" {
			info, ok := lookup(object)
			if !ok {
				return fmt.Errorf("rule resource %s object is unavailable", name)
			}
			fields := map[string]platform.ExprType{}
			for _, field := range info.Fields {
				fields[field.Name] = platform.ExprType{Kind: platform.RuleFieldKind(field.Type)}
			}
			v.RuleType = &platform.ExprType{Kind: "reference", Fields: fields}
		}
		if v.Mode == "property" && v.Source != nil && v.Source.Object != nil {
			info, ok := lookup(v.Source.Object.Name)
			field, found := info.Field(v.Source.Field)
			if !ok || !found {
				return fmt.Errorf("rule property is unavailable")
			}
			v.RuleType = &platform.ExprType{Kind: platform.RuleFieldKind(field.Type), Object: field.Ref}
		}
		d.Variables[name] = v
	}
	var walk func(platform.Expr, string) error
	walk = func(e platform.Expr, set string) error {
		if e.Agg != nil {
			set = e.Agg.Source
			if _, ok := d.PageRuleSchema().Sets[set]; !ok {
				return fmt.Errorf("rule collection %s is not a declared query window", set)
			}
		}
		expand := func(base string, path []string) error {
			item := strings.HasPrefix(base, "item.")
			name := base
			parts := path
			if item {
				name = set
				parts = append([]string{strings.TrimPrefix(base, "item.")}, path...)
			}
			object := p.RecordVariableObject(name)
			if item {
				object = p.WindowVariableObject(name)
			}
			if object == "" {
				return fmt.Errorf("rule path %s needs a declared resource", base)
			}
			if _, err := platform.RecordPathField(object, parts, lookup); err != nil {
				return err
			}
			var tree func(string, []string) platform.ExprType
			tree = func(typ string, segments []string) platform.ExprType {
				info, _ := lookup(typ)
				f, _ := info.Field(segments[0])
				leaf := platform.ExprType{Kind: platform.RuleFieldKind(f.Type)}
				if len(segments) > 1 {
					leaf = tree(f.Ref, segments[1:])
				}
				return platform.ExprType{Kind: "reference", Fields: map[string]platform.ExprType{segments[0]: leaf}}
			}
			v := d.Variables[name]
			if v.RuleType == nil {
				return fmt.Errorf("rule resource is unavailable")
			}
			t := platform.MergeRuleTypes(*v.RuleType, tree(object, parts))
			v.RuleType = &t
			d.Variables[name] = v
			return nil
		}
		if e.Op == "path" && len(e.Args) == 1 {
			if err := expand(e.Args[0].Ref, e.Path); err != nil {
				return err
			}
		}
		if e.Agg != nil && len(e.Agg.Field) > 1 {
			if err := expand("item."+e.Agg.Field[0], e.Agg.Field[1:]); err != nil {
				return err
			}
		}
		for _, a := range e.Args {
			if err := walk(a, set); err != nil {
				return err
			}
		}
		if e.In != nil {
			return walk(*e.In, set)
		}
		return nil
	}
	for _, v := range d.Variables {
		if v.Rule != nil {
			if err := walk(*v.Rule, ""); err != nil {
				return err
			}
		}
	}
	return nil
}

func CheckQueryFilters(p platform.Page, lookup func(string) (platform.EntityInfo, bool)) error {
	for id, query := range p.InputSchema.Queries {
		if query.Filter == nil {
			continue
		}
		object, ok := lookup(query.Object.Name)
		if !ok || object.App != query.Object.App {
			return fmt.Errorf("page query %s object is unavailable", id)
		}
		if err := checkQueryFilter(p, *query.Filter, object); err != nil {
			return fmt.Errorf("page query %s: %w", id, err)
		}
	}
	return nil
}
