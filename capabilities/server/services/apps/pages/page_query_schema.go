package pages

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
)

func CheckQuerySchema(p platform.Page, q platform.PageQuery, object platform.EntityInfo, named *platform.Definition, engines ...platform.RuleEngine) error {
	if q.Interface != "" {
		return checkInterfaceQuery(p, q, named)
	}
	if object.Type != q.Object.Name {
		return fmt.Errorf("query object is unavailable")
	}
	if q.Filter != nil {
		if err := checkQueryFilter(p, *q.Filter, object, engines...); err != nil {
			return err
		}
	}
	valueType := func(v platform.PageValue) string {
		if v.Variable != "" {
			return p.InputSchema.Variables[v.Variable].Type
		}
		if typ := literalType(v.Literal); typ != "" {
			return typ
		}
		return "number"
	}
	fieldType := func(field string) (platform.FieldInfo, bool) {
		if field == "id" {
			return platform.FieldInfo{Name: "id", Type: "text"}, true
		}
		if field == "created" || field == "changed" {
			return platform.FieldInfo{Name: field, Type: "datetime"}, true
		}
		return object.Field(field)
	}
	for _, field := range q.Sort {
		if _, ok := fieldType(strings.TrimPrefix(field, "-")); !ok {
			return fmt.Errorf("query sort field is unavailable")
		}
	}
	checkValue := func(field platform.FieldInfo, value platform.PageValue) bool {
		typ := valueType(value)
		switch field.Type {
		case "boolean":
			return typ == "boolean"
		case "integer", "decimal":
			return typ == "number" || typ == "decimal"
		case "reference":
			return typ == "string" || typ == "record" && p.RecordVariableObject(value.Variable) == field.Ref
		case "text", "choice", "date", "datetime":
			return typ == "string"
		}
		return false
	}
	if q.Search != nil && valueType(*q.Search) != "string" {
		return fmt.Errorf("query search requires text")
	}
	if q.Query != nil && q.Query.Ref.Kind == platform.AssetLinkType {
		if named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.LinkType == nil || q.For == nil || q.For.Variable == "" {
			return fmt.Errorf("link plan requires its retained relation and start record")
		}
		l := named.LinkType
		start, target := l.Parent, l.Child
		if q.Direction == "reverse" {
			start, target = l.Child, l.Parent
		}
		if q.Object != target || p.RecordVariableObject(q.For.Variable) != start.Name {
			return fmt.Errorf("link plan record or target object is incompatible")
		}
		return nil
	}
	if q.Query != nil {
		if named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.Query == nil || named.Query.Object != q.Object.Name {
			return fmt.Errorf("query requires its exact named query version and object")
		}
		decl := named.Query
		if q.ItemOwner != "" && decl.By == "" {
			return fmt.Errorf("item query requires its named parent reference")
		}
		if decl.By != "" {
			field, ok := fieldType(decl.By)
			if !ok || q.For == nil || !checkValue(field, *q.For) {
				return fmt.Errorf("named query needs its typed parent input")
			}
		} else if q.For != nil {
			return fmt.Errorf("named query has no parent input")
		}
		for _, field := range decl.Sort {
			if _, ok := fieldType(strings.TrimPrefix(field, "-")); !ok {
				return fmt.Errorf("named query sort field is unavailable")
			}
		}
		var terms []json.RawMessage
		if len(decl.Domain) > 0 && json.Unmarshal(decl.Domain, &terms) != nil {
			return fmt.Errorf("named query domain is unavailable")
		}
		for _, raw := range terms {
			var logic string
			if json.Unmarshal(raw, &logic) == nil {
				continue
			}
			var term []json.RawMessage
			var field string
			if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &field) != nil {
				return fmt.Errorf("named query domain is unavailable")
			}
			if _, ok := fieldType(field); !ok {
				return fmt.Errorf("named query field is unavailable")
			}
		}
	}
	return nil
}

