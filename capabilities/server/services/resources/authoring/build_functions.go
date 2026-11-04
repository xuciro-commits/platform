package build

import (
	"encoding/json"

	"platformserver/platform"
	functionauthoring "platformserver/services/functions/authoring"
)

type Function = functionauthoring.Function

type FunctionRun = functionauthoring.FunctionRun

const (
	FunctionType         = functionauthoring.FunctionType
	SchemaFunction       = functionauthoring.SchemaFunction
	FunctionCallType     = functionauthoring.FunctionCallType
	SchemaFunctionCall   = functionauthoring.SchemaFunctionCall
	SchemaFunctionAnswer = functionauthoring.SchemaFunctionAnswer
	SchemaProposalReview = functionauthoring.SchemaProposalReview
)

func (b *Build) FunctionDefinition(name string, version int) (platform.AIFunction, int, bool) {
	return b.functions.Definition(name, version)
}

func (b *Build) FunctionReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return b.functions.Resolve(name, sourceVersion)
}

func (b *Build) InstallFunctionAsset(asset platform.ReleaseAsset) error {
	return b.functions.InstallAsset(asset)
}

func (b *Build) TrainingCandidates(c platform.Caller, event platform.Event, images map[string]json.RawMessage) []platform.AITrainingCandidate {
	return b.functions.TrainingCandidates(c, event, images)
}
