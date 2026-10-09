package platformserver

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// An interface is a read contract, not a record type. Every result retains
// its actual target so duplicate IDs never collapse or route an action wrong.
type InterfaceRecord struct {
	Type   string         `json:"type"`
	ID     string         `json:"id"`
	Record map[string]any `json:"record"`
}
type InterfaceRecordPage struct {
	Records []InterfaceRecord `json:"records"`
	Total   int               `json:"total"`
}

func (t *Tenant) bindInterfaceQuery(q platform.NamedQuery, drafts ...platform.EntityInfo) (platform.NamedQuery, error) {
	overlay := map[string]platform.EntityInfo{}
	for _, info := range drafts {
		overlay[info.Type] = info
	}
	declared := (hostView{t: t}).Interfaces()
	i := slices.IndexFunc(declared, func(shape platform.Interface) bool { return shape.Name == q.Interface })
	if i < 0 {
		return q, fmt.Errorf("interface %s is not declared", q.Interface)
	}
	shape := declared[i]
	fresh := q.InterfaceShape == nil
	if fresh {
		q.InterfaceShape = &shape
		for _, et := range t.records.sortedTypes() {
			info := et.info
			if next, ok := overlay[info.Type]; ok {
				info = next
			}
			if slices.Contains(info.Implements, q.Interface) {
				q.Implementations = append(q.Implementations, info.Type)
			}
		}
		for _, info := range drafts {
			if slices.Contains(info.Implements, q.Interface) && !slices.Contains(q.Implementations, info.Type) {
				q.Implementations = append(q.Implementations, info.Type)
			}
		}
		slices.Sort(q.Implementations)
	} else {
		for _, field := range q.InterfaceShape.Fields {
			if !slices.ContainsFunc(shape.Fields, func(f platform.InterfaceField) bool { return f.Name == field.Name && f.Type == field.Type }) {
				return q, fmt.Errorf("interface %s changed its published field %s", q.Interface, field.Name)
			}
		}
	}
	if err := q.Check(); err != nil {
		return q, err
	}
	for _, typ := range q.Implementations {
		info, ok := t.entity(typ)
		if next, exists := overlay[typ]; exists {
			info, ok = next, true
		}
		if !ok || !slices.Contains(info.Implements, q.Interface) || q.InterfaceShape.Implements(info) != nil {
			return q, fmt.Errorf("interface %s needs its published implementation %s", q.Interface, typ)
		}
	}
	if err := checkInterfaceQueryFields(q); err != nil {
		return q, err
	}
	return q, nil
}

func checkInterfaceQueryFields(q platform.NamedQuery) error {
	// Schema validation only; these fields are never registered as an entity
	// or used to execute a predicate. Execution compiles each original type.
	info := platform.EntityInfo{}
	for _, f := range q.InterfaceShape.Fields {
		info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type})
	}
	return checkQueryFields(q, info)
}

func visibleInterfaceQuery(q platform.NamedQuery, entities map[string]platform.EntityInfo) (platform.NamedQuery, bool) {
	if q.InterfaceShape == nil {
		return q, false
	}
	empty := len(q.Implementations) == 0
	q.Implementations = slices.DeleteFunc(slices.Clone(q.Implementations), func(typ string) bool {
		info, ok := entities[typ]
		return !ok || !slices.Contains(info.Implements, q.Interface) || q.InterfaceShape.Implements(info) != nil
	})
	return q, empty || len(q.Implementations) > 0
}

// InterfaceRecords uses current implementers; published named queries use
// their frozen list through the same reader below.
func (t *Tenant) InterfaceRecords(m platform.Member, name string, query platform.Query, now time.Time) (InterfaceRecordPage, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return InterfaceRecordPage{}, err
	}
	q, err := t.bindInterfaceQuery(platform.NamedQuery{Name: "lookup", Title: "Interface lookup", Interface: name})
	if err != nil {
		return InterfaceRecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return t.interfaceRecordsFrom(t.records, m, q, query, now)
}

