package platform

import "fmt"

// CheckFilterSchema reads the original field descriptors, including the member
// projection used by discovery. It introduces no alternative query language.
func CheckFilterSchema(source PageResourceSource, object EntityInfo) error {
	if source.Object == nil || source.Object.Name != object.Type || source.Object.App != object.App {
		return fmt.Errorf("filter object is unavailable")
	}
	for _, name := range source.Fields {
		field, ok := object.Field(name)
		if !ok || field.Type != "choice" && field.Type != "boolean" && field.Type != "reference" {
			return fmt.Errorf("filter field %s is unavailable or unsupported", name)
		}
	}
	return nil
}
