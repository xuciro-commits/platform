package records

import "reflect"

func RowsOf[T any](store *Store, typ string) []T {
	et := store.Type(typ)
	if et == nil {
		return nil
	}
	var out []T
	for _, row := range et.Rows().All() {
		if item, ok := RowAs[T](row); ok {
			out = append(out, item)
		}
	}
	return out
}

func RowAs[T any](row *Row) (T, bool) {
	if row == nil {
		var zero T
		return zero, false
	}
	return ValueAs[T](row.Value)
}

func ValueAs[T any](value reflect.Value) (T, bool) {
	var zero T
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return zero, false
		}
		value = value.Elem()
	}
	if !value.IsValid() {
		return zero, false
	}
	item, ok := value.Interface().(T)
	return item, ok
}
