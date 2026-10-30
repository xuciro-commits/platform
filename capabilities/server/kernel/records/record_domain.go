package records

import (
	"encoding/json"
	"fmt"
	"math"
	"platformserver/internal/rules"
	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

type RowFilter struct {
	program *rules.RowProgram
	readers map[string]func(reflect.Value) rules.Value
	sql     []journal.StateFilter
}

type rowRefs struct {
	value   reflect.Value
	readers map[string]func(reflect.Value) rules.Value
}

func (r *rowRefs) Ref(name string) (rules.Value, bool) {
	read, ok := r.readers[name]
	if !ok {
		return rules.Value{}, false
	}
	return read(r.value), true
}

func (f *RowFilter) Match(v reflect.Value) bool {
	return f.program == nil || f.program.Match(&rowRefs{value: v, readers: f.readers})
}

func (f *RowFilter) Pushdown() []journal.StateFilter { return f.sql }

func CompileDomain(info platform.EntityInfo, raw json.RawMessage) (*RowFilter, error) {
	expr, err := DomainToExpr(info, raw)
	if err != nil {
		return nil, err
	}
	return CompileRowFilter(info, expr)
}

func CompileRowFilter(info platform.EntityInfo, expr rules.Expr) (*RowFilter, error) {
	filter := &RowFilter{readers: map[string]func(reflect.Value) rules.Value{}, sql: pushdown(info, expr)}
	if expr.Lit != nil && string(expr.Lit.Value) == "true" {
		return filter, nil
	}
	schema := rules.Schema{Refs: map[string]rules.Type{}}
	var visit func(rules.Expr) error
	visit = func(e rules.Expr) error {
		if e.Ref != "" {
			if _, done := schema.Refs[e.Ref]; !done {
				name, view, _ := strings.Cut(e.Ref, "#")
				field, err := resolveDomainField(info, name)
				if err != nil {
					return err
				}
				typ, read, err := field.reader(view)
				if err != nil {
					return err
				}
				schema.Refs[e.Ref], filter.readers[e.Ref] = typ, read
			}
		}
		for _, arg := range e.Args {
			if err := visit(arg); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(expr); err != nil {
		return nil, err
	}
	program, err := rules.CompileRow(expr, schema)
	if err != nil {
		return nil, err
	}
	filter.program = program
	return filter, nil
}

const (
	classNumber = iota
	classTime
	classText
	classList
	classOther
)

type domainField struct {
	name  string
	typ   string
	class int
	info  platform.FieldInfo
	read  func(reflect.Value) any
}

func resolveDomainField(info platform.EntityInfo, name string) (domainField, error) {
	f, ok := info.Field(name)
	read := func(v reflect.Value) any { return v.FieldByIndex(f.Index).Interface() }
	switch {
	case name == "id":
		f, read = platform.FieldInfo{Name: "id", Type: "text", Index: []int{0}}, func(v reflect.Value) any { return Header(v).ID }
	case name == "created" || name == "changed":
		f, read = platform.FieldInfo{Name: name, Type: "datetime"}, func(v reflect.Value) any {
			if name == "created" {
				return Header(v).Created.At
			}
			return Header(v).Changed.At
		}
	case !ok:
		return domainField{}, fmt.Errorf("unknown field %s", name)
	}
	class := classOther
	switch f.Type {
	case "integer", "decimal", "money", "boolean":
		class = classNumber
	case "datetime":
		class = classTime
	case "text", "longtext", "choice", "reference", "date":
		class = classText
	case "references", "tags":
		class = classList
	}
	return domainField{name: name, typ: f.Type, class: class, info: f, read: read}, nil
}

func (d domainField) reader(view string) (rules.Type, func(reflect.Value) rules.Value, error) {
	text := rules.Type{Kind: "text"}
	decimal := func(v reflect.Value) rules.Value {
		x, ok := Comparable(d.read(v)).(float64)
		if !ok || math.IsNaN(x) || math.IsInf(x, 0) {
			return rules.Null("decimal")
		}
		return rules.FloatValue(x)
	}
	switch view {
	case "search":
		return text, func(v reflect.Value) rules.Value { return rules.TextValue("text", Searchable(d.read(v))) }, nil
	case "text":
		return text, func(v reflect.Value) rules.Value { return comparableText(Comparable(d.read(v))) }, nil
	case "number":
		return rules.Type{Kind: "decimal"}, decimal, nil
	case "decimal":
		if d.typ != "integer" && d.typ != "decimal" {
			return rules.Type{Kind: "decimal"}, decimal, nil
		}
		return rules.Type{Kind: "decimal"}, func(v reflect.Value) rules.Value {
			raw, err := json.Marshal(d.read(v))
			if err != nil || string(raw) == "null" {
				return rules.Null("decimal")
			}
			return rules.Value{Kind: "decimal", Status: "value", Data: raw}
		}, nil
	case "":
		switch d.typ {
		case "boolean":
			return rules.Type{Kind: "boolean"}, func(v reflect.Value) rules.Value {
				value := d.read(v)
				if value == nil {
					return rules.Null("boolean")
				}
				b := reflect.ValueOf(value)
				for b.Kind() == reflect.Pointer {
					if b.IsNil() {
						return rules.Null("boolean")
					}
					b = b.Elem()
				}
				return rules.BoolValue(b.Bool())
			}, nil
		case "money":
			return rules.Type{Kind: "money"}, func(v reflect.Value) rules.Value {
				value := d.read(v)
				if value == nil {
					return rules.Null("money")
				}
				raw, err := json.Marshal(value)
				if err != nil || string(raw) == "null" {
					return rules.Null("money")
				}
				return rules.Value{Kind: "money", Status: "value", Data: raw}
			}, nil
		case "date":
			return rules.Type{Kind: "date"}, func(v reflect.Value) rules.Value {
				value := d.read(v)
				if value == nil || Comparable(value) == "" {
					return rules.Null("date")
				}
				return rules.Present("date", value)
			}, nil
		}
		switch d.class {
		case classNumber:
			return rules.Type{Kind: "decimal"}, decimal, nil
		case classTime:
			return rules.Type{Kind: "datetime"}, func(v reflect.Value) rules.Value {
				x, ok := Comparable(d.read(v)).(time.Time)
				if !ok || x.Year() < 1 || x.Year() > 9999 {
					return rules.Null("datetime")
				}
				return rules.TimeValue(x)
			}, nil
		case classText:
			return text, func(v reflect.Value) rules.Value { return comparableText(Comparable(d.read(v))) }, nil
		case classList:
			return rules.Type{Kind: "set", Element: &text}, func(v reflect.Value) rules.Value {
				fv := v.FieldByIndex(d.info.Index)
				for fv.Kind() == reflect.Pointer && !fv.IsNil() {
					fv = fv.Elem()
				}
				items := []string{}
				if fv.Kind() == reflect.Slice {
					for i := range fv.Len() {
						items = append(items, fv.Index(i).String())
					}
				}
				return rules.SetValue(items)
			}, nil
		}
	}
	return rules.Type{}, nil, fmt.Errorf("field %s has no %q view", d.name, view)
}

func comparableText(x any) rules.Value {
	switch s := x.(type) {
	case nil:
		return rules.Null("text")
	case string:
		return rules.TextValue("text", s)
	}
	return rules.TextValue("text", fmt.Sprint(x))
}

func DomainToExpr(info platform.EntityInfo, raw json.RawMessage) (rules.Expr, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return boolLit(true), nil
	}
	var terms []json.RawMessage
	if err := json.Unmarshal(raw, &terms); err != nil {
		return rules.Expr{}, err
	}
	var stack []rules.Expr
	for i := len(terms) - 1; i >= 0; i-- {
		var op string
		if json.Unmarshal(terms[i], &op) == nil {
			switch {
			case op == "!" && len(stack) >= 1:
				stack[len(stack)-1] = negate(stack[len(stack)-1])
			case (op == "&" || op == "|") && len(stack) >= 2:
				a, b := stack[len(stack)-1], stack[len(stack)-2]
				stack = stack[:len(stack)-2]
				stack = append(stack, combine(map[string]string{"&": "and", "|": "or"}[op], a, b))
			default:
				return rules.Expr{}, fmt.Errorf("operator %q without its terms", op)
			}
			continue
		}
		e, err := lowerCondition(info, terms[i])
		if err != nil {
			return rules.Expr{}, err
		}
		stack = append(stack, e)
	}
	return combine("and", stack...), nil
}

func ConditionExpr(info platform.EntityInfo, field, op string, value any) (rules.Expr, error) {
	raw, err := json.Marshal([]any{field, op, value})
	if err != nil {
		return rules.Expr{}, err
	}
	return lowerCondition(info, raw)
}

func AllOf(parts ...rules.Expr) rules.Expr { return combine("and", parts...) }

func lowerCondition(info platform.EntityInfo, raw json.RawMessage) (rules.Expr, error) {
	var term []json.RawMessage
	var name, op string
	if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &name) != nil || json.Unmarshal(term[1], &op) != nil {
		return rules.Expr{}, fmt.Errorf("a condition is [field, operator, value]")
	}
	d, err := resolveDomainField(info, name)
	if err != nil {
		return rules.Expr{}, err
	}
	if number, ok := platform.DecimalLiteral(term[2]); ok {
		if d.typ != "integer" && d.typ != "decimal" {
			return rules.Expr{}, fmt.Errorf("decimal condition needs a numeric field")
		}
		if !slices.Contains([]string{"=", "!=", "<", "<=", ">", ">="}, op) {
			return rules.Expr{}, fmt.Errorf("unsupported decimal condition")
		}
		return compare(op, rules.RefOf("decimal", name+"#decimal"), literal("decimal", number.Value)), nil
	}
	var tagged map[string]json.RawMessage
	if json.Unmarshal(term[2], &tagged) == nil && tagged != nil {
		return rules.Expr{}, fmt.Errorf("invalid typed condition value")
	}
	var value any
	if json.Unmarshal(term[2], &value) != nil {
		return rules.Expr{}, fmt.Errorf("bad value")
	}
	if b, ok := value.(bool); ok {
		value = map[bool]float64{true: 1, false: 0}[b]
	}
	if d.typ == "datetime" {
		if s, ok := value.(string); ok {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				if t, err = time.Parse(time.DateOnly, s); err != nil {
					return rules.Expr{}, err
				}
			}
			value = t
		}
	}
	list := d.class == classList
	var scalar rules.Expr
	var kind string
	switch d.class {
	case classNumber:
		scalar, kind = rules.RefOf("decimal", name+"#number"), "decimal"
	case classTime:
		scalar, kind = rules.RefOf("datetime", name), "datetime"
	case classText:
		scalar, kind = rules.RefOf("text", name+"#text"), "text"
	default:
		scalar, kind = rules.RefOf("text", name+"#text"), "text"
	}
	switch op {
	case "=", "!=":
		var eq rules.Expr
		if list {
			eq = rules.Expr{Op: "in", Kind: "boolean", Args: []rules.Expr{textLit(fmt.Sprint(value)), setRef(name)}}
		} else if lit, null := operand(kind, value); null {
			eq = rules.Expr{Op: "is-null", Kind: "boolean", Args: []rules.Expr{scalar}}
		} else {
			eq = compare("=", scalar, literal(kind, lit))
		}
		if op == "!=" {
			return negate(eq), nil
		}
		return eq, nil
	case "<", "<=", ">", ">=":
		var guard []rules.Expr
		if kind != "decimal" {
			guard = append(guard, negate(compare("=", scalar, literal(kind, zeroOperand(kind)))))
		}
		lit, null := operand(kind, value)
		if null {
			if op == "<" || op == "<=" {
				return boolLit(false), nil
			}
			return combine("and", append([]rules.Expr{{Op: "is-not-null", Kind: "boolean", Args: []rules.Expr{scalar}}}, guard...)...), nil
		}
		return combine("and", append([]rules.Expr{compare(op, scalar, literal(kind, lit))}, guard...)...), nil
	case "in", "not in":
		values, _ := value.([]any)
		if values == nil {
			return rules.Expr{}, fmt.Errorf("%s needs a list", op)
		}
		var has rules.Expr
		if list {
			var items []any
			for _, item := range values {
				if item != nil {
					items = append(items, fmt.Sprint(item))
				}
			}
			var options []rules.Expr
			for chunk := range slices.Chunk(items, rules.MaxSet) {
				options = append(options, rules.Expr{Op: "overlaps", Kind: "boolean", Args: []rules.Expr{setRef(name), setLit("text", chunk)}})
			}
			has = combine("or", options...)
		} else {
			var items []any
			nullItem := false
			for _, item := range values {
				lit, null := operand(kind, item)
				if null {
					nullItem = true
					continue
				}
				items = append(items, lit)
			}
			var options []rules.Expr
			for chunk := range slices.Chunk(items, rules.MaxSet) {
				options = append(options, rules.Expr{Op: "in", Kind: "boolean", Args: []rules.Expr{scalar, setLit(kind, chunk)}})
			}
			if nullItem {
				options = append(options, rules.Expr{Op: "is-null", Kind: "boolean", Args: []rules.Expr{scalar}})
			}
			has = combine("or", options...)
		}
		if op == "not in" {
			return negate(has), nil
		}
		return has, nil
	case "like":
		return rules.Expr{Op: "icontains", Kind: "boolean", Args: []rules.Expr{rules.RefOf("text", name+"#search"), textLit(strings.ToLower(fmt.Sprint(value)))}}, nil
	}
	return rules.Expr{}, fmt.Errorf("unknown operator %s", op)
}

