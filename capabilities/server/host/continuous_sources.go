package host

import (
	"bytes"
	"context"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
	"time"
)

func (t *Tenant) streamSourceLocked(m platform.Member, name string, intake *platform.StreamIntake, now time.Time) (dataowner.Source, dataowner.Connection, string, *kernel.Error) {
	var records dataowner.StreamRecords
	if intake != nil {
		records = dataowner.StreamRecords{
			Source: func(id string) (dataowner.Source, bool, error) {
				view, err := t.recordOf(m, dataowner.SourceType, id, now)
				if err != nil {
					return dataowner.Source{}, false, err
				}
				source, ok := view.Record.(dataowner.Source)
				if ok {
					var err error
					source, err = t.DataOwner().ExecutionSource(source)
					ok = err == nil
				}
				return source, ok, nil
			},
			Connection: func(id string) (dataowner.Connection, bool, error) {
				view, err := t.recordOf(m, dataowner.ConnectionType, id, now)
				if err != nil {
					return dataowner.Connection{}, false, err
				}
				connection, ok := view.Record.(dataowner.Connection)
				if ok {
					var err error
					connection, err = t.DataOwner().ExecutionConnection(connection)
					ok = err == nil
				}
				return connection, ok, nil
			},
		}
	}
	return records.Resolve(m, name, intake)
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
	columns := dataowner.StreamColumns(source, intake)
	rows, cursor, refusal := t.readContinuousSource(current, before, name, intake, source, connection, hash, latestBootstrap, columns, now)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	source.Cursor = cursor
	rows, refusal = dataowner.TrimStreamRows(source, rows, budget)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	if len(rows) == 0 && !latestBootstrap {
		if tick, due, tickErr := plan.WindowTick(now, t.frames()); tickErr != nil {
			return platform.FlowBatchOutcome{}, tickErr
		} else if due {
			return t.ConsumeFlowBatch(current, id, tick, now)
		}
		return platform.FlowBatchOutcome{}, nil
	}
	batch, refusal := dataowner.StreamBatch(before, source, connection, hash, intake, rows)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	return t.ConsumeFlowBatch(current, id, batch, now)
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
	if t.businessStopped() {
		return
	}
	var work automation.ContinuousWork
	_, queryErr := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		owner, _ := t.procs.(platform.ContinuousFlowOwner)
		var scanErr *kernel.Error
		work, scanErr = automation.ContinuousInventory{
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
		return struct{}{}, scanErr
	})
	if queryErr != nil {
		log.Printf("continuous Flow inventory unavailable; no artifacts retired")
		return
	}
	for _, cleanup := range work.Cleanups {
		if cleanup.Refusal != nil {
			log.Printf("continuous Flow artifact cleanup %s refused: %s", cleanup.Instance, cleanup.Refusal.Code)
			continue
		}
		if refusal := t.retireContinuousArtifacts(cleanup.Plan, now); refusal != nil {
			log.Printf("continuous Flow artifact cleanup %s refused: %s", cleanup.Instance, refusal.Code)
		}
	}
	for _, retry := range work.Retries {
		t.retryContinuousCompute(retry.Member, retry.Plan, now)
	}
	for _, instance := range work.Pending {
		member, refusal := recordRead(t, context.Background(), func(t *Tenant) (platform.Member, *kernel.Error) {
			member, exists := t.Member(instance.OnBehalf)
			if !exists {
				return member, unknown()
			}
			return member, nil
		})
		if refusal != nil {
			continue
		}
		if refusal := automation.DrainSource(8, func() (platform.FlowBatchOutcome, *kernel.Error) {
			return t.ConsumeFlowSource(member, instance.ID, now)
		}); refusal != nil {
			log.Printf("continuous source intake %s refused: %s", instance.ID, refusal.Code)
		}
	}
	if err := t.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		log.Printf("continuous Flow artifact collection deferred: %v", err)
	}
}
