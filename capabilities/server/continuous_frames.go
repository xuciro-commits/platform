package platformserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/flow"
	"platformserver/platform"
)

// Frozen Flow state belongs to the original file owner. Handles are accepted
// with native Flow records; no second cursor, job store or artifact index exists.
const maxFlowFrameBytes = 64 << 20

type flowFrameStore struct {
	tenant string
	files  FileStore
}

func (s flowFrameStore) key(ref platform.FlowStateArtifact) (string, error) {
	ticket, te := hex.DecodeString(ref.Ticket)
	digest, de := hex.DecodeString(ref.Digest)
	if ref.Tenant != s.tenant || ref.Instance == "" || ref.Version < 1 || te != nil || len(ticket) != 16 || de != nil || len(digest) != sha256.Size || ref.Size < 1 || ref.Size > maxFlowFrameBytes {
		return "", fmt.Errorf("the Flow state handle has an invalid owner or bound")
	}
	owner := sha256.Sum256(platform.Raw([]any{s.tenant, ref.Instance, ref.Version}))
	return fmt.Sprintf("%s/artifacts/flow/%x/%s/%s.json", s.tenant, owner, ref.Ticket, ref.Digest), nil
}

func (s flowFrameStore) Seal(instance string, version int, raw []byte, budget int) (platform.FlowStateArtifact, error) {
	if budget <= 0 || budget > maxFlowFrameBytes {
		budget = maxFlowFrameBytes
	}
	if len(raw) == 0 || len(raw) > budget || !json.Valid(raw) {
		return platform.FlowStateArtifact{}, fmt.Errorf("the Flow frame exceeds its sealed JSON budget")
	}
	var ticket [16]byte
	if _, err := rand.Read(ticket[:]); err != nil {
		return platform.FlowStateArtifact{}, err
	}
	ref := platform.FlowStateArtifact{Tenant: s.tenant, Instance: instance, Version: version, Ticket: hex.EncodeToString(ticket[:]), Digest: fmt.Sprintf("%x", sha256.Sum256(raw)), Size: len(raw)}
	key, err := s.key(ref)
	if err != nil {
		return ref, err
	}
	if err := s.files.Put(context.Background(), key, raw, "application/json"); err != nil {
		return ref, fmt.Errorf("seal Flow frame: %w", err)
	}
	return ref, nil
}

func (s flowFrameStore) Read(ref platform.FlowStateArtifact) ([]byte, error) {
	key, err := s.key(ref)
	if err != nil {
		return nil, err
	}
	reader, size, err := s.files.Get(context.Background(), key)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	if size != int64(ref.Size) {
		return nil, fmt.Errorf("the Flow artifact differs from its accepted size")
	}
	raw, err := io.ReadAll(io.LimitReader(reader, int64(ref.Size)+1))
	if err != nil || len(raw) != ref.Size || fmt.Sprintf("%x", sha256.Sum256(raw)) != ref.Digest || !json.Valid(raw) {
		return nil, fmt.Errorf("the Flow artifact differs from its accepted digest")
	}
	return raw, nil
}

func (s flowFrameStore) Discard(ref platform.FlowStateArtifact) {
	if key, err := s.key(ref); err == nil {
		s.files.Delete(context.Background(), key)
	}
}

func (t *Tenant) continuousSnapshot(m platform.Member, id string, now time.Time) (*flow.Flows, flow.FlowInstance, platform.Member, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.continuousSnapshotLocked(m, id, now)
}

