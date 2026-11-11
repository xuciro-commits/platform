package host

import (
	dataowner "platformserver/services/data"
	"reflect"
	"sync"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/data/lineage"
)

type readMarkingCache struct {
	mu    sync.Mutex
	index markingIndex
}

type markingIndex struct {
	store               *records.Store
	graph               lineage.Graph
	generation          uint64
	datasets            map[string]string
	types               map[string]string
	datasetCompartments map[string][]string
	typeCompartments    map[string][]string
}

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

func (t *Tenant) markingsIn(store *records.Store, held bool) markingIndex {
	if !held {
		store.Lock()
		defer store.Unlock()
	}
	var shared *readMarkingCache
	if store == t.Projection && t.readMarkings != nil {
		shared = t.readMarkings
		shared.mu.Lock()
		defer shared.mu.Unlock()
		if shared.index.types != nil {
			return shared.index
		}
	}
	t.markingMu.Lock()
	defer t.markingMu.Unlock()
	if t.markings.store == store && t.markings.generation == store.Generation() && t.markings.types != nil {
		return t.markings
	}
	graph := lineage.Build(t.lineageGuard(t.lineageRecordsLocked(store)), lineage.Guard)
	index := markingIndex{store: store, graph: graph, generation: store.Generation(), datasets: map[string]string{}, types: map[string]string{}, datasetCompartments: map[string][]string{}, typeCompartments: map[string][]string{}}
	for _, node := range graph.Nodes {
		switch node.Kind {
		case "dataset":
			index.datasets[node.Record] = node.Guard
			index.datasetCompartments[node.Record] = node.GuardCompartments
		case "object":
			index.types[node.Name] = platform.HigherMarking(index.types[node.Name], node.Guard)
			index.typeCompartments[node.Name] = platform.JoinCompartments(index.typeCompartments[node.Name], node.GuardCompartments)
		}
	}
	t.markings = index
	if shared != nil {
		shared.index = index
		shared.index.store = nil
	}
	return index
}

func (index markingIndex) row(typ string, row *records.Row) string {
	if typ != dataowner.DatasetVersionType {
		return index.types[typ]
	}
	if version, ok := rowAs[dataowner.DatasetVersion](row); ok {
		return index.datasets[version.Dataset]
	}
	return ""
}

func (index markingIndex) visible(m platform.Member, value reflect.Value) bool {
	version, ok := valueAs[dataowner.DatasetVersion](value)
	return !ok || (platform.MarkingSet{Level: index.datasets[version.Dataset], Compartments: index.datasetCompartments[version.Dataset]}).Cleared(m)
}

func (index markingIndex) highest(typ string, row *records.Row) string {
	if typ == dataowner.DatasetVersionType && row != nil {
		if version, ok := rowAs[dataowner.DatasetVersion](row); ok {
			return index.datasets[version.Dataset]
		}
	}
	if typ != dataowner.DatasetVersionType && typ != dataowner.DatasetType {
		return index.types[typ]
	}
	highest := ""
	for _, marking := range index.datasets {
		highest = platform.HigherMarking(highest, marking)
	}
	return highest
}

func (index markingIndex) compartments(typ string, row *records.Row) []string {
	if typ == dataowner.DatasetVersionType {
		if version, ok := rowAs[dataowner.DatasetVersion](row); ok {
			return index.datasetCompartments[version.Dataset]
		}
	}
	return index.typeCompartments[typ]
}
