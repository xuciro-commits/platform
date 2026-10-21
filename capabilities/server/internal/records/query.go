package records

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

// SortOrder contains validated field readers and the requested directions.
// Header ID is the final stable key for all ordinary record queries.
type SortOrder struct{ keys []sortKey }

func CompileOrder(info platform.EntityInfo, names []string) (SortOrder, error) {
	var order SortOrder
	for _, name := range names {
		bare := strings.TrimPrefix(name, "-")
		field, ok := info.Field(bare)
		if !ok && bare != "id" && bare != "created" && bare != "changed" {
			return SortOrder{}, fmt.Errorf("query sort field %s is unavailable", bare)
		}
		key := sortKey{field: field, desc: strings.HasPrefix(name, "-")}
		if !ok {
			key.stamp = bare
		}
		order.keys = append(order.keys, key)
	}
	return order, nil
}

// Keys reads each key once, including the stable record ID. Interface queries
// append the concrete object type to keep equal IDs in different objects distinct.
func (order SortOrder) Keys(value reflect.Value) []any {
	keys := make([]any, 0, len(order.keys)+1)
	for _, key := range order.keys {
		keys = append(keys, key.of(value))
	}
	return append(keys, Header(value).ID)
}

func (order SortOrder) Compare(a, b []any) int {
	for i := range a {
		c := CompareValues(a[i], b[i])
		if i < len(order.keys) && order.keys[i].desc {
			c = -c
		}
		if c != 0 {
			return c
		}
	}
	return 0
}

// SortedPage keeps only the requested prefix while counting all authorized
// matches. Unlimited owner queries retain their explicitly requested result.
type SortedPage struct {
	order                      SortOrder
	offset, limit, keep, total int
	rows                       []sortedRow
}
type sortedRow struct {
	value reflect.Value
	keys  []any
}

func (order SortOrder) Page(offset, limit int) *SortedPage {
	page := &SortedPage{order: order, offset: max(offset, 0), limit: limit}
	if limit > 0 {
		page.keep = int(^uint(0) >> 1)
		if page.offset <= page.keep-limit {
			page.keep = page.offset + limit
		}
	}
	return page
}

func (p *SortedPage) Add(value reflect.Value) {
	p.total++
	row := sortedRow{value: value, keys: p.order.Keys(value)}
	if p.keep == 0 {
		p.rows = append(p.rows, row)
		return
	}
	if len(p.rows) < p.keep {
		p.rows = append(p.rows, row)
		for i := len(p.rows) - 1; i > 0; {
			parent := (i - 1) / 2
			if p.order.Compare(p.rows[i].keys, p.rows[parent].keys) <= 0 {
				break
			}
			p.rows[i], p.rows[parent] = p.rows[parent], p.rows[i]
			i = parent
		}
		return
	}
	if p.order.Compare(row.keys, p.rows[0].keys) >= 0 {
		return
	}
	p.rows[0] = row
	for i := 0; ; {
		child := i*2 + 1
		if child >= len(p.rows) {
			break
		}
		if right := child + 1; right < len(p.rows) && p.order.Compare(p.rows[right].keys, p.rows[child].keys) > 0 {
			child = right
		}
		if p.order.Compare(p.rows[child].keys, p.rows[i].keys) <= 0 {
			break
		}
		p.rows[i], p.rows[child] = p.rows[child], p.rows[i]
		i = child
	}
}

func (p *SortedPage) Result() ([]reflect.Value, int) {
	slices.SortFunc(p.rows, func(a, b sortedRow) int { return p.order.Compare(a.keys, b.keys) })
	start := min(p.offset, len(p.rows))
	end := len(p.rows)
	if p.limit > 0 {
		end = start + min(p.limit, end-start)
	}
	out := make([]reflect.Value, 0, end-start)
	for _, row := range p.rows[start:end] {
		out = append(out, row.value)
	}
	p.rows = nil
	return out, p.total
}