func checkInterfaceQuery(p platform.Page, q platform.PageQuery, named *platform.Definition) error {
	if q.Query == nil || named == nil || named.Ref != q.Query.Ref || named.Version != q.Query.SourceVersion || named.Query == nil || named.Query.Interface != q.Interface || named.Query.InterfaceShape == nil || named.Query.Check() != nil {
		return fmt.Errorf("interface page query needs its exact published query and frozen shape")
	}
	if q.Object != (platform.AssetRef{}) || q.For != nil || q.Input != "" || q.Set != nil || q.Direction != "" || q.ItemOwner != "" || q.Filter != nil {
		return fmt.Errorf("interface page query cannot replace the published source or conditions")
	}
	for _, sort := range q.Sort {
		field := sort
		if len(field) > 0 && field[0] == '-' {
			field = field[1:]
		}
		if field != "id" && field != "created" && field != "changed" && !slices.ContainsFunc(named.Query.InterfaceShape.Fields, func(f platform.InterfaceField) bool {
			return f.Name == field
		}) {
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
		typ := literalType(q.Search.Literal)
		if q.Search.Variable != "" {
			typ = p.InputSchema.Variables[q.Search.Variable].Type
		}
		if typ != "string" {
			return fmt.Errorf("interface query search requires text")
		}
	}
	return nil
}

func checkQueryFilter(p platform.Page, filter platform.Expr, object platform.EntityInfo, engines ...platform.RuleEngine) error {
	if filter.Kind != "boolean" {
		return fmt.Errorf("page query filter must return boolean")
	}
	schema := platform.ExprSchema{Refs: map[string]platform.ExprType{}}
	var visit func(platform.Expr) error
	visit = func(expression platform.Expr) error {
		if expression.Agg != nil || expression.Window != nil || expression.Op == "path" || expression.Op == "$me" || expression.Op == "$now" || expression.Op == "$today" {
			return fmt.Errorf("page query filter cannot read caller, time or another collection")
		}
		if expression.Ref != "" {
			if name, ok := strings.CutPrefix(expression.Ref, "root."); ok {
				name, suffix, _ := strings.Cut(name, "#")
				variable, exists := p.InputSchema.Variables[name]
				if !exists || !platform.PageNodeID.MatchString(name) {
					return fmt.Errorf("page query filter parameter is unavailable")
				}
				kind := ""
				switch variable.Type {
				case "string":
					kind = "text"
					if suffix == "timestamp" {
						kind = "datetime"
					} else if suffix != "" && suffix != "date" {
						return fmt.Errorf("page query filter parameter conversion is unavailable")
					} else if suffix == "" && (expression.Kind == "date" || expression.Kind == "datetime" || expression.Kind == "decimal") {
						kind = expression.Kind
					}
				case "boolean":
					kind = "boolean"
					if suffix == "bool-number" {
						kind = "decimal"
					} else if suffix != "" {
						return fmt.Errorf("page query filter parameter conversion is unavailable")
					}
				case "decimal":
					kind = "decimal"
				case "number":
					kind = "number"
				case "record":
					kind = "text"
				case "string-set":
					kind = "set"
				}
				if variable.Rule != nil && suffix == "" {
					kind = variable.Rule.Kind
				}
				if kind == "" || expression.Kind != kind || suffix != "" && variable.Type != "string" && variable.Type != "boolean" {
					return fmt.Errorf("page query filter parameter type differs")
				}
				typ := platform.ExprType{Kind: kind}
				if kind == "set" {
					typ.Element = &platform.ExprType{Kind: "text"}
				}
				schema.Refs[expression.Ref] = typ
			} else {
				name, view, _ := strings.Cut(expression.Ref, "#")
				field := platform.FieldInfo{}
				if name == "id" {
					field = platform.FieldInfo{Name: "id", Type: "text"}
				} else if name == "created" || name == "changed" {
					field = platform.FieldInfo{Name: name, Type: "datetime"}
				} else {
					var exists bool
					field, exists = object.Field(name)
					if !exists {
						return fmt.Errorf("page query filter field is unavailable")
					}
				}
				kind := ""
				switch view {
				case "text", "search":
					kind = "text"
				case "number":
					if field.Type == "integer" || field.Type == "decimal" || field.Type == "boolean" {
						kind = "decimal"
					}
				case "decimal":
					if field.Type == "integer" || field.Type == "decimal" {
						kind = "decimal"
					}
				case "":
					switch field.Type {
					case "integer", "decimal":
						kind = "decimal"
					case "text", "longtext", "choice", "reference":
						kind = "text"
					case "references", "tags":
						kind = "set"
					case "boolean", "money", "date", "datetime":
						kind = field.Type
					}
				}
				if kind == "" || expression.Kind != kind || !platform.PageNodeID.MatchString(name) {
					return fmt.Errorf("page query filter field type differs")
				}
				typ := platform.ExprType{Kind: kind}
				if kind == "set" {
					typ.Element = &platform.ExprType{Kind: "text"}
				}
				schema.Refs[expression.Ref] = typ
			}
		}
		for _, argument := range expression.Args {
			if err := visit(argument); err != nil {
				return err
			}
		}
		if expression.In != nil {
			return visit(*expression.In)
		}
		return nil
	}
	if err := visit(filter); err != nil {
		return err
	}
	_, err := platform.RuleEngineOf(engines).CompileExpr(filter, schema)
	return err
}
