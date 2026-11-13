package host

import (
	"bytes"
	"context"
	"encoding/json"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
	"slices"
	"strings"
	"time"
)

func (t *Tenant) streamSourceLocked(m platform.Member, name string, intake *platform.StreamIntake, now time.Time) (dataowner.Source, dataowner.Connection, string, *kernel.Error) {
	refuse := func(message string) (dataowner.Source, dataowner.Connection, string, *kernel.Error) {
		return dataowner.Source{}, dataowner.Connection{}, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, message)
	}
	if intake == nil {
		return refuse("The Flow has no controlled source intake")
	}
	view, err := t.recordOf(m, dataowner.SourceType, intake.SourceRecord, now)
	if err != nil {
		return refuse("The Flow source is not readable")
	}
	source, ok := view.Record.(dataowner.Source)
	if ok {
		var err error
		source, err = t.DataOwner().ExecutionSource(source)
		if err != nil {
			ok = false
		}
	}
	if !ok {
		return refuse("The Flow source must be a published incremental stream source")
	}
	view, err = t.recordOf(m, dataowner.ConnectionType, source.Connection, now)
	if err != nil {
		return refuse("The Flow connection is not readable")
	}
	connection, ok := view.Record.(dataowner.Connection)
	if ok {
		var err error
		connection, err = t.DataOwner().ExecutionConnection(connection)
		if err != nil {
			ok = false
		}
	}
	if !ok {
		return refuse("The Flow connection is not ready")
	}
	hash, refusal := dataowner.BindStream(name, intake, source, connection, m)
	return source, connection, hash, refusal
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
		if refusal == nil && before.Batch != nil && before.Batch.Source != nil {
			checkpoint := before.Batch.Source
			if checkpoint.Record != source.ID || checkpoint.Config != hash || checkpoint.Initialized && checkpoint.Offset != intake.Offset || intake.Offset == "latest" && !checkpoint.Initialized {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source changed after this Flow began consuming it")
			} else {
				source.Cursor = checkpoint.Cursor
			}
		}
		return struct{}{}, refusal
	})
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	columns := append([]string{source.Since, intake.Key, intake.EventTime, intake.Value}, intake.Partition...)
	columns = slices.Compact(slices.Sorted(slices.Values(append(columns, intake.Fields...))))
	rows, cursor, refusal := t.readContinuousSource(current, before, name, intake, source, connection, hash, latestBootstrap, columns, now)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	source.Cursor = cursor
	if len(rows) > budget {
		left, right := source.Advance(rows[:budget]), source.Advance(rows[:budget+1])
		if left == right {
			return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source cursor group exceeds the Flow batch budget")
		}
		rows = rows[:budget]
	}
	if len(rows) == 0 {
		if !latestBootstrap {
			if tick, due, tickErr := plan.WindowTick(now, t.frames()); tickErr != nil {
				return platform.FlowBatchOutcome{}, tickErr
			} else if due {
				return t.ConsumeFlowBatch(current, id, tick, now)
			}
			return platform.FlowBatchOutcome{}, nil
		}
		batch := platform.FlowBatch{Bootstrap: true, Source: &platform.FlowSourceCheckpoint{Record: source.ID, Config: hash, Cursor: source.Cursor, Sources: []string{dataowner.SourceType + "/" + source.ID, dataowner.ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
		if before.Batch != nil {
			batch.Predecessor = before.Batch.Cursor
		}
		fingerprint, fingerprintErr := platform.CanonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
		if fingerprintErr != nil {
			return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source bootstrap cannot be bound")
		}
		batch.ID = "source:" + fingerprint
		return t.ConsumeFlowBatch(current, id, batch, now)
	}
	position := source
	for _, row := range rows {
		next := position.Advance([]map[string]any{row})
		if next == position.Cursor {
			return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Stream rows need unique increasing incremental positions")
		}
		position.Cursor = next
	}
	batch := platform.FlowBatch{Source: &platform.FlowSourceCheckpoint{Record: source.ID, Config: hash, Cursor: source.Advance(rows), Sources: []string{dataowner.SourceType + "/" + source.ID, dataowner.ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
	if before.Batch != nil {
		batch.Predecessor = before.Batch.Cursor
	}
	for _, row := range rows {
		key, _ := row[intake.Key].(string)
		stamp, _ := row[intake.EventTime].(string)
		at, _ := time.Parse(time.RFC3339Nano, stamp)
		partition := make([]string, 0, len(intake.Partition))
		for _, field := range intake.Partition {
			value, ok := row[field].(string)
			if !ok || value == "" {
				return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source partition field needs a nonempty string")
			}
			partition = append(partition, value)
		}
		input, exists := row[intake.Value]
		if !exists {
			return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source signal column is missing")
		}
		if len(intake.Fields) > 0 {
			projected := map[string]any{intake.Value: input}
			for _, field := range append(slices.Clone(intake.Partition), intake.Fields...) {
				value, exists := row[field]
				if !exists {
					return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A declared source metadata field is missing")
				}
				projected[field] = value
			}
			input = projected
		}
		value, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source signal needs a JSON value")
		}
		batch.Signals = append(batch.Signals, platform.FlowSignal{Key: key, Partition: string(platform.Raw(partition)), At: at, Value: value})
	}
	fingerprint, err := platform.CanonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
	if err != nil {
		return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source batch cannot be bound")
	}
	batch.ID = "source:" + fingerprint
	return t.ConsumeFlowBatch(current, id, batch, now)
}

func (t *Tenant) readContinuousSource(m platform.Member, before platform.FlowInstance, name string, intake *platform.StreamIntake, source dataowner.Source, connection dataowner.Connection, hash string, latestBootstrap bool, columns []string, now time.Time) ([]map[string]any, string, *kernel.Error) {
	retry := intake.Retry
	if err := retry.Check(); err != nil {
		return nil, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	cursor, initialized := source.Cursor, !latestBootstrap
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
	var readErr error
	for attempt := 0; attempt < retry.Tries(); attempt++ {
		if attempt > 0 && retry.BackoffMS > 0 {
			time.Sleep(time.Duration(retry.BackoffMS) * time.Millisecond)
		}
		if refusal := checkOriginal(); refusal != nil {
			return nil, "", refusal
		}
		if !initialized {
			cursor, readErr = t.dataClient().LatestTableCursor(source, connection)
			if readErr != nil {
				continue
			}
			initialized = true
		}
		source.Cursor = cursor
		rows, err := t.dataClient().ReadTable(source, connection, columns...)
		if err == nil {
			if refusal := checkOriginal(); refusal != nil {
				return nil, "", refusal
			}
			return rows, cursor, nil
		}
		readErr = err
	}
	return nil, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, readErr.Error())
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
	var pending []platform.FlowInstance
	type retry struct {
		plan   platform.FlowComputeRetryPreparation
		member platform.Member
	}
	type cleanup struct {
		plan     platform.FlowArtifactCleanupPreparation
		instance string
		refusal  *kernel.Error
	}
	var retries []retry
	var cleanups []cleanup
	_, queryErr := recordRead(t, context.Background(), func(t *Tenant) (struct{}, *kernel.Error) {
		instances, total, queryErr := platform.Find[platform.FlowInstance](t.automation(platform.FlowApp, false), platform.Query{Sort: []string{"id"}})
		if queryErr != nil {
			return struct{}{}, queryErr
		}
		if total != len(instances) {
			return struct{}{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Continuous Flow inventory is incomplete")
		}
		f, ok := t.procs.(platform.ContinuousFlowOwner)
		if ok {
			for _, instance := range instances {
				if instance.CleanupPending {
					plan, err := f.PlanArtifactCleanup(instance, now)
					if err != nil {
						cleanups = append(cleanups, cleanup{instance: instance.ID, refusal: err})
					} else {
						cleanups = append(cleanups, cleanup{plan: plan, instance: instance.ID})
					}
					continue
				}
				if instance.State != "waiting" {
					continue
				}
				member, exists := t.Member(instance.OnBehalf)
				if !exists {
					continue
				}
				if plan, err := f.PlanComputeRetry(instance, t.automation(platform.FlowApp, false), now); err == nil {
					retries = append(retries, retry{plan: plan, member: member})
					continue
				}
				if plan, err := f.PlanBatch(instance, t.automation(platform.FlowApp, false)); err == nil {
					if _, intake, _ := plan.Intake(); intake != nil {
						source, exists := platform.Get[dataowner.Source](t.automation(build.ID, false), intake.SourceRecord)
						if exists {
							var err error
							source, err = t.DataOwner().ExecutionSource(source)
							if err != nil {
								exists = false
							}
						}
						if exists && source.Stream && source.State == "published" {
							pending = append(pending, instance)
						}
					}
				}
			}
		}
		return struct{}{}, nil
	})
	if queryErr != nil {
		log.Printf("continuous Flow inventory unavailable; no artifacts retired")
		return
	}
	for _, cleanup := range cleanups {
		if cleanup.refusal != nil {
			log.Printf("continuous Flow artifact cleanup %s refused: %s", cleanup.instance, cleanup.refusal.Code)
			continue
		}
		if refusal := t.retireContinuousArtifacts(cleanup.plan, now); refusal != nil {
			log.Printf("continuous Flow artifact cleanup %s refused: %s", cleanup.instance, refusal.Code)
		}
	}
	for _, retry := range retries {
		t.retryContinuousCompute(retry.member, retry.plan, now)
	}
	for _, instance := range pending {
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
		for round := 0; round < 8; round++ {
			outcome, refusal := t.ConsumeFlowSource(member, instance.ID, now)
			if refusal != nil {
				log.Printf("continuous source intake %s refused: %s", instance.ID, refusal.Code)
				break
			}
			if outcome.Cursor == "" || strings.HasPrefix(outcome.Cursor, "tick:") {
				break
			}
		}
	}
	if err := t.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		log.Printf("continuous Flow artifact collection deferred: %v", err)
	}
}
