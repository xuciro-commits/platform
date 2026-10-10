package flow

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// PreparedBatch is an opaque, host-prepared update. There is no JSON input
// which can construct one: a native source prepares outside the tenant lock,
// then the original accepted-decision pipeline validates its predecessor.
type PreparedBatch struct {
	owner       *Flows
	before      FlowInstance
	frame       *BatchFrame
	artifacts   []platform.FlowStateArtifact
	retired     []platform.FlowStateArtifact
	outputs     map[string]json.RawMessage
	sources     []string
	batch       Batch
	at          time.Time
	entry       string
	windowInput json.RawMessage
	resumeInput json.RawMessage
	operation   *platform.OperationRequest
}

// ExpandFrame reads a frozen original frame on the I/O lane. Its summary,
// owner and native version must agree with the accepted instance projection.
func ExpandFrame(x FlowInstance, store host.FlowFrameStore) (*BatchFrame, error) {
	if x.Batch == nil {
		return &BatchFrame{State: map[string]json.RawMessage{}}, nil
	}
	if x.Batch.Sealed == nil {
		var frame BatchFrame
		if err := json.Unmarshal(platform.Raw(x.Batch), &frame); err != nil {
			return nil, err
		}
		return &frame, nil
	}
	ref := *x.Batch.Sealed
	if ref.Instance != x.ID || ref.Version != x.Version {
		return nil, fmt.Errorf("the sealed frame belongs to another instance or version")
	}
	raw, err := store.Read(ref)
	if err != nil {
		return nil, err
	}
	var frame BatchFrame
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&frame); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF || frame.Sealed != nil || frame.Cursor != x.Batch.Cursor || frame.Predecessor != x.Batch.Predecessor || frame.Fingerprint != x.Batch.Fingerprint || !frame.Watermark.Equal(x.Batch.Watermark) || frame.Consumed != x.Batch.Consumed || frame.Rejected != x.Batch.Rejected || stateSize(frame.State) != x.Batch.StateBytes || frame.CheckpointEvery != x.Batch.CheckpointEvery || frame.BatchesSinceCheckpoint != x.Batch.BatchesSinceCheckpoint || frame.CheckpointCursor != x.Batch.CheckpointCursor || !bytes.Equal(platform.Raw(frame.CheckpointArtifact), platform.Raw(x.Batch.CheckpointArtifact)) || !bytes.Equal(platform.Raw(frame.Source), platform.Raw(x.Batch.Source)) {
		return nil, fmt.Errorf("the sealed frame differs from its accepted summary")
	}
	if frame.CheckpointEvery < 0 || frame.CheckpointEvery > 1_000_000 || frame.BatchesSinceCheckpoint < 0 || frame.BatchesSinceCheckpoint >= frame.CheckpointEvery && frame.CheckpointEvery > 0 || (frame.CheckpointCursor == "") != (frame.CheckpointArtifact == nil) {
		return nil, fmt.Errorf("the sealed checkpoint schedule is invalid")
	}
	if frame.CheckpointArtifact != nil {
		checkpointRef := *frame.CheckpointArtifact
		if frame.CheckpointEvery == 0 || checkpointRef.Instance != x.ID || checkpointRef.Version != x.Version {
			return nil, fmt.Errorf("the checkpoint belongs to another Flow instance or version")
		}
		checkpointRaw, err := store.Read(checkpointRef)
		if err != nil {
			return nil, err
		}
		var checkpoint BatchFrame
		checkpointDecoder := json.NewDecoder(bytes.NewReader(checkpointRaw))
		checkpointDecoder.DisallowUnknownFields()
		if err := checkpointDecoder.Decode(&checkpoint); err != nil {
			return nil, err
		}
		if err := checkpointDecoder.Decode(new(any)); err != io.EOF || checkpoint.Sealed != nil || checkpoint.CheckpointArtifact != nil || checkpoint.Cursor != frame.CheckpointCursor || checkpoint.CheckpointCursor != frame.CheckpointCursor || checkpoint.CheckpointEvery != frame.CheckpointEvery || checkpoint.BatchesSinceCheckpoint != 0 || stateSize(checkpoint.State) != checkpoint.StateBytes {
			return nil, fmt.Errorf("the retained checkpoint differs from its accepted cursor")
		}
	}
	return &frame, nil
}

type BatchPreparation struct {
	owner     *Flows
	before    FlowInstance
	rule      platform.Continuous
	entry     string
	operation *platform.OperationStep
	caller    platform.Caller
}

