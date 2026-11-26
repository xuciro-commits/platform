package lineage

import (
	"platformserver/kernel/records"
	dataowner "platformserver/services/data"
	"platformserver/services/ontology"
)

var markingTypes = []string{dataowner.ConnectionType, dataowner.SourceType, dataowner.DatasetType, dataowner.PipelineType, ontology.ObjectType, dataowner.WritebackType}

// MarkingKey names everything a marking index over store reads. It is false while
// the store holds changes of its own or a release installation plans names.
type MarkingKey struct {
	Root        *records.Store
	Versions    string
	Definitions uint64
}

func MarkingKeyOf(store *records.Store, owner *dataowner.Owner, definitions uint64) (MarkingKey, bool) {
	if owner != nil && len(owner.DraftIDs()) > 0 {
		return MarkingKey{}, false
	}
	root, versions, ok := store.Fingerprint(markingTypes...)
	return MarkingKey{Root: root, Versions: versions, Definitions: definitions}, ok
}

func StoreInput(store *records.Store) Input {
	return Input{
		Connections: records.RowsOf[dataowner.Connection](store, dataowner.ConnectionType),
		Sources:     records.RowsOf[dataowner.Source](store, dataowner.SourceType),
		Datasets:    records.RowsOf[dataowner.Dataset](store, dataowner.DatasetType),
		Pipelines:   records.RowsOf[dataowner.Pipeline](store, dataowner.PipelineType),
		Objects:     records.RowsOf[ontology.Object](store, ontology.ObjectType),
		Writebacks:  records.RowsOf[dataowner.Writeback](store, dataowner.WritebackType),
	}
}
