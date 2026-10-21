package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Control changes participate in the business result. Each owned component has
// a typed immutable image and its own predecessor, rather than restoring an
// entire tenant over records or concurrent outside-lane bookkeeping.
type controlWork struct {
	Queues map[string][]string `json:"queues"`
	Failed []string            `json:"failed"`
	Tasks  []taskState         `json:"tasks"`
	Jobs   []Task              `json:"jobs"`
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
	if err := save("settings", t.settings.Clone()); err != nil {
		return nil, err
	}
	bindings := map[string]string{}
	for protocol, b := range t.bindings {
		bindings[protocol] = b.provider.Manifest().ID
	}
	if err := save("bindings", bindings); err != nil {
		return nil, err
	}
	state := tenantState{Queues: map[string][]string{}}
	if err := t.work.snapshot(&state); err != nil {
		return nil, err
	}
	if err := save("work", controlWork{state.Queues, state.Failed, state.Tasks, state.Jobs}); err != nil {
		return nil, err
	}
	if err := save("endpoints", t.endpoints); err != nil {
		return nil, err
	}
	effects := make([]resultformat.EffectState, 0, len(t.outbound))
	for _, e := range t.outbound {
		effects = append(effects, resultformat.EffectState{Effect: e.Effect, Since: e.since})
	}
	if err := save("effects", effects); err != nil {
		return nil, err
	}
	if err := save("words", t.words.clone()); err != nil {
		return nil, err
	}
	return out, nil
}

func decodeControl(raw []byte, v any) error {
	if len(raw) == 0 || len(raw) > acceptance.MaxResultBytes {
		return fmt.Errorf("control image is empty or too large")
	}
	if err := acceptance.DecodeJSON(raw, v, "control image"); err != nil {
		return err
	}
	return nil
}

