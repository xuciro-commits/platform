package platformserver

import (
	"platformserver/kernel/records"
	"platformserver/platform"
	"slices"
)

func (t *Tenant) aggregateView(member platform.Member, et *records.Type) *records.Type {
	view, _ := viewOf(member, et)
	if fields := t.narrowable(et); len(fields) > 0 {
		info := view.Info()
		info.Fields = slices.DeleteFunc(slices.Clone(info.Fields), func(field platform.FieldInfo) bool { return slices.Contains(fields, field.Name) })
		view = records.NewTypeView(info, view.Rows(), view.RetainedProjection())
	}
	return view
}
