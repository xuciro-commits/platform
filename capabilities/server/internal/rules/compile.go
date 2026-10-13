package rules

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Compile visits even short-circuited branches: no runtime branch can smuggle
// an unknown operator, path or undeclared set past publication.
func Compile(e Expr, s Schema) (*Program, error) { return compile(e, s, false) }

// CompileFrozenFormula is reachable only by trusted owners reading old frozen
// bytes. It shares the compiler; a JSON rule cannot request this mode.
func CompileFrozenFormula(e Expr, s Schema) (*Program, error) { return compile(e, s, true) }
func compile(e Expr, s Schema, frozen bool) (*Program, error) {
	nodes := 0
	if _, err := check(e, s, "$", 0, &nodes, frozen); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(e)
	limit := MaxBytes
	if frozen {
		limit = 1 << 20
	}
	if err != nil || len(raw) > limit {
		return nil, fail("budget", "$", "expression byte budget exceeded")
	}
	schemaRaw, err := json.Marshal(s)
	if err != nil || len(schemaRaw) > MaxBytes {
		return nil, fail("budget", "$", "schema budget exceeded or cyclic declaration")
	}
	var schema Schema
	json.Unmarshal(schemaRaw, &schema)
	return &Program{expr: cloneExpr(e), schema: schema}, nil
}
func cloneExpr(e Expr) Expr {
	e.Args = append([]Expr(nil), e.Args...)
	for i := range e.Args {
		e.Args[i] = cloneExpr(e.Args[i])
	}
	e.Path = append([]string(nil), e.Path...)
	if e.Lit != nil {
		literal := *e.Lit
		literal.Value = append(json.RawMessage(nil), literal.Value...)
		e.Lit = &literal
	}
	if e.Agg != nil {
		aggregate := *e.Agg
		aggregate.Field = append([]string(nil), aggregate.Field...)
		e.Agg = &aggregate
	}
	if e.In != nil {
		child := cloneExpr(*e.In)
		e.In = &child
	}
	return e
}
func check(e Expr, s Schema, at string, depth int, nodes *int, frozen bool) (Type, error) {
	*nodes++
	maxNodes, maxDepth := MaxNodes, MaxDepth
	if frozen {
		maxNodes, maxDepth = 8192, 4096
	}
	if *nodes > maxNodes || depth > maxDepth {
		return Type{}, fail("budget", at, "expression tree budget exceeded")
	}
	if frozen && e.Lit == nil && e.Ref == "" && !strings.Contains(" + - * / negate round ", " "+e.Op+" ") {
		return Type{}, fail("operator", at, "frozen formula is limited to old arithmetic")
	}
	valid := map[string]bool{"text": true, "string": true, "integer": true, "number": true, "decimal": true, "money": true, "boolean": true, "date": true, "datetime": true, "choice": true, "reference": true, "set": true}
	if !valid[e.Kind] {
		return Type{}, fail("type", at+".kind", "explicit supported result type required")
	}
	if e.Lit != nil || e.Ref != "" {
		if e.Op != "" || len(e.Args) > 0 || e.Agg != nil || e.In != nil || len(e.Path) > 0 || (e.Lit != nil) == (e.Ref != "") {
			return Type{}, fail("shape", at, "a leaf is exactly one literal or declared reference")
		}
		if e.Lit != nil {
			var value any
			var err error
			if frozen && numeric(e.Kind) && e.Kind != "money" {
				_, err = frozenNumeric(PresentRaw(e.Kind, e.Lit.Value), at)
			} else {
				value, err = validateValue(PresentRaw(e.Kind, e.Lit.Value), at)
			}
			if err != nil {
				return Type{}, err
			}
			t := Type{Kind: e.Kind}
			if e.Kind == "set" {
				if !valid[e.Lit.Element] || e.Lit.Element == "set" {
					return Type{}, fail("type", at, "set literal needs an explicit scalar element type")
				}
				t.Element = &Type{Kind: e.Lit.Element}
				items, ok := value.([]any)
				if !ok {
					return Type{}, fail("set", at, "set literal must be an array")
				}
				for _, item := range items {
					if _, err := validateValue(Present(e.Lit.Element, item), at); err != nil {
						return Type{}, err
					}
				}
			}
			if m, ok := value.(money); ok {
				t.Currency = m.currency
			}
			return t, nil
		}
		t, ok := s.Refs[e.Ref]
		if !ok || !same(t.Kind, e.Kind) {
			return Type{}, fail("reference", at+".ref", "reference is undeclared or has a different type")
		}
		return t, nil
	}
	if e.Op == "path" {
		if len(e.Args) != 1 || e.Args[0].Ref == "" || len(e.Path) == 0 || len(e.Path) > 16 || e.In != nil || e.Agg != nil {
			return Type{}, fail("path", at, "path needs one declared source and 1-16 literal segments")
		}
		t, err := check(e.Args[0], s, at+".args[0]", depth+1, nodes, frozen)
		if err != nil {
			return Type{}, err
		}
		for i, part := range e.Path {
			var ok bool
			t, ok = t.Fields[part]
			if !ok || part == "" || strings.Contains(part, ".") {
				return Type{}, fail("path", fmt.Sprintf("%s.path[%d]", at, i), "path segment is not declared")
			}
		}
		if !same(t.Kind, e.Kind) {
			return Type{}, fail("type", at, "path result type differs")
		}
		return t, nil
	}
	if e.Agg != nil {
		if len(e.Args) > 0 || len(e.Path) > 0 || e.In != nil && len(e.Agg.Field) > 0 {
			return Type{}, fail("shape", at, "aggregate cannot carry scalar arguments or a dynamic path")
		}
		decl, ok := s.Sets[e.Agg.Source]
		if !ok || decl.Policy != "visible" && decl.Policy != "complete" {
			return Type{}, fail("set", at+".agg.source", "a declared set and scope policy are required")
		}
		child := s
		child.Refs = map[string]Type{}
		for k, v := range s.Refs {
			child.Refs[k] = v
		}
		for k, v := range decl.Element.Fields {
			child.Refs["item."+k] = v
		}
		for _, part := range e.Agg.Field {
			if part == "" || strings.Contains(part, ".") {
				return Type{}, fail("path", at, "aggregate field must be a literal path")
			}
		}
		if len(e.Agg.Field) > 16 {
			return Type{}, fail("path", at, "aggregate path exceeds 16 segments")
		}
		value := decl.Element
		for _, part := range e.Agg.Field {
			var ok bool
			value, ok = value.Fields[part]
			if !ok {
				return Type{}, fail("path", at, "aggregate field is undeclared")
			}
		}
		if e.In != nil {
			var err error
			value, err = check(*e.In, child, at+".in", depth+1, nodes, frozen)
			if err != nil {
				return Type{}, err
			}
		}
		switch e.Op {
		case "any", "all", "none":
			if e.In == nil || value.Kind != "boolean" || e.Kind != "boolean" {
				return Type{}, fail("type", at, "quantifier needs a boolean predicate and result")
			}
		case "count":
			if e.In != nil || len(e.Agg.Field) > 0 || e.Kind != "integer" {
				return Type{}, fail("type", at, "count returns integer and needs only a set")
			}
		case "sum", "min", "max", "avg":
			if !numeric(value.Kind) || !numeric(e.Kind) || value.Kind == "money" && e.Kind != "money" || value.Kind != "money" && e.Kind == "money" || e.Op == "avg" && e.Kind == "integer" {
				return Type{}, fail("type", at, "aggregate needs a compatible numeric field or expression")
			}
		default:
			return Type{}, fail("operator", at+".op", "unknown aggregate operator")
		}
		if e.Op != "count" && e.Kind == "integer" && value.Kind != "integer" {
			return Type{}, fail("type", at, "aggregate cannot narrow a decimal to integer")
		}
		return Type{Kind: e.Kind, Currency: value.Currency}, nil
	}
	if e.In != nil || len(e.Path) > 0 {
		return Type{}, fail("shape", at, "scope and paths require their declared owner")
	}
	args := make([]Type, len(e.Args))
	for i, a := range e.Args {
		var err error
		args[i], err = check(a, s, fmt.Sprintf("%s.args[%d]", at, i), depth+1, nodes, frozen)
		if err != nil {
			return Type{}, err
		}
	}
	arity := func(lo, hi int) bool { return len(args) >= lo && len(args) <= hi }
	all := func(k string) bool {
		for _, a := range args {
			if !same(a.Kind, k) {
				return false
			}
		}
		return true
	}
	currency := ""
	moneyUnknown := false
	for _, a := range args {
		if a.Kind == "money" {
			if a.Currency == "" {
				moneyUnknown = true
			} else if currency != "" && currency != a.Currency {
				return Type{}, fail("currency", at, "declared currencies differ")
			} else {
				currency = a.Currency
			}
		}
	}
	good := false
	switch e.Op {
	case "$me":
		good = arity(0, 0) && e.Kind == "reference"
	case "$now":
		good = arity(0, 0) && e.Kind == "datetime"
	case "$today":
		good = arity(0, 0) && e.Kind == "date"
	case "and", "or":
		good = arity(2, 32) && all("boolean") && e.Kind == "boolean"
	case "not":
		good = arity(1, 1) && all("boolean") && e.Kind == "boolean"
	case "empty", "not-empty", "is-null", "is-not-null":
		good = arity(1, 1) && e.Kind == "boolean"
	case "=", "!=", "<", "<=", ">", ">=":
		good = arity(2, 2) && e.Kind == "boolean"
		if good {
			good = same(args[0].Kind, args[1].Kind) || numeric(args[0].Kind) && numeric(args[1].Kind) && (args[0].Kind != "money" && args[1].Kind != "money" || currency != "" && !moneyUnknown)
		}
		if good && args[0].Kind == "reference" && args[1].Kind == "reference" && args[0].Object != "" && args[1].Object != "" && args[0].Object != args[1].Object {
			return Type{}, fail("reference", at, "declared reference objects differ")
		}
		if good && args[0].Kind == "money" && args[0].Currency != "" && args[1].Currency != "" && args[0].Currency != args[1].Currency {
			return Type{}, fail("currency", at, "declared comparison currencies differ")
		}
		if good && e.Op != "=" && e.Op != "!=" {
			good = numeric(args[0].Kind) || args[0].Kind == "date" || args[0].Kind == "datetime" || args[0].Kind == "text"
		}
	case "+", "-", "*", "/", "min", "max", "negate", "abs", "round":
		lo, hi := 2, 2
		if e.Op == "min" || e.Op == "max" {
			hi = 32
		}
		if e.Op == "negate" || e.Op == "abs" {
			lo, hi = 1, 1
		}
		if e.Op == "round" {
			lo, hi = 1, 2
		}
		good = arity(lo, hi) && numeric(e.Kind)
		for i, a := range args {
			if e.Op == "round" && i == 1 {
				good = good && a.Kind == "integer"
				continue
			}
			good = good && numeric(a.Kind) && (a.Kind == "money" && e.Kind == "money" || a.Kind != "money" && (e.Kind != "money" || currency != "" && !moneyUnknown))
		}
		if good && e.Kind == "integer" && e.Op != "round" {
			good = all("integer")
		}
	case "concat":
		good = arity(2, 32) && all("text") && same(e.Kind, "text")
	case "lower", "upper", "trim":
		good = arity(1, 1) && all("text") && same(e.Kind, "text")
	case "contains", "starts-with", "ends-with":
		good = arity(2, 2) && all("text") && e.Kind == "boolean"
	case "length":
		good = arity(1, 1) && all("text") && e.Kind == "integer"
	case "replace":
		good = arity(3, 3) && all("text") && same(e.Kind, "text")
	case "parse-decimal", "parse-number":
		good = arity(1, 1) && all("text") && (e.Op == "parse-decimal" && e.Kind == "decimal" || e.Op == "parse-number" && e.Kind == "number")
	case "add-days", "add-months":
		good = arity(2, 2) && (args[0].Kind == "date" || args[0].Kind == "datetime") && args[1].Kind == "integer" && same(e.Kind, args[0].Kind)
	case "diff-days":
		good = arity(2, 2) && same(args[0].Kind, args[1].Kind) && (args[0].Kind == "date" || args[0].Kind == "datetime") && e.Kind == "integer"
	case "before", "after":
		good = arity(2, 2) && same(args[0].Kind, args[1].Kind) && (args[0].Kind == "date" || args[0].Kind == "datetime") && e.Kind == "boolean"
	case "within-period":
		good = arity(3, 3) && same(args[0].Kind, args[1].Kind) && same(args[0].Kind, args[2].Kind) && (args[0].Kind == "date" || args[0].Kind == "datetime") && e.Kind == "boolean"
	case "date-part":
		good = arity(2, 2) && (args[0].Kind == "date" || args[0].Kind == "datetime") && args[1].Kind == "text" && e.Args[1].Lit != nil && e.Kind == "integer"
		if good {
			var part string
			_ = json.Unmarshal(e.Args[1].Lit.Value, &part)
			good = part == "year" || part == "month" || part == "day" || part == "weekday"
		}
	case "in":
		good = arity(2, 2) && args[1].Kind == "set" && args[1].Element != nil && same(args[0].Kind, args[1].Element.Kind) && e.Kind == "boolean"
	case "range":
		good = arity(3, 3) && same(args[0].Kind, args[1].Kind) && same(args[0].Kind, args[2].Kind) && numeric(args[0].Kind) && e.Kind == "boolean"
	default:
		return Type{}, fail("operator", at+".op", "unknown operator")
	}
	if !good {
		return Type{}, fail("type", at, "operator arity or explicit operand/result types differ")
	}
	return Type{Kind: e.Kind, Currency: currency}, nil
}
func PresentRaw(kind string, raw json.RawMessage) Value {
	return Value{Kind: kind, Status: "value", Data: raw}
}
