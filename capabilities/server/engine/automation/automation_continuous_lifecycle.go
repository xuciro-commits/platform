package automation

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

// validateArtifactCleanup protects live frame/checkpoint references from a
// stale or malformed cleanup intent. A successful successor can retire at
// most its former sealed frame and, on checkpoint rollover, its former
// checkpoint.
func validateArtifactCleanup(x FlowInstance) error {
	if x.CleanupPending != (len(x.ArtifactCleanup) > 0) || len(x.ArtifactCleanup) > 2 {
		return fmt.Errorf("the Flow artifact cleanup marker is inconsistent")
	}
	seen := map[string]bool{}
	tenant := ""
	if x.Batch != nil && x.Batch.Sealed != nil {
		tenant = x.Batch.Sealed.Tenant
	}
	for _, ref := range x.ArtifactCleanup {
		if ref.Tenant == "" || ref.Instance != x.ID || ref.Version != x.Version || ref.Ticket == "" || ref.Digest == "" || ref.Size < 1 {
			return fmt.Errorf("a retired Flow artifact has an invalid owner or bound")
		}
		if tenant == "" {
			tenant = ref.Tenant
		} else if tenant != ref.Tenant {
			return fmt.Errorf("retired Flow artifacts belong to different tenants")
		}
		if x.Batch != nil {
			if x.Batch.Sealed != nil && sameArtifact(*x.Batch.Sealed, ref) || x.Batch.CheckpointArtifact != nil && sameArtifact(*x.Batch.CheckpointArtifact, ref) {
				return fmt.Errorf("a live Flow artifact cannot be retired")
			}
		}
		key := string(platform.Raw(ref))
		if seen[key] {
			return fmt.Errorf("a Flow artifact is listed for retirement more than once")
		}
		seen[key] = true
	}
	return nil
}

func sameArtifact(a, b platform.FlowStateArtifact) bool {
	return bytes.Equal(platform.Raw(a), platform.Raw(b))
}

func cloneArtifacts(refs []platform.FlowStateArtifact) []platform.FlowStateArtifact {
	return slices.Clone(refs)
}

// ArtifactCleanupPreparation snapshots the original Flow owner before deleting
// bytes outside the tenant lock. Its acknowledgement then uses the same accepted
// Flow ledger/result path as every other durable instance change.
type ArtifactCleanupPreparation struct {
	owner  *Flows
	before FlowInstance
	refs   []platform.FlowStateArtifact
	digest string
	at     time.Time
	tenant string
}

func (f *Flows) PlanArtifactCleanup(x FlowInstance, at time.Time) (platform.FlowArtifactCleanupPreparation, *kernel.Error) {
	if !x.CleanupPending || len(x.ArtifactCleanup) == 0 || at.IsZero() {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow has no due artifact cleanup")
	}
	if err := validateArtifactCleanup(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	refs := cloneArtifacts(x.ArtifactCleanup)
	raw := platform.Raw([]any{x.ID, x.Version, refs})
	digest := fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
	return &ArtifactCleanupPreparation{owner: f, before: x, refs: refs, digest: digest, at: at, tenant: refs[0].Tenant}, nil
}

func (p *ArtifactCleanupPreparation) Instance() string {
	if p == nil {
		return ""
	}
	return p.before.ID
}

func (p *ArtifactCleanupPreparation) Artifacts() []platform.FlowStateArtifact {
	return cloneArtifacts(p.refs)
}

func (p *ArtifactCleanupPreparation) Matches(x FlowInstance) bool {
	return p != nil && x.ID == p.before.ID && x.Revision == p.before.Revision && x.Flow == p.before.Flow && x.Version == p.before.Version &&
		x.CleanupPending && bytes.Equal(platform.Raw(x.ArtifactCleanup), platform.Raw(p.refs))
}

func (p *ArtifactCleanupPreparation) Submission(tenant string) *pb.Submission {
	return &pb.Submission{TenantId: tenant, Authority: ID, PrincipalId: "app:" + ID,
		IdempotencyKey: fmt.Sprintf("flow-artifact-cleanup:%x", sha256.Sum256(platform.Raw([]any{p.before.ID, p.before.Version, p.refs}))),
		Target:         &pb.EntityRef{Type: InstanceType, Id: p.before.ID}, Schema: &pb.SchemaRef{Name: SchemaFlowArtifactCleanup, Version: 1},
		Payload: platform.Raw(map[string]string{"digest": p.digest})}
}

func (p *ArtifactCleanupPreparation) Decision() platform.App {
	return preparedArtifactCleanupDecision{Flows: p.owner, prepared: p}
}

type preparedArtifactCleanupDecision struct {
	*Flows
	prepared *ArtifactCleanupPreparation
}

func (d preparedArtifactCleanupDecision) ForkDecision() (platform.App, error) {
	fork, err := d.Flows.ForkDecision()
	if err != nil {
		return nil, err
	}
	return preparedArtifactCleanupDecision{Flows: fork.(*Flows), prepared: d.prepared}, nil
}

func (d preparedArtifactCleanupDecision) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p := d.prepared
	if p == nil || !c.Automation || c.ID != "app:"+ID || c.Tenant != p.tenant || sub.GetTenantId() != p.tenant || sub.GetAuthority() != ID || sub.GetPrincipalId() != c.ID ||
		sub.GetTarget().GetType() != InstanceType || sub.GetTarget().GetId() != p.before.ID || sub.GetSchema().GetName() != SchemaFlowArtifactCleanup || sub.GetSchema().GetVersion() != 1 ||
		!bytes.Equal(sub.GetPayload(), p.Submission(p.tenant).GetPayload()) || sub.GetIdempotencyKey() != p.Submission(p.tenant).GetIdempotencyKey() || !now.Equal(p.at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the host can acknowledge retired Flow artifacts")
	}
	return d.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := d.session(c, now)
		x := ss.load(p.before.ID)
		if x == nil || !p.Matches(*x) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its artifacts were being retired")
		}
		x.ArtifactCleanup, x.CleanupPending = nil, false
		ss.trace(x, "", "artifact cleanup", fmt.Sprintf("retired %d superseded frame/checkpoint artifacts", len(p.refs)), "host")
		return ss.apply, nil
	})
}