// PlanBatch captures the retained definition and binding under the tenant
// lock. The later I/O lane never reads the mutable definition registry.
func (f *Flows) PlanBatch(x FlowInstance, c platform.Caller) (*BatchPreparation, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if d == nil || d.Continuous == nil || ended(x.State) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous instance is unavailable")
	}
	if err := f.checkBinding(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if d.Continuous.Entry != "" {
		stream := 0
		for _, token := range x.Tokens {
			if token.Waits == "stream" {
				stream++
				continue
			}
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A previous window Compute is still running; the source must apply backpressure")
		}
		if stream != 1 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous Compute entry has no retained stream token")
		}
	}
	rule := *d.Continuous
	if rule.Window != nil {
		window := *rule.Window
		rule.Window = &window
	}
	if rule.Intake != nil {
		intake := *rule.Intake
		intake.Partition = slices.Clone(intake.Partition)
		rule.Intake = &intake
	}
	plan := &BatchPreparation{owner: f, before: x, rule: rule}
	if rule.Entry != "" {
		dstep := d.steps[rule.Entry]
		if dstep == nil || dstep.Operation == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute entry is unavailable")
		}
		operation := *dstep.Operation
		plan.entry, plan.operation = rule.Entry, &operation
		plan.caller = f.host.Automation(c, d.app)
	}
	return plan, nil
}

func (plan *BatchPreparation) Intake() (string, *platform.StreamIntake, int) {
	if plan.rule.Intake == nil {
		return plan.rule.Source, nil, plan.rule.Batch
	}
	intake := *plan.rule.Intake
	intake.Partition = slices.Clone(intake.Partition)
	return plan.rule.Source, &intake, plan.rule.Batch
}

// WindowTick creates an idempotent timer batch when the event-time grace has
// closed a new slide. Its source checkpoint is byte-for-byte unchanged.
func (plan *BatchPreparation) WindowTick(now time.Time, store host.FlowFrameStore) (Batch, bool, *kernel.Error) {
	if plan == nil || plan.rule.Intake == nil || plan.rule.Window == nil || plan.before.Batch == nil || plan.before.Batch.Source == nil || now.IsZero() {
		return Batch{}, false, nil
	}
	frame, err := ExpandFrame(plan.before, store)
	if err != nil {
		return Batch{}, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	var state WindowState
	if raw := frame.State[plan.rule.Window.Node]; len(raw) == 0 || json.Unmarshal(raw, &state) != nil || state.Rows == nil {
		return Batch{}, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted event-time window cannot be decoded for its slide tick")
	}
	expiredDeadLetter := false
	if plan.rule.DeadLetterTTLMs > 0 {
		cutoff := now.Add(-time.Duration(plan.rule.DeadLetterTTLMs) * time.Millisecond)
		for _, letter := range frame.DeadLetters {
			recorded := letter.RecordedAt
			if recorded.IsZero() {
				recorded = letter.At
			}
			if !recorded.IsZero() && recorded.Before(cutoff) {
				expiredDeadLetter = true
				break
			}
		}
	}
	boundary := windowBoundary(*plan.rule.Window, frame.Watermark, now)
	slideDue := boundary.After(state.EmittedAt) && (len(state.Rows) > 0 || plan.entry != "")
	if !slideDue && !expiredDeadLetter {
		return Batch{}, false, nil
	}
	material := platform.Raw([]any{plan.before.ID, plan.before.Version, frame.Cursor, boundary})
	id := fmt.Sprintf("tick:%x", sha256.Sum256(material))
	checkpoint := *frame.Source
	checkpoint.Sources = slices.Clone(frame.Source.Sources)
	return Batch{ID: id, Predecessor: frame.Cursor, Tick: true, Source: &checkpoint}, true, nil
}

type ComputeRetryPreparation struct {
	owner     *Flows
	before    FlowInstance
	rule      platform.Continuous
	entry     string
	token     int
	operation *platform.OperationStep
	caller    platform.Caller
}

// PlanComputeRetry captures only a due entry retry. Its original rows are
// rebuilt from the accepted sealed frame on the outside I/O lane.
func (f *Flows) PlanComputeRetry(x FlowInstance, c platform.Caller, now time.Time) (*ComputeRetryPreparation, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if d == nil || d.Continuous == nil || d.Continuous.Entry == "" || d.Continuous.Window == nil || ended(x.State) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous Compute retry is unavailable")
	}
	if err := f.checkBinding(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	step := d.steps[d.Continuous.Entry]
	if step == nil || step.Operation == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute entry is unavailable")
	}
	var token *Token
	streams := 0
	for i := range x.Tokens {
		t := &x.Tokens[i]
		if t.Waits == "stream" {
			streams++
			continue
		}
		if t.Step == d.Continuous.Entry && t.Waits == "retry" && !t.Due.IsZero() && !t.Due.After(now) {
			if token != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous Flow has multiple entry retries")
			}
			token = t
			continue
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous Flow still has outstanding work")
	}
	if streams != 1 || token == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous Flow has no due window Compute retry")
	}
	rule := *d.Continuous
	window := *rule.Window
	rule.Window = &window
	operation := *step.Operation
	return &ComputeRetryPreparation{owner: f, before: x, rule: rule, entry: rule.Entry, token: token.ID,
		operation: &operation, caller: f.host.Automation(c, d.app)}, nil
}

