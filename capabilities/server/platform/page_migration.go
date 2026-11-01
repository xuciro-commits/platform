package platform

import (
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"time"
)

func (p Page) upgradePageVariableRules() (Page, error) {
	variables := maps.Clone(p.InputSchema.Variables)
	schema := p.InputSchema.PageRuleSchema()
	for id, variable := range variables {
		if variable.Expression == nil {
			continue
		}
		if variable.Rule != nil {
			return p, fmt.Errorf("page variable %s has two expressions", id)
		}
		expression, err := LowerPageExpr(Raw(variable.Expression), PageRuleKind(variable.Type), schema)
		if err != nil {
			return p, fmt.Errorf("page variable %s: %w", id, err)
		}
		variable.Rule = &expression
		variable.Expression = nil
		variables[id] = variable
	}
	p.InputSchema.Variables = variables
	return p, nil
}

func (p Page) UpgradeQueryConditions(lookup func(string) (EntityInfo, bool)) (Page, error) {
	queries := maps.Clone(p.InputSchema.Queries)
	for id, query := range queries {
		if len(query.Conditions) == 0 {
			continue
		}
		if query.Filter != nil {
			return p, fmt.Errorf("page query %s has two filters", id)
		}
		object, ok := lookup(query.Object.Name)
		if !ok || object.App != query.Object.App {
			return p, fmt.Errorf("page query %s object is unavailable", id)
		}
		parts := make([]Expr, 0, len(query.Conditions))
		for _, condition := range query.Conditions {
			expression, err := p.lowerPageQueryCondition(condition, object)
			if err != nil {
				return p, fmt.Errorf("page query %s: %w", id, err)
			}
			parts = append(parts, expression)
		}
		if len(parts) == 1 {
			query.Filter = &parts[0]
		} else {
			query.Filter = &Expr{Op: "and", Kind: "boolean", Args: parts}
		}
		query.Conditions = nil
		queries[id] = query
	}
	p.InputSchema.Queries = queries
	for id, query := range queries {
		if query.Filter == nil {
			continue
		}
		object, ok := lookup(query.Object.Name)
		if !ok || object.App != query.Object.App {
			return p, fmt.Errorf("page query %s object is unavailable", id)
		}
		if err := p.checkQueryFilter(*query.Filter, object); err != nil {
			return p, fmt.Errorf("page query %s: %w", id, err)
		}
	}
	return p, nil
}