type deadLetterReplayPreparation struct {
	batch *BatchPreparation
	asset platform.FlowDeadLetterAsset
}

func (f *Flows) PlanDeadLetterReplay(x FlowInstance, c platform.Caller, asset platform.FlowDeadLetterAsset) (platform.FlowDeadLetterReplayPreparation, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if asset.Instance != x.ID || asset.Flow != x.Flow || asset.Version != x.Version || asset.OnBehalf != x.OnBehalf || asset.Replayed || asset.Archived || asset.Withheld || asset.Action != "" || !json.Valid([]byte(asset.Letter.Value)) || d == nil || d.Continuous == nil || !d.Continuous.DeadLetterRetry || d.Continuous.Window == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained signal dead letter is unavailable for replay")
	}
	plan, err := f.PlanBatch(x, c)
	if err != nil {
		return nil, err
	}
	batch := plan.(*BatchPreparation)
	window := *batch.rule.Window
	window.LateEvents = "accept" // explicit replay reopens the retained window; ordinary intake keeps its policy
	batch.rule.Window = &window
	return &deadLetterReplayPreparation{batch: batch, asset: asset}, nil
}

func (p *deadLetterReplayPreparation) Prepare(sub *pb.Submission, at time.Time, store platform.FlowFrameStore) (platform.FlowPreparedBatch, *kernel.Error) {
	if !p.batch.owner.deadLetterReplayAllowed(p.batch.caller, p.asset, at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained signal dead letter is unavailable for replay")
	}
	if sub.GetSchema().GetName() != SchemaFlowReplay || sub.GetTarget().GetType() != "flow.dead-letter" || sub.GetTarget().GetId() != p.asset.ID || sub.GetPrincipalId() != p.asset.OnBehalf {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "A signal replay requires its original member and dead-letter asset")
	}
	x := p.batch.before
	batch := Batch{ID: "replay:" + p.asset.ID, Predecessor: x.Batch.Cursor, Source: x.Batch.Source, Signals: []Signal{{Key: p.asset.Letter.Key, Partition: p.asset.Letter.Partition, At: p.asset.Letter.At, Value: json.RawMessage(p.asset.Letter.Value)}}}
	prepared, err := p.batch.Prepare(batch, at, store)
	if err != nil {
		return nil, err
	}
	result := prepared.(*PreparedBatch)
	result.replayAsset = &p.asset
	result.replaySubmission = proto.Clone(sub).(*pb.Submission)
	return result, nil
}