func (plan *ComputeRetryPreparation) Instance() string { return plan.before.ID }

// Prepare recomputes only the retained operation request from the immutable
// accepted frame; it does not reread the source or advance the batch cursor.
func (plan *ComputeRetryPreparation) Prepare(now time.Time, store host.FlowFrameStore) (*PreparedComputeRetry, *kernel.Error) {
	x := plan.before
	frame, err := ExpandFrame(x, store)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	var state WindowState
	if raw := frame.State[plan.rule.Window.Node]; len(raw) == 0 || json.Unmarshal(raw, &state) != nil || !state.Ready {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted frame has no ready window for this retry")
	}
	outputs := maps.Clone(x.Outputs)
	if outputs == nil {
		outputs = map[string]json.RawMessage{}
	}
	w := plan.rule.Window
	projection := ContinuousWindowInput{
		Source: plan.rule.Source, Batch: frame.Cursor, Predecessor: frame.Predecessor,
		Cursor: frame.Cursor, Watermark: frame.Watermark,
		WindowStart: state.EmittedAt.Add(-time.Duration(w.WindowMS) * time.Millisecond),
		WindowEnd:   state.EmittedAt, EmittedAt: state.EmittedAt,
		Consumed: frame.Consumed, Rejected: frame.Rejected, Signals: windowSignals(state, *w), Outputs: outputs,
	}
	run := plan.owner.run(&x)
	if token := slices.IndexFunc(x.Tokens, func(t Token) bool { return t.ID == plan.token }); token >= 0 {
		run.Outputs, run.Frames = maps.Clone(x.Tokens[token].Outputs), slices.Clone(x.Tokens[token].Frames)
	}
	run.Now, run.Data, run.Sources = now, platform.Raw(projection), slices.Clone(x.Sources)
	request, refusal := plan.operation.Request(plan.caller, run)
	if refusal != nil {
		return nil, refusal
	}
	request.Key = fmt.Sprintf("flow:%s:%s:%d", x.ID, plan.entry, x.Seq)
	request.OnBehalf = x.OnBehalf
	request.Release = &x.Release
	request.Target = InstanceType + "/" + x.ID
	request.Sources = slices.Compact(slices.Sorted(slices.Values(append(append(slices.Clone(x.Sources), run.Sources...), request.Sources...))))
	if request.App == "" || request.Name == "" || request.OnBehalf != x.OnBehalf {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The retained Compute retry must name its operation and member")
	}
	digest := sha256.Sum256(platform.Raw(request))
	return &PreparedComputeRetry{owner: plan.owner, before: x, entry: plan.entry, token: plan.token, request: request, at: now, digest: fmt.Sprintf("sha256:%x", digest)}, nil
}

type PreparedComputeRetry struct {
	owner   *Flows
	before  FlowInstance
	entry   string
	token   int
	request platform.OperationRequest
	at      time.Time
	digest  string
}

func (p *PreparedComputeRetry) Matches(x FlowInstance) bool {
	return x.ID == p.before.ID && x.Revision == p.before.Revision && x.Flow == p.before.Flow && x.Version == p.before.Version && x.Release == p.before.Release && x.Dependencies == p.before.Dependencies && x.OnBehalf == p.before.OnBehalf && x.State == p.before.State && bytes.Equal(platform.Raw(x.Batch), platform.Raw(p.before.Batch))
}

