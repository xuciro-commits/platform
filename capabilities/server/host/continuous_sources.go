package host

import (
	"bytes"
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) streamSourceLocked(m platform.Member, name string, intake *platform.StreamIntake, now time.Time) (dataowner.Source, dataowner.Connection, string, *kernel.Error) {
	return dataowner.StreamRecords{
		Record: func(typ, id string) (any, error) {
			view, err := t.recordOf(m, typ, id, now)
			if err != nil {
				return nil, err
			}
			return view.Record, nil
		},
		Source: func(source dataowner.Source) (dataowner.Source, error) { return t.DataOwner().ExecutionSource(source) },
		Connection: func(connection dataowner.Connection) (dataowner.Connection, error) {
			return t.DataOwner().ExecutionConnection(connection)
		},
	}.Resolve(m, name, intake)
}

func (t *Tenant) ConsumeFlowSource(m platform.Member, id string, now time.Time) (platform.FlowBatchOutcome, *kernel.Error) {
	if refusal := t.cleanupFlowArtifactsFor(m, id, now); refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	var before platform.FlowInstance
	var current platform.Member
	var plan platform.FlowBatchPreparation
	var name, hash string
	var intake *platform.StreamIntake
	var budget int
	var source dataowner.Source
	var connection dataowner.Connection
	var latestBootstrap bool
	_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		f, instance, member, refusal := t.continuousSnapshotLocked(m, id, now)
		if refusal != nil {
			return struct{}{}, refusal
		}
		before, current = instance, member
		plan, refusal = f.PlanBatch(before, t.automation(platform.FlowApp, false))
		if refusal != nil {
			return struct{}{}, refusal
		}
		name, intake, budget = plan.Intake()
		source, connection, hash, refusal = t.streamSourceLocked(current, name, intake, now)
		latestBootstrap = refusal == nil && intake.Offset == "latest" && (before.Batch == nil || before.Batch.Source == nil)
		if refusal == nil {
			source, refusal = source.Resume(before, hash, intake)
		}
		return struct{}{}, refusal
	})
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	return dataowner.StreamConsumption{
		Read: func(source dataowner.Source, columns []string) ([]map[string]any, string, *kernel.Error) {
			return t.readContinuousSource(current, before, name, intake, source, connection, hash, latestBootstrap, columns, now)
		},
		Tick: func() (platform.FlowBatch, bool, *kernel.Error) { return plan.WindowTick(now, t.frames()) },
		Consume: func(batch platform.FlowBatch) (platform.FlowBatchOutcome, *kernel.Error) {
			return t.ConsumeFlowBatch(current, id, batch, now)
		},
	}.Run(before, source, connection, hash, intake, budget, latestBootstrap)
}

func (t *Tenant) readContinuousSource(m platform.Member, before platform.FlowInstance, name string, intake *platform.StreamIntake, source dataowner.Source, connection dataowner.Connection, hash string, latestBootstrap bool, columns []string, now time.Time) ([]map[string]any, string, *kernel.Error) {
	checkOriginal := func() *kernel.Error {
		_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
			owner, latest, current, refusal := t.continuousSnapshotLocked(m, before.ID, now)
			if refusal == nil && !bytes.Equal(platform.Raw(latest), platform.Raw(before)) {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
			}
			if refusal == nil {
				_, refusal = owner.PlanBatch(latest, t.automation(platform.FlowApp, false))
			}
			if refusal == nil {
				freshSource, freshConnection, freshHash, err := t.streamSourceLocked(current, name, intake, now)
				refusal = err
				if refusal == nil && (freshHash != hash || freshSource.DefinitionVersion != source.DefinitionVersion || freshSource.Published != source.Published || freshConnection.DefinitionVersion != connection.DefinitionVersion || freshConnection.Published != connection.Published) {
					refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source changed after this Flow began consuming it")
				}
			}
			return struct{}{}, refusal
		})
		return refusal
	}
	client := t.dataClient()
	return dataowner.StreamReader{Check: checkOriginal, Latest: client.LatestTableCursor, Read: client.ReadTable}.Rows(intake, source, connection, latestBootstrap, columns)
}

func (t *Tenant) checkStreamSourceLocked(m platform.Member, plan platform.FlowBatchPreparation, x platform.FlowInstance, checkpoint platform.FlowSourceCheckpoint, tick bool, now time.Time) *kernel.Error {
	name, intake, _ := plan.Intake()
	source, connection, hash, refusal := t.streamSourceLocked(m, name, intake, now)
	if refusal != nil {
		return refusal
	}
	return source.CheckCheckpoint(connection, hash, intake, x, checkpoint, tick)
}

func (t *Tenant) continuousSources(now time.Time) []func() {
	if t.businessStopped() || !t.OwnerActive(platform.FlowApp) || !t.streamMu.TryLock() {
		return nil
	}
	return []func(){func() {
		defer t.streamMu.Unlock()
		t.pullContinuousSources(now)
	}}
}

func (t *Tenant) pullContinuousSources(now time.Time) {
	automation.SourcePull{Stopped: t.businessStopped,
		Inventory: func(now time.Time) (automation.ContinuousWork, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (automation.ContinuousWork, *kernel.Error) {
				owner, _ := t.procs.(platform.ContinuousFlowOwner)
				return automation.ContinuousInventory{
					Owner:  owner,
					Caller: t.automation(platform.FlowApp, false),
					Member: t.Member,
					StreamPublished: func(record string) bool {
						source, exists := platform.Get[dataowner.Source](t.automation(build.ID, false), record)
						if exists {
							var err error
							source, err = t.DataOwner().ExecutionSource(source)
							exists = err == nil
						}
						return exists && source.Stream && source.State == "published"
					},
				}.Scan(now)
			})
		},
		Retire: t.retireContinuousArtifacts,
		Retry:  t.retryContinuousCompute,
		Member: func(id string) (platform.Member, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (platform.Member, *kernel.Error) {
				member, exists := t.Member(id)
				if !exists {
					return member, unknown()
				}
				return member, nil
			})
		},
		Consume: t.ConsumeFlowSource,
		Collect: func(now time.Time) error { return t.collectUnacceptedFlowArtifacts(context.Background(), now) },
	}.Run(now)
}