// A rejected business action is retained in the original Flow ledger. The
// captured key is its logical request, independent of retry or replay attempts.
func (ss *session) deadLetterAct(x *FlowInstance, token *Token, step *platform.Step, target string, payload json.RawMessage, reason string) {
	d := ss.def(x)
	if d == nil || d.Continuous == nil || step.Act == nil {
		return
	}
	limit := stepAttempts
	if step.Retry != nil && step.Retry.Attempts > 0 {
		limit = step.Retry.Attempts
	}
	if !step.NoRetry && token.Attempts+1 < limit {
		return
	}
	key := fmt.Sprintf("flow:%s:%s:%d", x.ID, token.Step, x.Seq)
	batch := ""
	if x.Batch != nil {
		batch = x.Batch.Cursor
	}
	asset := platform.FlowDeadLetterAsset{Record: platform.Record{ID: fmt.Sprintf("flow-dead-letter:%x", sha256.Sum256([]byte(key)))}, App: d.app,
		Instance: x.ID, Flow: x.Flow, Version: x.Version, OnBehalf: x.OnBehalf, Sources: slices.Clone(x.Sources),
		Letter: platform.FlowDeadLetter{Batch: batch, Node: step.Name, Key: target, RecordedAt: ss.now, Reason: reason, Value: string(payload)},
		Action: step.Act.Action, Protocol: step.Act.Protocol, Target: target, Payload: slices.Clone(payload), Key: key, AsMember: step.Act.AsMember}
	ss.assets = append(ss.assets, asset)
	// The rejected request owns this logical key just as an accepted act does.
	// Reserve its sequence before the path continues; otherwise the next alert
	// would reuse the same key with different content, including after replay.
	x.Seq++
}

func (f *Flows) replayDeadLetter(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return f.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		asset, ok := platform.Get[platform.FlowDeadLetterAsset](c, sub.GetTarget().GetId())
		if !ok {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "The Flow dead letter is unavailable")
		}
		if asset.Withheld || asset.Archived || asset.Replayed || asset.Action == "" || asset.Key == "" || !json.Valid(asset.Payload) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This dead letter has no replayable rejected action")
		}
		ss := f.session(f.host.Automation(c, ID), now)
		x := ss.load(asset.Instance)
		if x == nil || ended(x.State) || x.Flow != asset.Flow || x.Version != asset.Version || x.OnBehalf != asset.OnBehalf {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original Flow version must be available and running to replay its dead letter")
		}
		if err := f.checkBinding(*x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		d := ss.def(x)
		if d == nil || d.app != asset.App || d.Continuous == nil || !d.Continuous.DeadLetterRetry {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained dead-letter definition is unavailable")
		}
		if !f.deadLetterReplayAllowed(ss.c, asset, now) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained dead-letter definition is unavailable")
		}
		member := ""
		if asset.AsMember {
			member = asset.OnBehalf
		}
		if _, err := ss.act(x, asset.App, asset.Protocol, asset.Action, asset.Target, asset.Payload, asset.Key, member); err != nil {
			return nil, err
		}
		asset.Replayed = true
		ss.assets = append(ss.assets, asset)
		// Historical dead letters could retain the instance's next sequence.
		// Keep their exact replay key, then reserve it for future native acts.
		if asset.Key == fmt.Sprintf("flow:%s:%s:%d", x.ID, asset.Letter.Node, x.Seq) {
			x.Seq++
		}
		ss.trace(x, asset.Letter.Node, "dead letter replayed", asset.ID, c.ID)
		if err := ss.retainDeadLetters(); err != nil {
			return nil, err
		}
		return ss.apply, nil
	})
}

// PreparedBatch is an opaque, host-prepared update. There is no JSON input
// which can construct one: a native source prepares outside the tenant lock,
// then the original accepted-decision pipeline validates its predecessor.
type PreparedBatch struct {
	owner            *Flows
	before           FlowInstance
	frame            *BatchFrame
	artifacts        []platform.FlowStateArtifact
	retired          []platform.FlowStateArtifact
	outputs          map[string]json.RawMessage
	sources          []string
	batch            Batch
	at               time.Time
	entry            string
	windowInput      json.RawMessage
	resumeInput      json.RawMessage
	operation        *platform.OperationRequest
	assets           []any
	replayAsset      *platform.FlowDeadLetterAsset
	replaySubmission *pb.Submission
}

