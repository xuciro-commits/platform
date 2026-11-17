package records

import "reflect"

func RowsOf[T any](store *Store, typ string) []T {
	et := store.Type(typ)
	if et == nil {
		return nil
	}
	var out []T
	for _, row := range et.Rows().All() {
		if row == nil {
			continue
		}
		value := row.Value
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				continue
			}
			value = value.Elem()
		}
		var item T
		target := reflect.ValueOf(&item).Elem()
		if value.IsValid() && value.Type().AssignableTo(target.Type()) {
			target.Set(value)
			out = append(out, item)
		}
	}
	return out
}