func (t *Tenant) continuousSnapshotLocked(m platform.Member, id string, now time.Time) (*flow.Flows, flow.FlowInstance, platform.Member, *kernel.Error) {
	if t.quarantined() || m.Tenant != t.ID {
		return nil, flow.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	current, ok := t.Member(m.ID)
	if !ok || t.admits(current) != nil {
		return nil, flow.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	f, ok := t.procs.(*flow.Flows)
	if !ok {
		return nil, flow.FlowInstance{}, current, unknown()
	}
	v, refusal := t.RecordOf(current, flow.InstanceType, id, now)
	if refusal != nil {
		return nil, flow.FlowInstance{}, current, refusal
	}
	x, ok := v.Record.(flow.FlowInstance)
	if !ok || x.Withheld || x.OnBehalf != current.ID {
		return nil, flow.FlowInstance{}, current, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A source batch acts only for its original Flow member")
	}
	return f, x, current, nil
}

// ConsumeFlowBatch is the native source's outside-lane ingress. It prepares
// immutable bytes without the tenant lock, then commits through the existing
// accepted-result pipeline. HTTP submissions cannot construct the opaque plan.
func (t *Tenant) ConsumeFlowBatch(m platform.Member, id string, batch flow.Batch, now time.Time) (flow.BatchOutcome, *kernel.Error) {
	if t.AcceptResult == nil {
		return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Sealed Flow batches require accepted-result storage")
	}
	t.mu.Lock()
	f, before, current, refusal := t.continuousSnapshotLocked(m, id, now)
	var plan *flow.BatchPreparation
	if refusal == nil {
		plan, refusal = f.PlanBatch(before, t.automation(flow.ID, false))
	}
	t.mu.Unlock()
	if refusal != nil {
		return flow.BatchOutcome{}, refusal
	}
	digest, err := json.Marshal(batch)
	if err != nil {
		return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A batch needs valid JSON signal values")
	}
	batchDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(digest))
	key := flow.BatchKey(id, before.Version, batch.ID)
	// Native retries can arrive after later batches. The original accepted
	// ledger, not a second deduplication map, owns their stable identity.
	if prior := f.AcceptedLedger().AcceptedFor(t.ID, key); prior != nil {
		var saved struct{ Batch, Digest string }
		if json.Unmarshal(prior.GetSubmission().GetPayload(), &saved) != nil || saved.Batch != batch.ID || saved.Digest != batchDigest {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The batch identity was reused with different content")
		}
		return flow.BatchOutcome{Cursor: batch.ID}, nil
	}
	store := flowFrameStore{tenant: t.ID, files: t.files()}
	prepared, refusal := plan.Prepare(batch, now, store)
	if refusal != nil {
		return flow.BatchOutcome{}, refusal
	}
	var preparedInput *preparedOperationInput
	if request, ok := prepared.OperationRequest(); ok {
		preparedInput, refusal = t.prepareContinuousOperationInput(current, id, request)
		if refusal != nil {
			for _, artifact := range prepared.Artifacts() {
				store.Discard(artifact)
			}
			return flow.BatchOutcome{}, refusal
		}
	}
	retained, inputRetained := false, false
	defer func() {
		if !retained {
			for _, artifact := range prepared.Artifacts() {
				store.Discard(artifact)
			}
		}
		if preparedInput != nil && !inputRetained {
			t.staged.discardInput(preparedInput.ref)
		}
	}()
	t.mu.Lock()
	defer t.mu.Unlock()
	_, latest, current, refusal := t.continuousSnapshotLocked(current, id, now)
	if refusal != nil {
		return flow.BatchOutcome{}, refusal
	}
	if !prepared.Matches(latest) {
		return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
	}
	if batch.Source != nil {
		if refusal := t.checkStreamSourceLocked(current, plan, latest, *batch.Source, batch.Tick, now); refusal != nil {
			return flow.BatchOutcome{}, refusal
		}
	}
	if _, refusal = t.submitAccepted(prepared.Decision(), platform.Member{ID: "app:" + flow.ID, Tenant: t.ID}, prepared.Submission(t.ID), now, true, preparedInput); refusal != nil {
		// An append error may have committed despite losing its response.
		// Retain its bytes until the original journal resolves that outcome.
		retained, inputRetained = true, true
		return flow.BatchOutcome{}, refusal
	}
	t.enqueue(now)
	x, _ := platform.Get[flow.FlowInstance](t.automation(flow.ID, false), id)
	retained = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket
	inputRetained = preparedInput != nil
	return flow.BatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
}

func (t *Tenant) retryContinuousCompute(member platform.Member, plan *flow.ComputeRetryPreparation, now time.Time) {
	if t.AcceptResult == nil {
		return
	}
	prepared, refusal := plan.Prepare(now, flowFrameStore{tenant: t.ID, files: t.files()})
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
			t.staged.discardInput(preparedInput.ref)
		}
	}()
	t.mu.Lock()
	defer t.mu.Unlock()
	_, latest, _, refusal := t.continuousSnapshotLocked(member, instance, now)
	if refusal != nil || !prepared.Matches(latest) {
		if refusal == nil {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its Compute retry was prepared")
		}
		log.Printf("continuous Compute retry %s refused: %s", instance, refusal.Code)
		return
	}
	if _, refusal = t.submitAccepted(prepared.Decision(), platform.Member{ID: "app:" + flow.ID, Tenant: t.ID}, prepared.Submission(t.ID), now, true, preparedInput); refusal != nil {
		// Preserve a possibly committed call input until the accepted journal
		// resolves whether the retry decision was durable.
		inputRetained = true
		log.Printf("continuous Compute retry %s refused: %s", instance, refusal.Code)
		return
	}
	t.enqueue(now)
	inputRetained = preparedInput != nil
}