func operand(kind string, b any) (any, bool) {
	if b == nil {
		return nil, true
	}
	switch kind {
	case "decimal":
		n, _ := b.(float64)
		return n, false
	case "datetime":
		t, _ := b.(time.Time)
		return t.UTC().Format(time.RFC3339Nano), false
	}
	return fmt.Sprint(b), false
}

func zeroOperand(kind string) any {
	if kind == "datetime" {
		return time.Time{}.UTC().Format(time.RFC3339Nano)
	}
	return ""
}

func literal(kind string, value any) rules.Expr {
	return rules.Expr{Kind: kind, Lit: &rules.Literal{Value: rules.Raw(value)}}
}

func textLit(s string) rules.Expr { return literal("text", s) }

func boolLit(b bool) rules.Expr { return literal("boolean", b) }

func setRef(name string) rules.Expr { return rules.RefOf("set", name) }

func setLit(element string, items []any) rules.Expr {
	return rules.Expr{Kind: "set", Lit: &rules.Literal{Value: rules.Raw(items), Element: element}}
}

func compare(op string, left, right rules.Expr) rules.Expr {
	return rules.Expr{Op: op, Kind: "boolean", Args: []rules.Expr{left, right}}
}

func negate(e rules.Expr) rules.Expr {
	return rules.Expr{Op: "not", Kind: "boolean", Args: []rules.Expr{e}}
}

