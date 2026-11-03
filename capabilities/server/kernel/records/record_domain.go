package records

import (
	"encoding/json"
	"fmt"
	"math"
	ruleexpr "platformserver/expr"
	persistence "platformserver/kernel/store"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

type RowFilter struct {
	program platform.RowProgram
	readers map[string]func(reflect.Value) ruleexpr.Value
	sql     []persistence.StateFilter
}

type rowRefs struct {
	value   reflect.Value
	readers map[string]func(reflect.Value) ruleexpr.Value
}

func (r *rowRefs) Ref(name string) (ruleexpr.Value, bool) {
	read, ok := r.readers[name]
	if !ok {
		return ruleexpr.Value{}, false
	}
	return read(r.value), true
}

func (f *RowFilter) Match(v reflect.Value) bool {
	return f == nil || f.program == nil || f.program.Match(&rowRefs{value: v, readers: f.readers})
}

func (f *RowFilter) Pushdown() []persistence.StateFilter { return f.sql }

func CompileDomain(info platform.EntityInfo, raw json.RawMessage, engines ...platform.RuleEngine) (*RowFilter, error) {
	expr, err := DomainToExpr(info, raw)
	if err != nil {
		return nil, err
	}
	return CompileRowFilter(info, expr, engines...)
}

type RowAttribute struct {
	Ref  string
	Type ruleexpr.Type
	Read func(reflect.Value) ruleexpr.Value
}

func CompileRowFilter(info platform.EntityInfo, expr ruleexpr.Expr, engines ...platform.RuleEngine) (*RowFilter, error) {
	return compileRowFilter(info, expr, pushdown(info, expr), nil, engines...)
}

func CompilePolicy(info platform.EntityInfo, expr ruleexpr.Expr, attributes []RowAttribute, engines ...platform.RuleEngine) (*RowFilter, error) {
	return compileRowFilter(info, expr, nil, attributes, engines...)
}

func compileRowFilter(info platform.EntityInfo, expr ruleexpr.Expr, sql []persistence.StateFilter, attributes []RowAttribute, engines ...platform.RuleEngine) (*RowFilter, error) {
	filter := &RowFilter{readers: map[string]func(reflect.Value) ruleexpr.Value{}, sql: sql}
	if expr.Lit != nil && string(expr.Lit.Value) == "true" {
		return filter, nil
	}
	schema := ruleexpr.Schema{Refs: map[string]ruleexpr.Type{}}
	var visit func(ruleexpr.Expr) error
	visit = func(e ruleexpr.Expr) error {
		if e.Ref != "" {
			if _, done := schema.Refs[e.Ref]; !done {
				if i := slices.IndexFunc(attributes, func(a RowAttribute) bool { return a.Ref == e.Ref }); i >= 0 {
					schema.Refs[e.Ref], filter.readers[e.Ref] = attributes[i].Type, attributes[i].Read
					return nil
				}
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
	var engine platform.RuleEngine = platform.NativeRules{}
	if len(engines) > 0 {
		engine = engines[0]
	}
	program, err := engine.CompileRow(expr, schema)
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

func (d domainField) reader(view string) (ruleexpr.Type, func(reflect.Value) ruleexpr.Value, error) {
	text := ruleexpr.Type{Kind: "text"}
	decimal := func(v reflect.Value) ruleexpr.Value {
		x, ok := Comparable(d.read(v)).(float64)
		if !ok || math.IsNaN(x) || math.IsInf(x, 0) {
			return ruleexpr.Null("decimal")
		}
		return ruleexpr.FloatValue(x)
	}
	switch view {
	case "search":
		return text, func(v reflect.Value) ruleexpr.Value { return ruleexpr.TextValue("text", Searchable(d.read(v))) }, nil
	case "text":
		return text, func(v reflect.Value) ruleexpr.Value { return comparableText(Comparable(d.read(v))) }, nil
	case "number":
		return ruleexpr.Type{Kind: "decimal"}, decimal, nil
	case "decimal":
		if d.typ != "integer" && d.typ != "decimal" {
			return ruleexpr.Type{Kind: "decimal"}, decimal, nil
		}
		return ruleexpr.Type{Kind: "decimal"}, func(v reflect.Value) ruleexpr.Value {
			raw, err := json.Marshal(d.read(v))
			if err != nil || string(raw) == "null" {
				return ruleexpr.Null("decimal")
			}
			return ruleexpr.Value{Kind: "decimal", Status: "value", Data: raw}
		}, nil
	case "":
		switch d.typ {
		case "boolean":
			return ruleexpr.Type{Kind: "boolean"}, func(v reflect.Value) ruleexpr.Value {
				value := d.read(v)
				if value == nil {
					return ruleexpr.Null("boolean")
				}
				b := reflect.ValueOf(value)
				for b.Kind() == reflect.Pointer {
					if b.IsNil() {
						return ruleexpr.Null("boolean")
					}
					b = b.Elem()
				}
				return ruleexpr.BoolValue(b.Bool())
			}, nil
		case "money":
			return ruleexpr.Type{Kind: "money"}, func(v reflect.Value) ruleexpr.Value {
				value := d.read(v)
				if value == nil {
					return ruleexpr.Null("money")
				}
				raw, err := json.Marshal(value)
				if err != nil || string(raw) == "null" {
					return ruleexpr.Null("money")
				}
				return ruleexpr.Value{Kind: "money", Status: "value", Data: raw}
			}, nil
		case "date":
			return ruleexpr.Type{Kind: "date"}, func(v reflect.Value) ruleexpr.Value {
				value := d.read(v)
				if value == nil || Comparable(value) == "" {
					return ruleexpr.Null("date")
				}
				return ruleexpr.Present("date", value)
			}, nil
		}
		switch d.class {
		case classNumber:
			return ruleexpr.Type{Kind: "decimal"}, decimal, nil
		case classTime:
			return ruleexpr.Type{Kind: "datetime"}, func(v reflect.Value) ruleexpr.Value {
				x, ok := Comparable(d.read(v)).(time.Time)
				if !ok || x.Year() < 1 || x.Year() > 9999 {
					return ruleexpr.Null("datetime")
				}
				return ruleexpr.TimeValue(x)
			}, nil
		case classText:
			return text, func(v reflect.Value) ruleexpr.Value { return comparableText(Comparable(d.read(v))) }, nil
		case classList:
			return ruleexpr.Type{Kind: "set", Element: &text}, func(v reflect.Value) ruleexpr.Value {
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
				return ruleexpr.SetValue(items)
			}, nil
		}
	}
	return ruleexpr.Type{}, nil, fmt.Errorf("field %s has no %q view", d.name, view)
}

func comparableText(x any) ruleexpr.Value {
	switch s := x.(type) {
	case nil:
		return ruleexpr.Null("text")
	case string:
		return ruleexpr.TextValue("text", s)
	}
	return ruleexpr.TextValue("text", fmt.Sprint(x))
}

func DomainToExpr(info platform.EntityInfo, raw json.RawMessage) (ruleexpr.Expr, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return boolLit(true), nil
	}
	var terms []json.RawMessage
	if err := json.Unmarshal(raw, &terms); err != nil {
		return ruleexpr.Expr{}, err
	}
	var stack []ruleexpr.Expr
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
				return ruleexpr.Expr{}, fmt.Errorf("operator %q without its terms", op)
			}
			continue
		}
		e, err := lowerCondition(info, terms[i])
		if err != nil {
			return ruleexpr.Expr{}, err
		}
		stack = append(stack, e)
	}
	return combine("and", stack...), nil
}

func ConditionExpr(info platform.EntityInfo, field, op string, value any) (ruleexpr.Expr, error) {
	raw, err := json.Marshal([]any{field, op, value})
	if err != nil {
		return ruleexpr.Expr{}, err
	}
	return lowerCondition(info, raw)
}

func AllOf(parts ...ruleexpr.Expr) ruleexpr.Expr { return combine("and", parts...) }

func lowerCondition(info platform.EntityInfo, raw json.RawMessage) (ruleexpr.Expr, error) {
	var term []json.RawMessage
	var name, op string
	if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &name) != nil || json.Unmarshal(term[1], &op) != nil {
		return ruleexpr.Expr{}, fmt.Errorf("a condition is [field, operator, value]")
	}
	d, err := resolveDomainField(info, name)
	if err != nil {
		return ruleexpr.Expr{}, err
	}
	if number, ok := platform.DecimalLiteral(term[2]); ok {
		if d.typ != "integer" && d.typ != "decimal" {
			return ruleexpr.Expr{}, fmt.Errorf("decimal condition needs a numeric field")
		}
		if !slices.Contains([]string{"=", "!=", "<", "<=", ">", ">="}, op) {
			return ruleexpr.Expr{}, fmt.Errorf("unsupported decimal condition")
		}
		return compare(op, ruleexpr.RefOf("decimal", name+"#decimal"), literal("decimal", number.Value)), nil
	}
	var tagged map[string]json.RawMessage
	if json.Unmarshal(term[2], &tagged) == nil && tagged != nil {
		return ruleexpr.Expr{}, fmt.Errorf("invalid typed condition value")
	}
	var value any
	if json.Unmarshal(term[2], &value) != nil {
		return ruleexpr.Expr{}, fmt.Errorf("bad value")
	}
	if b, ok := value.(bool); ok {
		value = map[bool]float64{true: 1, false: 0}[b]
	}
	if d.typ == "datetime" {
		if s, ok := value.(string); ok {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				if t, err = time.Parse(time.DateOnly, s); err != nil {
					return ruleexpr.Expr{}, err
				}
			}
			value = t
		}
	}
	list := d.class == classList
	var scalar ruleexpr.Expr
	var kind string
	switch d.class {
	case classNumber:
		scalar, kind = ruleexpr.RefOf("decimal", name+"#number"), "decimal"
	case classTime:
		scalar, kind = ruleexpr.RefOf("datetime", name), "datetime"
	case classText:
		scalar, kind = ruleexpr.RefOf("text", name+"#text"), "text"
	default:
		scalar, kind = ruleexpr.RefOf("text", name+"#text"), "text"
	}
	switch op {
	case "=", "!=":
		var eq ruleexpr.Expr
		if list {
			eq = ruleexpr.Expr{Op: "in", Kind: "boolean", Args: []ruleexpr.Expr{textLit(fmt.Sprint(value)), setRef(name)}}
		} else if lit, null := operand(kind, value); null {
			eq = ruleexpr.Expr{Op: "is-null", Kind: "boolean", Args: []ruleexpr.Expr{scalar}}
		} else {
			eq = compare("=", scalar, literal(kind, lit))
		}
		if op == "!=" {
			return negate(eq), nil
		}
		return eq, nil
	case "<", "<=", ">", ">=":
		var guard []ruleexpr.Expr
		if kind != "decimal" {
			guard = append(guard, negate(compare("=", scalar, literal(kind, zeroOperand(kind)))))
		}
		lit, null := operand(kind, value)
		if null {
			if op == "<" || op == "<=" {
				return boolLit(false), nil
			}
			return combine("and", append([]ruleexpr.Expr{{Op: "is-not-null", Kind: "boolean", Args: []ruleexpr.Expr{scalar}}}, guard...)...), nil
		}
		return combine("and", append([]ruleexpr.Expr{compare(op, scalar, literal(kind, lit))}, guard...)...), nil
	case "in", "not in":
		values, _ := value.([]any)
		if values == nil {
			return ruleexpr.Expr{}, fmt.Errorf("%s needs a list", op)
		}
		var has ruleexpr.Expr
		if list {
			var items []any
			for _, item := range values {
				if item != nil {
					items = append(items, fmt.Sprint(item))
				}
			}
			var options []ruleexpr.Expr
			for chunk := range slices.Chunk(items, ruleexpr.MaxSet) {
				options = append(options, ruleexpr.Expr{Op: "overlaps", Kind: "boolean", Args: []ruleexpr.Expr{setRef(name), setLit("text", chunk)}})
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
			var options []ruleexpr.Expr
			for chunk := range slices.Chunk(items, ruleexpr.MaxSet) {
				options = append(options, ruleexpr.Expr{Op: "in", Kind: "boolean", Args: []ruleexpr.Expr{scalar, setLit(kind, chunk)}})
			}
			if nullItem {
				options = append(options, ruleexpr.Expr{Op: "is-null", Kind: "boolean", Args: []ruleexpr.Expr{scalar}})
			}
			has = combine("or", options...)
		}
		if op == "not in" {
			return negate(has), nil
		}
		return has, nil
	case "like":
		return ruleexpr.Expr{Op: "icontains", Kind: "boolean", Args: []ruleexpr.Expr{ruleexpr.RefOf("text", name+"#search"), textLit(strings.ToLower(fmt.Sprint(value)))}}, nil
	}
	return ruleexpr.Expr{}, fmt.Errorf("unknown operator %s", op)
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

func literal(kind string, value any) ruleexpr.Expr {
	return ruleexpr.Expr{Kind: kind, Lit: &ruleexpr.Literal{Value: ruleexpr.Raw(value)}}
}

func textLit(s string) ruleexpr.Expr { return literal("text", s) }

func boolLit(b bool) ruleexpr.Expr { return literal("boolean", b) }

func setRef(name string) ruleexpr.Expr { return ruleexpr.RefOf("set", name) }

func setLit(element string, items []any) ruleexpr.Expr {
	return ruleexpr.Expr{Kind: "set", Lit: &ruleexpr.Literal{Value: ruleexpr.Raw(items), Element: element}}
}

func compare(op string, left, right ruleexpr.Expr) ruleexpr.Expr {
	return ruleexpr.Expr{Op: op, Kind: "boolean", Args: []ruleexpr.Expr{left, right}}
}

func negate(e ruleexpr.Expr) ruleexpr.Expr {
	return ruleexpr.Expr{Op: "not", Kind: "boolean", Args: []ruleexpr.Expr{e}}
}

func combine(op string, parts ...ruleexpr.Expr) ruleexpr.Expr {
	var flat []ruleexpr.Expr
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
		var next []ruleexpr.Expr
		for group := range slices.Chunk(flat, 32) {
			if len(group) == 1 {
				next = append(next, group[0])
			} else {
				next = append(next, ruleexpr.Expr{Op: op, Kind: "boolean", Args: slices.Clone(group)})
			}
		}
		flat = next
	}
	return ruleexpr.Expr{Op: op, Kind: "boolean", Args: flat}
}

