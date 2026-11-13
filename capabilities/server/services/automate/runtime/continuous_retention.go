package automation

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type Retention struct {
	Artifacts     []platform.FlowStateArtifact
	Input         *platform.OperationInputArtifact
	Discard       func(platform.FlowStateArtifact) error
	DiscardInput  func(platform.OperationInputArtifact)
	KeepArtifacts bool
	KeepInput     bool
}

func (r *Retention) Release() {
	if !r.KeepArtifacts {
		for _, artifact := range r.Artifacts {
			_ = r.Discard(artifact)
		}
	}
	if r.Input != nil && !r.KeepInput {
		r.DiscardInput(*r.Input)
	}
}

func GuardPrepared(prepared platform.FlowPreparedDecision, f platform.ContinuousFlowOwner, caller platform.Caller, latest platform.FlowInstance, changed string, source *platform.FlowSourceCheckpoint, check func(platform.FlowBatchPreparation, platform.FlowSourceCheckpoint) *kernel.Error) *kernel.Error {
	if !prepared.Matches(latest) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, changed)
	}
	if source == nil {
		return nil
	}
	plan, refusal := f.PlanBatch(latest, caller)
	if refusal != nil {
		return refusal
	}
	return check(plan, *source)
}
