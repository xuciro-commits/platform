package objects

import (
	"platformserver/kernel/records"
	"platformserver/platform"
)

func ViewOf(m platform.Member, et *records.Type) (*records.Type, []platform.FieldInfo) {
	info, hidden := records.FieldView(m, et.Info())
	if len(hidden) == 0 {
		return et, nil
	}
	return records.NewTypeView(info, et.Rows(), et.RetainedProjection()), hidden
}
