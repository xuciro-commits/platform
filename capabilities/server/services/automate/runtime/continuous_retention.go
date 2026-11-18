package runtime

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type Retention struct {
	Artifacts     []protocol.FlowStateArtifact
	Input         *protocol.OperationInputArtifact
	Discard       func(protocol.FlowStateArtifact) error
	DiscardInput  func(protocol.OperationInputArtifact)
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

func GuardPrepared(prepared FlowPreparedDecision, f ContinuousFlowOwner, caller platform.Caller, latest protocol.FlowInstance, changed string, source *protocol.FlowSourceCheckpoint, check func(FlowBatchPreparation, protocol.FlowSourceCheckpoint) *platform.Error) *platform.Error {
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
