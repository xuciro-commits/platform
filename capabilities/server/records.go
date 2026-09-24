package platformserver

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The record store (ADR-0016 D1): the records of every app's entity types,
// kept by the host in memory and rebuilt by replay, since every record is put
// by a decision inside its input. Apps read their own records unscoped;
// members read through one contract, scoped by each type's declaration.
// A projection into PostgreSQL (stage 3) will serve the same reads.

type recordStore struct {
	mu    sync.Mutex
	types map[string]*entityType
	byGo  map[reflect.Type]*entityType
}

type entityType struct {
	info platform.EntityInfo
	rows map[string]*row
}

type row struct {
	value   reflect.Value // the entity struct, a private copy
	history []RecordChange
}

// RecordChange is one decision's change to a record: the fields it set.
type RecordChange struct {
	Change string        `json:"change"`
	Schema string        `json:"schema"`
	By     string        `json:"by"`
	At     time.Time     `json:"at"`
	Fields []FieldChange `json:"fields"`
}

type FieldChange struct {
	Field  string          `json:"field"`
	Before json.RawMessage `json:"before,omitempty"`
	After  json.RawMessage `json:"after,omitempty"`
}

func newRecordStore() *recordStore {
	return &recordStore{types: map[string]*entityType{}, byGo: map[reflect.Type]*entityType{}}
}

// declare registers an app's entity types; references resolve within the app.
func (s *recordStore) declare(a platform.App) error {
	m := a.Manifest()
	own := map[reflect.Type]string{}
	for _, e := range m.Entities {
		own[reflect.TypeOf(e.Model)] = e.Type
	}
	classes := map[string]bool{}
	for _, d := range a.Declarations() {
		classes[d.GetDataClass()] = true
	}
	for _, e := range m.Entities {
		info, err := platform.Describe(m.ID, e, func(t reflect.Type) string { return own[t] })
		if err != nil {
			return err
		}
		if !classes[e.Type] {
			return fmt.Errorf("entity %s is not a data class the app is authority for", e.Type)
		}
		if s.types[e.Type] != nil || s.byGo[info.Go] != nil {
			return fmt.Errorf("entity %s is declared twice", e.Type)
		}
		et := &entityType{info: info, rows: map[string]*row{}}
		s.types[e.Type], s.byGo[info.Go] = et, et
	}
	return nil
}

// of is c's app's entity type for a Go type; other apps' types are not reachable (D3).
func (s *recordStore) of(c platform.Caller, t reflect.Type) *entityType {
	et := s.byGo[t]
	if et == nil || et.info.App != c.App {
		return nil
	}
	return et
}

// copyOf is a deep copy of an entity struct, through its JSON.
func copyOf(t reflect.Type, v any) reflect.Value {
	raw, _ := json.Marshal(v)
	out := reflect.New(t).Elem()
	json.Unmarshal(raw, out.Addr().Interface())
	return out
}

func recordOf(v reflect.Value) *platform.Record {
	return v.Field(0).Addr().Interface().(*platform.Record)
}

func (s *recordStore) put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, reflect.TypeOf(entity))
	if et == nil || r == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	v := copyOf(et.info.Go, entity)
	rec := recordOf(v)
	if rec.ID == "" {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	stamp := platform.Stamp{By: r.GetSubmission().GetPrincipalId(), At: r.GetRecordedTime().AsTime(), Change: r.GetChangeId()}
	prev := et.rows[rec.ID]
	change := RecordChange{Change: r.GetChangeId(), Schema: r.GetSubmission().GetSchema().GetName(), By: stamp.By, At: stamp.At, Fields: []FieldChange{}}
	rec.Changed, rec.Created, rec.Revision = stamp, stamp, 1
	if target := r.GetSubmission().GetTarget(); target.GetType() == et.info.Type && target.GetId() == rec.ID {
		rec.Revision = r.GetRevision()
	}
	if owner := et.info.Scope.Owner; prev == nil && owner != "" { // a new record belongs to its creator unless the rules say who
		if f, _ := et.info.Field(owner); v.FieldByIndex(f.Index).String() == "" {
			v.FieldByIndex(f.Index).SetString(stamp.By)
		}
	}
	var history []RecordChange
	if prev != nil {
		old := recordOf(prev.value)
		rec.Created, history = old.Created, prev.history
		if rec.Revision <= old.Revision {
			rec.Revision = old.Revision + 1 // changed by a decision about another record
		}
		if old.Archived != rec.Archived {
			change.Fields = append(change.Fields, FieldChange{Field: "archived", Before: jsonOf(old.Archived), After: jsonOf(rec.Archived)})
		}
	}
	for _, f := range et.info.Fields {
		after := jsonOf(v.FieldByIndex(f.Index).Interface())
		var before json.RawMessage
		if prev != nil {
			before = jsonOf(prev.value.FieldByIndex(f.Index).Interface())
		}
		if string(before) != string(after) && !(prev == nil && v.FieldByIndex(f.Index).IsZero()) {
			change.Fields = append(change.Fields, FieldChange{Field: f.Name, Before: before, After: after})
		}
	}
	et.rows[rec.ID] = &row{value: v, history: append(history, change)}
	return nil
}