type interfaceRead struct {
	entity, view *entityType
	hidden       []platform.FieldInfo
	visible      func(reflect.Value) bool
}
type interfaceRow struct {
	read  interfaceRead
	value reflect.Value
	keys  []any
}

func (t *Tenant) interfaceRecordsFrom(store *recordStore, m platform.Member, declaration platform.NamedQuery, query platform.Query, now time.Time) (InterfaceRecordPage, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return InterfaceRecordPage{}, err
	}
	q := declaration
	q.Domain, q.Sort = query.Domain, query.Sort
	if err := q.Check(); err != nil {
		return InterfaceRecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if err := checkInterfaceQueryFields(q); err != nil {
		return InterfaceRecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	if query.Traversal != nil || query.Set != nil {
		return InterfaceRecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Interface reads use common-field conditions, search and sorting")
	}
	if query.Offset < 0 || query.Limit < 0 {
		return InterfaceRecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "offset and limit are non-negative")
	}
	if query.Limit == 0 || query.Limit > 200 {
		query.Limit = 200
	}
	var reads []interfaceRead
	for _, typ := range q.Implementations {
		store.mu.Lock()
		et := store.types[typ]
		store.mu.Unlock()
		if et == nil {
			return InterfaceRecordPage{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published query implementation is unavailable")
		}
		visible, err := t.visibleIn(store, m, et, now)
		if err != nil {
			if err.Code == pb.ErrorCode_ERROR_CODE_POLICY_DENIED {
				continue
			}
			return InterfaceRecordPage{}, err
		}
		view, hidden := viewOf(m, et)
		if q.InterfaceShape.Implements(view.info) != nil {
			continue
		}
		reads = append(reads, interfaceRead{et, view, hidden, visible})
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	var rows []interfaceRow
	for _, read := range reads {
		part, _, err := store.find(read.view, platform.Query{Domain: query.Domain, Search: query.Search, Sort: query.Sort, Archived: query.Archived}, read.visible)
		if err != nil {
			return InterfaceRecordPage{}, err
		}
		for _, value := range part {
			row := interfaceRow{read: read, value: value}
			for _, name := range query.Sort {
				key := strings.TrimPrefix(name, "-")
				field, found := read.view.info.Field(key)
				sort := sortKey{field: field}
				if !found {
					sort.stamp = key
				}
				row.keys = append(row.keys, sort.of(value))
			}
			row.keys = append(row.keys, recordOf(value).ID, read.entity.info.Type)
			rows = append(rows, row)
		}
	}
	slices.SortFunc(rows, func(a, b interfaceRow) int {
		for i, key := range a.keys {
			order := compareValues(key, b.keys[i])
			if i < len(query.Sort) && strings.HasPrefix(query.Sort[i], "-") {
				order = -order
			}
			if order != 0 {
				return order
			}
		}
		return 0
	})
	out := InterfaceRecordPage{Records: []InterfaceRecord{}, Total: len(rows)}
	start := min(query.Offset, len(rows))
	end := min(start+query.Limit, len(rows))
	for _, row := range rows[start:end] {
		value := reflect.ValueOf(masked(row.read.entity, row.value, row.read.hidden))
		if len(row.read.entity.info.Derived) > 0 {
			value, _ = t.narrowerIn(store, m, now, true).derive(row.read.entity, value)
		}
		rec := recordOf(row.value)
		fields := map[string]any{"id": rec.ID, "revision": rec.Revision, "created": rec.Created, "changed": rec.Changed}
		for _, common := range q.InterfaceShape.Fields {
			f, _ := row.read.entity.info.Field(common.Name)
			fields[f.Name] = value.FieldByIndex(f.Index).Interface()
		}
		out.Records = append(out.Records, InterfaceRecord{Type: row.read.entity.info.Type, ID: rec.ID, Record: fields})
		t.readPersonal(m, row.read.view, []string{rec.ID}, now)
	}
	return out, nil
}
