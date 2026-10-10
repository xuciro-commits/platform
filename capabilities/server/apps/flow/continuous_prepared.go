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
	owner   *Flows
	before  FlowInstance
	frame   *BatchFrame
	outputs map[string]json.RawMessage
	sources []string
	batch   Batch
	at      time.Time
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
	if err := decoder.Decode(new(any)); err != io.EOF || frame.Sealed != nil || frame.Cursor != x.Batch.Cursor || frame.Fingerprint != x.Batch.Fingerprint || !frame.Watermark.Equal(x.Batch.Watermark) || frame.Consumed != x.Batch.Consumed || frame.Rejected != x.Batch.Rejected || stateSize(frame.State) != x.Batch.StateBytes || !bytes.Equal(platform.Raw(frame.Source), platform.Raw(x.Batch.Source)) {
		return nil, fmt.Errorf("the sealed frame differs from its accepted summary")
	}
	return &frame, nil
}

type BatchPreparation struct {
	owner  *Flows
	before FlowInstance
	rule   platform.Continuous
}

// PlanBatch captures the retained definition and binding under the tenant
// lock. The later I/O lane never reads the mutable definition registry.
func (f *Flows) PlanBatch(x FlowInstance) (*BatchPreparation, *kernel.Error) {
	d := f.def(x.Flow, x.Version)
	if d == nil || d.Continuous == nil || ended(x.State) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The continuous instance is unavailable")
	}
	if err := f.checkBinding(x); err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
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
	return &BatchPreparation{owner: f, before: x, rule: rule}, nil
}

func (plan *BatchPreparation) Intake() (string, *platform.StreamIntake, int) {
	if plan.rule.Intake == nil {
		return plan.rule.Source, nil, plan.rule.Batch
	}
	intake := *plan.rule.Intake
	intake.Partition = slices.Clone(intake.Partition)
	return plan.rule.Source, &intake, plan.rule.Batch
}

// Prepare uses the original folding rule outside the tenant lock. No file
// I/O or mutable registry read occurs in the accepted ledger callback.
func (plan *BatchPreparation) Prepare(batch Batch, now time.Time, store host.FlowFrameStore) (*PreparedBatch, *kernel.Error) {
	x := plan.before
	frame, err := ExpandFrame(x, store)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	_, _, refusal := foldBatch(frame, plan.rule, batch, now)
	if refusal != nil {
		return nil, refusal
	}
	// The declared window→aggregate→threshold operators advance in the same
	// prepared decision as the fold (ADR-0047 §13.1): the sealed frame carries
	// their state, the accepted result carries their outputs, and a refusal
	// discards the preparation, so no artifact survives a batch whose
	// operators could not commit.
	stats, alerts, refusal := foldOperators(frame, plan.rule, batch.ID, now)
	if refusal != nil {
		return nil, refusal
	}
	queueEffects(frame, plan.rule, x.ID, batch.ID, alerts, now)
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
	if plan.rule.Aggregate != nil {
		outputs["stats"] = platform.Raw(stats)
	}
	if plan.rule.Threshold != nil {
		outputs["alerts"] = platform.Raw(alerts)
	}
	if len(platform.Raw(outputs)) > 60<<10 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The batch outputs exceed the Flow output budget")
	}
	ref, err := store.Seal(x.ID, x.Version, platform.Raw(frame), plan.rule.FrameBytes)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
	}
	frame.State, frame.DeadLetters, frame.Sealed = nil, nil, &ref
	sources := append([]string(nil), x.Sources...)
	if batch.Source != nil {
		for _, source := range batch.Source.Sources {
			if !slices.Contains(sources, source) {
				sources = append(sources, source)
			}
		}
	}
	return &PreparedBatch{owner: plan.owner, before: x, frame: frame, outputs: outputs, sources: sources, batch: batch, at: now}, nil
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
	return x.ID == p.before.ID && x.Revision == p.before.Revision && x.Flow == p.before.Flow && x.Version == p.before.Version && x.Release == p.before.Release && x.Dependencies == p.before.Dependencies && x.OnBehalf == p.before.OnBehalf && x.State == p.before.State && bytes.Equal(platform.Raw(x.Batch), platform.Raw(p.before.Batch))
}

func (p *PreparedBatch) Artifact() platform.FlowStateArtifact { return *p.frame.Sealed }

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
		ss.advance(x)
		return ss.apply, nil
	})
}
