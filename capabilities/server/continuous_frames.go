package platformserver

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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

func (s flowFrameStore) discard(ref platform.FlowStateArtifact) {
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
		plan, refusal = f.PlanBatch(before)
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
	retained := false
	defer func() {
		if !retained {
			store.discard(prepared.Artifact())
		}
	}()
	t.mu.Lock()
	defer t.mu.Unlock()
	_, latest, _, refusal := t.continuousSnapshotLocked(current, id, now)
	if refusal != nil {
		return flow.BatchOutcome{}, refusal
	}
	if !prepared.Matches(latest) {
		return flow.BatchOutcome{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
	}
	if _, refusal = t.submitAccepted(prepared.Decision(), platform.Member{ID: "app:" + flow.ID, Tenant: t.ID}, prepared.Submission(t.ID), now, true); refusal != nil {
		// An append error may have committed despite losing its response.
		// Retain its bytes until the original journal resolves that outcome.
		retained = true
		return flow.BatchOutcome{}, refusal
	}
	t.enqueue(now)
	x, _ := platform.Get[flow.FlowInstance](t.automation(flow.ID, false), id)
	retained = x.Batch.Sealed != nil && x.Batch.Sealed.Ticket == prepared.Artifact().Ticket
	return flow.BatchOutcome{Cursor: x.Batch.Cursor, Watermark: x.Batch.Watermark, Consumed: x.Batch.Consumed, Rejected: x.Batch.Rejected, StateSize: x.Batch.StateBytes}, nil
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
