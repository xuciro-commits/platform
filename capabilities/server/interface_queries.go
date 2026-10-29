package platformserver

import (
	"fmt"
	"platformserver/kernel/records"
	"reflect"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

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
		for _, et := range t.records.SortedTypes() {
			info := et.Info()
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
	info := platform.EntityInfo{}
	for _, f := range q.InterfaceShape.Fields {
		info.Fields = append(info.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type})
	}
	return checkQueryFields(q, info)
}

func (t *Tenant) interfaceRecordsRead(m platform.Member, name string, query platform.Query, now time.Time) (InterfaceRecordPage, *kernel.Error) {
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
	entity, view *records.Type
	hidden       []platform.FieldInfo
	visible      func(reflect.Value) bool
}
type interfaceRow struct {
	read  interfaceRead
	value reflect.Value
	keys  []any
}

func (t *Tenant) interfaceRecordsFrom(store *records.Store, m platform.Member, declaration platform.NamedQuery, query platform.Query, now time.Time) (InterfaceRecordPage, *kernel.Error) {
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
		store.Lock()
		et := store.Type(typ)
		store.Unlock()
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
		if q.InterfaceShape.Implements(view.Info()) != nil {
			continue
		}
		commonInfo := view.Info()
		commonInfo.Fields = slices.DeleteFunc(slices.Clone(view.Info().Fields), func(field platform.FieldInfo) bool {
			return !slices.ContainsFunc(q.InterfaceShape.Fields, func(common platform.InterfaceField) bool { return common.Name == field.Name })
		})
		commonView := records.NewTypeView(commonInfo, view.Rows(), view.RetainedProjection())
		reads = append(reads, interfaceRead{et, commonView, hidden, visible})
	}
	store.Lock()
	defer store.Unlock()
	var rows []interfaceRow
	var order records.SortOrder
	for _, read := range reads {
		var problem error
		order, problem = records.CompileOrder(read.view.Info(), query.Sort)
		if problem != nil {
			return InterfaceRecordPage{}, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		part, err := store.MatchingQuery(read.view, platform.Query{Domain: query.Domain, Search: query.Search, Archived: query.Archived}, read.visible)
		if err != nil {
			return InterfaceRecordPage{}, err
		}
		for _, value := range part {
			row := interfaceRow{read: read, value: value, keys: order.Keys(value)}
			row.keys = append(row.keys, read.entity.Info().Type)
			rows = append(rows, row)
		}
	}
	slices.SortFunc(rows, func(a, b interfaceRow) int { return order.Compare(a.keys, b.keys) })
	out := InterfaceRecordPage{Records: []InterfaceRecord{}, Total: len(rows)}
	start := min(query.Offset, len(rows))
	end := min(start+query.Limit, len(rows))
	for _, row := range rows[start:end] {
		value := reflect.ValueOf(records.Mask(row.read.entity.Info(), row.value, row.read.hidden))
		if row.read.entity.Info().HasReadSources() {
			value, _ = t.narrowerIn(store, m, now, true).derive(row.read.entity, value)
		}
		rec := records.Header(row.value)
		fields := map[string]any{"id": rec.ID, "revision": rec.Revision, "created": rec.Created, "changed": rec.Changed}
		for _, common := range q.InterfaceShape.Fields {
			f, _ := row.read.entity.Info().Field(common.Name)
			fields[f.Name] = value.FieldByIndex(f.Index).Interface()
		}
		out.Records = append(out.Records, InterfaceRecord{Type: row.read.entity.Info().Type, ID: rec.ID, Record: fields})
		t.readPersonal(m, row.read.view, []string{rec.ID}, now)
	}
	return out, nil
}
