package automation

import (
	"log"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type FlowCommit struct {
	Holder   string
	Member   platform.Member
	Instance string
	At       time.Time
	Prepared platform.FlowPreparedDecision
	Input    *OperationInput
	Guard    func(platform.ContinuousFlowOwner, FlowInstance, platform.Member) (bool, *kernel.Error)
	Accepted func(bool)
}

type BatchIntake struct {
	Tenant        string
	Frames        platform.FlowFrameStore
	Caller        platform.Caller
	Preparing     func() func()
	Snapshot      func(platform.Member, string, time.Time) (platform.ContinuousFlowOwner, FlowInstance, platform.Member, *kernel.Error)
	Read          func(member platform.Member, id string, now time.Time, use func(platform.ContinuousFlowOwner, FlowInstance, platform.Caller) *kernel.Error) *kernel.Error
	PrepareInput  func(platform.Member, string, platform.OperationRequest) (*OperationInput, *kernel.Error)
	Commit        func(FlowCommit) (*pb.ChangeRecord, *kernel.Error)
	CommitCleanup func(platform.FlowArtifactCleanupPreparation, time.Time) *kernel.Error
	Instance      func(string) (FlowInstance, *kernel.Error)
	CheckSource   func(platform.Member, platform.FlowBatchPreparation, FlowInstance, SourceCheckpoint, bool, time.Time) *kernel.Error
	DiscardInput  func(platform.OperationInputArtifact)
}

func (b BatchIntake) ConsumeBatch(m platform.Member, id string, batch Batch, now time.Time) (platform.FlowBatchOutcome, *kernel.Error) {
	releasePreparation := b.Preparing()
	defer releasePreparation()
	f, before, _, refusal := b.Snapshot(m, id, now)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	if outcome, done, refusal := ReplayBatch(f, b.Tenant, before, batch); done || refusal != nil {
		return outcome, refusal
	}
	if before.CleanupPending {
		if refusal := b.Cleanup(m, id, now); refusal != nil {
			return platform.FlowBatchOutcome{}, refusal
		}
	}
	prepared, refusal := b.prepare(m, id, batch, now)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	keep := Retention{Artifacts: prepared.Artifacts(), Discard: b.Frames.Discard, DiscardInput: b.DiscardInput}
	defer keep.Release()
	var input *OperationInput
	if request, ok := prepared.OperationRequest(); ok {
		input, refusal = b.PrepareInput(m, id, request)
		if refusal != nil {
			return platform.FlowBatchOutcome{}, refusal
		}
		keep.Input = input.ArtifactPointer()
	}
	actor := platform.Member{ID: "app:" + platform.FlowApp, Tenant: b.Tenant}
	_, refusal = b.Commit(FlowCommit{
		Holder:   "flow:batch",
		Member:   actor,
		Instance: id,
		At:       now,
		Prepared: prepared,
		Input:    input,
		Guard: func(f platform.ContinuousFlowOwner, latest FlowInstance, current platform.Member) (bool, *kernel.Error) {
			return true, GuardPrepared(prepared, f, b.Caller, latest, "The Flow changed while its source batch was prepared", batch.Source, func(plan platform.FlowBatchPreparation, checkpoint SourceCheckpoint) *kernel.Error {
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
		return platform.FlowBatchOutcome{}, refusal
	}
	x, refusal := b.Instance(id)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	keep.KeepArtifacts, keep.KeepInput = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket, true
	return platform.FlowBatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
}

func (b BatchIntake) prepare(member platform.Member, id string, batch Batch, now time.Time) (prepared platform.FlowPreparedBatch, refusal *kernel.Error) {
	refusal = b.Read(member, id, now, func(f platform.ContinuousFlowOwner, latest FlowInstance, caller platform.Caller) *kernel.Error {
		plan, refusal := f.PlanBatch(latest, caller)
		if refusal != nil {
			return refusal
		}
		prepared, refusal = plan.Prepare(batch, now, b.Frames)
		return refusal
	})
	return prepared, refusal
}

func (b BatchIntake) prepareRetry(member platform.Member, id string, now time.Time) (prepared platform.FlowPreparedComputeRetry, refusal *kernel.Error) {
	refusal = b.Read(member, id, now, func(f platform.ContinuousFlowOwner, latest FlowInstance, caller platform.Caller) *kernel.Error {
		fresh, refusal := f.PlanComputeRetry(latest, caller, now)
		if refusal != nil {
			return refusal
		}
		prepared, refusal = fresh.Prepare(now, b.Frames)
		return refusal
	})
	return prepared, refusal
}

func (b BatchIntake) Cleanup(m platform.Member, id string, now time.Time) *kernel.Error {
	f, before, _, refusal := b.Snapshot(m, id, now)
	if refusal != nil || !before.CleanupPending {
		return refusal
	}
	plan, refusal := f.PlanArtifactCleanup(before, now)
	if refusal != nil || plan == nil {
		return refusal
	}
	return b.Retire(plan, now)
}

func (b BatchIntake) Retire(plan platform.FlowArtifactCleanupPreparation, now time.Time) *kernel.Error {
	if refusal := RetireArtifacts(plan, b.Tenant, now, b.Frames.Discard); refusal != nil {
		return refusal
	}
	return b.CommitCleanup(plan, now)
}

func (b BatchIntake) RetryCompute(member platform.Member, plan platform.FlowComputeRetryPreparation, now time.Time) {
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
	actor := platform.Member{ID: "app:" + platform.FlowApp, Tenant: b.Tenant}
	_, refusal = b.Commit(FlowCommit{
		Holder:   "flow:retry",
		Member:   actor,
		Instance: instance,
		At:       now,
		Prepared: prepared,
		Input:    input,
		Guard: func(_ platform.ContinuousFlowOwner, latest FlowInstance, _ platform.Member) (bool, *kernel.Error) {
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
