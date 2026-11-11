package host

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
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
	descriptors, _ := t.connectors.kernel.State()
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
	work, workErr := t.work.Snapshot()
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
		bindings: t.bindings.Clone(), connectors: newConnectorRoster(), work: automation.NewWorkBoard(), works: t.forkWorkState()}
	for _, j := range t.work.Jobs {
		copy := *j
		view.work.AddJob(&copy)
	}
	t.opsMu.Lock()
	_, marks := t.connectors.kernel.State()
	t.opsMu.Unlock()
	view.connectors.kernel.Restore(nil, marks)
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
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		descriptors, err := platform.Unprotos[*pb.ConnectorDescriptor](raw)
		if err != nil {
			return err
		}
		_, marks := t.connectors.kernel.State()
		t.connectors.kernel = kernel.NewConnectors()
		t.connectors.kernel.Restore(descriptors, marks)
		t.connectors.byID = map[string]*pb.ConnectorDescriptor{}
		for _, desc := range descriptors {
			if desc.GetTenantId() != t.ID || t.connectors.byID[desc.GetConnectorId()] != nil {
				return fmt.Errorf("control connector identity differs")
			}
			t.connectors.byID[desc.GetConnectorId()] = desc
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
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var w controlWork
		if err := acceptedformat.DecodeControl(raw, &w); err != nil {
			return err
		}
		state := tenantState{Queues: w.Queues, Failed: w.Failed, Tasks: w.Tasks, Jobs: w.Jobs}
		known := map[string]bool{}
		for _, task := range w.Tasks {
			if task.ID == "" || known[task.ID] || t.App(task.App) == nil {
				return fmt.Errorf("control work identity differs")
			}
			known[task.ID] = true
		}
		indexed := map[string]bool{}
		for app, queue := range w.Queues {
			if t.App(app) == nil {
				return fmt.Errorf("control queue owner missing")
			}
			for _, id := range queue {
				if !known[id] || indexed[id] {
					return fmt.Errorf("control queue references missing or duplicate work")
				}
				indexed[id] = true
			}
		}
		for _, id := range w.Failed {
			if !known[id] || indexed[id] {
				return fmt.Errorf("control failed work identity differs")
			}
			indexed[id] = true
		}
		if len(indexed) != len(known) {
			return fmt.Errorf("control work is unassigned")
		}
		old := map[string]*automation.Task{}
		for _, queue := range t.work.Queues {
			for _, task := range queue {
				old[task.ID] = task
			}
		}
		for _, task := range t.work.Failed {
			old[task.ID] = task
		}
		if err := t.work.Restore(t.ID, automation.WorkState{Queues: state.Queues, Failed: state.Failed, Tasks: state.Tasks, Jobs: state.Jobs}); err != nil {
			return err
		}
		reuse := func(task *automation.Task) *automation.Task {
			if prior := old[task.ID]; prior != nil {
				prior.RestoreOutcome(*task)
				prior.Since = task.Since
				return prior
			}
			return task
		}
		for app, queue := range t.work.Queues {
			for i, task := range queue {
				t.work.Queues[app][i] = reuse(task)
			}
		}
		for i, task := range t.work.Failed {
			t.work.Failed[i] = reuse(task)
		}
	case "endpoints":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
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
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
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
	view.connectors.kernel = d.connectors
	view.connectors.byID = map[string]*pb.ConnectorDescriptor{}
	connectors, _ := d.connectors.State()
	for _, descriptor := range connectors {
		view.connectors.byID[descriptor.GetConnectorId()] = descriptor
	}
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
	result := &acceptedformat.Control{}
	for _, kind := range slices.Sorted(maps.Keys(current)) {
		base := d.control.base[kind]
		image := current[kind]
		if kind == "effects" {
			var prior, after []acceptedformat.EffectState
			if err := acceptedformat.DecodeControl(base, &prior); err != nil {
				return nil, err
			}
			if err := acceptedformat.DecodeControl(image, &after); err != nil {
				return nil, err
			}
			lookup := map[string]acceptedformat.EffectState{}
			for _, x := range prior {
				lookup[x.ID] = x
			}
			var beforeChanges, afterChanges []acceptedformat.EffectState
			for _, x := range after {
				old, known := lookup[x.ID]
				if !known {
					return nil, fmt.Errorf("control may not create effect intents")
				}
				oldHash, _ := platform.CanonicalDigest(old)
				newHash, _ := platform.CanonicalDigest(x)
				if oldHash != newHash && !d.operationCancels[x.ID] {
					beforeChanges = append(beforeChanges, old)
					afterChanges = append(afterChanges, x)
				}
			}
			if len(afterChanges) == 0 {
				continue
			}
			base, _ = json.Marshal(beforeChanges)
			image, _ = json.Marshal(afterChanges)
		}
		before, err := platform.CanonicalDigest(base)
		if err != nil {
			return nil, err
		}
		after, err := platform.CanonicalDigest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			result.Changes = append(result.Changes, acceptedformat.ControlChange{Kind: kind, Before: before, Image: image})
		}
	}
	if len(result.Changes) == 0 {
		return nil, nil
	}
	return result, nil
}

func (t *Tenant) validateAcceptedControl(saved *acceptedformat.Control) error {
	if saved == nil {
		return nil
	}
	current, err := t.captureControl()
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, change := range saved.Changes {
		prior := current[change.Kind]
		if change.Kind == "effects" {
			var after, all []acceptedformat.EffectState
			if err := acceptedformat.DecodeControl(change.Image, &after); err != nil {
				return err
			}
			if err := acceptedformat.DecodeControl(prior, &all); err != nil {
				return err
			}
			selected := []acceptedformat.EffectState{}
			seenIDs := map[string]bool{}
			for _, next := range after {
				i := slices.IndexFunc(all, func(x acceptedformat.EffectState) bool { return x.ID == next.ID })
				if i < 0 || seenIDs[next.ID] {
					return fmt.Errorf("control effect is missing or duplicated")
				}
				seenIDs[next.ID] = true
				old := all[i]
				fixed := next
				fixed.State, fixed.Due, fixed.Since = old.State, old.Due, old.Since
				fixed.Last, fixed.Error, fixed.CallbackError = old.Last, old.Error, old.CallbackError
				oldHash, _ := platform.CanonicalDigest(old)
				fixedHash, _ := platform.CanonicalDigest(fixed)
				if oldHash != fixedHash {
					return fmt.Errorf("control effect rewrites immutable intent")
				}
				selected = append(selected, old)
				all[i] = next
			}
			prior, _ = json.Marshal(selected)
			current[change.Kind], _ = json.Marshal(all)
		}
		before, err := platform.CanonicalDigest(prior)
		if err != nil || seen[change.Kind] || current[change.Kind] == nil || change.Before != before {
			return fmt.Errorf("control %s predecessor differs", change.Kind)
		}
		seen[change.Kind] = true
		if change.Kind != "effects" {
			current[change.Kind] = change.Image
		}
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
