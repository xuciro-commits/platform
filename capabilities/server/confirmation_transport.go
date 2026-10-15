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
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

func (h hostView) FreezeConfirmation(c platform.Caller, subject string, spec platform.Confirmation, at time.Time) (string, []platform.ReleaseAsset, error) {
	if h.decision == nil || !c.Staging() || c.App != build.ID {
		return "", nil, fmt.Errorf("Confirmation requires its original private authority")
	}
	assets, err := h.t.releaseAssetsLocked(nil, false)
	if err != nil {
		return "", nil, err
	}
	roots := []platform.AssetRef{{App: build.ID, Kind: platform.AssetObject, Name: subject}}
	for _, p := range spec.Parties {
		for _, id := range []string{p.Writeback, p.CompensateWriteback} {
			if id == "" {
				continue
			}
			w, ok := platform.Get[build.Writeback](c, id)
			if !ok {
				return "", nil, fmt.Errorf("Confirmation writeback is unavailable")
			}
			roots = append(roots, platform.AssetRef{App: build.ID, Kind: platform.AssetWriteback, Name: w.Name})
		}
		if p.Reconcile != nil {
			s, ok := platform.Get[build.Source](c, p.Reconcile.Source)
			if !ok {
				return "", nil, fmt.Errorf("Reconciliation source is unavailable")
			}
			roots = append(roots, platform.AssetRef{App: build.ID, Kind: platform.AssetSource, Name: s.Name})
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

func (h hostView) QueueConfirmation(c platform.Caller, r *pb.ChangeRecord, run platform.ConfirmationRun, index int, compensate bool, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != build.ID || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation transport requires its original owner")
	}
	p := run.Parties[index]
	declaration := run.Definition.Parties[index]
	intent, body := p.Intent, p.Request
	if compensate {
		intent, body = p.Compensation, p.CompensationRequest
	}
	if declaration.Protocol != "" {
		action := declaration.Action
		if compensate {
			action = declaration.CompensateAction
		}
		receipt := proto.Clone(r).(*pb.ChangeRecord)
		receipt.Submission.Target = &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}
		target := declaration.Target
		if target == "" || target == "$subject" {
			_, target, _ = strings.Cut(run.Subject, "/")
		}
		c.Request(receipt, platform.Request{Tag: p.Name + map[bool]string{true: ":compensate"}[compensate], Protocol: declaration.Protocol, Action: action, Target: target, Payload: run.Input, Reply: platform.ConfirmationAnswerSchema})
		return nil
	}
	var req build.WritebackRequest
	if json.Unmarshal(body, &req) != nil || req.Confirmation != run.ID || req.Party != p.Name || req.Version != 1 {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Confirmation delivery has no frozen request")
	}
	applications, err := h.t.bindApplications(platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: strings.SplitN(run.Subject, "/", 2)[0]}, h.t.releases.active, "")
	if err != nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "{why}", err.Error())
	}
	x := platform.Effect{ID: intent, App: build.ID, Endpoint: build.WritebackEndpoint + req.Connection, Event: "confirmation/" + run.ID, Key: p.Key, Target: run.Subject, At: at, State: "pending", Due: at, Body: string(body), Applications: applications, Compensates: req.Compensates}
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
	sub := &pb.Submission{TenantId: c.Tenant, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: "confirmation:" + run.ID + ":" + action, Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: action, Version: 1}, Payload: []byte(`{}`), CorrelationId: run.ID}
	receipt, refusal := platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
		return bound.Submit(bound.Caller(c, member, build.ID), sub, at)
	})
	if refusal != nil {
		return "", refusal
	}
	_ = r
	return receipt.GetChangeId(), nil
}

func (h hostView) ResolveConfirmationDelivery(c platform.Caller, run platform.ConfirmationRun, index int, compensated bool, at time.Time) *kernel.Error {
	if h.decision == nil || !c.Staging() || c.App != build.ID || index < 0 || index >= len(run.Parties) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Confirmation resolution requires its original owner")
	}
	view := h.decision.controlHost()
	for _, effect := range view.outbound {
		if effect.ID != run.Parties[index].Intent || effect.State != "unknown" {
			continue
		}
		var request build.WritebackRequest
		if effect.App != build.ID || json.Unmarshal([]byte(effect.Body), &request) != nil || request.Confirmation != run.ID || request.Party != run.Parties[index].Name {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The confirmation does not own this delivery")
		}
		p := run.Parties[index]
		if compensated {
			effect.State = "compensated"
		} else if p.State == "confirmed" {
			effect.State = "delivered"
		} else if p.State == "rejected" {
			effect.State = "rejected"
		} else {
			return nil
		}
		effect.Error, effect.Last = p.Reason, at
	}
	return nil
}