// ExpandFrame reads a frozen original frame on the I/O lane. Its summary,
// owner and native version must agree with the accepted instance projection.
func (f *Flows) ExpandFrame(x FlowInstance, store platform.FlowFrameStore) (*BatchFrame, error) {
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
func (f *Flows) PlanBatch(x FlowInstance, c platform.Caller) (platform.FlowBatchPreparation, *kernel.Error) {
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
		intake.Fields = slices.Clone(intake.Fields)
		rule.Intake = &intake
	}
	plan := &BatchPreparation{owner: f, before: x, rule: rule, caller: f.host.Automation(c, d.app)}
	if rule.Entry != "" {
		dstep := d.steps[rule.Entry]
		if dstep == nil || dstep.Operation == nil && (rule.Aggregate == nil || dstep.Evaluate == nil && dstep.Loop == nil) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute entry is unavailable")
		}
		plan.entry = rule.Entry
		if dstep.Operation != nil {
			operation := *dstep.Operation
			plan.operation = &operation
		}
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
	intake.Fields = slices.Clone(intake.Fields)
	return plan.rule.Source, &intake, plan.rule.Batch
}

// WindowTick creates an idempotent timer batch when the event-time grace has
// closed a new slide. Its source checkpoint is byte-for-byte unchanged.
func (plan *BatchPreparation) WindowTick(now time.Time, store platform.FlowFrameStore) (Batch, bool, *kernel.Error) {
	if plan == nil || plan.rule.Intake == nil || plan.rule.Window == nil || plan.before.Batch == nil || plan.before.Batch.Source == nil || now.IsZero() {
		return Batch{}, false, nil
	}
	frame, err := plan.owner.ExpandFrame(plan.before, store)
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
func (f *Flows) PlanComputeRetry(x FlowInstance, c platform.Caller, now time.Time) (platform.FlowComputeRetryPreparation, *kernel.Error) {
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
func (plan *ComputeRetryPreparation) Prepare(now time.Time, store platform.FlowFrameStore) (platform.FlowPreparedComputeRetry, *kernel.Error) {
	x := plan.before
	frame, err := plan.owner.ExpandFrame(x, store)
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

func (p *PreparedComputeRetry) Decision() platform.App {
	return preparedComputeRetryDecision{Flows: p.owner, prepared: p}
}

type preparedComputeRetryDecision struct {
	*Flows
	prepared *PreparedComputeRetry
}

func (d preparedComputeRetryDecision) ForkDecision() (platform.App, error) {
	fork, err := d.Flows.ForkDecision()
	if err != nil {
		return nil, err
	}
	return preparedComputeRetryDecision{Flows: fork.(*Flows), prepared: d.prepared}, nil
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
func (plan *BatchPreparation) Prepare(batch Batch, now time.Time, store platform.FlowFrameStore) (platform.FlowPreparedBatch, *kernel.Error) {
	var retry *platform.StreamPreparationRetry
	if plan.rule.Window != nil {
		retry = plan.rule.Window.Retry
	}
	if err := retry.Check(); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	var prepared platform.FlowPreparedBatch
	var refusal *kernel.Error
	for attempt := 0; attempt < retry.Tries(); attempt++ {
		if attempt > 0 && retry.BackoffMS > 0 {
			time.Sleep(time.Duration(retry.BackoffMS) * time.Millisecond)
		}
		io := &preparationFrameStore{FlowFrameStore: store}
		prepared, refusal = plan.prepare(batch, now, io)
		if refusal == nil || refusal.Code != pb.ErrorCode_ERROR_CODE_CONFLICT || !io.failed {
			return prepared, refusal
		}
	}
	return nil, refusal
}

// Only failed outside I/O can benefit from repeating the same frozen plan.
// Budget, malformed history and kernel refusals retain their first answer.
type preparationFrameStore struct {
	platform.FlowFrameStore
	failed bool
}

func (s *preparationFrameStore) Read(ref platform.FlowStateArtifact) ([]byte, error) {
	raw, err := s.FlowFrameStore.Read(ref)
	s.failed = s.failed || err != nil
	return raw, err
}

func (s *preparationFrameStore) Seal(instance string, version int, raw []byte, budget int) (platform.FlowStateArtifact, error) {
	if budget > 0 && len(raw) > budget {
		return platform.FlowStateArtifact{}, fmt.Errorf("the Flow frame exceeds its sealed JSON budget")
	}
	ref, err := s.FlowFrameStore.Seal(instance, version, raw, budget)
	if err != nil {
		s.failed = true
		// A failed Put may have persisted its newly generated ticket. These
		// bytes cannot have been accepted and must not accumulate per retry.
		_ = s.FlowFrameStore.Discard(ref)
	}
	return ref, err
}

func (plan *BatchPreparation) prepare(batch Batch, now time.Time, store platform.FlowFrameStore) (platform.FlowPreparedBatch, *kernel.Error) {
	x := plan.before
	if err := validateArtifactCleanup(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	if x.CleanupPending {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Superseded Flow artifacts must be retired before another source batch is accepted")
	}
	frame, err := plan.owner.ExpandFrame(x, store)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	priorCursor := frame.Cursor
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
	projectOperatorOutputs(outputs, x.ID, frame)
	if len(platform.Raw(outputs)) > 60<<10 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch outputs exceed the Flow output budget")
	}
	// Batch-local operator outputs are empty between slide boundaries, not a
	// new completed window. Decode once for both publication and continuation;
	// an intake-only batch must retain the last application output unchanged.
	var state WindowState
	if !batch.Bootstrap && (plan.entry != "" || plan.rule.Output != nil) {
		if plan.rule.Window == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared continuous output has no event-time window")
		}
		if raw := frame.State[plan.rule.Window.Node]; len(raw) == 0 || json.Unmarshal(raw, &state) != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared event-time window cannot be decoded")
		}
	}
	var assets []any
	for _, letter := range frame.DeadLetters {
		if x.Batch != nil && letter.Seq <= x.Batch.DeadLetterSequence {
			continue
		}
		assetID := fmt.Sprintf("flow-dead-letter:%x", sha256.Sum256(platform.Raw([]any{x.ID, x.Version, letter.Seq})))
		assets = append(assets, platform.FlowDeadLetterAsset{Record: platform.Record{ID: assetID}, App: plan.caller.App, Instance: x.ID, Flow: x.Flow, Version: x.Version,
			OnBehalf: x.OnBehalf, Sources: slices.Clone(x.Sources), Letter: letter})
	}
	if output := plan.rule.Output; output != nil && !batch.Bootstrap && state.Ready {
		value := frame.OperatorOutputs[output.Name]
		if len(value) > 48<<10 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The read-only Flow output exceeds its asset budget")
		}
		if len(value) > 0 {
			assetID := fmt.Sprintf("flow-output:%x", sha256.Sum256(platform.Raw([]any{x.ID, output.Name})))
			assets = append(assets, platform.FlowOutput{Record: platform.Record{ID: assetID}, App: plan.caller.App, Instance: x.ID, Flow: x.Flow, Version: x.Version,
				Name: output.Name, Batch: frame.Cursor, OnBehalf: x.OnBehalf, Sources: slices.Clone(x.Sources), Value: slices.Clone(value)})
		}
	}
	sources := append([]string(nil), x.Sources...)
	if batch.Source != nil {
		for _, source := range batch.Source.Sources {
			if !slices.Contains(sources, source) {
				sources = append(sources, source)
			}
		}
	}
	for i, asset := range assets {
		switch value := asset.(type) {
		case platform.FlowOutput:
			value.Sources = slices.Clone(sources)
			assets[i] = value
		case platform.FlowDeadLetterAsset:
			value.Sources = slices.Clone(sources)
			assets[i] = value
		}
	}
	var windowInput, resumeInput json.RawMessage
	var operationRequest *platform.OperationRequest
	if plan.entry != "" && !batch.Bootstrap && state.Ready {
		w := plan.rule.Window
		previousOutputs := maps.Clone(x.Outputs)
		if plan.operation == nil {
			previousOutputs = maps.Clone(outputs)
		}
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
		if plan.operation != nil {
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
		} else {
			var stats []AggregateRecord
			var alerts []Alert
			json.Unmarshal(frame.OperatorOutputs[plan.rule.Aggregate.Node], &stats)
			if plan.rule.Threshold != nil {
				json.Unmarshal(frame.OperatorOutputs[plan.rule.Threshold.Node], &alerts)
			}
			triggered, normal := []Alert{}, []Alert{}
			for _, alert := range alerts {
				if alert.State == "triggered" {
					triggered = append(triggered, alert)
				} else {
					normal = append(normal, alert)
				}
			}
			windowInput = platform.Raw(map[string]any{"outputs": previousOutputs, "signals": projection.Signals, "stats": aggregateOutput(stats), "alerts": alertOutput(alerts), "triggered": alertOutput(triggered), "normal": alertOutput(normal)})
			resumeInput = slices.Clone(windowInput)
		}
	}
	var artifacts, retired []platform.FlowStateArtifact
	if x.Batch != nil && x.Batch.Sealed != nil {
		retired = append(retired, *x.Batch.Sealed)
	}
	if frame.Cursor != priorCursor && frame.CheckpointEvery > 0 && frame.CheckpointCursor == frame.Cursor && !batch.Bootstrap && !batch.Tick {
		if frame.CheckpointEvery < 1 || frame.CheckpointCursor == "" || frame.CheckpointCursor != frame.Cursor || frame.BatchesSinceCheckpoint != 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared periodic checkpoint has an invalid cursor")
		}
		checkpoint := *frame
		checkpoint.State = maps.Clone(frame.State)
		checkpoint.DeadLetters = slices.Clone(frame.DeadLetters)
		checkpoint.CheckpointArtifact, checkpoint.Sealed = nil, nil
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
		entry: plan.entry, windowInput: windowInput, resumeInput: resumeInput, operation: operationRequest, assets: assets}, nil
}

