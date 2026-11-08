package automation

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
	"time"
)

type ComputeRetryPreparation struct {
	owner     *Flows
	before    FlowInstance
	rule      platform.Continuous
	entry     string
	token     int
	operation *platform.OperationStep
	caller    platform.Caller
}

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
	return &PreparedComputeRetry{before: x, entry: plan.entry, token: plan.token, request: request, at: now, digest: fmt.Sprintf("sha256:%x", digest)}, nil
}

type PreparedComputeRetry struct {
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

func (p *PreparedComputeRetry) Decision(owner platform.App) platform.App {
	return preparedComputeRetryDecision{Flows: owner.(*Flows), prepared: p}
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
