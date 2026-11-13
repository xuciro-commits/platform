package lineage

import (
	"reflect"
	"slices"
	"sync"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

type MarkingIndex struct {
	Graph               Graph
	Generation          uint64
	datasets            map[string]string
	types               map[string]string
	datasetCompartments map[string][]string
	typeCompartments    map[string][]string
}

func IndexMarkings(graph Graph, generation uint64) MarkingIndex {
	index := MarkingIndex{Graph: graph, Generation: generation, datasets: map[string]string{}, types: map[string]string{}, datasetCompartments: map[string][]string{}, typeCompartments: map[string][]string{}}
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
	return index
}

func datasetVersion(value reflect.Value) (dataowner.DatasetVersion, bool) {
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return dataowner.DatasetVersion{}, false
		}
		value = value.Elem()
	}
	if !value.IsValid() || value.Type() != reflect.TypeFor[dataowner.DatasetVersion]() {
		return dataowner.DatasetVersion{}, false
	}
	return value.Interface().(dataowner.DatasetVersion), true
}

func (x MarkingIndex) built() bool { return x.types != nil }

func (x MarkingIndex) Dataset(id string) string { return x.datasets[id] }

func (x MarkingIndex) Type(name string) string { return x.types[name] }

func (x MarkingIndex) Row(typ string, value reflect.Value) string {
	if typ != dataowner.DatasetVersionType {
		return x.types[typ]
	}
	if version, ok := datasetVersion(value); ok {
		return x.datasets[version.Dataset]
	}
	return ""
}

func (x MarkingIndex) Visible(m platform.Member, value reflect.Value) bool {
	version, ok := datasetVersion(value)
	return !ok || (platform.MarkingSet{Level: x.datasets[version.Dataset], Compartments: x.datasetCompartments[version.Dataset]}).Cleared(m)
}

func (x MarkingIndex) Highest(typ string, value reflect.Value) string {
	if typ == dataowner.DatasetVersionType {
		if version, ok := datasetVersion(value); ok {
			return x.datasets[version.Dataset]
		}
	}
	if typ != dataowner.DatasetVersionType && typ != dataowner.DatasetType {
		return x.types[typ]
	}
	highest := ""
	for _, marking := range x.datasets {
		highest = platform.HigherMarking(highest, marking)
	}
	return highest
}

func (x MarkingIndex) Compartments(typ string, value reflect.Value) []string {
	if typ == dataowner.DatasetVersionType {
		if version, ok := datasetVersion(value); ok {
			return x.datasetCompartments[version.Dataset]
		}
	}
	return x.typeCompartments[typ]
}

type ImageMarkings struct {
	mu    sync.Mutex
	index MarkingIndex
}

type MarkingCache[K comparable] struct {
	mu    sync.Mutex
	key   K
	index MarkingIndex
	Image *ImageMarkings
}

func (c *MarkingCache[K]) Get(key K, generation uint64, image bool, build func() MarkingIndex) MarkingIndex {
	var shared *ImageMarkings
	if image && c.Image != nil {
		shared = c.Image
		shared.mu.Lock()
		defer shared.mu.Unlock()
		if shared.index.built() {
			return shared.index
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.key == key && c.index.Generation == generation && c.index.built() {
		return c.index
	}
	index := build()
	c.key, c.index = key, index
	if shared != nil {
		shared.index = index
	}
	return index
}

func (i MarkingIndex) Marked(id string) map[platform.AssetRef]bool {
	marked := map[platform.AssetRef]bool{}
	for _, node := range i.Graph.Nodes {
		if slices.Contains(node.EffectiveCompartments, id) || slices.Contains(platform.Markings[1:], id) && platform.MarkingRank(node.Effective) >= platform.MarkingRank(id) {
			marked[platform.AssetRef{App: build.ID, Kind: platform.AssetKind(node.Kind), Name: node.Name}] = true
		}
	}
	return marked
}
