package host

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/functions/runtime"
	"time"
)

func (t *Tenant) continuousSnapshot(m platform.Member, id string, now time.Time) (platform.ContinuousFlowOwner, platform.FlowInstance, platform.Member, *kernel.Error) {
	type snapshot struct {
		owner    platform.ContinuousFlowOwner
		instance platform.FlowInstance
		member   platform.Member
	}
	v, refusal := recordRead(t, context.Background(), func(t *Tenant) (snapshot, *kernel.Error) {
		owner, instance, member, refusal := t.continuousSnapshotLocked(m, id, now)
		return snapshot{owner, instance, member}, refusal
	})
	return v.owner, v.instance, v.member, refusal
}

func (t *Tenant) continuousSnapshotLocked(m platform.Member, id string, now time.Time) (platform.ContinuousFlowOwner, platform.FlowInstance, platform.Member, *kernel.Error) {
	return automation.FlowSnapshot{Tenant: t.ID, Quarantined: t.quarantined, Member: t.Member, Admits: t.Admits,
		Owner: func() (platform.ContinuousFlowOwner, bool) {
			f, ok := t.procs.(platform.ContinuousFlowOwner)
			return f, ok
		},
		Record: func(current platform.Member, id string) (any, *kernel.Error) {
			view, refusal := t.recordOf(current, platform.FlowInstanceType, id, now)
			return view.Record, refusal
		},
	}.Take(m, id)
}

func (t *Tenant) cleanupFlowArtifactsFor(m platform.Member, id string, now time.Time) *kernel.Error {
	plan, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.FlowArtifactCleanupPreparation, *kernel.Error) {
		f, before, _, refusal := t.continuousSnapshotLocked(m, id, now)
		if refusal != nil || !before.CleanupPending {
			return nil, refusal
		}
		return f.PlanArtifactCleanup(before, now)
	})
	if refusal != nil || plan == nil {
		return refusal
	}
	return t.retireContinuousArtifacts(plan, now)
}

func (t *Tenant) ConsumeFlowBatch(m platform.Member, id string, batch platform.FlowBatch, now time.Time) (platform.FlowBatchOutcome, *kernel.Error) {
	releasePreparation := t.beginFlowArtifactPreparation()
	defer releasePreparation()
	f, before, _, refusal := t.continuousSnapshot(m, id, now)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	if outcome, done, refusal := automation.ReplayBatch(f, t.ID, before, batch); done || refusal != nil {
		return outcome, refusal
	}
	if before.CleanupPending {
		if refusal := t.cleanupFlowArtifactsFor(m, id, now); refusal != nil {
			return platform.FlowBatchOutcome{}, refusal
		}
	}
	current := m
	store := t.frames()
	prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.FlowPreparedBatch, *kernel.Error) {
		f, latest, _, refusal := t.continuousSnapshotLocked(m, id, now)
		if refusal != nil {
			return nil, refusal
		}
		plan, refusal := f.PlanBatch(latest, t.automation(platform.FlowApp, false))
		if refusal != nil {
			return nil, refusal
		}
		return plan.Prepare(batch, now, store)
	})
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	var preparedInput *preparedOperationInput
	if request, ok := prepared.OperationRequest(); ok {
		preparedInput, refusal = t.prepareContinuousOperationInput(current, id, request)
		if refusal != nil {
			for _, artifact := range prepared.Artifacts() {
				_ = store.Discard(artifact)
			}
			return platform.FlowBatchOutcome{}, refusal
		}
	}
	retained, inputRetained := false, false
	defer func() {
		if !retained {
			for _, artifact := range prepared.Artifacts() {
				_ = store.Discard(artifact)
			}
		}
		if preparedInput != nil && !inputRetained {
			t.staged.DiscardInput(preparedInput.ref)
		}
	}()
	member := platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}
	_, refusal = decideOnImage(t, context.Background(), "flow:batch", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		f, latest, current, refusal := t.continuousSnapshotLocked(m, id, now)
		if refusal != nil {
			return nil, refusal
		}
		if !prepared.Matches(latest) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
		}
		if batch.Source != nil {
			plan, refusal := f.PlanBatch(latest, t.automation(platform.FlowApp, false))
			if refusal != nil {
				return nil, refusal
			}
			if refusal := t.checkStreamSourceLocked(current, plan, latest, *batch.Source, batch.Tick, now); refusal != nil {
				return nil, refusal
			}
		}
		return t.accept(prepared.Decision(f), member, prepared.Submission(t.ID), now, acceptOptions{automation: true, inputs: []*preparedOperationInput{preparedInput}})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			retained, inputRetained = true, true
		}
		return t.finishPreparedSubmission(member, candidate, record, refusal)
	})
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	x, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.FlowInstance, *kernel.Error) {
		x, ok := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), id)
		if !ok {
			return x, unknown()
		}
		return x, nil
	})
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	retained = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket
	inputRetained = preparedInput != nil
	return platform.FlowBatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
}

