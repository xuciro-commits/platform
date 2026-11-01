package platform

import (
	"fmt"
	"slices"
	"strings"
)

func (q PageQuery) Variables() []string {
	ids := []string{}
	if q.Input != "" {
		ids = append(ids, q.Input)
	}
	for _, v := range q.Values() {
		if v.Variable != "" {
			ids = append(ids, v.Variable)
		}
	}
	if q.Filter != nil {
		for _, ref := range ConditionRefs(*q.Filter) {
			if name, ok := strings.CutPrefix(ref, "root."); ok {
				name, _, _ = strings.Cut(name, "#")
				if name != "" && !slices.Contains(ids, name) {
					ids = append(ids, name)
				}
			}
		}
	}
	return ids
}

func (p Page) checkQueryFilter(filter Expr, object EntityInfo, engines ...RuleEngine) error {
	if filter.Kind != "boolean" {
		return fmt.Errorf("page query filter must return boolean")
	}
	schema := ExprSchema{Refs: map[string]ExprType{}}
	var visit func(Expr) error
	visit = func(expression Expr) error {
		if expression.Agg != nil || expression.Window != nil || expression.Op == "path" || expression.Op == "$me" || expression.Op == "$now" || expression.Op == "$today" {
			return fmt.Errorf("page query filter cannot read caller, time or another collection")
		}
		if expression.Ref != "" {
			if name, ok := strings.CutPrefix(expression.Ref, "root."); ok {
				name, suffix, _ := strings.Cut(name, "#")
				variable, exists := p.inputSchema().Variables[name]
				if !exists || !pageNodeID.MatchString(name) {
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
				typ := ExprType{Kind: kind}
				if kind == "set" {
					typ.Element = &ExprType{Kind: "text"}
				}
				schema.Refs[expression.Ref] = typ
			} else {
				name, view, _ := strings.Cut(expression.Ref, "#")
				field := FieldInfo{}
				if name == "id" {
					field = FieldInfo{Name: "id", Type: "text"}
				} else if name == "created" || name == "changed" {
					field = FieldInfo{Name: name, Type: "datetime"}
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
				if kind == "" || expression.Kind != kind || !pageNodeID.MatchString(name) {
					return fmt.Errorf("page query filter field type differs")
				}
				typ := ExprType{Kind: kind}
				if kind == "set" {
					typ.Element = &ExprType{Kind: "text"}
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
	_, err := ruleEngineOf(engines).CompileExpr(filter, schema)
	return err
}