// prepareContinuousOperationInput seals an entry's window projection in the
// existing per-call compute channel when its retained operation declares the
// v2 data input ABI. Only an opaque, call/member/definition-bound reference is
// handed into the accepted decision; the full projection stays off Flow.Data.
func (t *Tenant) prepareContinuousOperationInput(member platform.Member, instance string, request platform.OperationRequest) (*preparedOperationInput, *kernel.Error) {
	refuse := func(code pb.ErrorCode, message string) (*preparedOperationInput, *kernel.Error) {
		return nil, platform.Refuse(code, message)
	}
	if request.App == "" || request.Name == "" || request.Key == "" || request.OnBehalf != member.ID || request.Target != flow.InstanceType+"/"+instance {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The prepared window names an invalid Compute operation")
	}
	t.mu.Lock()
	current, ok := t.Member(member.ID)
	owner := t.app(request.App)
	if !ok || current.Tenant != t.ID || owner == nil {
		t.mu.Unlock()
		return refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The prepared window Compute owner or member is unavailable")
	}
	op, version, ok := operationDefinition(owner, request.Name, request.Version)
	if !ok || op.Check() != nil {
		t.mu.Unlock()
		return refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The prepared window Compute operation is not retained")
	}
	definition, err := canonicalDigest([]any{request.App, op, version})
	budget := op.Limits.DataInputBytes
	t.mu.Unlock()
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute definition cannot be bound")
	}
	if budget == 0 {
		return nil, nil
	}
	if err := op.Input.Validate(request.Inputs, budget); err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The window does not satisfy its Compute input: "+err.Error())
	}
	hash, err := canonicalDigest(request.Inputs)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The prepared window Compute input cannot be encoded")
	}
	call := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, request.App, request.Key, operationEndpoint)
	ref, err := t.staged.sealInput(call, member.ID, definition, hash, request.Inputs, budget)
	if err != nil {
		t.staged.discardInput(ref)
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute input could not be sealed: "+err.Error())
	}
	requestHash, err := canonicalDigest(request)
	if err != nil {
		t.staged.discardInput(ref)
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute request cannot be bound")
	}
	return &preparedOperationInput{ref: ref, requestHash: requestHash}, nil
}

// ReadFlowFrame resolves only a currently readable original instance. A role
// change during artifact I/O cannot disclose its previously captured contents.
func (t *Tenant) ReadFlowFrame(m platform.Member, id string, now time.Time) (*flow.BatchFrame, *kernel.Error) {
	_, before, current, refusal := t.continuousSnapshot(m, id, now)
	if refusal != nil {
		return nil, refusal
	}
	frame, err := flow.ExpandFrame(before, flowFrameStore{tenant: t.ID, files: t.files()})
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	_, latest, _, refusal := t.continuousSnapshot(current, id, now)
	if refusal != nil {
		return nil, refusal
	}
	if latest.Revision != before.Revision {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its state was read")
	}
	return frame, nil
}

// streamSource captures only controlled source/connection definitions. Their
// mutable pull summaries and record clocks are not the stream's configuration.
func (t *Tenant) streamSourceLocked(m platform.Member, name string, intake *platform.StreamIntake, now time.Time) (build.Source, build.Connection, string, *kernel.Error) {
	refuse := func(message string) (build.Source, build.Connection, string, *kernel.Error) {
		return build.Source{}, build.Connection{}, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, message)
	}
	if intake == nil {
		return refuse("The Flow has no controlled source intake")
	}
	view, err := t.RecordOf(m, build.SourceType, intake.SourceRecord, now)
	if err != nil {
		return refuse("The Flow source is not readable")
	}
	source, ok := view.Record.(build.Source)
	if !ok || !source.Stream || source.State != "published" || source.Name != name || source.Since == "" || source.Profile != "table" {
		return refuse("The Flow source must be a published incremental stream source")
	}
	view, err = t.RecordOf(m, build.ConnectionType, source.Connection, now)
	if err != nil {
		return refuse("The Flow connection is not readable")
	}
	connection, ok := view.Record.(build.Connection)
	if !ok || connection.State != "ready" {
		return refuse("The Flow connection is not ready")
	}
	frozenSource, frozenConnection := source, connection
	frozenSource.Record, frozenSource.State, frozenSource.Cursor, frozenSource.Requested, frozenSource.Last = platform.Record{}, "", "", false, nil
	frozenConnection.Record, frozenConnection.State, frozenConnection.Requested, frozenConnection.Last = platform.Record{}, "", false, nil
	frozenSource.Title, frozenSource.Puller = "", ""
	frozenConnection.Name, frozenConnection.Title = "", ""
	hash, hashErr := canonicalDigest([]any{frozenSource, frozenConnection, intake})
	if hashErr != nil {
		return refuse("The Flow source configuration cannot be bound")
	}
	return source, connection, hash, nil
}

