package host

import (
	"reflect"

	"platformserver/kernel/records"
	"platformserver/services/data/lineage"
)

func storeRows[T any](store *records.Store, typ string) []T {
	et := store.Type(typ)
	if et == nil {
		return nil
	}
	var out []T
	for _, row := range et.Rows().All() {
		if item, ok := rowAs[T](row); ok {
			out = append(out, item)
		}
	}
	return out
}

func rowAs[T any](row *records.Row) (T, bool) {
	if row == nil {
		var zero T
		return zero, false
	}
	return valueAs[T](row.Value)
}

func valueAs[T any](value reflect.Value) (T, bool) {
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

func (t *Tenant) markingsIn(store *records.Store, held bool) lineage.MarkingIndex {
	if !held {
		store.Lock()
		defer store.Unlock()
	}
	return t.markings.Get(store, store.Generation(), store == t.Projection, func() lineage.MarkingIndex {
		return lineage.IndexMarkings(lineage.Build(t.lineageGuard(t.lineageRecordsLocked(store)), lineage.Guard), store.Generation())
	})
}

func rowValue(row *records.Row) reflect.Value {
	if row == nil {
		return reflect.Value{}
	}
	return row.Value
}
