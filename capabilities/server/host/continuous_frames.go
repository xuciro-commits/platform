package host

import (
	"context"
	"encoding/json"
	"fmt"
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

func (t *Tenant) decideAutomated(holder, app string, decide func(*Tenant) (*pb.ChangeRecord, *kernel.Error)) (*pb.ChangeRecord, *kernel.Error) {
	member := platform.Member{ID: "app:" + app, Tenant: t.ID}
	return decideOnImage(t, context.Background(), holder, decide, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		return t.finishPreparedSubmission(member, candidate, record, refusal)
	})
}

func (t *Tenant) ConsumeFlowBatch(m platform.Member, id string, batch platform.FlowBatch, now time.Time) (platform.FlowBatchOutcome, *kernel.Error) {
	return t.flowIntake().ConsumeBatch(m, id, batch, now)
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
				return t.SchemaBinding(owner, sub, opts.contentVersion, t.actionSchemaVersion)
			},
			Current: func(sub *pb.Submission) *pb.Submission { return t.CurrentSchema(owner, sub) },
			Settled: func() bool {
				prior := t.PriorOf(owner, sub.GetIdempotencyKey())
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
		if err := t.flowIntake().Cleanup(initial.Member, initial.Instance.ID, now); err != nil {
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
	keep := automation.Retention{Artifacts: prepared.Artifacts(), Discard: store.Discard, DiscardInput: t.staged.DiscardInput}
	defer keep.Release()
	var preparedInput *preparedOperationInput
	if request, ok := prepared.OperationRequest(); ok {
		preparedInput, err = t.prepareContinuousOperationInput(preparedContext.Member, preparedContext.Instance.ID, request)
		if err != nil {
			return nil, err, true
		}
		keep.Input = preparedInput.ArtifactPointer()
	}
	record, err := decideOnImage(t, context.Background(), "flow:replay", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		f, latest, current, refusal := t.continuousSnapshotLocked(preparedContext.Member, preparedContext.Instance.ID, now)
		if refusal != nil {
			return nil, refusal
		}
		if refusal := automation.GuardPrepared(prepared, f, t.automation(platform.FlowApp, false), latest, "The original Flow changed while dead-letter replay was prepared", latest.Batch.Source, func(plan platform.FlowBatchPreparation, checkpoint platform.FlowSourceCheckpoint) *kernel.Error {
			return t.checkStreamSourceLocked(current, plan, latest, checkpoint, true, now)
		}); refusal != nil {
			return nil, refusal
		}
		return t.accept(prepared.Decision(f), current, sub, now, acceptOptions{binding: first.Binding, inputs: []*preparedOperationInput{preparedInput}})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		if len(candidate.entry.Body) > 0 {
			keep.KeepArtifacts, keep.KeepInput = true, true
		}
		record, refusal = t.finishPreparedSubmission(preparedContext.Member, candidate, record, refusal)
		if _, rejected := t.Committed.Refusal(platform.FlowApp + "/" + sub.GetIdempotencyKey()); rejected {
			keep.KeepArtifacts, keep.KeepInput = false, false
		}
		return record, refusal
	})
	return record, err, true
}

func (t *Tenant) flowIntake() automation.BatchIntake {
	return automation.BatchIntake{
		Tenant:    t.ID,
		Frames:    t.frames(),
		Caller:    t.automation(platform.FlowApp, false),
		Preparing: t.beginFlowArtifactPreparation,
		Snapshot:  t.continuousSnapshot,
		Read: func(member platform.Member, id string, now time.Time, use func(platform.ContinuousFlowOwner, platform.FlowInstance, platform.Caller) *kernel.Error) *kernel.Error {
			_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
				f, latest, _, refusal := t.continuousSnapshotLocked(member, id, now)
				if refusal != nil {
					return struct{}{}, refusal
				}
				return struct{}{}, use(f, latest, t.automation(platform.FlowApp, false))
			})
			return refusal
		},
		PrepareInput:  t.prepareContinuousOperationInput,
		Commit:        t.commitFlow,
		CommitCleanup: t.commitFlowCleanup,
		Instance:      t.readFlowInstance,
		CheckSource:   t.checkStreamSourceLocked,
		DiscardInput:  t.staged.DiscardInput,
	}
}

func (t *Tenant) commitFlowCleanup(plan platform.FlowArtifactCleanupPreparation, now time.Time) *kernel.Error {
	member := platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}
	_, refusal := t.decideAutomated("flow:cleanup", platform.FlowApp, func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		latest, ok := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), plan.Instance())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow artifact cleanup owner is unavailable")
		}
		if !plan.Matches(latest) {
			return nil, nil
		}
		return t.accept(plan.Decision(t.App(platform.FlowApp)), member, plan.Submission(t.ID), now, acceptOptions{automation: true})
	})
	return refusal
}

func (t *Tenant) commitFlow(c automation.FlowCommit) (*pb.ChangeRecord, *kernel.Error) {
	return decideOnImage(t, context.Background(), c.Holder, func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		f, latest, current, refusal := t.continuousSnapshotLocked(c.Member, c.Instance, c.At)
		if refusal != nil {
			return nil, refusal
		}
		commit, refusal := c.Guard(f, latest, current)
		if refusal != nil || !commit {
			return nil, refusal
		}
		return t.accept(c.Prepared.Decision(f), c.Member, c.Prepared.Submission(t.ID), c.At, acceptOptions{automation: true, inputs: []*preparedOperationInput{c.Input}})
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		if c.Accepted != nil {
			c.Accepted(len(candidate.entry.Body) != 0)
		}
		return t.finishPreparedSubmission(c.Member, candidate, record, refusal)
	})
}

func (t *Tenant) readFlowInstance(id string) (platform.FlowInstance, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (platform.FlowInstance, *kernel.Error) {
		x, ok := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), id)
		if !ok {
			return x, unknown()
		}
		return x, nil
	})
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
	return sealer.Prepare(member, instance, request)
}
