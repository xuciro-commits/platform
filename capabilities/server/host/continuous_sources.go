package host

import (
	"bytes"
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) streamSourceLocked(m platform.Member, name string, intake *platform.StreamIntake, now time.Time) (dataowner.Source, dataowner.Connection, string, *platform.Error) {
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

func (t *Tenant) ConsumeFlowSource(m platform.Member, id string, now time.Time) (protocol.FlowBatchOutcome, *platform.Error) {
	if refusal := t.flowIntake().Cleanup(m, id, now); refusal != nil {
		return protocol.FlowBatchOutcome{}, refusal
	}
	var before protocol.FlowInstance
	var current platform.Member
	var plan automation.FlowBatchPreparation
	var start dataowner.StreamStart
	_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *platform.Error) {
		f, instance, member, refusal := t.continuousSnapshotLocked(m, id, now)
		if refusal != nil {
			return struct{}{}, refusal
		}
		before, current = instance, member
		plan, refusal = f.PlanBatch(before, t.automation(protocol.FlowApp, false))
		if refusal != nil {
			return struct{}{}, refusal
		}
		start, refusal = dataowner.BeginStream(plan, before, func(name string, intake *platform.StreamIntake) (dataowner.Source, dataowner.Connection, string, *platform.Error) {
			return t.streamSourceLocked(current, name, intake, now)
		})
		return struct{}{}, refusal
	})
	if refusal != nil {
		return protocol.FlowBatchOutcome{}, refusal
	}
	return dataowner.StreamConsumption{
		Read: func(source dataowner.Source, columns []string) ([]map[string]any, string, *platform.Error) {
			return t.readContinuousSource(current, before, start, source, columns, now)
		},
		Tick: func() (protocol.FlowBatch, bool, *platform.Error) { return plan.WindowTick(now, t.frames()) },
		Consume: func(batch protocol.FlowBatch) (protocol.FlowBatchOutcome, *platform.Error) {
			return t.flowIntake().ConsumeBatch(current, id, batch, now)
		},
	}.Run(before, start)
}

func (t *Tenant) readContinuousSource(m platform.Member, before protocol.FlowInstance, start dataowner.StreamStart, source dataowner.Source, columns []string, now time.Time) ([]map[string]any, string, *platform.Error) {
	checkOriginal := func() *platform.Error {
		_, refusal := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *platform.Error) {
			owner, latest, current, refusal := t.continuousSnapshotLocked(m, before.ID, now)
			if refusal == nil && !bytes.Equal(platform.Raw(latest), platform.Raw(before)) {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
			}
			if refusal == nil {
				_, refusal = owner.PlanBatch(latest, t.automation(protocol.FlowApp, false))
			}
			if refusal == nil {
				freshSource, freshConnection, freshHash, err := t.streamSourceLocked(current, start.Name, start.Intake, now)
				refusal = err
				if refusal == nil && !start.Unchanged(freshSource, freshConnection, freshHash) {
					refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source changed after this Flow began consuming it")
				}
			}
			return struct{}{}, refusal
		})
		return refusal
	}
	client := t.dataClient()
	return dataowner.StreamReader{Check: checkOriginal, Latest: client.LatestTableCursor, Read: client.ReadTable}.Rows(start.Intake, source, start.Connection, start.Bootstrap, columns)
}

func (t *Tenant) checkStreamSourceLocked(m platform.Member, plan automation.FlowBatchPreparation, x protocol.FlowInstance, checkpoint protocol.FlowSourceCheckpoint, tick bool, now time.Time) *platform.Error {
	name, intake, _ := plan.Intake()
	source, connection, hash, refusal := t.streamSourceLocked(m, name, intake, now)
	if refusal != nil {
		return refusal
	}
	return source.CheckCheckpoint(connection, hash, intake, x, checkpoint, tick)
}

func (t *Tenant) continuousSources(now time.Time) []func() {
	if t.businessStopped() || !t.OwnerActive(protocol.FlowApp) || !t.streamMu.TryLock() {
		return nil
	}
	return []func(){func() {
		defer t.streamMu.Unlock()
		t.pullContinuousSources(now)
	}}
}

func (t *Tenant) pullContinuousSources(now time.Time) {
	automation.SourcePull{Stopped: t.businessStopped,
		Inventory: func(now time.Time) (automation.ContinuousWork, *platform.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (automation.ContinuousWork, *platform.Error) {
				owner := t.Processes
				return automation.ContinuousInventory{
					Owner:  owner,
					Caller: t.automation(protocol.FlowApp, false),
					Member: t.Member,
					StreamPublished: func(record string) bool {
						return t.DataOwner().StreamPublished(t.automation(build.ID, false), record)
					},
				}.Scan(now)
			})
		},
		Retire: func(plan automation.FlowArtifactCleanupPreparation, now time.Time) *platform.Error {
			return t.flowIntake().Retire(plan, now)
		},
		Retry: func(member platform.Member, plan automation.FlowComputeRetryPreparation, now time.Time) {
			t.flowIntake().RetryCompute(member, plan, now)
		},
		Member: func(id string) (platform.Member, *platform.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (platform.Member, *platform.Error) {
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