func pushdown(info platform.EntityInfo, expr ruleexpr.Expr) []persistence.StateFilter {
	var out []persistence.StateFilter
	var walk func(ruleexpr.Expr)
	walk = func(e ruleexpr.Expr) {
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

func stateFilter(info platform.EntityInfo, e ruleexpr.Expr) (persistence.StateFilter, bool) {
	if len(e.Args) != 2 {
		return persistence.StateFilter{}, false
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
		return persistence.StateFilter{}, false
	}
	f, ok := info.Field(field)
	if field == "id" {
		f, ok = platform.FieldInfo{Name: "id", Type: "text", Index: []int{0}}, true
	}
	if !ok || len(f.Index) == 0 || f.AliasOf != "" || f.Property != nil {
		return persistence.StateFilter{}, false
	}
	isList := f.Type == "references" || f.Type == "tags"
	if isList != list || !isList && f.Type != "text" && f.Type != "longtext" && f.Type != "reference" && f.Type != "choice" && f.Type != "date" {
		return persistence.StateFilter{}, false
	}
	filter := persistence.StateFilter{Path: []string{"value", field}, List: isList}
	if e.Op == "=" || list && e.Op == "in" {
		var value string
		if literal.Kind != "text" || json.Unmarshal(literal.Lit.Value, &value) != nil {
			return persistence.StateFilter{}, false
		}
		filter.Values = []string{value}
		return filter, true
	}
	if literal.Lit.Element != "text" || json.Unmarshal(literal.Lit.Value, &filter.Values) != nil {
		return persistence.StateFilter{}, false
	}
	return filter, true
}