func combine(op string, parts ...rules.Expr) rules.Expr {
	var flat []rules.Expr
	for _, part := range parts {
		if part.Op == op {
			flat = append(flat, part.Args...)
		} else {
			flat = append(flat, part)
		}
	}
	switch len(flat) {
	case 0:
		return boolLit(op == "and")
	case 1:
		return flat[0]
	}
	for len(flat) > 32 {
		var next []rules.Expr
		for group := range slices.Chunk(flat, 32) {
			if len(group) == 1 {
				next = append(next, group[0])
			} else {
				next = append(next, rules.Expr{Op: op, Kind: "boolean", Args: slices.Clone(group)})
			}
		}
		flat = next
	}
	return rules.Expr{Op: op, Kind: "boolean", Args: flat}
}

func pushdown(info platform.EntityInfo, expr rules.Expr) []journal.StateFilter {
	var out []journal.StateFilter
	var walk func(rules.Expr)
	walk = func(e rules.Expr) {
		if e.Op == "and" {
			for _, arg := range e.Args {
				walk(arg)
			}
			return
		}
		if filter, ok := stateFilter(info, e); ok {
			out = append(out, filter)
		}
	}
	walk(expr)
	return out
}

func stateFilter(info platform.EntityInfo, e rules.Expr) (journal.StateFilter, bool) {
	if len(e.Args) != 2 {
		return journal.StateFilter{}, false
	}
	field, literal, list := "", e.Args[1], false
	switch e.Op {
	case "=", "in":
		if e.Args[0].Ref != "" && literal.Lit != nil {
			field = e.Args[0].Ref
		} else if e.Op == "in" && e.Args[0].Lit != nil && e.Args[1].Ref != "" {
			field, literal, list = e.Args[1].Ref, e.Args[0], true
		}
	case "overlaps":
		if e.Args[0].Ref != "" && literal.Lit != nil {
			field, list = e.Args[0].Ref, true
		}
	}
	if field == "" || strings.Contains(field, "#") || literal.Lit == nil {
		return journal.StateFilter{}, false
	}
	f, ok := info.Field(field)
	if field == "id" {
		f, ok = platform.FieldInfo{Name: "id", Type: "text", Index: []int{0}}, true
	}
	if !ok || len(f.Index) == 0 || f.AliasOf != "" || f.Property != nil {
		return journal.StateFilter{}, false
	}
	isList := f.Type == "references" || f.Type == "tags"
	if isList != list || !isList && f.Type != "text" && f.Type != "longtext" && f.Type != "reference" && f.Type != "choice" && f.Type != "date" {
		return journal.StateFilter{}, false
	}
	filter := journal.StateFilter{Path: []string{"value", field}, List: isList}
	if e.Op == "=" || list && e.Op == "in" {
		var value string
		if literal.Kind != "text" || json.Unmarshal(literal.Lit.Value, &value) != nil {
			return journal.StateFilter{}, false
		}
		filter.Values = []string{value}
		return filter, true
	}
	if literal.Lit.Element != "text" || json.Unmarshal(literal.Lit.Value, &filter.Values) != nil {
		return journal.StateFilter{}, false
	}
	return filter, true
}