func (t *Tenant) prepareContinuousDeadLetterReplay(m platform.Member, sub *pb.Submission, now time.Time, opts submitOptions) (*pb.ChangeRecord, *kernel.Error, bool) {
	releasePreparation := t.beginFlowArtifactPreparation()
	defer releasePreparation()
	first, err := decideOnImage(t, context.Background(), "flow:replay-preflight", func(t *Tenant) (automation.ReplayPreflightResult, *kernel.Error) {
		owner, ok := t.procs.(platform.ContinuousFlowOwner)
		if !ok {
			return automation.ReplayPreflightResult{Sub: sub}, nil
		}
		return automation.ReplayPreflight{Versioned: opts.versioned,
			Version: func() (*platform.SchemaVersion, *kernel.Error) {
				return t.submissionSchemaVersion(owner, sub, opts.contentVersion)
			},
			Current: func(sub *pb.Submission) *pb.Submission { return t.currentSubmissionSchema(owner, sub) },
			Settled: func() bool {
				prior := t.priorOf(owner, sub.GetIdempotencyKey())
				return prior.Refusal != nil || prior.Record != nil
			},
			Accept: func(sub *pb.Submission, binding *platform.SchemaVersion, schemaRefusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
				return t.accept(owner, m, sub, now, acceptOptions{binding: binding, schemaRefusal: schemaRefusal})
			},
		}.Run(sub)
	}, func(t *Tenant, candidate preparedAccepted, value automation.ReplayPreflightResult, refusal *kernel.Error) (automation.ReplayPreflightResult, *kernel.Error) {
		value.Record, refusal = t.finishPreparedSubmission(m, candidate, value.Record, refusal)
		return value, refusal
	})
	if err != nil || first.Handled {
		return first.Record, err, true
	}
	sub = first.Sub
	load := func(t *Tenant) (automation.ReplayContext, *kernel.Error) {
		return automation.DeadLetterLoad{
			Record: func(id string) (any, *kernel.Error) {
				view, refusal := t.recordOf(m, "flow.dead-letter", id, now)
				return view.Record, refusal
			},
			Snapshot: func(instance string) (platform.FlowInstance, platform.Member, *kernel.Error) {
				_, x, current, refusal := t.continuousSnapshotLocked(m, instance, now)
				return x, current, refusal
			},
		}.Load(sub.GetTarget().GetId())
	}
	initial, err := recordRead(t, context.Background(), load)
	if err != nil {
		return nil, err, true
	}
	if initial.Asset.Action != "" {
		return nil, nil, false
	}
	if initial.Instance.CleanupPending {
		if err := t.cleanupFlowArtifactsFor(initial.Member, initial.Instance.ID, now); err != nil {
			return nil, err, true
		}
	}
	store := t.frames()
	var preparedContext automation.ReplayContext
	prepared, err := recordRead(t, context.Background(), func(t *Tenant) (platform.FlowPreparedBatch, *kernel.Error) {
		var refusal *kernel.Error
		preparedContext, refusal = load(t)
		if refusal != nil {
			return nil, refusal
		}
		if preparedContext.Asset.Action != "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dead letter changed while its replay was prepared")
		}
		f := t.procs.(platform.ContinuousFlowOwner)
		plan, refusal := f.PlanDeadLetterReplay(preparedContext.Instance, t.automation(platform.FlowApp, false), preparedContext.Asset)
		if refusal != nil {
			return nil, refusal
		}
		return plan.Prepare(sub, now, store)
	})
	if err != nil {
		return nil, err, true
	}
	var preparedInput *preparedOperationInput
	retained, inputRetained := false, false
	defer func() {
		if !retained {
			for _, artifact := range prepared.Artifacts() {
				_ = store.Discard(artifact)
			}
		}
		if preparedInput != nil && !inputRetained {
			t.staged.DiscardInput(preparedInput.ref)
		}
	}()
	if request, ok := prepared.OperationRequest(); ok {
		preparedInput, err = t.prepareContinuousOperationInput(preparedContext.Member, preparedContext.Instance.ID, request)
		if err != nil {
			return nil, err, true
		}
	}
	record, err := decideOnImage(t, context.Background(), "flow:replay", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		f, latest, current, refusal := t.continuousSnapshotLocked(preparedContext.Member, preparedContext.Instance.ID, now)
		if refusal != nil {
			return nil, refusal
		}
		if !prepared.Matches(latest) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original Flow changed while dead-letter replay was prepared")
		}
		if latest.Batch.Source != nil {
			plan, refusal := f.PlanBatch(latest, t.automation(platform.FlowApp, false))
			if refusal != nil {
				return nil, refusal
			}
			if refusal := t.checkStreamSourceLocked(current, plan, latest, *latest.Batch.Source, true, now); refusal != nil {
				return nil, refusal
			}
		}
		return t.accept(prepared.Decision(f), current, sub, now, acceptOptions{binding: first.Binding, inputs: []*preparedOperationInput{preparedInput}})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		if len(candidate.entry.Body) > 0 {
			retained, inputRetained = true, preparedInput != nil
		}
		record, refusal = t.finishPreparedSubmission(preparedContext.Member, candidate, record, refusal)
		if _, rejected := t.Committed.Refusal(platform.FlowApp + "/" + sub.GetIdempotencyKey()); rejected {
			retained, inputRetained = false, false
		}
		return record, refusal
	})
	return record, err, true
}

