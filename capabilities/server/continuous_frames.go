package platformserver

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"platformserver/internal/host"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	build "platformserver/internal/host/buildapi"
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

func (s flowFrameStore) Discard(ref platform.FlowStateArtifact) error {
	key, err := s.key(ref)
	if err != nil {
		return err
	}
	if err := s.files.Delete(context.Background(), key); err != nil {
		return fmt.Errorf("discard Flow artifact: %w", err)
	}
	return nil
}

func (t *Tenant) continuousSnapshot(m platform.Member, id string, now time.Time) (platform.ContinuousFlowOwner, platform.FlowInstance, platform.Member, *kernel.Error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.continuousSnapshotLocked(m, id, now)
}

func (t *Tenant) continuousSnapshotLocked(m platform.Member, id string, now time.Time) (platform.ContinuousFlowOwner, platform.FlowInstance, platform.Member, *kernel.Error) {
	if t.quarantined() || m.Tenant != t.ID {
		return nil, platform.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	current, ok := t.Member(m.ID)
	// Token scope is part of the original caller, not directory state. Refresh
	// current grants without widening the read/preparation/acceptance scope.
	current.Scopes = slices.Clone(m.Scopes)
	if !ok || t.admits(current) != nil {
		return nil, platform.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	f, ok := t.procs.(platform.ContinuousFlowOwner)
	if !ok {
		return nil, platform.FlowInstance{}, current, unknown()
	}
	v, refusal := t.RecordOf(current, platform.FlowInstanceType, id, now)
	if refusal != nil {
		return nil, platform.FlowInstance{}, current, refusal
	}
	x, ok := v.Record.(platform.FlowInstance)
	if !ok || x.Withheld || x.OnBehalf != current.ID {
		return nil, platform.FlowInstance{}, current, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A source batch acts only for its original Flow member")
	}
	return f, x, current, nil
}

func (t *Tenant) cleanupFlowArtifactsFor(m platform.Member, id string, now time.Time) *kernel.Error {
	t.mu.Lock()
	f, before, _, refusal := t.continuousSnapshotLocked(m, id, now)
	if refusal != nil {
		t.mu.Unlock()
		return refusal
	}
	if !before.CleanupPending {
		t.mu.Unlock()
		return nil
	}
	plan, refusal := f.PlanArtifactCleanup(before, now)
	t.mu.Unlock()
	if refusal != nil {
		return refusal
	}
	return t.retireContinuousArtifacts(plan, now)
}

// ConsumeFlowBatch is the native source's outside-lane ingress. It prepares
// immutable bytes without the tenant lock, then commits through the existing
// accepted-result pipeline. HTTP submissions cannot construct the opaque plan.
func (t *Tenant) ConsumeFlowBatch(m platform.Member, id string, batch platform.FlowBatch, now time.Time) (platform.FlowBatchOutcome, *kernel.Error) {
	releasePreparation := t.beginFlowArtifactPreparation()
	defer releasePreparation()
	t.mu.Lock()
	f, before, _, refusal := t.continuousSnapshotLocked(m, id, now)
	t.mu.Unlock()
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	digest, err := json.Marshal(batch)
	if err != nil {
		return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A batch needs valid JSON signal values")
	}
	batchDigest := fmt.Sprintf("sha256:%x", sha256.Sum256(digest))
	key := platform.FlowBatchKey(id, before.Version, batch.ID)
	// Native retries can arrive after later batches. The original accepted
	// ledger, not a second deduplication map, owns their stable identity.
	if prior := f.AcceptedLedger().AcceptedFor(t.ID, key); prior != nil {
		var saved struct{ Batch, Digest string }
		if json.Unmarshal(prior.GetSubmission().GetPayload(), &saved) != nil || saved.Batch != batch.ID || saved.Digest != batchDigest {
			return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT, "The batch identity was reused with different content")
		}
		return platform.FlowBatchOutcome{Cursor: batch.ID}, nil
	}
	if before.CleanupPending {
		if refusal := t.cleanupFlowArtifactsFor(m, id, now); refusal != nil {
			return platform.FlowBatchOutcome{}, refusal
		}
	}
	var current platform.Member
	var plan platform.FlowBatchPreparation
	t.mu.Lock()
	f, before, current, refusal = t.continuousSnapshotLocked(m, id, now)
	if refusal == nil {
		plan, refusal = f.PlanBatch(before, t.automation(platform.FlowApp, false))
	}
	t.mu.Unlock()
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	store := flowFrameStore{tenant: t.ID, files: t.files()}
	prepared, refusal := plan.Prepare(batch, now, store)
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
			t.staged.discardInput(preparedInput.ref)
		}
	}()
	t.mu.Lock()
	defer t.mu.Unlock()
	_, latest, current, refusal := t.continuousSnapshotLocked(current, id, now)
	if refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	if !prepared.Matches(latest) {
		return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
	}
	if batch.Source != nil {
		if refusal := t.checkStreamSourceLocked(current, plan, latest, *batch.Source, batch.Tick, now); refusal != nil {
			return platform.FlowBatchOutcome{}, refusal
		}
	}
	if _, refusal = t.submitAccepted(prepared.Decision(), platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}, prepared.Submission(t.ID), now, true, preparedInput); refusal != nil {
		// An append error may have committed despite losing its response.
		// Retain its bytes until the original journal resolves that outcome.
		retained, inputRetained = true, true
		return platform.FlowBatchOutcome{}, refusal
	}
	t.enqueue(now)
	x, _ := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), id)
	retained = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket
	inputRetained = preparedInput != nil
	return platform.FlowBatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
}

