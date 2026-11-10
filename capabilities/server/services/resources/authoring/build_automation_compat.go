package build

import automationauthoring "platformserver/services/automate/authoring"

type Process = automationauthoring.Process
type ProcessStep = automationauthoring.ProcessStep
type ProcessLane = automationauthoring.ProcessLane
type NodePosition = automationauthoring.NodePosition
type OperationRef = automationauthoring.OperationRef

const (
	ProcessType         = automationauthoring.ProcessType
	SchemaProcess       = automationauthoring.SchemaProcess
	SchemaProcessRun    = automationauthoring.SchemaProcessRun
	SchemaProcessPause  = automationauthoring.SchemaProcessPause
	SchemaProcessResume = automationauthoring.SchemaProcessResume
)
