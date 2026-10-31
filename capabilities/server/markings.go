package platformserver

import (
	"reflect"

	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/studio/build"
)

type markingIndex struct {
	store      *records.Store
	generation uint64
	datasets   map[string]string
	types      map[string]string
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
	t.markingMu.Lock()
	defer t.markingMu.Unlock()
	if t.markings.store == store && t.markings.generation == store.Generation() && t.markings.types != nil {
		return t.markings
	}
	graph := lineageGraph(lineageInputs{
		connections: storeRows[build.Connection](store, build.ConnectionType),
		datasets:    storeRows[build.Dataset](store, build.DatasetType),
		objects:     storeRows[build.Object](store, build.ObjectType),
		sources:     storeRows[build.Source](store, build.SourceType),
		pipelines:   storeRows[build.Pipeline](store, build.PipelineType),
		writebacks:  storeRows[build.Writeback](store, build.WritebackType),
	})
	index := markingIndex{store: store, generation: store.Generation(), datasets: map[string]string{}, types: map[string]string{}}
	for _, node := range graph.Nodes {
		switch node.Kind {
		case "dataset":
			index.datasets[node.Record] = node.Effective
		case "object":
			index.types[node.Name] = platform.HigherMarking(index.types[node.Name], node.Effective)
		}
	}
	t.markings = index
	return index
}

func (index markingIndex) row(typ string, row *records.Row) string {
	if typ != build.DatasetVersionType {
		return index.types[typ]
	}
	if version, ok := rowAs[build.DatasetVersion](row); ok {
		return index.datasets[version.Dataset]
	}
	return ""
}

func (index markingIndex) visible(m platform.Member, value reflect.Value) bool {
	version, ok := valueAs[build.DatasetVersion](value)
	return !ok || platform.MarkingCleared(m, index.datasets[version.Dataset])
}

func (index markingIndex) highest(typ string) string {
	if typ != build.DatasetVersionType && typ != build.DatasetType {
		return index.types[typ]
	}
	highest := ""
	for _, marking := range index.datasets {
		highest = platform.HigherMarking(highest, marking)
	}
	return highest
}
