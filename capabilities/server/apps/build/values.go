package build

import buildapi "platformserver/internal/host/buildapi"

type ReleasePublication = buildapi.ReleasePublication
type JointDraftRef = buildapi.JointDraftRef

const AlertType = buildapi.AlertType

type Alert = buildapi.Alert

const EvaluationType = buildapi.EvaluationType
const SchemaEvaluationStart = buildapi.SchemaEvaluationStart
const SchemaEvaluationAnswer = buildapi.SchemaEvaluationAnswer
const EvaluationRepeats = buildapi.EvaluationRepeats

type Evaluation = buildapi.Evaluation
type EvaluationAttempt = buildapi.EvaluationAttempt
type EvaluationStart = buildapi.EvaluationStart

const PipelineType = buildapi.PipelineType
const SchemaPipelineRan = buildapi.SchemaPipelineRan

type Pipeline = buildapi.Pipeline
type Step = buildapi.Step
type Measure = buildapi.Measure
type Expectation = buildapi.Expectation
type PipelineRun = buildapi.PipelineRun
type QuarantinedRow = buildapi.QuarantinedRow
type Table = buildapi.Table
type TableColumn = buildapi.TableColumn
type TableRow = buildapi.TableRow

const AppType = buildapi.AppType
const SchemaHandOver = buildapi.SchemaHandOver
const pagesHelp = buildapi.ValuePagesHelp
const FunctionType = buildapi.FunctionType

type Function = buildapi.Function

const PageType = buildapi.PageType
const SchemaRelease = buildapi.SchemaRelease
const QueryType = buildapi.QueryType
const ID = buildapi.ID
const definitionVersion = buildapi.ValueDefinitionVersion
const ObjectType = buildapi.ObjectType
const Builder = buildapi.Builder
const Publisher = buildapi.Publisher
const User = buildapi.User
const SchemaPublish = buildapi.SchemaPublish

type Object = buildapi.Object
type Field = buildapi.Field

const ConnectionType = buildapi.ConnectionType
const SchemaConnectionChecked = buildapi.SchemaConnectionChecked

type Connection = buildapi.Connection
type ConnectionCheck = buildapi.ConnectionCheck
type formulaParser = buildapi.ValueFormulaParser

const CodeType = buildapi.CodeType
const BooksEndpoint = buildapi.BooksEndpoint

type Numbering = buildapi.Numbering

const Integrator = buildapi.Integrator

type Match = buildapi.Match
type MatchKey = buildapi.MatchKey
type Preference = buildapi.Preference
type Resolver = buildapi.Resolver

const ProcessType = buildapi.ProcessType
const SchemaProcess = buildapi.SchemaProcess
const SchemaProcessRun = buildapi.SchemaProcessRun

type Process = buildapi.Process
type ProcessLane = buildapi.ProcessLane
type NodePosition = buildapi.NodePosition
type ProcessStep = buildapi.ProcessStep

const LinkTypeType = buildapi.LinkTypeType
const SchemaLinkType = buildapi.SchemaLinkType
const SourceType = buildapi.SourceType
const SchemaSourcePulled = buildapi.SchemaSourcePulled
const SourceBody = buildapi.SourceBody
const sourceRows = buildapi.ValueSourceRows
const sourceFailuresKept = buildapi.ValueSourceFailuresKept

type Source = buildapi.Source
type SourceField = buildapi.SourceField
type SourcePull = buildapi.SourcePull
type SourceFailure = buildapi.SourceFailure
type SourceRow = buildapi.SourceRow

const WritebackType = buildapi.WritebackType
const SchemaWritebackAnswered = buildapi.SchemaWritebackAnswered
const writebackAnswerKept = buildapi.ValueWritebackAnswerKept
const WritebackEndpoint = buildapi.WritebackEndpoint

type Writeback = buildapi.Writeback
type WritebackAns = buildapi.WritebackAns
type WritebackRequest = buildapi.WritebackRequest
type State = buildapi.State
type Action = buildapi.Action
type Create = buildapi.Create
type ActionApproval = buildapi.ActionApproval
type ApproverLevel = buildapi.ApproverLevel
type Input = buildapi.Input
type Set = buildapi.Set
type Condition = buildapi.Condition

var inputTypes = buildapi.ValueInputTypes

type Access = buildapi.Access
type ObjectScope = buildapi.ObjectScope
type EnterpriseTarget = buildapi.EnterpriseTarget

const PropertyTypeType = buildapi.PropertyTypeType
const DatasetType = buildapi.DatasetType
const DatasetVersionType = buildapi.DatasetVersionType
const SchemaDatasetLoad = buildapi.SchemaDatasetLoad
const DatasetKeepDefault = buildapi.DatasetKeepDefault

type Dataset = buildapi.Dataset
type DatasetField = buildapi.DatasetField
type DatasetLoad = buildapi.DatasetLoad

const FunctionCallType = buildapi.FunctionCallType
const SchemaFunctionCall = buildapi.SchemaFunctionCall
const SchemaFunctionAnswer = buildapi.SchemaFunctionAnswer

type FunctionRun = buildapi.FunctionRun

const TestPlanType = buildapi.TestPlanType

type TestPlan = buildapi.TestPlan
type EvaluationPolicy = buildapi.EvaluationPolicy
type EvaluationCase = buildapi.EvaluationCase
type TestSample = buildapi.TestSample
type TestStep = buildapi.TestStep
type ComputeFixture = buildapi.ComputeFixture
type FunctionFixture = buildapi.FunctionFixture
type condition = buildapi.ValueCondition
type Group = buildapi.Group
type Post = buildapi.Post

var ConnectionKinds = buildapi.ConnectionKinds

type formulaNode = buildapi.ValueFormulaNode
type JournalPost = buildapi.JournalPost
type JournalPostLine = buildapi.JournalPostLine
type OperationRef = buildapi.OperationRef

var sourceName = buildapi.ValueSourceName

type DatasetVersion = buildapi.DatasetVersion
type ConnectionDefinition = buildapi.ConnectionDefinition
type SourceDefinition = buildapi.SourceDefinition
type DatasetDefinition = buildapi.DatasetDefinition
type PipelineDefinition = buildapi.PipelineDefinition
type WritebackDefinition = buildapi.WritebackDefinition
type MatchingDefinition = buildapi.MatchingDefinition

var Markings = buildapi.Markings
