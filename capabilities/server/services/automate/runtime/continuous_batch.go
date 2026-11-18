package automation

import (
	"log"
	"platformserver/kernel/protocol"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

type FlowCommit struct {
	Holder   string
	Member   platform.Member
	Instance string
	At       time.Time
	Prepared FlowPreparedDecision
	Input    *OperationInput
	Guard    func(ContinuousFlowOwner, FlowInstance, platform.Member) (bool, *platform.Error)
	Accepted func(bool)
}

type BatchIntake struct {
	Tenant        string
	Frames        FlowFrameStore
	Caller        platform.Caller
	Preparing     func() func()
	Read          func(member platform.Member, id string, now time.Time, use func(ContinuousFlowOwner, FlowInstance, platform.Caller) *platform.Error) *platform.Error
	PrepareInput  func(platform.Member, string, platform.OperationRequest) (*OperationInput, *platform.Error)
	Commit        func(FlowCommit) (*pb.ChangeRecord, *platform.Error)
	CommitCleanup func(FlowArtifactCleanupPreparation, time.Time) *platform.Error
	Instance      func(string) (FlowInstance, *platform.Error)
	CheckSource   func(platform.Member, FlowBatchPreparation, FlowInstance, SourceCheckpoint, bool, time.Time) *platform.Error
	DiscardInput  func(platform.OperationInputArtifact)
}

func (b BatchIntake) ConsumeBatch(m platform.Member, id string, batch Batch, now time.Time) (protocol.FlowBatchOutcome, *platform.Error) {
	releasePreparation := b.Preparing()
	defer releasePreparation()
	var outcome protocol.FlowBatchOutcome
	var done, cleanup bool
	refusal := b.Read(m, id, now, func(f ContinuousFlowOwner, before FlowInstance, _ platform.Caller) *platform.Error {
		var err *platform.Error
		outcome, done, err = ReplayBatch(f, b.Tenant, before, batch)
		cleanup = before.CleanupPending
		return err
	})
	if refusal != nil {
		return outcome, refusal
	}
	if done {
		return outcome, nil
	}
	if cleanup {
		if refusal := b.Cleanup(m, id, now); refusal != nil {
			return protocol.FlowBatchOutcome{}, refusal
		}
	}
	prepared, refusal := b.prepare(m, id, batch, now)
	if refusal != nil {
		return protocol.FlowBatchOutcome{}, refusal
	}
	keep := Retention{Artifacts: prepared.Artifacts(), Discard: b.Frames.Discard, DiscardInput: b.DiscardInput}
	defer keep.Release()
	var input *OperationInput
	if request, ok := prepared.OperationRequest(); ok {
		input, refusal = b.PrepareInput(m, id, request)
		if refusal != nil {
			return protocol.FlowBatchOutcome{}, refusal
		}
		keep.Input = input.ArtifactPointer()
	}
	actor := platform.Member{ID: "app:" + protocol.FlowApp, Tenant: b.Tenant}
	_, refusal = b.Commit(FlowCommit{
		Holder:   "flow:batch",
		Member:   actor,
		Instance: id,
		At:       now,
		Prepared: prepared,
		Input:    input,
		Guard: func(f ContinuousFlowOwner, latest FlowInstance, current platform.Member) (bool, *platform.Error) {
			return true, GuardPrepared(prepared, f, b.Caller, latest, "The Flow changed while its source batch was prepared", batch.Source, func(plan FlowBatchPreparation, checkpoint SourceCheckpoint) *platform.Error {
				return b.CheckSource(current, plan, latest, checkpoint, batch.Tick, now)
			})
		},
		Accepted: func(committed bool) {
			if committed {
				keep.KeepArtifacts, keep.KeepInput = true, true
			}
		},
	})
	if refusal != nil {
		return protocol.FlowBatchOutcome{}, refusal
	}
	x, refusal := b.Instance(id)
	if refusal != nil {
		return protocol.FlowBatchOutcome{}, refusal
	}
	keep.KeepArtifacts, keep.KeepInput = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket, true
	return protocol.FlowBatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
}

func (b BatchIntake) prepare(member platform.Member, id string, batch Batch, now time.Time) (prepared FlowPreparedBatch, refusal *platform.Error) {
	refusal = b.Read(member, id, now, func(f ContinuousFlowOwner, latest FlowInstance, caller platform.Caller) *platform.Error {
		plan, refusal := f.PlanBatch(latest, caller)
		if refusal != nil {
			return refusal
		}
		prepared, refusal = plan.Prepare(batch, now, b.Frames)
		return refusal
	})
	return prepared, refusal
}

func (b BatchIntake) prepareRetry(member platform.Member, id string, now time.Time) (prepared FlowPreparedComputeRetry, refusal *platform.Error) {
	refusal = b.Read(member, id, now, func(f ContinuousFlowOwner, latest FlowInstance, caller platform.Caller) *platform.Error {
		fresh, refusal := f.PlanComputeRetry(latest, caller, now)
		if refusal != nil {
			return refusal
		}
		prepared, refusal = fresh.Prepare(now, b.Frames)
		return refusal
	})
	return prepared, refusal
}

func (b BatchIntake) Cleanup(m platform.Member, id string, now time.Time) *platform.Error {
	var plan FlowArtifactCleanupPreparation
	refusal := b.Read(m, id, now, func(f ContinuousFlowOwner, before FlowInstance, _ platform.Caller) *platform.Error {
		if !before.CleanupPending {
			return nil
		}
		var err *platform.Error
		plan, err = f.PlanArtifactCleanup(before, now)
		return err
	})
	if refusal != nil || plan == nil {
		return refusal
	}
	return b.Retire(plan, now)
}

func (b BatchIntake) Retire(plan FlowArtifactCleanupPreparation, now time.Time) *platform.Error {
	if refusal := RetireArtifacts(plan, b.Tenant, now, b.Frames.Discard); refusal != nil {
		return refusal
	}
	return b.CommitCleanup(plan, now)
}

func (b BatchIntake) RetryCompute(member platform.Member, plan FlowComputeRetryPreparation, now time.Time) {
	prepared, refusal := b.prepareRetry(member, plan.Instance(), now)
	if refusal != nil {
		log.Printf("continuous Compute retry %s refused: %s", plan.Instance(), refusal.Code)
		return
	}
	instance := prepared.Instance()
	input, refusal := b.PrepareInput(member, instance, prepared.OperationRequest())
	if refusal != nil {
		log.Printf("continuous Compute retry %s input refused: %s", instance, refusal.Code)
		return
	}
	keep := Retention{Input: input.ArtifactPointer(), DiscardInput: b.DiscardInput}
	defer keep.Release()
	actor := platform.Member{ID: "app:" + protocol.FlowApp, Tenant: b.Tenant}
	_, refusal = b.Commit(FlowCommit{
		Holder:   "flow:retry",
		Member:   actor,
		Instance: instance,
		At:       now,
		Prepared: prepared,
		Input:    input,
		Guard: func(_ ContinuousFlowOwner, latest FlowInstance, _ platform.Member) (bool, *platform.Error) {
			if prepared.Matches(latest) {
				return true, nil
			}
			return false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its Compute retry was prepared")
		},
		Accepted: func(committed bool) {
			if committed {
				keep.KeepInput = true
			}
		},
	})
	if refusal != nil {
		log.Printf("continuous Compute retry %s refused: %s", instance, refusal.Code)
		return
	}
	keep.KeepInput = true
}
