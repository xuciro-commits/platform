package build

import (
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

type Connection = dataowner.Connection
type ConnectionCheck = dataowner.ConnectionCheck
type Dataset = dataowner.Dataset
type DatasetField = dataowner.DatasetField
type DatasetProjection = dataowner.DatasetProjection
type DatasetLoad = dataowner.DatasetLoad
type DatasetVersion = dataowner.DatasetVersion
type DatasetTransactionType = dataowner.DatasetTransactionType
type Source = dataowner.Source
type SourceField = dataowner.SourceField
type SourcePull = dataowner.SourcePull
type SourceFailure = dataowner.SourceFailure
type SourceRow = dataowner.SourceRow
type Listener = dataowner.Listener
type Pipeline = dataowner.Pipeline
type PipelineRun = dataowner.PipelineRun
type Writeback = dataowner.Writeback
type WritebackAns = dataowner.WritebackAns
type WritebackRequest = dataowner.WritebackRequest
type Match = dataowner.Match
type MatchKey = dataowner.MatchKey
type Preference = dataowner.Preference
type Resolver = dataowner.Resolver
type Observation = dataowner.Observation
type EnterpriseTarget = dataowner.EnterpriseTarget
type IntegrationExecution = dataowner.IntegrationExecution
type ConnectionDefinition = dataowner.ConnectionDefinition
type ListenerDefinition = dataowner.ListenerDefinition
type SourceDefinition = dataowner.SourceDefinition
type DatasetDefinition = dataowner.DatasetDefinition
type PipelineDefinition = dataowner.PipelineDefinition
type WritebackDefinition = dataowner.WritebackDefinition
type MatchingDefinition = dataowner.MatchingDefinition
type MediaSetDefinition = dataowner.MediaSetDefinition
type ScheduleDefinition = dataowner.ScheduleDefinition

const ConnectionType = dataowner.ConnectionType
const SchemaConnectionChecked = dataowner.SchemaConnectionChecked
const DatasetType = dataowner.DatasetType
const DatasetVersionType = dataowner.DatasetVersionType
const SchemaDatasetLoad = dataowner.SchemaDatasetLoad
const SchemaDatasetProjected = dataowner.SchemaDatasetProjected
const DatasetKeepDefault = dataowner.DatasetKeepDefault
const DatasetTransactionSnapshot = dataowner.DatasetTransactionSnapshot
const DatasetTransactionAppend = dataowner.DatasetTransactionAppend
const DatasetTransactionUpdate = dataowner.DatasetTransactionUpdate
const DatasetTransactionDelete = dataowner.DatasetTransactionDelete
const SourceType = dataowner.SourceType
const SchemaSourcePulled = dataowner.SchemaSourcePulled
const SourceBody = dataowner.SourceBody
const ValueSourceRows = dataowner.ValueSourceRows
const ValueSourceFailuresKept = dataowner.ValueSourceFailuresKept
const ListenerType = dataowner.ListenerType
const PipelineType = dataowner.PipelineType
const SchemaPipelineRan = dataowner.SchemaPipelineRan
const WritebackType = dataowner.WritebackType
const SchemaWritebackAnswered = dataowner.SchemaWritebackAnswered
const ValueWritebackAnswerKept = dataowner.ValueWritebackAnswerKept
const WritebackEndpoint = dataowner.WritebackEndpoint
const MatchType = dataowner.MatchType
const ObservationType = dataowner.ObservationType
const SchemaObservation = dataowner.SchemaObservation

var ConnectionKinds = dataowner.ConnectionKinds
var StepKinds = dataowner.StepKinds
var ValueSourceName = dataowner.ValueSourceName

func DatasetRows(c platform.Caller, dataset string, version int) ([]map[string]any, int, error) {
	return dataowner.DatasetRows(c, dataset, version)
}

func VersionID(dataset string, version int) string { return dataowner.VersionID(dataset, version) }

func NewResolver(rule Match, producer string, records []map[string]any) *Resolver {
	return dataowner.NewResolver(rule, producer, records)
}

func CheckWritebackTransport(transport, method string) error {
	return dataowner.CheckWritebackTransport(transport, method)
}

func Guarded(marking string) bool { return dataowner.Guarded(marking) }

func UnguardedFields(info platform.EntityInfo, columns []string) []string {
	return dataowner.UnguardedFields(info, columns)
}

func ValueDecodeIntegration(raw []byte, value any) error {
	return dataowner.DecodeDescriptor(raw, value)
}