// projectOperatorOutputs keeps small historical projections inline. A wide
// native graph still keeps every original output in its sealed frame, but does
// not copy aggregate/alerts/variable values into each token's bounded output
// map. The reference is inspectable via the same authorized frame read; native
// actions consume the actual prepared items, never these reference markers.
func projectOperatorOutputs(outputs map[string]json.RawMessage, instance string, frame *BatchFrame) {
	wide := false
	for node, raw := range frame.OperatorOutputs {
		outputs[node] = slices.Clone(raw)
		wide = wide || len(raw) > 48<<10
	}
	if !wide && len(platform.Raw(outputs)) <= 60<<10 {
		return
	}
	for node, raw := range frame.OperatorOutputs {
		outputs[node] = platform.Raw(BatchStateReference{Instance: instance, Node: node, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(raw)), Size: len(raw), Output: true})
	}
}

// Decision wraps the original app only for this native delivery. It has no
// ledger, record store or executor of its own and is never registered as an app.
func (p *PreparedBatch) Decision() platform.App {
	return preparedDecision{Flows: p.owner, prepared: p}
}

func (p *PreparedBatch) Submission(tenant string) *pb.Submission {
	if p.replaySubmission != nil {
		return proto.Clone(p.replaySubmission).(*pb.Submission)
	}
	return &pb.Submission{TenantId: tenant, Authority: ID, PrincipalId: "app:" + ID,
		IdempotencyKey: platform.FlowBatchKey(p.before.ID, p.before.Version, p.batch.ID),
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

func (d preparedDecision) ForkDecision() (platform.App, error) {
	fork, err := d.Flows.ForkDecision()
	if err != nil {
		return nil, err
	}
	return preparedDecision{Flows: fork.(*Flows), prepared: d.prepared}, nil
}

func (d preparedDecision) Submit(c platform.Caller, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	p := d.prepared
	validPrincipal := c.Automation && c.ID == "app:"+ID
	if p.replaySubmission != nil {
		validPrincipal = !c.Automation && c.ID == p.before.OnBehalf && proto.Equal(sub, p.replaySubmission)
	}
	if !validPrincipal || c.Tenant != p.frame.Sealed.Tenant || !proto.Equal(sub, p.Submission(c.Tenant)) || !now.Equal(p.at) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the host can accept a prepared Flow batch")
	}
	return d.ledger.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		ss := d.session(d.host.Automation(c, ID), now)
		if p.replayAsset != nil {
			asset, ok := platform.Get[platform.FlowDeadLetterAsset](c, p.replayAsset.ID)
			if !ok || !bytes.Equal(platform.Raw(asset), platform.Raw(p.replayAsset)) || !d.deadLetterReplayAllowed(c, asset, now) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original dead letter changed while its replay was prepared")
			}
			asset.Replayed = true
			ss.assets = append(ss.assets, asset)
		}
		x := ss.load(p.before.ID)
		if x == nil || !p.Matches(*x) || ended(x.State) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The Flow changed while its source batch was prepared")
		}
		if err := d.checkBinding(*x); err != nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		x.Batch, x.Outputs, x.Sources = p.frame, p.outputs, p.sources
		ss.assets = append(ss.assets, p.assets...)
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
		if len(p.windowInput) > 0 {
			if len(p.windowInput) == 0 || p.entry == "" {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The prepared window Compute input is incomplete")
			}
			token := Token{ID: ss.tokenID(x), Step: p.entry, Waits: "ready", Input: slices.Clone(p.windowInput), Outputs: maps.Clone(x.Outputs)}
			x.Tokens = append(x.Tokens, token)
			if p.operation != nil {
				ss.preparedOperations[token.ID] = *p.operation
			}
			ss.advance(x)
			if p.operation != nil && (len(ss.operations) != 1 || ss.operations[0].instance != x.ID || ss.operations[0].token != token.ID || !bytes.Equal(platform.Raw(ss.operations[0].request), platform.Raw(*p.operation))) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The retained Compute entry did not produce its prepared request")
			}
			ss.token(x, token.ID).Input = slices.Clone(p.resumeInput)
		} else {
			ss.advance(x)
		}
		if err := ss.retainDeadLetters(); err != nil {
			return nil, err
		}
		return ss.apply, nil
	})
}

