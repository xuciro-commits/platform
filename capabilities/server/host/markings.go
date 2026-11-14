package host

import (
	"reflect"

	"platformserver/kernel/records"
	"platformserver/services/data/lineage"
)

func (t *Tenant) markingsIn(store *records.Store, held bool) lineage.MarkingIndex {
	if !held {
		store.Lock()
		defer store.Unlock()
	}
	return t.markings.Get(store, store.Generation(), store == t.Projection, func() lineage.MarkingIndex {
		return lineage.IndexMarkings(lineage.Build(lineage.NewReader(t).Guard(lineage.StoreInput(store)), lineage.Guard), store.Generation())
	})
}

func rowValue(row *records.Row) reflect.Value {
	if row == nil {
		return reflect.Value{}
	}
	return row.Value
}