func (t *Tenant) forkControl(images map[string]json.RawMessage) (*Tenant, error) {
	view := &Tenant{ID: t.ID, identity: t.identity, apps: t.apps, owner: t.owner, definitions: t.definitions, records: t.records, procs: t.procs, tasks: t.tasks, linker: t.linker, directory: t.directory, agents: t.agents, ai: t.ai,
		bindings: maps.Clone(t.bindings), connectors: newConnectorRoster(), work: newWorkBoard(), works: t.forkWorkState()}
	for _, j := range t.work.jobs {
		copy := *j
		view.work.addJob(&copy)
	}
	t.opsMu.Lock()
	_, marks := t.connectors.kernel.State()
	t.opsMu.Unlock()
	view.connectors.kernel.Restore(nil, marks)
	view.i18n = translator{apps: func() []platform.App { return view.apps }, tenant: view.words.clone}
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
		if err := decodeControl(raw, &values); err != nil {
			return err
		}
		t.settings.Restore(values)
	case "bindings":
		var bindings map[string]string
		if err := decodeControl(raw, &bindings); err != nil {
			return err
		}
		t.bindings = map[string]binding{}
		for protocol, provider := range bindings {
			if !t.rebind(protocol, provider) {
				return fmt.Errorf("control protocol provider is missing")
			}
		}
	case "work":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var w controlWork
		if err := decodeControl(raw, &w); err != nil {
			return err
		}
		state := tenantState{Queues: w.Queues, Failed: w.Failed, Tasks: w.Tasks, Jobs: w.Jobs}
		known := map[string]bool{}
		for _, task := range w.Tasks {
			if task.ID == "" || known[task.ID] || t.app(task.App) == nil {
				return fmt.Errorf("control work identity differs")
			}
			known[task.ID] = true
		}
		indexed := map[string]bool{}
		for app, queue := range w.Queues {
			if t.app(app) == nil {
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
		old := map[string]*Task{}
		for _, queue := range t.work.queues {
			for _, task := range queue {
				old[task.ID] = task
			}
		}
		for _, task := range t.work.failed {
			old[task.ID] = task
		}
		if err := t.work.restore(t.ID, &state); err != nil {
			return err
		}
		reuse := func(task *Task) *Task {
			if prior := old[task.ID]; prior != nil {
				prior.restoreOutcome(*task)
				prior.since = task.since
				return prior
			}
			return task
		}
		for app, queue := range t.work.queues {
			for i, task := range queue {
				t.work.queues[app][i] = reuse(task)
			}
		}
		for i, task := range t.work.failed {
			t.work.failed[i] = reuse(task)
		}
	case "endpoints":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var endpoints []*Endpoint
		if err := decodeControl(raw, &endpoints); err != nil {
			return err
		}
		ids := map[string]bool{}
		for _, endpoint := range endpoints {
			if endpoint == nil || endpoint.ID == "" || ids[endpoint.ID] {
				return fmt.Errorf("control endpoint identity differs")
			}
			ids[endpoint.ID] = true
		}
		t.endpoints = endpoints
	case "effects":
		t.opsMu.Lock()
		defer t.opsMu.Unlock()
		var states []resultformat.EffectState
		if err := decodeControl(raw, &states); err != nil {
			return err
		}
		existing := map[string]*effect{}
		for _, e := range t.outbound {
			existing[e.ID] = e
		}
		ids := map[string]bool{}
		effects := make([]*effect, 0, len(states))
		for _, state := range states {
			if state.ID == "" || ids[state.ID] || !slices.Contains([]string{"held", "pending", "retrying", "unknown", "compensated", "delivered", "failed", "rejected", "discarded"}, state.State) {
				return fmt.Errorf("control effect identity or state differs")
			}
			ids[state.ID] = true
			e := existing[state.ID]
			if e == nil {
				e = &effect{}
			}
			e.Effect, e.since = state.Effect, state.Since
			effects = append(effects, e)
		}
		t.outbound = effects
	case "words":
		var words map[string]map[string]string
		if err := decodeControl(raw, &words); err != nil {
			return err
		}
		t.words.restore(words)
		t.i18n.reset()
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

// Both a first control change and a savepoint use the same private owners.
// Their predecessor images remain selected by the calling operation.
func (d *stagedDecision) newControlView(images map[string]json.RawMessage) *Tenant {
	view, err := d.tenant.forkControl(images)
	if err != nil {
		unsupportedStagedEffect()
	}
	view.records = d.records
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
		view.agents = d.stateApp(d.tenant.agents).(AgentRuntime)
	}
	return view
}

func (d *stagedDecision) savedControl() (*resultformat.Control, error) {
	if d.control == nil {
		return nil, nil
	}
	current, err := d.control.host.captureControl()
	if err != nil {
		return nil, err
	}
	result := &resultformat.Control{}
	for _, kind := range slices.Sorted(maps.Keys(current)) {
		base := d.control.base[kind]
		image := current[kind]
		if kind == "effects" {
			var prior, after []resultformat.EffectState
			if err := decodeControl(base, &prior); err != nil {
				return nil, err
			}
			if err := decodeControl(image, &after); err != nil {
				return nil, err
			}
			lookup := map[string]resultformat.EffectState{}
			for _, x := range prior {
				lookup[x.ID] = x
			}
			var beforeChanges, afterChanges []resultformat.EffectState
			for _, x := range after {
				old, known := lookup[x.ID]
				if !known {
					return nil, fmt.Errorf("control may not create effect intents")
				}
				oldHash, _ := acceptance.Digest(old)
				newHash, _ := acceptance.Digest(x)
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
		before, err := acceptance.Digest(base)
		if err != nil {
			return nil, err
		}
		after, err := acceptance.Digest(image)
		if err != nil {
			return nil, err
		}
		if before != after {
			result.Changes = append(result.Changes, resultformat.ControlChange{Kind: kind, Before: before, Image: image})
		}
	}
	if len(result.Changes) == 0 {
		return nil, nil
	}
	return result, nil
}

func (t *Tenant) validateAcceptedControl(saved *resultformat.Control) error {
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
			var after, all []resultformat.EffectState
			if err := decodeControl(change.Image, &after); err != nil {
				return err
			}
			if err := decodeControl(prior, &all); err != nil {
				return err
			}
			selected := []resultformat.EffectState{}
			seenIDs := map[string]bool{}
			for _, next := range after {
				i := slices.IndexFunc(all, func(x resultformat.EffectState) bool { return x.ID == next.ID })
				if i < 0 || seenIDs[next.ID] {
					return fmt.Errorf("control effect is missing or duplicated")
				}
				seenIDs[next.ID] = true
				old := all[i]
				fixed := next
				fixed.State, fixed.Due, fixed.Since = old.State, old.Due, old.Since
				fixed.Last, fixed.Error, fixed.CallbackError = old.Last, old.Error, old.CallbackError
				oldHash, _ := acceptance.Digest(old)
				fixedHash, _ := acceptance.Digest(fixed)
				if oldHash != fixedHash {
					return fmt.Errorf("control effect rewrites immutable intent")
				}
				selected = append(selected, old)
				all[i] = next
			}
			prior, _ = json.Marshal(selected)
			current[change.Kind], _ = json.Marshal(all)
		}
		before, err := acceptance.Digest(prior)
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

func (t *Tenant) applyAcceptedControl(saved *resultformat.Control) error {
	if saved == nil {
		return nil
	}
	for _, change := range saved.Changes {
		// Tenant.mu serializes the authoritative promotion. Only this component's
		// owned bytes change; effects preserve outside-lane sending flags by ID.
		image := change.Image
		if change.Kind == "effects" {
			var patches []resultformat.EffectState
			if err := decodeControl(image, &patches); err != nil {
				return err
			}
			t.opsMu.Lock()
			for _, patch := range patches {
				i := slices.IndexFunc(t.outbound, func(x *effect) bool { return x.ID == patch.ID })
				if i < 0 {
					t.opsMu.Unlock()
					return fmt.Errorf("control effect disappeared")
				}
				t.outbound[i].Effect, t.outbound[i].since = patch.Effect, patch.Since
			}
			t.opsMu.Unlock()
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
	case ConnectorType:
		return t.decideConnector(c, s, at)
	case SettingType:
		return t.decideSetting(c, s, at)
	case ProtocolType:
		return t.decideBinding(c, s, at)
	case WorkType:
		return t.decideWork(c, s, at)
	case NotificationType:
		return t.decideNotification(c, s, at)
	case EndpointType:
		return t.decideEndpoint(c, s, at)
	case EffectType:
		return t.decideEffect(c, s, at)
	case TranslationType:
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
	if c.App != PlatformApp || s.GetAuthority() != PlatformApp || c.ID != s.GetPrincipalId() || c.Tenant != d.tenant.ID || s.GetTenantId() != d.tenant.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Only the console may decide tenant controls")
	}
	if s.GetTarget().GetType() == NotificationType {
		id := s.GetTarget().GetId()
		i := slices.IndexFunc(d.notices, func(n platform.Notification) bool { return n.ID == id })
		if i < 0 {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "")
		}
		if d.notices[i].Member != c.ID {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "")
		}
		return func(*pb.ChangeRecord) { d.notices[i].Read = true; d.noticeChanged = true }, nil
	}
	if s.GetTarget().GetType() == EffectType && s.GetSchema().GetName() == SchemaEffectDiscard {
		d.tenant.opsMu.Lock()
		var current platform.Effect
		for _, effect := range d.tenant.outbound {
			if effect.ID == s.GetTarget().GetId() {
				current = effect.Effect
				break
			}
		}
		d.tenant.opsMu.Unlock()
		if current.Endpoint == resultformat.OperationEndpoint {
			if settled(current.State) {
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
	// Effect callbacks stay on the original staged Caller; only their control
	// metadata is owned by this private host view.
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

func (t *Tenant) validateControlDeliveries(saved *resultformat.Control, deliveries []resultformat.ConnectorDelivery) error {
	if saved == nil || len(deliveries) == 0 {
		return t.validateAcceptedDeliveries(deliveries)
	}
	images, err := t.captureControl()
	if err != nil {
		return err
	}
	for _, change := range saved.Changes {
		if change.Kind == "connectors" {
			images[change.Kind] = change.Image
		}
	}
	view, err := t.forkControl(images)
	if err != nil {
		return err
	}
	return view.validateAcceptedDeliveries(deliveries)
}