func jsonOf(v any) json.RawMessage { raw, _ := json.Marshal(v); return raw }

func (s *recordStore) get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, t)
	if et == nil || et.rows[id] == nil {
		return nil, false
	}
	return copyOf(t, et.rows[id].value.Interface()).Interface(), true
}

// check validates an entity against its declaration (Caller.Check).
func (s *recordStore) check(c platform.Caller, entity any) *kernel.Error {
	s.mu.Lock()
	defer s.mu.Unlock()
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	et := s.of(c, reflect.TypeOf(entity))
	if et == nil {
		return invalid
	}
	v := reflect.ValueOf(entity)
	for _, f := range et.info.Fields {
		fv := v.FieldByIndex(f.Index)
		if f.Required && fv.IsZero() {
			return invalid
		}
		switch f.Type {
		case "choice":
			if !fv.IsZero() && !slices.Contains(f.Choices, fv.String()) {
				return invalid
			}
		case "date":
			if _, err := time.Parse(time.DateOnly, fv.String()); !fv.IsZero() && err != nil {
				return invalid
			}
		case "text", "longtext":
			if f.Required && strings.TrimSpace(fv.String()) == "" {
				return invalid
			}
		case "money":
			if m := fv.Interface().(platform.Money); m.Amount != 0 && len(m.Currency) != 3 {
				return invalid
			}
		case "reference", "references":
			ids := []string{fv.String()}
			if f.Type == "references" {
				ids = ids[:0]
				for i := range fv.Len() {
					ids = append(ids, fv.Index(i).String())
				}
			}
			for _, id := range ids {
				if id != "" && s.types[f.Ref].rows[id] == nil {
					return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE}
				}
			}
		}
	}
	return nil
}

// find selects records of a type; visible, when set, is the caller's scope.
func (s *recordStore) find(et *entityType, q platform.Query, visible func(reflect.Value) bool) ([]reflect.Value, int, *kernel.Error) {
	invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	match, err := compileDomain(et.info, q.Domain)
	if err != nil {
		return nil, 0, invalid
	}
	search := strings.ToLower(strings.TrimSpace(q.Search))
	var searched []platform.FieldInfo
	for _, f := range et.info.Fields {
		if f.Search {
			searched = append(searched, f)
		}
	}
	var sorts []sortKey
	for _, name := range q.Sort {
		desc := strings.HasPrefix(name, "-")
		bare := strings.TrimPrefix(name, "-")
		f, ok := et.info.Field(bare)
		if !ok && bare != "id" && bare != "created" && bare != "changed" {
			return nil, 0, invalid
		}
		sorts = append(sorts, sortKey{field: f, stamp: map[bool]string{true: bare, false: ""}[!ok], desc: desc})
	}
	var out []reflect.Value
	for _, r := range et.rows {
		v := r.value
		if !q.Archived && recordOf(v).Archived || visible != nil && !visible(v) || !match(v) {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(recordOf(v).ID), search) && !slices.ContainsFunc(searched, func(f platform.FieldInfo) bool {
			return strings.Contains(strings.ToLower(fmt.Sprint(v.FieldByIndex(f.Index).Interface())), search)
		}) {
			continue
		}
		out = append(out, v)
	}
	keys := make([][]any, len(out)) // sort keys read once, then compared
	for i, v := range out {
		for _, k := range sorts {
			keys[i] = append(keys[i], k.of(v))
		}
		keys[i] = append(keys[i], recordOf(v).ID)
	}
	index := make([]int, len(out))
	for i := range index {
		index[i] = i
	}
	slices.SortFunc(index, func(a, b int) int {
		for j := range keys[a] {
			c := compareValues(keys[a][j], keys[b][j])
			if j < len(sorts) && sorts[j].desc {
				c = -c
			}
			if c != 0 {
				return c
			}
		}
		return 0
	})
	total := len(out)
	start := min(max(q.Offset, 0), total)
	end := total
	if q.Limit > 0 {
		end = min(start+q.Limit, total)
	}
	page := make([]reflect.Value, 0, end-start)
	for _, i := range index[start:end] {
		page = append(page, out[i])
	}
	return page, total, nil
}