func (p Page) lowerPageQueryCondition(condition PageQueryCondition, object EntityInfo) (Expr, error) {
	field := FieldInfo{}
	if condition.Field == "id" {
		field = FieldInfo{Name: "id", Type: "text"}
	} else if condition.Field == "created" || condition.Field == "changed" {
		field = FieldInfo{Name: condition.Field, Type: "datetime"}
	} else {
		var ok bool
		field, ok = object.Field(condition.Field)
		if !ok {
			return Expr{}, fmt.Errorf("query condition field is unavailable")
		}
	}
	if condition.AsDecimal && (field.Type != "integer" && field.Type != "decimal" || condition.Value.Variable == "") || condition.AsDate && (field.Type != "date" || condition.Value.Variable == "") || condition.AsDateTime && (field.Type != "datetime" || condition.Value.Variable == "") {
		return Expr{}, fmt.Errorf("query condition cast does not match its field")
	}
	if condition.AsDecimal && (condition.AsDate || condition.AsDateTime) || condition.AsDate && condition.AsDateTime {
		return Expr{}, fmt.Errorf("query condition has conflicting casts")
	}
	if condition.Op == "like" && field.Type != "text" && field.Type != "choice" || (condition.Op == "in" || condition.Op == "not in") && field.Type != "text" && field.Type != "choice" || (field.Type == "boolean" || field.Type == "reference") && condition.Op != "=" && condition.Op != "!=" {
		return Expr{}, fmt.Errorf("query condition operator does not match its field")
	}
	if !strings.Contains("|=|!=|<|<=|>|>=|like|in|not in|", "|"+condition.Op+"|") {
		return Expr{}, fmt.Errorf("query condition operator is unavailable")
	}
	valueType := ""
	if condition.Value.Variable != "" {
		variable, ok := p.InputSchema.Variables[condition.Value.Variable]
		if !ok || len(condition.Value.Literal) != 0 {
			return Expr{}, fmt.Errorf("query condition variable is unavailable")
		}
		valueType = variable.Type
	} else {
		valueType = pageLiteralType(condition.Value.Literal)
		if valueType == "" {
			var value any
			if json.Unmarshal(condition.Value.Literal, &value) == nil {
				if _, ok := value.(float64); ok {
					valueType = "number"
				}
			}
		}
		if valueType == "" {
			return Expr{}, fmt.Errorf("query condition literal is invalid")
		}
	}
	if (condition.Op == "in" || condition.Op == "not in") != (valueType == "string-set") {
		return Expr{}, fmt.Errorf("query condition set type does not match")
	}
	if condition.AsDecimal || condition.AsDate || condition.AsDateTime {
		if valueType != "string" {
			return Expr{}, fmt.Errorf("query condition cast needs text")
		}
	} else {
		valid := false
		switch field.Type {
		case "boolean":
			valid = valueType == "boolean"
		case "integer", "decimal":
			valid = valueType == "number" || valueType == "decimal"
		case "reference":
			valid = valueType == "string" || valueType == "record" && p.RecordVariableObject(condition.Value.Variable) == field.Ref
		case "text", "choice", "date", "datetime":
			valid = valueType == "string" || valueType == "string-set" && (field.Type == "text" || field.Type == "choice")
		}
		if !valid {
			return Expr{}, fmt.Errorf("query condition value does not match its field")
		}
	}
	fieldRef, valueKind, valueSuffix := condition.Field, "text", ""
	switch {
	case condition.Op == "like":
		fieldRef += "#search"
	case condition.Op == "in" || condition.Op == "not in":
		fieldRef += "#text"
		valueKind = "set"
	case condition.AsDecimal || valueType == "decimal" || valueType == "number":
		valueKind = "decimal"
		if condition.AsDecimal || valueType == "decimal" {
			fieldRef += "#decimal"
		} else {
			fieldRef += "#number"
		}
	case condition.AsDateTime || field.Type == "datetime":
		valueKind = "datetime"
		if condition.AsDateTime {
			valueSuffix = "#timestamp"
		}
	case condition.AsDate:
		fieldRef += "#text"
		valueSuffix = "#date"
	case field.Type == "boolean":
		fieldRef += "#number"
		valueKind = "decimal"
		valueSuffix = "#bool-number"
	default:
		fieldRef += "#text"
	}
	if condition.Op == "like" {
		valueKind = "text"
	}
	if condition.Optional && condition.Value.Variable == "" {
		if string(condition.Value.Literal) == `""` {
			return ExprLiteralValue("boolean", true), nil
		}
		if valueKind == "set" {
			var values struct {
				Values []string `json:"values"`
			}
			if json.Unmarshal(condition.Value.Literal, &values) == nil && len(values.Values) == 0 {
				return ExprLiteralValue("boolean", true), nil
			}
		}
	}
	leftKind := valueKind
	if valueKind == "set" {
		leftKind = "text"
	}
	left := ExprReference(leftKind, fieldRef)
	var right Expr
	if condition.Value.Variable != "" {
		right = ExprReference(valueKind, "root."+condition.Value.Variable+valueSuffix)
	} else {
		raw := condition.Value.Literal
		if valueKind == "set" {
			var value struct {
				Values []string `json:"values"`
			}
			if json.Unmarshal(raw, &value) != nil {
				return Expr{}, fmt.Errorf("query condition set is invalid")
			}
			right = Expr{Kind: "set", Lit: &ExprLiteral{Value: Raw(value.Values), Element: "text"}}
		} else if decimal, ok := DecimalLiteral(raw); ok {
			right = Expr{Kind: "decimal", Lit: &ExprLiteral{Value: Raw(decimal.Value)}}
		} else if field.Type == "boolean" {
			var value bool
			if json.Unmarshal(raw, &value) != nil {
				return Expr{}, fmt.Errorf("query condition boolean is invalid")
			}
			if value {
				right = ExprLiteralValue("decimal", "1")
			} else {
				right = ExprLiteralValue("decimal", "0")
			}
		} else if valueKind == "datetime" {
			var value string
			if json.Unmarshal(raw, &value) != nil {
				return Expr{}, fmt.Errorf("query condition datetime is invalid")
			}
			instant, err := time.Parse(time.RFC3339, value)
			if err != nil {
				instant, err = time.Parse(time.DateOnly, value)
			}
			if err != nil {
				return Expr{}, fmt.Errorf("query condition datetime is invalid")
			}
			right = ExprLiteralValue("datetime", instant.UTC().Format(time.RFC3339Nano))
		} else {
			right = Expr{Kind: valueKind, Lit: &ExprLiteral{Value: raw}}
		}
	}
	op := condition.Op
	if op == "like" {
		op = "icontains"
	}
	if op == "in" || op == "not in" {
		op = "in"
	}
	if op == "!=" {
		op = "="
	}
	expression := Expr{Op: op, Kind: "boolean", Args: []Expr{left, right}}
	if condition.Op == "not in" || condition.Op == "!=" {
		expression = Expr{Op: "not", Kind: "boolean", Args: []Expr{expression}}
	}
	if condition.Op == "<" || condition.Op == "<=" || condition.Op == ">" || condition.Op == ">=" {
		if valueKind != "decimal" {
			zero := ""
			if valueKind == "datetime" {
				zero = "0001-01-01T00:00:00Z"
			}
			guard := Expr{Op: "not", Kind: "boolean", Args: []Expr{{Op: "=", Kind: "boolean", Args: []Expr{left, ExprLiteralValue(valueKind, zero)}}}}
			expression = Expr{Op: "and", Kind: "boolean", Args: []Expr{expression, guard}}
		}
	}
	if condition.Optional && condition.Value.Variable != "" && (valueType == "string" || valueType == "string-set") {
		guardOp := "empty"
		if condition.AsDecimal || condition.AsDate || condition.AsDateTime || valueKind == "datetime" {
			guardOp = "is-null"
		}
		guard := Expr{Op: guardOp, Kind: "boolean", Args: []Expr{right}}
		expression = Expr{Op: "or", Kind: "boolean", Args: []Expr{guard, expression}}
	}
	return expression, nil
}