// prepareContinuousDeadLetterReplay recognizes only signal replay. Business
// action replay stays inside the original Flow decision without outside I/O.
func (t *Tenant) prepareContinuousDeadLetterReplay(m platform.Member, sub *pb.Submission, now time.Time, contentVersion ...string) (*pb.ChangeRecord, *kernel.Error, bool) {
	releasePreparation := t.beginFlowArtifactPreparation()
	defer releasePreparation()
	t.mu.Lock()
	var binding *platform.SchemaVersion
	if owner, ok := t.procs.(platform.ContinuousFlowOwner); ok {
		var schemaRefusal *kernel.Error
		if len(contentVersion) > 0 {
			binding, schemaRefusal = t.submissionSchemaVersion(owner, sub, contentVersion[0])
			if schemaRefusal != nil && schemaRefusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
				t.mu.Unlock()
				return nil, schemaRefusal, true
			}
		} else {
			sub = t.currentSubmissionSchema(owner, sub)
		}
		_, refused := t.committed.refusals[owner.Manifest().ID+"/"+sub.GetIdempotencyKey()]
		if schemaRefusal != nil || refused || owner.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
			record, err := t.submitAcceptedVersion(owner, m, sub, now, false, binding, schemaRefusal, nil)
			t.mu.Unlock()
			return record, err, true
		}
	}
	view, err := t.RecordOf(m, "flow.dead-letter", sub.GetTarget().GetId(), now)
	if err != nil {
		t.mu.Unlock()
		return nil, err, true
	}
	asset, ok := view.Record.(platform.FlowDeadLetterAsset)
	if !ok || asset.Withheld {
		t.mu.Unlock()
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dead letter's protected source is unavailable"), true
	}
	if asset.Action != "" {
		t.mu.Unlock()
		return nil, nil, false
	}
	f, before, current, err := t.continuousSnapshotLocked(m, asset.Instance, now)
	var plan platform.FlowDeadLetterReplayPreparation
	if err == nil {
		plan, err = f.PlanDeadLetterReplay(before, t.automation(platform.FlowApp, false), asset)
	}
	t.mu.Unlock()
	if err != nil {
		return nil, err, true
	}
	if before.CleanupPending {
		if err := t.cleanupFlowArtifactsFor(current, before.ID, now); err != nil {
			return nil, err, true
		}
		t.mu.Lock()
		f, before, current, err = t.continuousSnapshotLocked(current, before.ID, now)
		if err == nil {
			plan, err = f.PlanDeadLetterReplay(before, t.automation(platform.FlowApp, false), asset)
		}
		t.mu.Unlock()
		if err != nil {
			return nil, err, true
		}
	}
	store := flowFrameStore{tenant: t.ID, files: t.files()}
	prepared, err := plan.Prepare(sub, now, store)
	if err != nil {
		return nil, err, true
	}
	var preparedInput *preparedOperationInput
	if request, ok := prepared.OperationRequest(); ok {
		preparedInput, err = t.prepareContinuousOperationInput(current, before.ID, request)
		if err != nil {
			for _, artifact := range prepared.Artifacts() {
				_ = store.Discard(artifact)
			}
			return nil, err, true
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
			t.staged.discardInput(preparedInput.ref)
		}
	}()
	t.mu.Lock()
	defer t.mu.Unlock()
	_, latest, current, err := t.continuousSnapshotLocked(current, before.ID, now)
	if err != nil {
		return nil, err, true
	}
	if !prepared.Matches(latest) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original Flow changed while dead-letter replay was prepared"), true
	}
	if latest.Batch.Source != nil {
		batchPlan, refusal := f.PlanBatch(latest, t.automation(platform.FlowApp, false))
		if refusal != nil {
			return nil, refusal, true
		}
		if refusal := t.checkStreamSourceLocked(current, batchPlan, latest, *latest.Batch.Source, true, now); refusal != nil {
			return nil, refusal, true
		}
	}
	record, err := t.submitAcceptedVersion(prepared.Decision(), current, sub, now, false, binding, nil, nil, preparedInput)
	_, refused := t.committed.refusals[platform.FlowApp+"/"+sub.GetIdempotencyKey()]
	// A durable refusal names no prepared bytes. An uncertain append must
	// retain them until the original accepted journal resolves its outcome.
	retained = err == nil || !refused
	inputRetained = preparedInput != nil && retained
	if err == nil {
		t.enqueue(now)
	}
	return record, err, true
}