type sortKey struct {
	field platform.FieldInfo
	stamp string // id, created, changed: the record's own
	desc  bool
}

func (k sortKey) of(v reflect.Value) any {
	switch k.stamp {
	case "id":
		return recordOf(v).ID
	case "created":
		return recordOf(v).Created.At
	case "changed":
		return recordOf(v).Changed.At
	}
	return comparable(k.field, v.FieldByIndex(k.field.Index).Interface())
}

// comparable is a field value as it is compared and sorted: strings, numbers,
// times; money by its amount.
func comparable(f platform.FieldInfo, v any) any {
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

func compareValues(a, b any) int {
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

// compileDomain turns a domain in Odoo's prefix form into a predicate (D7):
// [["field", "op", value], "|", [...], [...], "!", [...]]; terms not joined
// by an operator are joined by "&". Operators: = != < <= > >= in "not in" like.
func compileDomain(info platform.EntityInfo, raw json.RawMessage) (func(reflect.Value) bool, error) {
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
		f, read = platform.FieldInfo{Name: "id", Type: "text"}, func(v reflect.Value) any { return recordOf(v).ID }
	case !ok:
		return nil, fmt.Errorf("unknown field %s", name)
	}
	var value any
	if json.Unmarshal(term[2], &value) != nil {
		return nil, fmt.Errorf("bad value")
	}
	if f.Type == "datetime" {
		if s, ok := value.(string); ok {
			t, err := time.Parse(time.RFC3339, s)
			if err != nil {
				return nil, err
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
		return slices.ContainsFunc(values, func(y any) bool { return compareValues(comparable(f, x), y) == 0 })
	}
	switch op {
	case "=", "!=":
		eq := func(v reflect.Value) bool {
			if list {
				return slices.Contains(items(v), fmt.Sprint(value))
			}
			return compareValues(comparable(f, read(v)), value) == 0
		}
		if op == "!=" {
			return func(v reflect.Value) bool { return !eq(v) }, nil
		}
		return eq, nil
	case "<", "<=", ">", ">=":
		textual := f.Type != "integer" && f.Type != "decimal" && f.Type != "money" && f.Type != "boolean"
		return func(v reflect.Value) bool {
			x := read(v)
			if textual && reflect.ValueOf(x).IsZero() {
				return false // an empty value is neither before nor after anything
			}
			c := compareValues(comparable(f, x), value)
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
		return func(v reflect.Value) bool { return strings.Contains(strings.ToLower(fmt.Sprint(read(v))), s) }, nil
	}
	return nil, fmt.Errorf("unknown operator %s", op)
}

// Runtime: apps put and read their own records.

func (r runtime) Put(c platform.Caller, rec *pb.ChangeRecord, entity any) *kernel.Error {
	return r.t.records.put(c, rec, entity)
}

func (r runtime) Get(c platform.Caller, t reflect.Type, id string) (any, bool) {
	return r.t.records.get(c, t, id)
}

func (r runtime) Find(c platform.Caller, t reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	s := r.t.records
	s.mu.Lock()
	defer s.mu.Unlock()
	et := s.of(c, t)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	page, total, err := s.find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = copyOf(t, v.Interface()).Interface()
	}
	return out, total, err
}

func (r runtime) Check(c platform.Caller, entity any) *kernel.Error {
	return r.t.records.check(c, entity)
}

// Members' reads (the HTTP contract).

// Entities are the entity types m may read: those of apps m holds a role in.
func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range t.records.types {
		if m.Roles[et.info.App] != "" {
			out = append(out, et.info)
		}
	}
	slices.SortFunc(out, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	return out
}

// visible is m's scope over a type's records on now's day (D4).
func (t *Tenant) visible(m platform.Member, et *entityType, now time.Time) (func(reflect.Value) bool, *kernel.Error) {
	role := m.Roles[et.info.App]
	if role == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	scope := et.info.Scope
	field := func(name string) func(reflect.Value) string {
		f, _ := et.info.Field(name)
		return func(v reflect.Value) string { return v.FieldByIndex(f.Index).String() }
	}
	switch scope.Level(role) {
	case platform.ScopeOwn:
		owner := field(scope.Owner)
		return func(v reflect.Value) bool { return owner(v) == m.ID }, nil
	case platform.ScopeUnit, platform.ScopeBelow:
		var units []string
		if t.org != nil {
			t.org.mu.Lock()
			structure := scope.Structure
			if scope.Level(role) == platform.ScopeUnit {
				structure = ""
			}
			units = t.org.units("member:"+m.ID, structure, now.UTC().Format(time.DateOnly))
			t.org.mu.Unlock()
		}
		unit := field(scope.Unit)
		return func(v reflect.Value) bool { return slices.Contains(units, unit(v)) }, nil
	}
	return nil, nil
}

// RecordPage is a page of records and how many match in all.
type RecordPage struct {
	Records []any `json:"records"`
	Total   int   `json:"total"`
}

// Records serves a member's query over a type, within the member's scope.
func (t *Tenant) Records(m platform.Member, typ string, q platform.Query, now time.Time) (RecordPage, *kernel.Error) {
	s := t.records
	s.mu.Lock()
	et := s.types[typ]
	s.mu.Unlock()
	if et == nil {
		return RecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return RecordPage{}, err
	}
	if q.Limit == 0 || q.Limit > 500 {
		q.Limit = 500
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	page, total, err := s.find(et, q, visible)
	if err != nil {
		return RecordPage{}, err
	}
	out := RecordPage{Records: make([]any, len(page)), Total: total}
	for i, v := range page {
		out.Records[i] = v.Interface()
	}
	return out, nil
}

// RecordView is one record with its history, newest first, and the records of
// the app's other types that refer to it.
type RecordView struct {
	Record  any            `json:"record"`
	History []RecordChange `json:"history"`
	Related []Related      `json:"related"`
}

type Related struct {
	Type    string `json:"type"`
	Field   string `json:"field"`
	Title   string `json:"title"`
	Records []any  `json:"records"`
	Total   int    `json:"total"`
}

func (t *Tenant) RecordOf(m platform.Member, typ, id string, now time.Time) (RecordView, *kernel.Error) {
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	s := t.records
	s.mu.Lock()
	et := s.types[typ]
	s.mu.Unlock()
	if et == nil {
		return RecordView{}, notFound
	}
	visible, err := t.visible(m, et, now)
	if err != nil {
		return RecordView{}, err
	}
	s.mu.Lock()
	r := et.rows[id]
	s.mu.Unlock()
	if r == nil || visible != nil && !visible(r.value) {
		return RecordView{}, notFound
	}
	view := RecordView{Record: r.value.Interface(), History: []RecordChange{}, Related: []Related{}}
	for i := len(r.history) - 1; i >= 0; i-- {
		view.History = append(view.History, r.history[i])
	}
	s.mu.Lock()
	var referring []*entityType
	for _, other := range s.types {
		if other.info.App == et.info.App {
			referring = append(referring, other)
		}
	}
	s.mu.Unlock()
	slices.SortFunc(referring, func(a, b *entityType) int { return strings.Compare(a.info.Type, b.info.Type) })
	for _, other := range referring {
		for _, f := range other.info.Fields {
			if f.Ref != typ {
				continue
			}
			domain, _ := json.Marshal([]any{[]any{f.Name, "=", id}})
			page, err := t.Records(m, other.info.Type, platform.Query{Domain: domain, Sort: []string{"-id"}, Limit: 20}, now)
			if err != nil {
				continue // the member may not read that type
			}
			view.Related = append(view.Related, Related{Type: other.info.Type, Field: f.Name, Title: other.info.Plural, Records: page.Records, Total: page.Total})
		}
	}
	return view, nil
}
