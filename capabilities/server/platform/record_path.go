package platform

import "fmt"

// A record path crosses only declared single-record references. The same
// lookup can validate a publication or a member-filtered discovery projection.
func RecordPathField(typ string, path []string, entity func(string) (EntityInfo, bool)) (FieldInfo, error) {
	if len(path) == 0 || len(path) > 16 {
		return FieldInfo{}, fmt.Errorf("a record path needs 1–16 declared fields")
	}
	var field FieldInfo
	for i, name := range path {
		info, known := entity(typ)
		var found bool
		field, found = info.Field(name)
		if !known || !found {
			return field, fmt.Errorf("record path field %s.%s is unavailable", typ, name)
		}
		if i < len(path)-1 {
			if field.Type != "reference" || field.Ref == "" {
				return field, fmt.Errorf("record path field %s.%s is not a single-record reference", typ, name)
			}
			typ = field.Ref
		}
	}
	return field, nil
}