// retireContinuousArtifacts deletes only refs named by an already-accepted
// cleanup intent. The intent is acknowledged through the ordinary Flow result
// ledger after every delete succeeds, so process restarts safely retry partial
// object-store failures without a second queue or cursor.
func (t *Tenant) retireContinuousArtifacts(plan platform.FlowArtifactCleanupPreparation, now time.Time) *kernel.Error {
	if plan == nil || plan.Instance() == "" || now.IsZero() {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Flow artifact cleanup is unavailable")
	}
	refs := plan.Artifacts()
	if len(refs) == 0 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow artifact cleanup intent is empty")
	}
	store := flowFrameStore{tenant: t.ID, files: t.files()}
	for _, ref := range refs {
		if ref.Tenant != t.ID {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A retired Flow artifact belongs to another tenant")
		}
		if err := store.Discard(ref); err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A superseded Flow artifact could not be retired: "+err.Error())
		}
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	latest, ok := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), plan.Instance())
	if !ok {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow artifact cleanup owner is unavailable")
	}
	if !plan.Matches(latest) {
		// Another host pass may have acknowledged the same intent while these
		// idempotent deletes ran. The artifact bytes are already safe to discard.
		return nil
	}
	if _, refusal := t.submitAccepted(plan.Decision(), platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}, plan.Submission(t.ID), now, true); refusal != nil {
		return refusal
	}
	t.enqueue(now)
	return nil
}

