package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/construction"
	"platformserver/platform"
)

func (d *stagedDecision) ConfirmationTransport() platform.ConfirmationTransport {
	return hostView{t: d.tenant, app: d.tenant.app(construction.ID), decision: d}
}

func (h hostView) FreezeConfirmation(c platform.Caller, subject string, spec platform.Confirmation, at time.Time) (string, []platform.ReleaseAsset, error) {
	if h.decision == nil || !c.Staging() || c.App != construction.ID {
		return "", nil, fmt.Errorf("Confirmation requires its original private authority")
	}
	assets, err := h.t.releaseAssetsLocked(nil, false)
	if err != nil {
		return "", nil, err
	}
	roots := []platform.AssetRef{{App: construction.ID, Kind: platform.AssetObject, Name: subject}}
	for _, p := range spec.Parties {
		if p.Protocol != "" {
			original, err := h.FreezeConfirmationProtocol(p)
			if err != nil {
				return "", nil, err
			}
			for _, schema := range []string{original.Action, original.CompensateAction} {
				if schema != "" {
					roots = append(roots, platform.AssetRef{App: original.Provider, Kind: platform.AssetAction, Name: schema})
				}
			}
		}
		for _, id := range []string{p.Writeback, p.CompensateWriteback} {
			if id == "" {
				continue
			}
			w, ok := platform.Get[construction.Writeback](c, id)
			if !ok {
				return "", nil, fmt.Errorf("Confirmation writeback is unavailable")
			}
			roots = append(roots, platform.AssetRef{App: construction.ID, Kind: platform.AssetWriteback, Name: w.Name})
		}
		if p.Reconcile != nil {
			s, ok := platform.Get[construction.Source](c, p.Reconcile.Source)
			if !ok {
				return "", nil, fmt.Errorf("Reconciliation source is unavailable")
			}
			roots = append(roots, platform.AssetRef{App: construction.ID, Kind: platform.AssetSource, Name: s.Name})
		}
	}
	closed, err := platform.Candidate(roots, assets)
	if err != nil {
		return "", nil, err
	}
	if len(closed.Bytes) > 192<<10 {
		return "", nil, fmt.Errorf("Confirmation definition exceeds its retained execution budget")
	}
	return h.t.releases.active, closed.Assets, nil
}

func (h hostView) FreezeConfirmationProtocol(p platform.ConfirmationParty) (*platform.ConfirmationProtocol, error) {
	b, ok := h.t.resolve(p.Protocol)
	if !ok || b.provision.Actions[p.Action] == "" || p.CompensateAction != "" && b.provision.Actions[p.CompensateAction] == "" {
		return nil, fmt.Errorf("The confirmation protocol has no declared provider mapping")
	}
	return &platform.ConfirmationProtocol{Provider: b.provider.Manifest().ID, Version: b.provider.Manifest().Version, Action: b.provision.Actions[p.Action], CompensateAction: b.provision.Actions[p.CompensateAction]}, nil
}

func (h hostView) QueueConfirmation(c platform.Caller, r *pb.ChangeRecord, run platform.ConfirmationRun, index int, compensate bool, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != construction.ID || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation transport requires its original owner")
	}
	p := run.Parties[index]
	declaration := run.Definition.Parties[index]
	intent, body := p.Intent, p.Request
	if compensate {
		intent, body = p.Compensation, p.CompensationRequest
	}
	if declaration.Protocol != "" {
		if p.Protocol == nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation has no original protocol mapping")
		}
		provider := h.t.app(p.Protocol.Provider)
		if provider == nil || provider.Manifest().Version != p.Protocol.Version {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original confirmation provider version is unavailable")
		}
		action := declaration.Action
		if compensate {
			action = declaration.CompensateAction
		}
		mapped := false
		for _, pv := range provider.Manifest().Provides {
			if pv.Protocol.ID() == declaration.Protocol && pv.Actions[action] == map[bool]string{false: p.Protocol.Action, true: p.Protocol.CompensateAction}[compensate] && pv.Actions[action] != "" {
				mapped = true
			}
		}
		if !mapped {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original confirmation protocol mapping is unavailable")
		}
		member, known := h.Member(run.Member)
		applicant := platform.NewCaller(h.decision, member, construction.ID, false, false)
		if !known || !member.Active() || !h.decision.Readable(applicant, run.Subject) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation applicant can no longer read the subject")
		}
		receipt := proto.Clone(r).(*pb.ChangeRecord)
		receipt.Submission.PrincipalId = member.ID
		receipt.Submission.Target = &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}
		target := declaration.Target
		if target == "" || target == "$subject" {
			_, target, _ = strings.Cut(run.Subject, "/")
		}
		applicant.Request(receipt, platform.Request{Tag: p.Name + map[bool]string{true: ":compensate"}[compensate], Key: intent, Provider: p.Protocol.Provider, Protocol: declaration.Protocol, Action: action, Target: target, Payload: run.Input, Reply: platform.ConfirmationAnswerSchema})
		return nil
	}
	var req construction.WritebackRequest
	if json.Unmarshal(body, &req) != nil || req.Confirmation != run.ID || req.Party != p.Name || req.Version != 1 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Confirmation delivery has no frozen request")
	}
	applications, err := h.t.bindApplications(platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: strings.SplitN(run.Subject, "/", 2)[0]}, run.Release, "")
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	key := p.Key
	if compensate {
		key += ":compensate"
	}
	x := platform.Effect{ID: intent, App: construction.ID, Endpoint: construction.WritebackEndpoint + req.Connection, Event: construction.ID + "/confirmation", Key: key, Target: run.Subject, At: at, State: "pending", Due: at, Body: string(body), Applications: applications, Compensates: req.Compensates}
	if !slices.ContainsFunc(h.decision.intents, func(e platform.Effect) bool { return e.ID == intent }) {
		h.decision.intents = append(h.decision.intents, x)
	}
	return nil
}