func (p *PreparedComputeRetry) Submission(tenant string) *pb.Submission {
	key := sha256.Sum256(platform.Raw([]any{p.before.ID, p.before.Version, p.before.Revision, p.token, p.digest}))
	return &pb.Submission{TenantId: tenant, Authority: ID, PrincipalId: "app:" + ID,
		IdempotencyKey: fmt.Sprintf("flow-compute-retry:%x", key),
		Target:         &pb.EntityRef{Type: InstanceType, Id: p.before.ID}, Schema: &pb.SchemaRef{Name: SchemaFlowStep, Version: 1}, Payload: json.RawMessage("{}")}
}

func (p *PreparedComputeRetry) Instance() string { return p.before.ID }

func (p *PreparedComputeRetry) OperationRequest() platform.OperationRequest {
	request := p.request
	request.Inputs = slices.Clone(p.request.Inputs)
	request.Sources = slices.Clone(p.request.Sources)
	if p.request.Release != nil {
		release := *p.request.Release
		request.Release = &release
	}
	return request
}

func (p *PreparedComputeRetry) Decision() platform.ResultApp {
	return preparedComputeRetryDecision{Flows: p.owner, prepared: p}
}

type preparedComputeRetryDecision struct {
	*Flows
	prepared *PreparedComputeRetry
}

func (d preparedComputeRetryDecision) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p := d.prepared
	if p.before.Batch == nil || p.before.Batch.Sealed == nil || !c.Automation || c.ID != "app:"+ID || c.Tenant != p.before.Batch.Sealed.Tenant || sub.GetTenantId() != c.Tenant || sub.GetAuthority() != ID || sub.GetPrincipalId() != c.ID || sub.GetSchema().GetName() != SchemaFlowStep || sub.GetTarget().GetId() != p.before.ID || !bytes.Equal(sub.GetPayload(), []byte("{}")) || !bytes.Equal(sub.GetPayload(), p.Submission(c.Tenant).GetPayload()) || sub.GetIdempotencyKey() != p.Submission(c.Tenant).GetIdempotencyKey() || !now.Equal(p.at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the host can accept a prepared Flow Compute retry")
	}
	return d.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := d.session(c, now)
		x := ss.load(p.before.ID)
		if x == nil || !p.Matches(*x) || ended(x.State) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its Compute retry was prepared")
		}
		if err := d.checkBinding(*x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		token := ss.token(x, p.token)
		if token.ID < 0 || token.Step != p.entry || token.Waits != "retry" || token.Due.After(now) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute retry is no longer due")
		}
		token.Waits, token.Due = "ready", time.Time{}
		ss.preparedOperations[p.token] = p.request
		ss.advance(x)
		if len(ss.operations) != 1 || ss.operations[0].instance != x.ID || ss.operations[0].token != p.token || !bytes.Equal(platform.Raw(ss.operations[0].request), platform.Raw(p.request)) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute retry differs from its prepared request")
		}
		return ss.apply, nil
	})
}

