package host

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/platform"
	"slices"
	"time"
)

const maxFlowFrameBytes = 64 << 20

type flowFrameStore struct {
	tenant string
	files  platform.FileStore
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
	current.Scopes = slices.Clone(m.Scopes)
	if !ok || t.admits(current) != nil {
		return nil, platform.FlowInstance{}, m, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The Flow member is unavailable")
	}
	f, ok := t.procs.(platform.ContinuousFlowOwner)
	if !ok {
		return nil, platform.FlowInstance{}, current, unknown()
	}
	v, refusal := t.recordOf(current, platform.FlowInstanceType, id, now)
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
			t.staged.DiscardInput(preparedInput.ref)
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
		retained, inputRetained = true, true
		return platform.FlowBatchOutcome{}, refusal
	}
	t.enqueue(now)
	x, _ := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), id)
	retained = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket
	inputRetained = preparedInput != nil
	return platform.FlowBatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
}

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
		_, refused := t.committed.Refusal(owner.Manifest().ID + "/" + sub.GetIdempotencyKey())
		if schemaRefusal != nil || refused || owner.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
			record, err := t.submitAcceptedVersion(owner, m, sub, now, false, binding, schemaRefusal, nil, nil)
			t.mu.Unlock()
			return record, err, true
		}
	}
	view, err := t.recordOf(m, "flow.dead-letter", sub.GetTarget().GetId(), now)
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
			t.staged.DiscardInput(preparedInput.ref)
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
	record, err := t.submitAcceptedVersion(prepared.Decision(), current, sub, now, false, binding, nil, nil, nil, preparedInput)
	_, refused := t.committed.Refusal(platform.FlowApp + "/" + sub.GetIdempotencyKey())
	retained = err == nil || !refused
	inputRetained = preparedInput != nil && retained
	if err == nil {
		t.enqueue(now)
	}
	return record, err, true
}

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
			t.staged.DiscardInput(preparedInput.ref)
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
		inputRetained = true
		log.Printf("continuous Compute retry %s refused: %s", instance, refusal.Code)
		return
	}
	t.enqueue(now)
	inputRetained = preparedInput != nil
}

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
	definition, err := platform.CanonicalDigest([]any{request.App, op, version})
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
	hash, err := platform.CanonicalDigest(request.Inputs)
	if err != nil {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The prepared window Compute input cannot be encoded")
	}
	call := fmt.Sprintf("%s:%s:operation:%s:%s", t.ID, request.App, request.Key, acceptedformat.OperationEndpoint)
	ref, err := t.staged.SealInput(call, member.ID, definition, hash, request.Inputs, budget)
	if err != nil {
		t.staged.DiscardInput(ref)
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute input could not be sealed: "+err.Error())
	}
	requestHash, err := platform.CanonicalDigest(request)
	if err != nil {
		t.staged.DiscardInput(ref)
		return refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute request cannot be bound")
	}
	return &preparedOperationInput{ref: ref, requestHash: requestHash}, nil
}

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