// Archived rows keep their original content and history. A retention decision
// writes at most 64 KiB of additional images; subsequent timer passes continue
// any backlog through the same original Flow ledger.
const deadLetterArchiveBytes = 64 << 10

const deadLetterArchivePage = 64

func deadLetterDomain(x FlowInstance) []any {
	return []any{[]any{"instance", "=", x.ID}, []any{"flow", "=", x.Flow}, []any{"version", "=", x.Version}}
}

func deadLetterBorn(asset platform.FlowDeadLetterAsset) time.Time {
	if !asset.Created.At.IsZero() {
		return asset.Created.At
	}
	return asset.Letter.RecordedAt
}

func (f *Flows) deadLetterReplayAllowed(c platform.Caller, asset platform.FlowDeadLetterAsset, now time.Time) bool {
	d := f.def(asset.Flow, asset.Version)
	if d == nil || d.Continuous == nil || asset.Archived || asset.Replayed {
		return false
	}
	rule := d.Continuous
	if rule.DeadLetterTTLMs > 0 {
		born := deadLetterBorn(asset)
		if born.IsZero() || !born.Add(time.Duration(rule.DeadLetterTTLMs)*time.Millisecond).After(now) {
			return false
		}
	}
	if rule.DeadLetterMaxRecords > 0 {
		x := FlowInstance{Record: platform.Record{ID: asset.Instance}, Flow: asset.Flow, Version: asset.Version}
		rows, total, err := platform.Find[platform.FlowDeadLetterAsset](f.host.Automation(c, ID), platform.Query{Domain: platform.Raw(deadLetterDomain(x)), Sort: []string{"-created", "-id"}, Offset: rule.DeadLetterMaxRecords - 1, Limit: 1})
		if err != nil || total >= rule.DeadLetterMaxRecords && len(rows) != 1 {
			return false
		}
		if len(rows) == 1 {
			boundary := rows[0]
			if born, cutoff := deadLetterBorn(asset), deadLetterBorn(boundary); born.Before(cutoff) || born.Equal(cutoff) && asset.ID < boundary.ID {
				return false
			}
		}
	}
	return true
}