func (t *Tenant) retireContinuousArtifacts(plan platform.FlowArtifactCleanupPreparation, now time.Time) *kernel.Error {
	store := t.frames()
	if refusal := automation.RetireArtifacts(plan, t.ID, now, store.Discard); refusal != nil {
		return refusal
	}

	member := platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}
	_, refusal := decideOnImage(t, context.Background(), "flow:cleanup", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		latest, ok := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), plan.Instance())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow artifact cleanup owner is unavailable")
		}
		if !plan.Matches(latest) {
			return nil, nil
		}
		return t.accept(plan.Decision(t.App(platform.FlowApp)), member, plan.Submission(t.ID), now, acceptOptions{automation: true})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		return t.finishPreparedSubmission(member, candidate, record, refusal)
	})
	return refusal
}

func (t *Tenant) retryContinuousCompute(member platform.Member, plan platform.FlowComputeRetryPreparation, now time.Time) {
	prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.FlowPreparedComputeRetry, *kernel.Error) {
		f, latest, _, refusal := t.continuousSnapshotLocked(member, plan.Instance(), now)
		if refusal != nil {
			return nil, refusal
		}
		fresh, refusal := f.PlanComputeRetry(latest, t.automation(platform.FlowApp, false), now)
		if refusal != nil {
			return nil, refusal
		}
		return fresh.Prepare(now, t.frames())
	})
	if refusal != nil {
		log.Printf("continuous Compute retry %s refused: %s", plan.Instance(), refusal.Code)
		return
	}
	instance := prepared.Instance()
	var preparedInput *preparedOperationInput
	preparedInput, refusal = t.prepareContinuousOperationInput(member, instance, prepared.OperationRequest())
	if refusal != nil {
		log.Printf("continuous Compute retry %s input refused: %s", instance, refusal.Code)
		return
	}
	inputRetained := false
	defer func() {
		if preparedInput != nil && !inputRetained {
			t.staged.DiscardInput(preparedInput.ref)
		}
	}()
	actor := platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}
	_, refusal = decideOnImage(t, context.Background(), "flow:retry", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		f, latest, _, refusal := t.continuousSnapshotLocked(member, instance, now)
		if refusal != nil {
			return nil, refusal
		}
		if !prepared.Matches(latest) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its Compute retry was prepared")
		}
		return t.accept(prepared.Decision(f), actor, prepared.Submission(t.ID), now, acceptOptions{automation: true, inputs: []*preparedOperationInput{preparedInput}})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		if len(candidate.entry.Body) != 0 {
			inputRetained = true
		}
		return t.finishPreparedSubmission(actor, candidate, record, refusal)
	})
	if refusal != nil {
		log.Printf("continuous Compute retry %s refused: %s", instance, refusal.Code)
		return
	}
	inputRetained = preparedInput != nil
}

func (t *Tenant) prepareContinuousOperationInput(member platform.Member, instance string, request platform.OperationRequest) (*preparedOperationInput, *kernel.Error) {
	sealer := automation.OperationInputSealer{
		Operation: func(request platform.OperationRequest) (platform.Operation, int, *kernel.Error) {
			type binding struct {
				operation platform.Operation
				version   int
			}
			bound, refusal := recordRead(t, context.Background(), func(t *Tenant) (binding, *kernel.Error) {
				current, ok := t.Member(member.ID)
				owner := t.App(request.App)
				if !ok || current.Tenant != t.ID || owner == nil {
					return binding{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The prepared window Compute owner or member is unavailable")
				}
				op, version, ok := compute.OperationDefinition(owner, request.Name, request.Version)
				if !ok || op.Check() != nil {
					return binding{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The prepared window Compute operation is not retained")
				}
				return binding{op, version}, nil
			})
			return bound.operation, bound.version, refusal
		},
		Seal: func(app, key, member, definition, hash string, inputs json.RawMessage, budget int) (platform.OperationInputArtifact, error) {
			return t.staged.SealInput(fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, app, key, acceptedformat.OperationEndpoint), member, definition, hash, inputs, budget)
		},
		Discard: t.staged.DiscardInput,
	}
	ref, requestHash, sealed, refusal := sealer.Prepare(member, instance, request)
	if refusal != nil || !sealed {
		return nil, refusal
	}
	return &preparedOperationInput{ref: ref, requestHash: requestHash}, nil
}
