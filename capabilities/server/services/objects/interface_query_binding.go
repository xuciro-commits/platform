package objects

import (
	"fmt"
	"slices"

	"platformserver/platform"
)

func BindInterfaceQuery(q platform.NamedQuery, declared []platform.Interface, types []platform.EntityInfo, entity func(string) (platform.EntityInfo, bool), engine platform.RuleEngine, drafts ...platform.EntityInfo) (platform.NamedQuery, error) {
	overlay := map[string]platform.EntityInfo{}
	for _, info := range drafts {
		overlay[info.Type] = info
	}
	i := slices.IndexFunc(declared, func(shape platform.Interface) bool { return shape.Name == q.Interface })
	if i < 0 {
		return q, fmt.Errorf("interface %s is not declared", q.Interface)
	}
	shape := declared[i]
	fresh := q.InterfaceShape == nil
	if fresh {
		q.InterfaceShape = &shape
		for _, info := range types {
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
		info, ok := entity(typ)
		if next, exists := overlay[typ]; exists {
			info, ok = next, true
		}
		if !ok || !slices.Contains(info.Implements, q.Interface) || q.InterfaceShape.Implements(info) != nil {
			return q, fmt.Errorf("interface %s needs its published implementation %s", q.Interface, typ)
		}
	}
	if err := CheckInterfaceQueryFields(q, engine); err != nil {
		return q, err
	}
	return q, nil
}

func CheckInterfaceQueryFields(q platform.NamedQuery, engine platform.RuleEngine) error {
	info := platform.EntityInfo{}
	for _, field := range q.InterfaceShape.Fields {
		info.Fields = append(info.Fields, platform.FieldInfo{Name: field.Name, Type: field.Type})
	}
	return CheckQueryFields(q, info, engine)
}