func (t *Tenant) retryContinuousCompute(member platform.Member, plan platform.FlowComputeRetryPreparation, now time.Time) {
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
	if _, refusal = t.submitAccepted(prepared.Decision(), platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}, prepared.Submission(t.ID), now, true, preparedInput); refusal != nil {
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
	if request.App == "" || request.Name == "" || request.Key == "" || request.OnBehalf != member.ID || request.Target != platform.FlowInstanceType+"/"+instance {
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
func (t *Tenant) ReadFlowFrame(m platform.Member, id string, now time.Time) (*platform.FlowBatchFrame, *kernel.Error) {
	owner, before, current, refusal := t.continuousSnapshot(m, id, now)
	if refusal != nil {
		return nil, refusal
	}
	frame, err := owner.ExpandFrame(before, flowFrameStore{tenant: t.ID, files: t.files()})
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
	if ok {
		var err error
		source, err = t.app(build.ID).(host.IntegrationDefinitions).ExecutionSource(source)
		if err != nil {
			ok = false
		}
	}
	if !ok || !source.Stream || source.State != "published" || source.Name != name || source.Since == "" || source.Profile != "table" {
		return refuse("The Flow source must be a published incremental stream source")
	}
	view, err = t.RecordOf(m, build.ConnectionType, source.Connection, now)
	if err != nil {
		return refuse("The Flow connection is not readable")
	}
	connection, ok := view.Record.(build.Connection)
	if ok {
		var err error
		connection, err = t.app(build.ID).(host.IntegrationDefinitions).ExecutionConnection(connection)
		if err != nil {
			ok = false
		}
	}
	if !ok || connection.State != "ready" {
		return refuse("The Flow connection is not ready")
	}
	frozenSource, frozenConnection := source, connection
	frozenSource.Record, frozenSource.State, frozenSource.Cursor, frozenSource.Requested, frozenSource.Last = platform.Record{}, "", "", false, nil
	frozenConnection.Record, frozenConnection.State, frozenConnection.Requested, frozenConnection.Last = platform.Record{}, "", false, nil
	frozenSource.Title, frozenSource.Puller = "", ""
	frozenSource.DefinitionVersion, frozenSource.Published, frozenSource.Versions = 0, "", nil
	frozenConnection.DefinitionVersion, frozenConnection.Published, frozenConnection.Versions = 0, "", nil
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
func (t *Tenant) ConsumeFlowSource(m platform.Member, id string, now time.Time) (platform.FlowBatchOutcome, *kernel.Error) {
	if refusal := t.cleanupFlowArtifactsFor(m, id, now); refusal != nil {
		return platform.FlowBatchOutcome{}, refusal
	}
	t.mu.Lock()
	f, before, current, refusal := t.continuousSnapshotLocked(m, id, now)
	var plan platform.FlowBatchPreparation
	if refusal == nil {
		plan, refusal = f.PlanBatch(before, t.automation(platform.FlowApp, false))
	}
	if refusal != nil {
		t.mu.Unlock()
		return platform.FlowBatchOutcome{}, refusal
	}
	name, intake, budget := plan.Intake()
	source, connection, hash, refusal := t.streamSourceLocked(current, name, intake, now)
	latestBootstrap := refusal == nil && intake.Offset == "latest" && (before.Batch == nil || before.Batch.Source == nil)
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
		// The remaining rows stay behind the accepted incremental position.
		rows = rows[:budget]
	}
	if len(rows) == 0 {
		if !latestBootstrap {
			if tick, due, tickErr := plan.WindowTick(now, flowFrameStore{tenant: t.ID, files: t.files()}); tickErr != nil {
				return platform.FlowBatchOutcome{}, tickErr
			} else if due {
				return t.ConsumeFlowBatch(current, id, tick, now)
			}
			return platform.FlowBatchOutcome{}, nil
		}
		batch := platform.FlowBatch{Bootstrap: true, Source: &platform.FlowSourceCheckpoint{Record: source.ID, Config: hash, Cursor: source.Cursor, Sources: []string{build.SourceType + "/" + source.ID, build.ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
		if before.Batch != nil {
			batch.Predecessor = before.Batch.Cursor
		}
		fingerprint, fingerprintErr := canonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
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
	batch := platform.FlowBatch{Source: &platform.FlowSourceCheckpoint{Record: source.ID, Config: hash, Cursor: source.Advance(rows), Sources: []string{build.SourceType + "/" + source.ID, build.ConnectionType + "/" + connection.ID}, Offset: intake.Offset, Initialized: true}}
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
	fingerprint, err := canonicalDigest([]any{batch.Predecessor, batch.Source, batch.Bootstrap, batch.Signals})
	if err != nil {
		return platform.FlowBatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The source batch cannot be bound")
	}
	batch.ID = "source:" + fingerprint
	return t.ConsumeFlowBatch(current, id, batch, now)
}

// Source retries retain the original instance, definition and source cursor.
// Backoff and database I/O run outside the writer lock; each attempt refreshes
// the original member and proves the same retained automation is still allowed.
func (t *Tenant) readContinuousSource(m platform.Member, before platform.FlowInstance, name string, intake *platform.StreamIntake, source build.Source, connection build.Connection, hash string, latestBootstrap bool, columns []string, now time.Time) ([]map[string]any, string, *kernel.Error) {
	retry := intake.Retry
	if err := retry.Check(); err != nil {
		return nil, "", platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	cursor, initialized := source.Cursor, !latestBootstrap
	checkOriginal := func() *kernel.Error {
		t.mu.Lock()
		defer t.mu.Unlock()
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
		// Once latest resolves, retry reads from that exact initial position.
		// A later latest query would silently skip rows arriving during backoff.
		if !initialized {
			cursor, readErr = t.latestTableCursor(source, connection)
			if readErr != nil {
				continue
			}
			initialized = true
		}
		source.Cursor = cursor
		rows, err := t.readTable(source, connection, columns...)
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
	refs := []string{build.SourceType + "/" + source.ID, build.ConnectionType + "/" + connection.ID}
	if checkpoint.Record != source.ID || checkpoint.Config != hash || !slices.Equal(checkpoint.Sources, refs) || x.Batch != nil && x.Batch.Source != nil && (x.Batch.Source.Record != source.ID || x.Batch.Source.Config != hash) || intake.Offset == "latest" && (!checkpoint.Initialized || checkpoint.Offset != "latest") || checkpoint.Initialized && checkpoint.Offset != intake.Offset {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow source configuration or protected references changed")
	}
	if x.Batch != nil && x.Batch.Source != nil {
		source.Cursor = x.Batch.Source.Cursor
	}
	if tick {
		var prior *platform.FlowSourceCheckpoint
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
	instances, total, queryErr := platform.Find[platform.FlowInstance](t.automation(platform.FlowApp, false), platform.Query{Sort: []string{"id"}})
	if queryErr != nil || total != len(instances) {
		t.mu.Unlock()
		log.Printf("continuous Flow inventory unavailable; no artifacts retired")
		return
	}
	f, ok := t.procs.(platform.ContinuousFlowOwner)
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
	if ok {
		for _, instance := range instances {
			if instance.CleanupPending {
				plan, err := f.PlanArtifactCleanup(instance, now)
				if err != nil {
					cleanups = append(cleanups, cleanup{instance: instance.ID, refusal: err})
				} else {
					cleanups = append(cleanups, cleanup{plan: plan, instance: instance.ID})
				}
				// Cleanup is independent host work. Do not prepare a source batch
				// against an instance whose prior retirement intent is unresolved.
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
					source, exists := platform.Get[build.Source](t.automation(build.ID, false), intake.SourceRecord)
					if exists {
						var err error
						source, err = t.app(build.ID).(host.IntegrationDefinitions).ExecutionSource(source)
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
	t.mu.Unlock()
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
	if err := t.collectUnacceptedFlowArtifacts(context.Background(), now); err != nil {
		log.Printf("continuous Flow artifact collection deferred: %v", err)
	}
}