// One predicate evaluates all source sets against one authorized row. Every
// branch is compiled using the member's field projection before any row runs.
func CompileSet(info platform.EntityInfo, p platform.RecordSetPredicate) (func(reflect.Value) bool, error) {
	domain, err := CompileDomain(info, p.Domain)
	if err != nil {
		return nil, err
	}
	search := strings.ToLower(strings.TrimSpace(p.Search))
	var searched []platform.FieldInfo
	for _, f := range info.Fields {
		if f.Search {
			searched = append(searched, f)
		}
	}
	base := func(v reflect.Value) bool {
		return domain(v) && (search == "" || strings.Contains(strings.ToLower(Header(v).ID), search) || slices.ContainsFunc(searched, func(f platform.FieldInfo) bool {
			return strings.Contains(strings.ToLower(Searchable(v.FieldByIndex(f.Index).Interface())), search)
		}))
	}
	if p.Set == nil {
		return base, nil
	}
	// CheckSet bounds the shape before this compiler is called.
	left, err := CompileSet(info, *p.Set.Inputs[0])
	if err != nil {
		return nil, err
	}
	right, err := CompileSet(info, *p.Set.Inputs[1])
	if err != nil {
		return nil, err
	}
	switch p.Set.Op {
	case "union":
		return func(v reflect.Value) bool { return base(v) && (left(v) || right(v)) }, nil
	case "intersect":
		return func(v reflect.Value) bool { return base(v) && left(v) && right(v) }, nil
	case "subtract":
		return func(v reflect.Value) bool { return base(v) && left(v) && !right(v) }, nil
	}
	return nil, fmt.Errorf("unsupported record set operation")
}

type sortKey struct {
	field platform.FieldInfo
	stamp string // id, created, changed: the record's own
	desc  bool
}

func (k sortKey) of(v reflect.Value) any {
	switch k.stamp {
	case "id":
		return Header(v).ID
	case "created":
		return Header(v).Created.At
	case "changed":
		return Header(v).Changed.At
	}
	return Comparable(v.FieldByIndex(k.field.Index).Interface())
}

// Comparable is a field value as it is compared and sorted: strings, numbers,
// times; money by its amount.
func Comparable(v any) any {
	if v != nil {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			rv = rv.Elem()
		}
		v = rv.Interface()
	}
	switch x := v.(type) {
	case platform.Money:
		return float64(x.Amount)
	case time.Time:
		return x
	case bool:
		if x {
			return 1.0
		}
		return 0.0
	}
	if v == nil {
		return nil
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint())
	case reflect.Float32, reflect.Float64:
		return rv.Float()
	case reflect.String:
		return rv.String()
	}
	return fmt.Sprint(v)
}

func Searchable(v any) string {
	x := Comparable(v)
	if x == nil {
		return ""
	}
	return fmt.Sprint(x)
}

func CompareValues(a, b any) int {
	if a == nil {
		if b == nil {
			return 0
		}
		return -1
	}
	if b == nil {
		return 1
	}
	switch x := a.(type) {
	case float64:
		y, _ := b.(float64)
		switch {
		case x < y:
			return -1
		case x > y:
			return 1
		}
		return 0
	case time.Time:
		y, _ := b.(time.Time)
		return x.Compare(y)
	}
	return strings.Compare(fmt.Sprint(a), fmt.Sprint(b))
}

// CompileDomain turns a domain in Odoo's prefix form into a predicate (D7):
// [["field", "op", value], "|", [...], [...], "!", [...]]; terms not joined
// by an operator are joined by "&". Operators: = != < <= > >= in "not in" like.
func CompileDomain(info platform.EntityInfo, raw json.RawMessage) (func(reflect.Value) bool, error) {
	all := func(reflect.Value) bool { return true }
	if len(raw) == 0 || string(raw) == "null" {
		return all, nil
	}
	var terms []json.RawMessage
	if err := json.Unmarshal(raw, &terms); err != nil {
		return nil, err
	}
	type pred = func(reflect.Value) bool
	var stack []pred
	for i := len(terms) - 1; i >= 0; i-- {
		var op string
		if json.Unmarshal(terms[i], &op) == nil {
			switch {
			case op == "!" && len(stack) >= 1:
				p := stack[len(stack)-1]
				stack[len(stack)-1] = func(v reflect.Value) bool { return !p(v) }
			case (op == "&" || op == "|") && len(stack) >= 2:
				a, b := stack[len(stack)-1], stack[len(stack)-2]
				stack = stack[:len(stack)-2]
				if op == "&" {
					stack = append(stack, func(v reflect.Value) bool { return a(v) && b(v) })
				} else {
					stack = append(stack, func(v reflect.Value) bool { return a(v) || b(v) })
				}
			default:
				return nil, fmt.Errorf("operator %q without its terms", op)
			}
			continue
		}
		p, err := condition(info, terms[i])
		if err != nil {
			return nil, err
		}
		stack = append(stack, p)
	}
	return func(v reflect.Value) bool {
		for _, p := range stack {
			if !p(v) {
				return false
			}
		}
		return true
	}, nil
}