// Prepare uses the original folding rule outside the tenant lock. No file
// I/O or mutable registry read occurs in the accepted ledger callback.
func (plan *BatchPreparation) Prepare(batch Batch, now time.Time, store host.FlowFrameStore) (*PreparedBatch, *kernel.Error) {
	x := plan.before
	if err := validateArtifactCleanup(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if x.CleanupPending {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Superseded Flow artifacts must be retired before another source batch is accepted")
	}
	frame, err := ExpandFrame(x, store)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	_, _, refusal := foldBatch(frame, plan.rule, batch, now)
	if refusal != nil {
		return nil, refusal
	}
	outputs := maps.Clone(x.Outputs)
	if outputs == nil {
		outputs = map[string]json.RawMessage{}
	}
	for node, raw := range frame.State {
		// All prepared state outputs identify immutable original bytes, even
		// when one window happens to fit inline on its first iteration.
		outputs["state:"+node] = platform.Raw(BatchStateReference{Instance: x.ID, Node: node, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), Size: len(raw)})
	}
	outputs["batch"] = platform.Raw(map[string]any{"cursor": frame.Cursor, "consumed": frame.Consumed, "rejected": frame.Rejected})
	if len(platform.Raw(outputs)) > 60<<10 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch outputs exceed the Flow output budget")
	}
	sources := append([]string(nil), x.Sources...)
	if batch.Source != nil {
		for _, source := range batch.Source.Sources {
			if !slices.Contains(sources, source) {
				sources = append(sources, source)
			}
		}
	}
	var windowInput, resumeInput json.RawMessage
	var operationRequest *platform.OperationRequest
	if plan.entry != "" {
		var state WindowState
		if raw := frame.State[plan.rule.Window.Node]; len(raw) == 0 || json.Unmarshal(raw, &state) != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared event-time window cannot be decoded")
		}
		if state.Ready {
			w := plan.rule.Window
			previousOutputs := maps.Clone(x.Outputs)
			if previousOutputs == nil {
				previousOutputs = map[string]json.RawMessage{}
			}
			projection := ContinuousWindowInput{
				Source: plan.rule.Source, Batch: batch.ID, Predecessor: batch.Predecessor,
				Cursor: frame.Cursor, Watermark: frame.Watermark,
				WindowStart: state.EmittedAt.Add(-time.Duration(w.WindowMS) * time.Millisecond),
				WindowEnd:   state.EmittedAt, EmittedAt: state.EmittedAt,
				Consumed: frame.Consumed, Rejected: frame.Rejected, Signals: windowSignals(state, *w),
				Outputs: previousOutputs,
			}
			windowInput = platform.Raw(projection)
			resumeInput = platform.Raw(map[string]any{"outputs": previousOutputs})
			if len(resumeInput) > 60<<10 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained window continuation exceeds the Flow input budget")
			}
			run := plan.owner.run(&x)
			run.Now, run.Data, run.Sources = now, slices.Clone(windowInput), slices.Clone(sources)
			request, requestErr := plan.operation.Request(plan.caller, run)
			if requestErr != nil {
				return nil, requestErr
			}
			request.Key = fmt.Sprintf("flow:%s:%s:%d", x.ID, plan.entry, x.Seq)
			request.OnBehalf = x.OnBehalf
			request.Release = &x.Release
			request.Target = InstanceType + "/" + x.ID
			request.Sources = slices.Compact(slices.Sorted(slices.Values(append(append(slices.Clone(x.Sources), run.Sources...), request.Sources...))))
			if request.App == "" || request.Name == "" || request.OnBehalf != x.OnBehalf {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The retained Compute entry must name its operation and member")
			}
			operationRequest = &request
		}
	}
	var artifacts, retired []platform.FlowStateArtifact
	if x.Batch != nil && x.Batch.Sealed != nil {
		retired = append(retired, *x.Batch.Sealed)
	}
	if frame.checkpointDue {
		if frame.CheckpointEvery < 1 || frame.CheckpointCursor == "" || frame.CheckpointCursor != frame.Cursor || frame.BatchesSinceCheckpoint != 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared periodic checkpoint has an invalid cursor")
		}
		checkpoint := *frame
		checkpoint.State = maps.Clone(frame.State)
		checkpoint.DeadLetters = slices.Clone(frame.DeadLetters)
		checkpoint.CheckpointArtifact, checkpoint.Sealed = nil, nil
		checkpoint.checkpointDue = false
		checkpointRef, checkpointErr := store.Seal(x.ID, x.Version, platform.Raw(checkpoint), plan.rule.FrameBytes)
		if checkpointErr != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, checkpointErr.Error())
		}
		if frame.CheckpointArtifact != nil {
			retired = append(retired, *frame.CheckpointArtifact)
		}
		frame.CheckpointArtifact = &checkpointRef
		artifacts = append(artifacts, checkpointRef)
	}
	frame.checkpointDue = false
	ref, err := store.Seal(x.ID, x.Version, platform.Raw(frame), plan.rule.FrameBytes)
	if err != nil {
		for _, artifact := range artifacts {
			_ = store.Discard(artifact)
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	artifacts = append(artifacts, ref)
	frame.State, frame.DeadLetters, frame.Sealed = nil, nil, &ref
	return &PreparedBatch{owner: plan.owner, before: x, frame: frame, artifacts: artifacts, retired: retired, outputs: outputs, sources: sources, batch: batch, at: now,
		entry: plan.entry, windowInput: windowInput, resumeInput: resumeInput, operation: operationRequest}, nil
}

// BatchKey is stable across preparation races and process recovery. Content
// is separately bound in the original submission payload and accepted frame.
func BatchKey(instance string, version int, batch string) string {
	return fmt.Sprintf("flow-batch:%x", sha256.Sum256(platform.Raw([]any{instance, version, batch})))
}

// Decision wraps the original app only for this native delivery. It has no
// ledger, record store or executor of its own and is never registered as an app.
func (p *PreparedBatch) Decision() platform.ResultApp {
	return preparedDecision{Flows: p.owner, prepared: p}
}

func (p *PreparedBatch) Submission(tenant string) *pb.Submission {
	return &pb.Submission{TenantId: tenant, Authority: ID, PrincipalId: "app:" + ID,
		IdempotencyKey: BatchKey(p.before.ID, p.before.Version, p.batch.ID),
		Target:         &pb.EntityRef{Type: InstanceType, Id: p.before.ID}, Schema: &pb.SchemaRef{Name: SchemaFlowBatch, Version: 1},
		Payload: platform.Raw(map[string]string{"batch": p.batch.ID, "digest": p.frame.Fingerprint})}
}

func (p *PreparedBatch) Matches(x FlowInstance) bool {
	return x.ID == p.before.ID && x.Revision == p.before.Revision && x.Flow == p.before.Flow && x.Version == p.before.Version && x.Release == p.before.Release && x.Dependencies == p.before.Dependencies && x.OnBehalf == p.before.OnBehalf && x.State == p.before.State &&
		bytes.Equal(platform.Raw(x.Batch), platform.Raw(p.before.Batch)) && x.CleanupPending == p.before.CleanupPending && bytes.Equal(platform.Raw(x.ArtifactCleanup), platform.Raw(p.before.ArtifactCleanup))
}

func (p *PreparedBatch) Artifact() platform.FlowStateArtifact { return *p.frame.Sealed }

// Artifacts lists every outside-lane file prepared for this decision so the
// original host can discard all of them if its existing acceptance refuses.
func (p *PreparedBatch) Artifacts() []platform.FlowStateArtifact {
	return slices.Clone(p.artifacts)
}

// OperationRequest is present only when this accepted batch closes a ready
// event-time slide. Its bounded projection is transient and never instance data.
func (p *PreparedBatch) OperationRequest() (platform.OperationRequest, bool) {
	if p.operation == nil {
		return platform.OperationRequest{}, false
	}
	request := *p.operation
	request.Inputs = slices.Clone(p.operation.Inputs)
	request.Sources = slices.Clone(p.operation.Sources)
	if p.operation.Release != nil {
		release := *p.operation.Release
		request.Release = &release
	}
	return request, true
}

type preparedDecision struct {
	*Flows
	prepared *PreparedBatch
}

func (d preparedDecision) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p := d.prepared
	if !c.Automation || c.ID != "app:"+ID || c.Tenant != p.frame.Sealed.Tenant || sub.GetSchema().GetName() != SchemaFlowBatch || sub.GetTarget().GetId() != p.before.ID || !bytes.Equal(sub.GetPayload(), p.Submission(c.Tenant).GetPayload()) || !now.Equal(p.at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the host can accept a prepared Flow batch")
	}
	return d.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := d.session(c, now)
		x := ss.load(p.before.ID)
		if x == nil || !p.Matches(*x) || ended(x.State) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
		}
		if err := d.checkBinding(*x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		x.Batch, x.Outputs, x.Sources = p.frame, p.outputs, p.sources
		if len(p.retired) > 0 {
			if x.CleanupPending || len(x.ArtifactCleanup) != 0 {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A previous Flow artifact cleanup is still pending")
			}
			x.ArtifactCleanup = cloneArtifacts(p.retired)
			x.CleanupPending = true
			if err := validateArtifactCleanup(*x); err != nil {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
			}
		}
		if p.operation != nil {
			if len(p.windowInput) == 0 || p.entry == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute input is incomplete")
			}
			token := Token{ID: ss.tokenID(x), Step: p.entry, Waits: "ready", Input: slices.Clone(p.windowInput), Outputs: maps.Clone(x.Outputs)}
			x.Tokens = append(x.Tokens, token)
			ss.preparedOperations[token.ID] = *p.operation
			ss.advance(x)
			if len(ss.operations) != 1 || ss.operations[0].instance != x.ID || ss.operations[0].token != token.ID || !bytes.Equal(platform.Raw(ss.operations[0].request), platform.Raw(*p.operation)) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute entry did not produce its prepared request")
			}
			ss.token(x, token.ID).Input = slices.Clone(p.resumeInput)
		} else {
			ss.advance(x)
		}
		return ss.apply, nil
	})
}
