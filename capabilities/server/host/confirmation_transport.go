package host

import (
	"fmt"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func (d *stagedDecision) ConfirmationTransport() platform.ConfirmationTransport { return d.transport() }

func (d *stagedDecision) DispatchTransport() platform.DispatchTransport { return d.transport() }

type confirmationCompletion struct{ Member, Subject, Action string }

func (t *Tenant) readTransport() dataowner.Transport {
	h := hostView{t: t}
	return dataowner.Transport{Member: h.Member, Readable: h.Readable, Publisher: t.sourcePublisher, Markings: t.connectionMarkings,
		ObservationRead: func(member platform.Member, definition platform.ObservationDefinition, typ, id string, at time.Time) (bool, *kernel.Error) {
			caller := platform.NewCaller(runtime{t: t}, member, definition.Object.App, false, false)
			if refusal := authorizationRefusal(t.authorizeNative(caller, definition.Name, "read", definition.Roles, true, authorizationDetails{At: at})); refusal != nil {
				return false, refusal
			}
			permission := t.authorizeFrom(t.Projection, caller, platform.AuthzRequest{Principal: member.ID, Tenant: t.ID, Kind: "read", Type: typ, Record: id, Fields: []string{definition.DeviceField}}, at, false)
			if refusal := authorizationRefusal(permission); refusal != nil {
				return false, refusal
			}
			return len(permission.Withheld) > 0, nil
		}}
}

func (d *stagedDecision) transport() dataowner.Transport {
	t := d.tenant
	h := hostView{t: t, app: t.App(build.ID), decision: d}
	x := t.readTransport()
	x.Member, x.Readable, x.Record, x.Protocol = h.Member, h.Readable, h.Record, h.FreezeConfirmationProtocol
	x.Release, x.App, x.Credential, x.Owner, x.ApplicantReads = t.releases.Active, t.App, t.checkWritebackCredential, t.DataOwner(), d.Readable
	x.Assets = func() ([]platform.ReleaseAsset, error) { return t.releaseAssetsLocked(nil, false) }
	x.Applicant = func(m platform.Member) platform.Caller { return platform.NewCaller(d, m, build.ID, false, false) }
	x.Bind = func(object, release string) ([]platform.AssetBinding, error) {
		return t.bindApplications(platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: object}, release, "")
	}
	x.Intend = func(effect platform.Effect) {
		if !slices.ContainsFunc(d.Intents, func(e platform.Effect) bool { return e.ID == effect.ID }) {
			d.Intents = append(d.Intents, effect)
		}
	}
	x.Complete = func(c platform.Caller, run platform.ConfirmationRun, member platform.Member, action string, sub *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
		execution, err := h.retainedExecution(c, run.Release, run.Assets)
		if err != nil {
			return nil, runtimekernel.ConflictRefusal(err)
		}
		bound := execution.(interface {
			Caller(platform.Caller, platform.Member, string) platform.Caller
			Submit(platform.Caller, *pb.Submission, time.Time) (*pb.ChangeRecord, *kernel.Error)
		})
		return platform.Attempt(c, func() (*pb.ChangeRecord, *kernel.Error) {
			previous := d.confirmationCompleting
			d.confirmationCompleting = &confirmationCompletion{Member: member.ID, Subject: run.Subject, Action: action}
			defer func() { d.confirmationCompleting = previous }()
			return bound.Submit(bound.Caller(c, member, build.ID), sub, at)
		})
	}
	x.InFlight = func(owned func(platform.Effect) bool) bool {
		return t.outbound.Any(func(q actions.Queued) bool {
			answering := d.CallbackOrigin != nil && d.CallbackOrigin.ID == q.ID
			return owned(q.Effect) && q.Sending && !answering
		})
	}
	x.Queued = func(id string) (platform.Effect, bool, bool) {
		q, ok := d.controlHost().outbound.Get(id)
		return q.Effect, q.Sending, ok
	}
	x.Settle = func(id, state, reason string, at time.Time) {
		d.controlHost().outbound.Update(id, func(q *actions.Queued) { q.State, q.Error, q.Last = state, reason, at })
	}
	x.SettleAll = func(owned func(platform.Effect) bool, state, reason string, at time.Time) {
		d.controlHost().outbound.UpdateAll(func(q *actions.Queued) {
			if owned(q.Effect) && !platform.EffectSettled(q.State) {
				q.State, q.Error, q.Last = state, reason, at
			}
		})
	}
	x.Retry = func(c platform.Caller, id string, at time.Time) *kernel.Error {
		apply, err := d.controlHost().effectControl().DecideEffect(c, &pb.Submission{Target: &pb.EntityRef{Id: id}, Schema: &pb.SchemaRef{Name: controlowner.SchemaEffectRetry}}, at,
			controlowner.SchemaEffectApprove, controlowner.SchemaEffectDiscard, controlowner.SchemaEffectResolve)
		if err != nil {
			return err
		}
		apply(nil)
		return nil
	}
	return x
}

func (h hostView) FreezeConfirmationProtocol(p platform.ConfirmationParty) (*platform.ConfirmationProtocol, error) {
	b, ok := h.t.router().Resolve(p.Protocol)
	if !ok || b.Provision.Actions[p.Action] == "" || p.CompensateAction != "" && b.Provision.Actions[p.CompensateAction] == "" {
		return nil, fmt.Errorf("The confirmation protocol has no declared provider mapping")
	}
	return &platform.ConfirmationProtocol{Provider: b.Provider.Manifest().ID, Version: b.Provider.Manifest().Version, Action: b.Provision.Actions[p.Action], CompensateAction: b.Provision.Actions[p.CompensateAction]}, nil
}

func (d *stagedDecision) ConfirmationCompletion(c platform.Caller, object, id, action string) bool {
	proof := d.confirmationCompleting
	return c.Staging() && !c.Replaying && c.Tenant == d.tenant.ID && proof != nil && proof.Member == c.ID && proof.Subject == object+"/"+id && proof.Action == action
}
