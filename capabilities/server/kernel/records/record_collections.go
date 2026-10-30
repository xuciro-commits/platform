package records

import (
	"fmt"
	"iter"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"time"
)

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

func (order SortOrder) Keys(value reflect.Value) []any {
	keys := make([]any, 0, len(order.keys)+1)
	for _, key := range order.keys {
		keys = append(keys, key.Of(value))
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

func CompileSet(info platform.EntityInfo, p platform.RecordSetPredicate) (Plan, error) {
	expr, err := DomainToExpr(info, p.Domain)
	if err != nil {
		return Plan{}, err
	}
	base, err := CompilePlan(info, expr, p.Search)
	if err != nil {
		return Plan{}, err
	}
	if p.Set == nil {
		return base, nil
	}
	left, err := CompileSet(info, *p.Set.Inputs[0])
	if err != nil {
		return Plan{}, err
	}
	right, err := CompileSet(info, *p.Set.Inputs[1])
	if err != nil {
		return Plan{}, err
	}
	switch p.Set.Op {
	case "union":
		return Intersect(base, Union(left, right)), nil
	case "intersect":
		return Intersect(base, left, right), nil
	case "subtract":
		return Intersect(base, Subtract(left, right)), nil
	}
	return Plan{}, fmt.Errorf("unsupported record set operation")
}

type sortKey struct {
	field platform.FieldInfo
	stamp string
	desc  bool
}

func (k sortKey) Of(v reflect.Value) any {
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

type RowSource interface {
	Get(string) *Row
	IDs(after string, limit int) []string
}

type Rows struct {
	index  Index[*Row]
	source RowSource
}

func MemoryRows(index Index[*Row]) Rows { return Rows{index: index} }

func SourceRows(source RowSource) Rows { return Rows{source: source} }

func (r Rows) Stored() int { return r.index.Len() }

func (r Rows) Sourced() bool { return r.source != nil }

func (r Rows) Rebind(source RowSource) Rows { r.source = source; return r }

func (r Rows) Get(id string) *Row {
	if row := r.index.Get(id); row != nil || r.index.Has(id) {
		return row
	}
	if r.source != nil {
		return r.source.Get(id)
	}
	return nil
}

func (r *Rows) Set(id string, row *Row) { r.index.Set(id, row) }

func (r *Rows) Remove(id string) {
	if r.source == nil {
		r.index.Remove(id)
	} else {
		r.index.Set(id, nil)
	}
}

func (r Rows) Has(id string) bool { return r.Get(id) != nil }

func (r Rows) All() iter.Seq2[string, *Row] {
	if r.source == nil {
		return r.index.All()
	}
	return func(yield func(string, *Row) bool) {
		next, stop := iter.Pull2(r.index.All())
		defer stop()
		overlayID, overlayRow, hasOverlay := next()
		emitOverlay := func() bool {
			if overlayRow != nil && !yield(overlayID, overlayRow) {
				return false
			}
			overlayID, overlayRow, hasOverlay = next()
			return true
		}
		after := ""
		for {
			ids := r.source.IDs(after, 256)
			for _, id := range ids {
				for hasOverlay && overlayID < id {
					if !emitOverlay() {
						return
					}
				}
				if hasOverlay && overlayID == id {
					if !emitOverlay() {
						return
					}
					continue
				}
				if row := r.source.Get(id); row != nil && !yield(id, row) {
					return
				}
			}
			if len(ids) < 256 {
				break
			}
			after = ids[len(ids)-1]
		}
		for hasOverlay {
			if !emitOverlay() {
				return
			}
		}
	}
}

func (r Rows) Len() int {
	if r.source == nil {
		return r.index.Len()
	}
	n := 0
	after := ""
	for {
		ids := r.source.IDs(after, 256)
		for _, id := range ids {
			if !r.index.Has(id) {
				n++
			}
		}
		if len(ids) < 256 {
			break
		}
		after = ids[len(ids)-1]
	}
	for _, row := range r.index.All() {
		if row != nil {
			n++
		}
	}
	return n
}

func (r Rows) Overlay() Index[*Row] { return r.index }

func (r Rows) IDs(after string, limit int) []string {
	if limit <= 0 {
		return nil
	}
	if r.source == nil {
		var ids []string
		for id, row := range r.index.after(after) {
			if row != nil {
				ids = append(ids, id)
				if len(ids) == limit {
					break
				}
			}
		}
		return ids
	}
	ids := r.source.IDs(after, limit+r.index.Len())
	for id, row := range r.index.after(after) {
		if row != nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	ids = slices.Compact(ids)
	out := ids[:0]
	for _, id := range ids {
		if r.index.Has(id) && r.index.Get(id) == nil {
			continue
		}
		out = append(out, id)
		if len(out) == limit {
			break
		}
	}
	return out
}