// ConsumeFlowSource reads an incremental registered Source on the I/O lane.
// Its consumer offset is accepted with the original window frame. No raw
// source rows enter an import record or a second cursor/queue database.
func (t *Tenant) ConsumeFlowSource(m platform.Member, id string, now time.Time) (flow.BatchOutcome, *kernel.Error) {
	t.mu.Lock()
	f, before, current, refusal := t.continuousSnapshotLocked(m, id, now)
	var plan *flow.BatchPreparation
	if refusal == nil {
		plan, refusal = f.PlanBatch(before, t.automation(flow.ID, false))
	}
	if refusal != nil {
		t.mu.Unlock()
		return flow.BatchOutcome{}, refusal
	}
	name, intake, budget := plan.Intake()
	source, connection, hash, refusal := t.streamSourceLocked(current, name, intake, now)
	latestBootstrap := intake.Offset == "latest" && (before.Batch == nil || before.Batch.Source == nil)
	if refusal == nil && before.Batch != nil && before.Batch.Source != nil {
		checkpoint := before.Batch.Source
		if checkpoint.Record != source.ID || checkpoint.Config != hash || checkpoint.Initialized && checkpoint.Offset != intake.Offset || intake.Offset == "latest" && !checkpoint.Initialized {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source changed after this Flow began consuming it")
		} else {
			source.Cursor = checkpoint.Cursor
		}
	}
	t.mu.Unlock()
	if refusal != nil {
		return flow.BatchOutcome{}, refusal
	}
	if latestBootstrap {
		cursor, cursorErr := t.latestTableCursor(source, connection)
		if cursorErr != nil {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, cursorErr.Error())
		}
		source.Cursor = cursor
	}
	columns := append([]string{source.Since, intake.Key, intake.EventTime, intake.Value}, intake.Partition...)
	rows, err := t.readTable(source, connection, columns...)
	if err != nil {
		if tick, due, tickErr := plan.WindowTick(now, flowFrameStore{tenant: t.ID, files: t.files()}); tickErr != nil {
			return flow.BatchOutcome{}, tickErr
		} else if due {
			return t.ConsumeFlowBatch(current, id, tick, now)
		}
		return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if len(rows) > budget {
		left, right := source.Advance(rows[:budget]), source.Advance(rows[:budget+1])
		if left == right {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source cursor group exceeds the Flow batch budget")
		}
		// The remaining rows stay behind the accepted incremental position.
		rows = rows[:budget]
	}
	if len(rows) == 0 {
		if !latestBootstrap {
			if tick, due, tickErr := plan.WindowTick(now, flowFrameStore{tenant: t.ID, files: t.files()}); tickErr != nil {
				return flow.BatchOutcome{}, tickErr
			} else if due {
				return t.ConsumeFlowBatch(current, id, tick, now)
			}
			return flow.BatchOutcome{}, nil
		}
		batch := flow.Batch{Bootstrap: true, Source: &flow.SourceCheckpoint{Record: source.ID, Config: hash, Cursor: source.Cursor, Sources: []string{build.SourceType + "/" + source.ID, build.ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
		if before.Batch != nil {
			batch.Predecessor = before.Batch.Cursor
		}
		fingerprint, fingerprintErr := canonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
		if fingerprintErr != nil {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source bootstrap cannot be bound")
		}
		batch.ID = "source:" + fingerprint
		return t.ConsumeFlowBatch(current, id, batch, now)
	}
	position := source
	for _, row := range rows {
		next := position.Advance([]map[string]any{row})
		if next == position.Cursor {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Stream rows need unique increasing incremental positions")
		}
		position.Cursor = next
	}
	batch := flow.Batch{Source: &flow.SourceCheckpoint{Record: source.ID, Config: hash, Cursor: source.Advance(rows), Sources: []string{build.SourceType + "/" + source.ID, build.ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
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
				return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source partition field needs a nonempty string")
			}
			partition = append(partition, value)
		}
		input, exists := row[intake.Value]
		if !exists {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source signal column is missing")
		}
		value, encodeErr := json.Marshal(input)
		if encodeErr != nil {
			return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A source signal needs a JSON value")
		}
		batch.Signals = append(batch.Signals, flow.Signal{Key: key, Partition: string(platform.Raw(partition)), At: at, Value: value})
	}
	fingerprint, err := canonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
	if err != nil {
		return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source batch cannot be bound")
	}
	batch.ID = "source:" + fingerprint
	return t.ConsumeFlowBatch(current, id, batch, now)
}

func (t *Tenant) checkStreamSourceLocked(m platform.Member, plan *flow.BatchPreparation, x flow.FlowInstance, checkpoint flow.SourceCheckpoint, tick bool, now time.Time) *kernel.Error {
	name, intake, _ := plan.Intake()
	source, connection, hash, refusal := t.streamSourceLocked(m, name, intake, now)
	if refusal != nil {
		return refusal
	}
	refs := []string{build.SourceType + "/" + source.ID, build.ConnectionType + "/" + connection.ID}
	if checkpoint.Record != source.ID || checkpoint.Config != hash || !slices.Equal(checkpoint.Sources, refs) || x.Batch != nil && x.Batch.Source != nil && (x.Batch.Source.Record != source.ID || x.Batch.Source.Config != hash) || intake.Offset == "latest" && (!checkpoint.Initialized || checkpoint.Offset != "latest") || checkpoint.Initialized && checkpoint.Offset != intake.Offset {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow source configuration or protected references changed")
	}
	if x.Batch != nil && x.Batch.Source != nil {
		source.Cursor = x.Batch.Source.Cursor
	}
	if tick {
		var prior *flow.SourceCheckpoint
		if x.Batch != nil {
			prior = x.Batch.Source
		}
		if prior == nil || prior.Cursor != checkpoint.Cursor || prior.Initialized != checkpoint.Initialized || prior.Offset != checkpoint.Offset || !slices.Equal(prior.Sources, checkpoint.Sources) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A slide tick cannot advance or replace the source checkpoint")
		}
		return nil
	}
	if checkpoint.Cursor == "" {
		if intake.Offset != "latest" || !checkpoint.Initialized || x.Batch != nil && x.Batch.Source != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source checkpoint must advance its accepted position")
		}
		return nil // an empty source was initialized at its latest offset
	}
	if source.Advance([]map[string]any{{source.Since: checkpoint.Cursor}}) != checkpoint.Cursor || source.Cursor == checkpoint.Cursor {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The source checkpoint must advance its accepted position")
	}
	return nil
}

// PullContinuousSources uses the host's existing outside loop. The Flow
// ledger remains the only consumer cursor owner; an overlapping pass backs
// off instead of creating duplicate concurrent reads of one predecessor.
func (t *Tenant) PullContinuousSources(now time.Time) {
	for _, pull := range t.continuousSources(now) {
		pull()
	}
}

// Reserve before entering the shared I/O lane, so a slow database does not
// accumulate one waiting goroutine per timer tick for the same tenant.
func (t *Tenant) continuousSources(now time.Time) []func() {
	if t.quarantined() || !t.streamMu.TryLock() {
		return nil
	}
	return []func(){func() {
		defer t.streamMu.Unlock()
		t.pullContinuousSources(now)
	}}
}

func (t *Tenant) pullContinuousSources(now time.Time) {
	if t.quarantined() {
		return
	}
	t.mu.Lock()
	instances, _, _ := platform.Find[flow.FlowInstance](t.automation(flow.ID, false), platform.Query{Domain: json.RawMessage(`[["state","=","waiting"]]`), Sort: []string{"id"}})
	f, ok := t.procs.(*flow.Flows)
	var pending []flow.FlowInstance
	type retry struct {
		plan   *flow.ComputeRetryPreparation
		member platform.Member
	}
	var retries []retry
	if ok {
		for _, instance := range instances {
			member, exists := t.Member(instance.OnBehalf)
			if !exists {
				continue
			}
			if plan, err := f.PlanComputeRetry(instance, t.automation(flow.ID, false), now); err == nil {
				retries = append(retries, retry{plan: plan, member: member})
				continue
			}
			if plan, err := f.PlanBatch(instance, t.automation(flow.ID, false)); err == nil {
				if _, intake, _ := plan.Intake(); intake != nil {
					source, exists := platform.Get[build.Source](t.automation(build.ID, false), intake.SourceRecord)
					if exists && source.Stream && source.State == "published" {
						pending = append(pending, instance)
					}
				}
			}
		}
	}
	t.mu.Unlock()
	for _, retry := range retries {
		t.retryContinuousCompute(retry.member, retry.plan, now)
	}
	for _, instance := range pending {
		member, exists := t.Member(instance.OnBehalf)
		if !exists {
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
}
