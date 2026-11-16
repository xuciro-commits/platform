package pages

import (
	"fmt"

	"platformserver/platform"
	"platformserver/services/objects"
)

func CheckPropertySchema(p platform.Page, variable platform.PageVariable, object platform.EntityInfo) error {
	source := variable.Source
	if source == nil || source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App || p.RecordVariableObject(source.Variable) != object.Type {
		return fmt.Errorf("property record object is unavailable or incompatible")
	}
	field, ok := object.Field(source.Field)
	if !ok || objects.PropertyValueType(field) == "" || objects.PropertyValueType(field) != variable.Type {
		return fmt.Errorf("property field is unavailable or incompatible")
	}
	return nil
}
