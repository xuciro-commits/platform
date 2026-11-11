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
	"slices"
	"time"
)

type controlWork struct {
	Queues map[string][]string           `json:"queues"`
	Failed []string                      `json:"failed"`
	Tasks  []platform.ScheduledTaskState `json:"tasks"`
	Jobs   []automation.Task             `json:"jobs"`
}

type controlDecision struct {
	host *Tenant
	base map[string]json.RawMessage
}

func (t *Tenant) captureControl() (map[string]json.RawMessage, error) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	out := map[string]json.RawMessage{}
	save := func(kind string, v any) error { raw, err := json.Marshal(v); out[kind] = raw; return err }
	descriptors, _ := t.connectors.State()
	raw, err := platform.Protos(descriptors)
	if err != nil {
		return nil, err
	}
	out["connectors"] = raw
	if err := save("settings", t.settings.Snapshot()); err != nil {
		return nil, err
	}
	if err := save("bindings", t.bindings.All()); err != nil {
		return nil, err
	}
	state := tenantState{Queues: map[string][]string{}}
	t.work.Lock()
	work, workErr := t.work.Snapshot()
	t.work.Unlock()
	state.Queues = work.Queues
	state.Failed = append(state.Failed, work.Failed...)
	state.Tasks = append(state.Tasks, work.Tasks...)
	state.Jobs = append(state.Jobs, work.Jobs...)
	if err := workErr; err != nil {
		return nil, err
	}
	if err := save("work", controlWork{state.Queues, state.Failed, state.Tasks, state.Jobs}); err != nil {
		return nil, err
	}
	if err := save("endpoints", t.endpoints.Saved()); err != nil {
		return nil, err
	}
	effects := []acceptedformat.EffectState{}
	for _, e := range t.outbound.Select(nil) {
		effects = append(effects, acceptedformat.EffectState{Effect: e.Effect, Since: e.Since})
	}
	if err := save("effects", effects); err != nil {
		return nil, err
	}
	if err := save("words", t.words.Clone()); err != nil {
		return nil, err
	}
	return out, nil
}

func (t *Tenant) forkControl(images map[string]json.RawMessage) (*Tenant, error) {
	view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, Store: t.Log(), identity: t.identity, ownerSet: t.ownerSet, definitions: t.definitions, Projection: t.Projection, procs: t.procs, tasks: t.tasks, linker: t.linker, directory: t.directory, agents: t.agents, ai: t.ai,
		bindings: t.bindings.Clone(), connectors: runtimekernel.NewConnectorRoster(t.ID), work: automation.NewWorkBoard(), works: t.forkWorkState()}
	t.work.Lock()
	for _, j := range t.work.Jobs {
		copy := *j
		view.work.AddJob(&copy)
	}
	t.work.Unlock()
	_, marks := t.connectors.State()
	view.connectors.RestoreMarks(marks)
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
		descriptors, err := platform.Unprotos[*pb.ConnectorDescriptor](raw)
		if err != nil {
			return err
		}
		if err := t.connectors.Install(descriptors); err != nil {
			return err
		}
	case "settings":
		var values map[string]string
		if err := acceptedformat.DecodeControl(raw, &values); err != nil {
			return err
		}
		t.settings.Restore(values)
	case "bindings":
		var bindings map[string]string
		if err := acceptedformat.DecodeControl(raw, &bindings); err != nil {
			return err
		}
		t.bindings = actions.NewBindings(nil)
		for protocol, provider := range bindings {
			if !t.router().Rebind(protocol, provider) {
				return fmt.Errorf("control protocol provider is missing")
			}
		}
	case "work":
		var w controlWork
		if err := acceptedformat.DecodeControl(raw, &w); err != nil {
			return err
		}
		t.work.Lock()
		defer t.work.Unlock()
		return t.work.Install(t.ID, automation.WorkState{Queues: w.Queues, Failed: w.Failed, Tasks: w.Tasks, Jobs: w.Jobs}, func(app string) bool { return t.App(app) != nil })
	case "endpoints":
		var endpoints []*actions.Endpoint
		if err := acceptedformat.DecodeControl(raw, &endpoints); err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, endpoint := range endpoints {
			if endpoint == nil || endpoint.ID == "" || ids[endpoint.ID] {
				return fmt.Errorf("control endpoint identity differs")
			}
			ids[endpoint.ID] = true
		}
		t.endpoints.Restore(endpoints)
	case "effects":
		var states []acceptedformat.EffectState
		if err := acceptedformat.DecodeControl(raw, &states); err != nil {
			return err
		}
		ids := map[string]bool{}
		effects := make([]actions.Queued, 0, len(states))
		for _, state := range states {
			if state.ID == "" || ids[state.ID] || !slices.Contains([]string{"held", "pending", "retrying", "unknown", "compensated", "delivered", "failed", "rejected", "discarded"}, state.State) {
				return fmt.Errorf("control effect identity or state differs")
			}
			ids[state.ID] = true
			effects = append(effects, actions.Queued{Effect: state.Effect, Since: state.Since})
		}
		t.outbound.Restore(effects)
	case "words":
		var words map[string]map[string]string
		if err := acceptedformat.DecodeControl(raw, &words); err != nil {
			return err
		}
		t.words.Restore(words)
		t.i18n.Reset()
	default:
		return fmt.Errorf("unknown control owner %s", kind)
	}
	return nil
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
	view.Projection = d.records
	view.connectors.Use(d.connectors)
	view.controlAnswer = func(x platform.Effect, out platform.Outcome, at time.Time) *kernel.Error {
		if err := d.tenant.stageEffectAnswer(d, x, out, nil, at); err != nil {
			if refusal, ok := err.(*kernel.Error); ok {
				d.failure = refusal
			} else {
				unsupportedStagedEffect()
			}
		}
		return d.failure
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
	return runtimekernel.ControlDiff(d.control.base, current, d.operationCancels)
}