func (f *Flows) deadLetterArchives(c platform.Caller, x FlowInstance, now time.Time, pending []platform.FlowDeadLetterAsset) ([]platform.FlowDeadLetterAsset, []platform.FlowDeadLetterAsset, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if d == nil || d.Continuous == nil {
		return nil, pending, nil
	}
	rule, domain := d.Continuous, deadLetterDomain(x)
	if rule.DeadLetterTTLMs == 0 && rule.DeadLetterMaxRecords == 0 {
		return nil, pending, nil
	}
	c = f.host.Automation(c, ID)
	var candidates []platform.FlowDeadLetterAsset
	if rule.DeadLetterTTLMs > 0 {
		expired := append(slices.Clone(domain), []any{"created", "<=", now.Add(-time.Duration(rule.DeadLetterTTLMs) * time.Millisecond).Format(time.RFC3339Nano)})
		rows, _, err := platform.Find[platform.FlowDeadLetterAsset](c, platform.Query{Domain: platform.Raw(expired), Sort: []string{"created", "id"}, Limit: deadLetterArchivePage})
		if err != nil {
			return nil, pending, err
		}
		candidates = append(candidates, rows...)
	}
	fresh := 0
	for i := range pending {
		if rule.DeadLetterTTLMs > 0 && !deadLetterBorn(pending[i]).Add(time.Duration(rule.DeadLetterTTLMs)*time.Millisecond).After(now) {
			pending[i].Archived = true
		}
		if _, exists := platform.Get[platform.FlowDeadLetterAsset](c, pending[i].ID); !exists && !pending[i].Archived {
			fresh++
		}
	}
	if rule.DeadLetterMaxRecords > 0 {
		rows, total, err := platform.Find[platform.FlowDeadLetterAsset](c, platform.Query{Domain: platform.Raw(domain), Sort: []string{"created", "id"}, Limit: deadLetterArchivePage})
		if err != nil {
			return nil, pending, err
		}
		overflow := max(0, total+fresh-rule.DeadLetterMaxRecords)
		candidates = append(candidates, rows[:min(overflow, len(rows))]...)
		// A single action loop may create more new failures than its whole
		// retained bound. Archive those extra pending rows in this same result.
		extra := max(0, fresh-rule.DeadLetterMaxRecords)
		slices.SortFunc(pending, func(a, b platform.FlowDeadLetterAsset) int { return cmp.Compare(a.ID, b.ID) })
		for i := range pending {
			if extra == 0 {
				break
			}
			if _, exists := platform.Get[platform.FlowDeadLetterAsset](c, pending[i].ID); !exists && !pending[i].Archived {
				pending[i].Archived = true
				extra--
			}
		}
	}
	seen, used := map[string]bool{}, 0
	var archived []platform.FlowDeadLetterAsset
	for _, asset := range candidates {
		if seen[asset.ID] {
			continue
		}
		seen[asset.ID] = true
		asset.Archived = true
		size := len(platform.Raw(asset))
		if used+size > deadLetterArchiveBytes && len(archived) > 0 {
			break
		}
		// The first historical image may exceed the paging target. The original
		// accepted-result bound decides whether that record can commit.
		used += size
		archived = append(archived, asset)
	}
	return archived, pending, nil
}

