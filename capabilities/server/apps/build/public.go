// Package build owns construction authoring and publication. Internal consumers
// use internal/construction directly; these names preserve the public Go surface.
package build

import "platformserver/internal/construction"

type JointDraftAssets = construction.JointDraftAssets

const ObservationType = construction.ObservationType

const SchemaObservation = construction.SchemaObservation

type Observation = construction.Observation

const TableType = construction.TableType

type ReleasePublication = construction.ReleasePublication

type JointDraftRef = construction.JointDraftRef

const AlertType = construction.AlertType

type Alert = construction.Alert

const EvaluationType = construction.EvaluationType

const SchemaEvaluationStart = construction.SchemaEvaluationStart

const SchemaEvaluationAnswer = construction.SchemaEvaluationAnswer

const EvaluationRepeats = construction.EvaluationRepeats

type Evaluation = construction.Evaluation

type EvaluationAttempt = construction.EvaluationAttempt

type EvaluationStart = construction.EvaluationStart

const PipelineType = construction.PipelineType

const SchemaPipelineRan = construction.SchemaPipelineRan

type Pipeline = construction.Pipeline

type Step = construction.Step

type Measure = construction.Measure

type Expectation = construction.Expectation

type PipelineRun = construction.PipelineRun

type QuarantinedRow = construction.QuarantinedRow

type Table = construction.Table

type TableColumn = construction.TableColumn

type TableRow = construction.TableRow

const AppType = construction.AppType

const SchemaHandOver = construction.SchemaHandOver

const FunctionType = construction.FunctionType

type Function = construction.Function

const PageType = construction.PageType

const SchemaRelease = construction.SchemaRelease

const QueryType = construction.QueryType

const ID = construction.ID

const ObjectType = construction.ObjectType

const Builder = construction.Builder

const Publisher = construction.Publisher

const User = construction.User

const SchemaPublish = construction.SchemaPublish

type Object = construction.Object

type Field = construction.Field

const ConnectionType = construction.ConnectionType

const SchemaConnectionChecked = construction.SchemaConnectionChecked

type Connection = construction.Connection

type ConnectionCheck = construction.ConnectionCheck

const CodeType = construction.CodeType

const BooksEndpoint = construction.BooksEndpoint

type Numbering = construction.Numbering

const Integrator = construction.Integrator

type Match = construction.Match

type MatchKey = construction.MatchKey

type Preference = construction.Preference

type Resolver = construction.Resolver

const ProcessType = construction.ProcessType

const SchemaProcess = construction.SchemaProcess

const SchemaProcessRun = construction.SchemaProcessRun

type Process = construction.Process

type ProcessLane = construction.ProcessLane

type NodePosition = construction.NodePosition

type ProcessStep = construction.ProcessStep

const LinkTypeType = construction.LinkTypeType

const SchemaLinkType = construction.SchemaLinkType

const SourceType = construction.SourceType

const SchemaSourcePulled = construction.SchemaSourcePulled

const SourceBody = construction.SourceBody

type Source = construction.Source

type SourceField = construction.SourceField

type SourcePull = construction.SourcePull

type SourceFailure = construction.SourceFailure

type SourceRow = construction.SourceRow

const WritebackType = construction.WritebackType

const SchemaWritebackAnswered = construction.SchemaWritebackAnswered

const WritebackEndpoint = construction.WritebackEndpoint

type Writeback = construction.Writeback

type WritebackAns = construction.WritebackAns

type WritebackRequest = construction.WritebackRequest

type State = construction.State

type Action = construction.Action

type Create = construction.Create

type ActionApproval = construction.ActionApproval

type ApproverLevel = construction.ApproverLevel

type Input = construction.Input

type Set = construction.Set

type Condition = construction.Condition

type Access = construction.Access

type ObjectScope = construction.ObjectScope

type EnterpriseTarget = construction.EnterpriseTarget

const PropertyTypeType = construction.PropertyTypeType

const DatasetType = construction.DatasetType

const DatasetVersionType = construction.DatasetVersionType

const SchemaDatasetLoad = construction.SchemaDatasetLoad

const DatasetKeepDefault = construction.DatasetKeepDefault

type Dataset = construction.Dataset

type DatasetField = construction.DatasetField

type DatasetLoad = construction.DatasetLoad

const FunctionCallType = construction.FunctionCallType

const SchemaFunctionCall = construction.SchemaFunctionCall

const SchemaFunctionAnswer = construction.SchemaFunctionAnswer

type FunctionRun = construction.FunctionRun

const TestPlanType = construction.TestPlanType

type TestPlan = construction.TestPlan

type EvaluationPolicy = construction.EvaluationPolicy

type EvaluationCase = construction.EvaluationCase

type TestSample = construction.TestSample

type TestStep = construction.TestStep

type ComputeFixture = construction.ComputeFixture

type FunctionFixture = construction.FunctionFixture

type Group = construction.Group

type Post = construction.Post

var ConnectionKinds = construction.ConnectionKinds

type JournalPost = construction.JournalPost

type JournalPostLine = construction.JournalPostLine

type OperationRef = construction.OperationRef

type DatasetVersion = construction.DatasetVersion

type ConnectionDefinition = construction.ConnectionDefinition

type SourceDefinition = construction.SourceDefinition

type DatasetDefinition = construction.DatasetDefinition

type PipelineDefinition = construction.PipelineDefinition

type WritebackDefinition = construction.WritebackDefinition

type MatchingDefinition = construction.MatchingDefinition

var Markings = construction.Markings