func condition(info platform.EntityInfo, raw json.RawMessage) (func(reflect.Value) bool, error) {
	var term []json.RawMessage
	var name, op string
	if json.Unmarshal(raw, &term) != nil || len(term) != 3 || json.Unmarshal(term[0], &name) != nil || json.Unmarshal(term[1], &op) != nil {
		return nil, fmt.Errorf("a condition is [field, operator, value]")
	}
	f, ok := info.Field(name)
	read := func(v reflect.Value) any { return v.FieldByIndex(f.Index).Interface() }
	switch {
	case name == "id":
		f, read = platform.FieldInfo{Name: "id", Type: "text"}, func(v reflect.Value) any { return Header(v).ID }
	case name == "created" || name == "changed": // the record's stamps, as datetimes
		f, read = platform.FieldInfo{Name: name, Type: "datetime"}, func(v reflect.Value) any {
			if name == "created" {
				return Header(v).Created.At
			}
			return Header(v).Changed.At
		}
	case !ok:
		return nil, fmt.Errorf("unknown field %s", name)
	}
	if value, ok := platform.DecimalLiteral(term[2]); ok {
		return value.Condition(f, op, read)
	}
	var tagged map[string]json.RawMessage
	if json.Unmarshal(term[2], &tagged) == nil && tagged != nil {
		return nil, fmt.Errorf("invalid typed condition value")
	}
	var value any
	if json.Unmarshal(term[2], &value) != nil {
		return nil, fmt.Errorf("bad value")
	}
	if b, ok := value.(bool); ok { // compared as booleans are: 1 and 0
		value = map[bool]float64{true: 1, false: 0}[b]
	}
	if f.Type == "datetime" {
		if s, ok := value.(string); ok { // a time, or a day from its midnight (UTC)
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				if t, err = time.Parse(time.DateOnly, s); err != nil {
					return nil, err
				}
			}
			value = t
		}
	}
	list := f.Type == "references" || f.Type == "tags"
	items := func(v reflect.Value) []string {
		var out []string
		fv := v.FieldByIndex(f.Index)
		for i := range fv.Len() {
			out = append(out, fv.Index(i).String())
		}
		return out
	}
	values, _ := value.([]any)
	in := func(x any) bool {
		return slices.ContainsFunc(values, func(y any) bool { return CompareValues(Comparable(x), y) == 0 })
	}
	switch op {
	case "=", "!=":
		eq := func(v reflect.Value) bool {
			if list {
				return slices.Contains(items(v), fmt.Sprint(value))
			}
			return CompareValues(Comparable(read(v)), value) == 0
		}
		if op == "!=" {
			return func(v reflect.Value) bool { return !eq(v) }, nil
		}
		return eq, nil
	case "<", "<=", ">", ">=":
		textual := f.Type != "integer" && f.Type != "decimal" && f.Type != "money" && f.Type != "boolean"
		return func(v reflect.Value) bool {
			x := Comparable(read(v))
			if x == nil || textual && reflect.ValueOf(x).IsZero() {
				return false // an empty value is neither before nor after anything
			}
			c := CompareValues(x, value)
			return op == "<" && c < 0 || op == "<=" && c <= 0 || op == ">" && c > 0 || op == ">=" && c >= 0
		}, nil
	case "in", "not in":
		if values == nil {
			return nil, fmt.Errorf("%s needs a list", op)
		}
		has := func(v reflect.Value) bool {
			if list {
				return slices.ContainsFunc(items(v), func(x string) bool { return in(x) })
			}
			return in(read(v))
		}
		if op == "not in" {
			return func(v reflect.Value) bool { return !has(v) }, nil
		}
		return has, nil
	case "like":
		s := strings.ToLower(fmt.Sprint(value))
		return func(v reflect.Value) bool { return strings.Contains(strings.ToLower(Searchable(read(v))), s) }, nil
	}
	return nil, fmt.Errorf("unknown operator %s", op)
}