func (t *Tenant) validateAcceptedControl(saved *acceptedformat.Control) error {
	if saved == nil {
		return nil
	}
	current, err := t.captureControl()
	if err != nil {
		return err
	}
	current, err = runtimekernel.ControlNext(current, saved)
	if err != nil {
		return err
	}
	_, err = t.forkControl(current)
	return err
}

func (t *Tenant) applyAcceptedControl(saved *acceptedformat.Control) error {
	if saved == nil {
		return nil
	}
	for _, change := range saved.Changes {
		image := change.Image
		if change.Kind == "effects" {
			var patches []acceptedformat.EffectState
			if err := acceptedformat.DecodeControl(image, &patches); err != nil {
				return err
			}
			for _, patch := range patches {
				if !t.outbound.Update(patch.ID, func(x *actions.Queued) { x.Effect, x.Since = patch.Effect, patch.Since }) {
					return fmt.Errorf("control effect disappeared")
				}
			}
			continue
		}
		if err := t.installControl(change.Kind, image); err != nil {
			return err
		}
	}
	return nil
}

func (t *Tenant) controlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	switch s.GetTarget().GetType() {
	case controlowner.ConnectorType:
		return t.decideConnector(c, s, at)
	case controlowner.SettingType:
		return t.decideSetting(c, s, at)
	case controlowner.ProtocolType:
		return t.decideBinding(c, s, at)
	case controlowner.WorkType:
		return t.decideWork(c, s, at)
	case controlowner.NotificationType:
		return t.decideNotification(c, s, at)
	case controlowner.EndpointType:
		return t.decideEndpoint(c, s, at)
	case controlowner.EffectType:
		return t.decideEffect(c, s, at)
	case controlowner.TranslationType:
		return t.decideTranslation(c, s, at)
	default:
		return nil, unknown()
	}
}

func (r runtime) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Control actions require a private decision")
	}
	return r.t.controlDecision(c, s, at)
}

func (d *stagedDecision) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *kernel.Error) {
	if c.App != controlowner.PlatformApp || s.GetAuthority() != controlowner.PlatformApp || c.ID != s.GetPrincipalId() || c.Tenant != d.tenant.ID || s.GetTenantId() != d.tenant.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the console may decide tenant controls")
	}
	if s.GetTarget().GetType() == controlowner.NotificationType {
		id := s.GetTarget().GetId()
		owner, found := d.notices.Owner(id)
		if !found {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "")
		}
		if owner != c.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "")
		}
		return func(*pb.ChangeRecord) { d.notices.MarkRead(id) }, nil
	}
	if s.GetTarget().GetType() == controlowner.EffectType && s.GetSchema().GetName() == controlowner.SchemaEffectDiscard {
		queued, _ := d.tenant.outbound.Get(s.GetTarget().GetId())
		current := queued.Effect
		if current.Endpoint == acceptedformat.OperationEndpoint {
			if platform.EffectSettled(current.State) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "")
			}
			return func(*pb.ChangeRecord) {
				if d.operationCancels == nil {
					d.operationCancels = map[string]bool{}
				}
				d.operationCancels[current.ID] = true
				after := current
				after.State, after.Error, after.Output = "discarded", "Cancelled", nil
				if err := d.tenant.stageEffectAnswer(d, after, platform.Outcome{Effect: current.ID, Result: "discarded", Detail: "Cancelled"}, nil, at); err != nil {
					if refusal, ok := err.(*kernel.Error); ok {
						d.failure = refusal
					} else {
						unsupportedStagedEffect()
					}
				}
			}, nil
		}
	}
	view := d.controlHost()
	return view.controlDecision(c, s, at)
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
