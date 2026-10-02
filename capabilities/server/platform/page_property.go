package platform

import "fmt"

func PropertyValueType(field FieldInfo) string {
	switch field.Type {
	case "text", "longtext", "choice":
		return "string"
	case "boolean":
		return "boolean"
	case "integer", "decimal":
		return "decimal"
	}
	return ""
}
func (p Page) CheckPropertySchema(variable PageVariable, object EntityInfo) error {
	source := variable.Source
	if source == nil || source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App || p.RecordVariableObject(source.Variable) != object.Type {
		return fmt.Errorf("property record object is unavailable or incompatible")
	}
	field, ok := object.Field(source.Field)
	if !ok || PropertyValueType(field) == "" || PropertyValueType(field) != variable.Type {
		return fmt.Errorf("property field is unavailable or incompatible")
	}
	return nil
}