func (ss *session) retainDeadLetters() *kernel.Error {
	groups := map[string]*FlowInstance{}
	for id, x := range ss.changed {
		groups[id] = x
	}
	pending := map[string][]platform.FlowDeadLetterAsset{}
	var other []any
	for _, value := range ss.assets {
		if asset, ok := value.(platform.FlowDeadLetterAsset); ok {
			pending[asset.Instance] = append(pending[asset.Instance], asset)
			if groups[asset.Instance] == nil {
				groups[asset.Instance] = ss.load(asset.Instance)
			}
		} else {
			other = append(other, value)
		}
	}
	archiveBytes := 0
	for _, id := range slices.Sorted(maps.Keys(groups)) {
		x := groups[id]
		if x == nil {
			continue
		}
		archived, next, err := ss.f.deadLetterArchives(ss.c, *x, ss.now, pending[id])
		if err != nil {
			return err
		}
		byID := map[string]platform.FlowDeadLetterAsset{}
		for _, asset := range next {
			byID[asset.ID] = asset
		}
		for _, asset := range archived {
			if archiveBytes >= deadLetterArchiveBytes {
				break
			}
			if updated, ok := byID[asset.ID]; ok {
				updated.Archived = true
				asset = updated
			}
			byID[asset.ID] = asset
			archiveBytes += len(platform.Raw(asset))
		}
		for _, key := range slices.Sorted(maps.Keys(byID)) {
			other = append(other, byID[key])
		}
	}
	ss.assets = other
	return nil
}

func (f *Flows) retainExpiredDeadLetters(c platform.Caller, now time.Time) (bool, *kernel.Error) {
	for offset := 0; ; {
		rows, total, err := platform.Find[FlowInstance](c, platform.Query{Archived: true, Sort: []string{"id"}, Limit: 128, Offset: offset})
		if err != nil {
			return false, err
		}
		for _, x := range rows {
			archived, _, err := f.deadLetterArchives(c, x, now, nil)
			if err != nil {
				return false, err
			}
			if len(archived) == 0 {
				continue
			}
			if _, err := f.decide(c, SchemaFlowStep, x.ID, fmt.Sprintf("%s:%d", x.ID, x.Revision+1), now, func(ss *session) *kernel.Error { ss.load(x.ID); return nil }); err != nil {
				return false, err
			}
			// The timer itself is one accepted host result. Do not accumulate
			// one archive budget per instance in that enclosing result.
			return true, nil
		}
		offset += len(rows)
		if offset >= total {
			return false, nil
		}
		if len(rows) == 0 {
			return false, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT, Message: "The retained Flow inventory is incomplete"}
		}
	}
}
