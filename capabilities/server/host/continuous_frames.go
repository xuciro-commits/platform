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
	current := m
	store := flowFrameStore{tenant: t.ID, files: t.files()}
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
		return t.submitAccepted(prepared.Decision(f), member, prepared.Submission(t.ID), now, true, preparedInput)
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

func (t *Tenant) prepareContinuousDeadLetterReplay(m platform.Member, sub *pb.Submission, now time.Time, contentVersion ...string) (*pb.ChangeRecord, *kernel.Error, bool) {
	releasePreparation := t.beginFlowArtifactPreparation()
	defer releasePreparation()
	type preflight struct {
		sub     *pb.Submission
		binding *platform.SchemaVersion
		record  *pb.ChangeRecord
		handled bool
	}
	first, err := decideOnImage(t, context.Background(), "flow:replay-preflight", func(t *Tenant) (preflight, *kernel.Error) {
		value := preflight{sub: sub}
		if owner, ok := t.procs.(platform.ContinuousFlowOwner); ok {
			var refusal *kernel.Error
			if len(contentVersion) > 0 {
				value.binding, refusal = t.submissionSchemaVersion(owner, sub, contentVersion[0])
				if refusal != nil && refusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
					return value, refusal
				}
			} else {
				value.sub = t.currentSubmissionSchema(owner, sub)
			}
			_, refused := t.committed.Refusal(owner.Manifest().ID + "/" + sub.GetIdempotencyKey())
			if refusal != nil || refused || owner.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
				value.handled = true
				value.record, refusal = t.submitAcceptedVersion(owner, m, value.sub, now, false, value.binding, refusal, nil, nil)
				return value, refusal
			}
		}
		return value, nil
	}, func(t *Tenant, candidate preparedAccepted, value preflight, refusal *kernel.Error) (preflight, *kernel.Error) {
		value.record, refusal = t.finishPreparedSubmission(m, candidate, value.record, refusal)
		return value, refusal
	})
	if err != nil || first.handled {
		return first.record, err, true
	}
	sub = first.sub
	type replayContext struct {
		asset    platform.FlowDeadLetterAsset
		member   platform.Member
		instance platform.FlowInstance
	}
	load := func(t *Tenant) (replayContext, *kernel.Error) {
		view, refusal := t.recordOf(m, "flow.dead-letter", sub.GetTarget().GetId(), now)
		if refusal != nil {
			return replayContext{}, refusal
		}
		asset, ok := view.Record.(platform.FlowDeadLetterAsset)
		if !ok || asset.Withheld {
			return replayContext{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The dead letter's protected source is unavailable")
		}
		if asset.Action != "" {
			return replayContext{asset: asset}, nil
		}
		_, instance, current, refusal := t.continuousSnapshotLocked(m, asset.Instance, now)
		return replayContext{asset, current, instance}, refusal
	}
	initial, err := recordRead(t, context.Background(), load)
	if err != nil {
		return nil, err, true
	}
	if initial.asset.Action != "" {
		return nil, nil, false
	}
	if initial.instance.CleanupPending {
		if err := t.cleanupFlowArtifactsFor(initial.member, initial.instance.ID, now); err != nil {
			return nil, err, true
		}
	}
	store := flowFrameStore{tenant: t.ID, files: t.files()}
	var preparedContext replayContext
	prepared, err := recordRead(t, context.Background(), func(t *Tenant) (platform.FlowPreparedBatch, *kernel.Error) {
		var refusal *kernel.Error
		preparedContext, refusal = load(t)
		if refusal != nil {
			return nil, refusal
		}
		if preparedContext.asset.Action != "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dead letter changed while its replay was prepared")
		}
		f := t.procs.(platform.ContinuousFlowOwner)
		plan, refusal := f.PlanDeadLetterReplay(preparedContext.instance, t.automation(platform.FlowApp, false), preparedContext.asset)
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
		preparedInput, err = t.prepareContinuousOperationInput(preparedContext.member, preparedContext.instance.ID, request)
		if err != nil {
			return nil, err, true
		}
	}
	record, err := decideOnImage(t, context.Background(), "flow:replay", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		f, latest, current, refusal := t.continuousSnapshotLocked(preparedContext.member, preparedContext.instance.ID, now)
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
		return t.submitAcceptedVersion(prepared.Decision(f), current, sub, now, false, first.binding, nil, nil, nil, preparedInput)
	}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
		if len(candidate.entry.Body) > 0 {
			retained, inputRetained = true, preparedInput != nil
		}
		record, refusal = t.finishPreparedSubmission(preparedContext.member, candidate, record, refusal)
		if _, rejected := t.committed.Refusal(platform.FlowApp + "/" + sub.GetIdempotencyKey()); rejected {
			retained, inputRetained = false, false
		}
		return record, refusal
	})
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

	member := platform.Member{ID: "app:" + platform.FlowApp, Tenant: t.ID}
	_, refusal := decideOnImage(t, context.Background(), "flow:cleanup", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
		latest, ok := platform.Get[platform.FlowInstance](t.automation(platform.FlowApp, false), plan.Instance())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow artifact cleanup owner is unavailable")
		}
		if !plan.Matches(latest) {
			return nil, nil
		}
		return t.submitAccepted(plan.Decision(t.app(platform.FlowApp)), member, plan.Submission(t.ID), now, true)
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
		return fresh.Prepare(now, flowFrameStore{tenant: t.ID, files: t.files()})
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
		return t.submitAccepted(prepared.Decision(f), actor, prepared.Submission(t.ID), now, true, preparedInput)
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
	refuse := func(code pb.ErrorCode, message string) (*preparedOperationInput, *kernel.Error) {
		return nil, platform.Refuse(code, message)
	}
	if request.App == "" || request.Name == "" || request.Key == "" || request.OnBehalf != member.ID || request.Target != platform.FlowInstanceType+"/"+instance {
		return refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The prepared window names an invalid Compute operation")
	}
	type binding struct {
		operation  platform.Operation
		definition string
		budget     int
	}
	bound, refusal := recordRead(t, context.Background(), func(t *Tenant) (binding, *kernel.Error) {
		current, ok := t.Member(member.ID)
		owner := t.app(request.App)
		if !ok || current.Tenant != t.ID || owner == nil {
			return binding{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The prepared window Compute owner or member is unavailable")
		}
		op, version, ok := operationDefinition(owner, request.Name, request.Version)
		if !ok || op.Check() != nil {
			return binding{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The prepared window Compute operation is not retained")
		}
		definition, err := platform.CanonicalDigest([]any{request.App, op, version})
		if err != nil {
			return binding{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute definition cannot be bound")
		}
		return binding{op, definition, op.Limits.DataInputBytes}, nil
	})
	if refusal != nil {
		return nil, refusal
	}
	op, definition, budget := bound.operation, bound.definition, bound.budget
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