func (h hostView) CompleteConfirmation(c platform.Caller, r *pb.ChangeRecord, run platform.ConfirmationRun, action string, at time.Time) (string, *kernel.Error) {
	if h.decision == nil || !c.Staging() {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Confirmation completion requires a private decision")
	}
	member, known := h.Member(run.Member)
	if !known || !member.Active() {
		return "", platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation applicant is no longer active")
	}
	execution, err := h.retainedExecution(c, run.Release, run.Assets)
	if err != nil {
		return "", migrationRefusal(err)
	}
	bound := execution.(interface {
		Caller(platform.Caller, platform.Member, string) platform.Caller
		Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
	})
	typ, id, _ := strings.Cut(run.Subject, "/")
	if !strings.HasPrefix(action, typ+".") {
		action = typ + "." + action
	}
	sub := &pb.Submission{TenantId: c.Tenant, PrincipalId: member.ID, Authority: construction.ID, IdempotencyKey: "confirmation:" + run.ID + ":" + action, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: action, Version: 1}, Payload: []byte(`{}`), CorrelationId: run.ID}
	receipt, refusal := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		return bound.Submit(bound.Caller(c, member, construction.ID), sub, at)
	})
	if refusal != nil {
		return "", refusal
	}
	_ = r
	return receipt.GetChangeId(), nil
}

func (h hostView) ResolveConfirmationDelivery(c platform.Caller, run platform.ConfirmationRun, index int, compensated bool, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != construction.ID || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation resolution requires its original owner")
	}
	p := run.Parties[index]
	h.t.opsMu.Lock()
	inFlight := slices.ContainsFunc(h.t.outbound, func(e *effect) bool {
		answering := h.decision.callbackOrigin != nil && h.decision.callbackOrigin.ID == e.ID
		return (e.ID == p.Intent || e.ID == p.Compensation) && e.sending && !answering
	})
	h.t.opsMu.Unlock()
	if inFlight {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation delivery is still in flight")
	}
	view := h.decision.controlHost()
	resolve := func(id, state, reason string) *kernel.Error {
		for _, effect := range view.outbound {
			if effect.ID != id || settled(effect.State) {
				continue
			}
			var request construction.WritebackRequest
			if effect.App != construction.ID || json.Unmarshal([]byte(effect.Body), &request) != nil || request.Confirmation != run.ID || request.Party != p.Name {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation does not own this delivery")
			}
			if effect.sending {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation delivery is still in flight")
			}
			effect.State, effect.Error, effect.Last = state, reason, at
		}
		return nil
	}
	if compensated {
		if p.CompensationState == "confirmed" {
			if err := resolve(p.Compensation, "delivered", p.CompensationReason); err != nil {
				return err
			}
			return resolve(p.Intent, "compensated", p.CompensationReason)
		}
		if p.CompensationState == "rejected" {
			return resolve(p.Compensation, "rejected", p.CompensationReason)
		}
	} else if p.State == "confirmed" {
		return resolve(p.Intent, "delivered", p.Reason)
	} else if p.State == "rejected" {
		return resolve(p.Intent, "rejected", p.Reason)
	}
	return nil
}

func (h hostView) RetryConfirmationDelivery(c platform.Caller, run platform.ConfirmationRun, index int, compensated bool, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != construction.ID || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation resolution requires its original owner")
	}
	p := run.Parties[index]
	if p.Protocol != nil {
		return nil // QueueConfirmation reuses this intent on the original provider.
	}
	id := p.Intent
	if compensated {
		id = p.Compensation
	}
	view := h.decision.controlHost()
	apply, err := view.decideEffect(c, &pb.Submission{Target: &pb.EntityRef{Id: id}, Schema: &pb.SchemaRef{Name: SchemaEffectRetry}}, at)
	if err != nil {
		return err
	}
	apply(nil)
	return nil
}

func (h hostView) CheckConfirmationRead(run platform.ConfirmationRun, index int, at time.Time) *kernel.Error {
	if index < 0 || index >= len(run.Parties) || index >= len(run.Definition.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation declaration is unavailable")
	}
	member, known := h.Member(run.Member)
	if !known || !member.Active() || !h.Readable(member, run.Subject, at) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation applicant can no longer read the subject")
	}
	var source construction.Source
	var connection construction.Connection
	policy := run.Definition.Parties[index].Reconcile
	if policy == nil || json.Unmarshal(run.Parties[index].Source, &source) != nil || json.Unmarshal(run.Parties[index].Connection, &connection) != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The original reconciliation descriptor is unavailable")
	}
	publisher, known := h.Member(source.Puller)
	if !known || !publisher.Active() || !h.Readable(publisher, construction.SourceType+"/"+policy.Source, at) || construction.Guarded(connection.Marking) || construction.Guarded(h.t.connectionMarking(connection.ID)) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Reconciliation source access is unavailable")
	}
	return nil
}
