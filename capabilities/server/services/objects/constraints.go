package objects

import (
	"platformserver/kernel/records"
	"platformserver/platform"
	"reflect"
)

func constraintValues(info platform.EntityInfo, value reflect.Value) map[string]any {
	values := map[string]any{"id": records.Header(value).ID, "archived": records.Header(value).Archived}
	for _, field := range info.Fields {
		values[field.Name] = value.FieldByIndex(field.Index).Interface()
	}
	return values
}

func ConstraintValues(info platform.EntityInfo, value reflect.Value) map[string]any {
	return constraintValues(info, value)
}
