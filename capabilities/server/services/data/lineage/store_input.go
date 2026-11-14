package lineage

import (
	"platformserver/kernel/records"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
)

func StoreInput(store *records.Store) Input {
	return Input{
		Connections: records.RowsOf[dataowner.Connection](store, dataowner.ConnectionType),
		Sources:     records.RowsOf[dataowner.Source](store, dataowner.SourceType),
		Datasets:    records.RowsOf[dataowner.Dataset](store, dataowner.DatasetType),
		Pipelines:   records.RowsOf[dataowner.Pipeline](store, dataowner.PipelineType),
		Objects:     records.RowsOf[build.Object](store, build.ObjectType),
		Writebacks:  records.RowsOf[dataowner.Writeback](store, dataowner.WritebackType),
	}
}
