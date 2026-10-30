package platformserver

import (
	"fmt"
	"slices"

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
