package host

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
	controlowner "platformserver/services/security/control"
	"time"
)

type controlDecision struct {
	host *Tenant
	base map[string]json.RawMessage
}

func (t *Tenant) captureControl() (map[string]json.RawMessage, error) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	connectors, err := t.ConnectorRoster.Image()
	if err != nil {
		return nil, err
	}
	t.work.Lock()
	work, err := t.work.ControlImage()
	t.work.Unlock()
	if err != nil {
		return nil, err
	}
	out, err := runtimekernel.ControlImages(map[string]any{"settings": t.settings.Snapshot(), "bindings": t.bindings.All(), "work": work,
		"endpoints": t.endpoints.Saved(), "effects": t.outbound.States(), "words": t.words.Clone()})
	if err != nil {
		return nil, err
	}
	out["connectors"] = connectors
	return out, nil
}

func (t *Tenant) newControlFork() *Tenant {
	view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, Store: t.Log(), identity: t.identity, ownerSet: t.ownerSet, definitions: t.definitions, Projection: t.Projection, procs: t.procs, tasks: t.tasks, linker: t.linker, directory: t.directory, agents: t.agents, ai: t.ai,
		bindings: t.bindings.Clone(), ConnectorRoster: runtimekernel.NewConnectorRoster(t.ID), work: automation.NewWorkBoard(), Works: t.forkWorkState()}
	t.work.CopyJobs(&view.work)
	return view
}

func (t *Tenant) forkControl(images map[string]json.RawMessage) (*Tenant, error) {
	view := t.newControlFork()
	_, marks := t.ConnectorRoster.State()
	view.ConnectorRoster.RestoreMarks(marks)
	view.i18n = transport.NewTranslator(func() []platform.App { return view.apps }, nil, view.words.Clone, PlatformLanguages)
	view.i18n.InstallApplications(view.definitions)
	for _, kind := range []string{"connectors", "settings", "bindings", "work", "endpoints", "effects", "words"} {
		if err := view.installControl(kind, images[kind]); err != nil {
			return nil, err
		}
	}
	return view, nil
}

func (t *Tenant) installControl(kind string, raw json.RawMessage) error {
	switch kind {
	case "connectors":
		return t.ConnectorRoster.InstallImage(raw)
	case "settings":
		return t.settings.InstallImage(raw)
	case "bindings":
		return actions.InstallBindings(raw, func() func(string, string) bool {
			t.bindings = actions.NewBindings(nil)
			return t.router().Rebind
		})
	case "work":
		t.work.Lock()
		defer t.work.Unlock()
		return t.work.InstallImage(t.ID, raw, func(app string) bool { return t.App(app) != nil })
	case "endpoints":
		return t.endpoints.InstallImage(raw)
	case "effects":
		return t.outbound.InstallImage(raw)
	case "words":
		if err := t.words.InstallImage(raw); err != nil {
			return err
		}
		t.i18n.Reset()
		return nil
	default:
		return fmt.Errorf("unknown control owner %s", kind)
	}
}

func (d *stagedDecision) controlHost() *Tenant {
	if d.control != nil {
		return d.control.host
	}
	base, err := d.tenant.captureControl()
	if err != nil {
		unsupportedStagedEffect()
	}
	d.control = &controlDecision{d.newControlView(base), base}
	return d.control.host
}

func (d *stagedDecision) newControlView(images map[string]json.RawMessage) *Tenant {
	view, err := d.tenant.forkControl(images)
	if err != nil {
		unsupportedStagedEffect()
	}
	view.Projection = d.Records
	view.ConnectorRoster.Use(d.Connectors)
	view.controlAnswer = func(x platform.Effect, out platform.Outcome, at time.Time) *kernel.Error {
		d.answerControlEffect(x, out, at)
		return d.Failure
	}
	if d.tenant.agents != nil {
		view.agents = d.stateApp(d.tenant.agents).(platform.AgentRuntime)
	}
	return view
}

func (d *stagedDecision) savedControl() (*acceptedformat.Control, error) {
	if d.control == nil {
		return nil, nil
	}
	current, err := d.control.host.captureControl()
	if err != nil {
		return nil, err
	}
	return runtimekernel.ControlDiff(d.control.base, current, d.OperationCancels)
}

func (d *stagedDecision) answerControlEffect(x platform.Effect, out platform.Outcome, at time.Time) {
	if err := d.tenant.stageEffectAnswer(d, x, out, nil, at); err != nil {
		if refusal, ok := err.(*kernel.Error); ok {
			d.Failure = refusal
		} else {
			unsupportedStagedEffect()
		}
	}
}

func (t *Tenant) validateAcceptedControl(saved *acceptedformat.Control) error {
	return runtimekernel.ValidateControl(saved, t.captureControl, func(images map[string]json.RawMessage) error {
		_, err := t.forkControl(images)
		return err
	})
}

func (t *Tenant) applyAcceptedControl(saved *acceptedformat.Control) error {
	return runtimekernel.ApplyControl(saved, t.installControl, t.outbound.PatchImage)
}

func (t *Tenant) controlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	return controlowner.Deciders{Connector: t.decideConnector, Setting: t.decideSetting, Binding: t.decideBinding, Work: t.decideWork,
		Notification: t.decideNotification, Endpoint: t.decideEndpoint, Effect: t.decideEffect, Translation: t.decideTranslation}.Decide(c, s, at)
}

func (r runtime) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Control actions require a private decision")
	}
	return r.t.controlDecision(c, s, at)
}

func (d *stagedDecision) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if apply, refusal, handled := (controlowner.StagedControl{Tenant: d.tenant.ID, NoticeOwner: func(id string) (string, bool) { return d.Notices.Owner(id) }, MarkRead: func(id string) { d.Notices.MarkRead(id) },
		Effect: func(id string) platform.Effect { queued, _ := d.tenant.outbound.Get(id); return queued.Effect },
		Cancel: func(current, after platform.Effect, out platform.Outcome) {
			d.MarkOperationCancelled(current.ID)
			d.answerControlEffect(after, out, at)
		}}).Decide(c, s); handled {
		return apply, refusal
	}
	return d.controlHost().controlDecision(c, s, at)
}

func (d *stagedDecision) forkControlDecision(original *controlDecision) {
	if original == nil {
		return
	}
	images, err := original.host.captureControl()
	if err != nil {
		unsupportedStagedEffect()
	}
	d.control = &controlDecision{d.newControlView(images), maps.Clone(original.base)}
}

func (d *stagedDecision) routingTenant() *Tenant {
	if d.control != nil {
		return d.control.host
	}
	return d.tenant
}
