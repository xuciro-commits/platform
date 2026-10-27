package platformserver

import (
	"time"

	"bytes"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/construction"
	"platformserver/internal/systemscope"
	"platformserver/platform"
	"slices"
	"strings"
)

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a, ok := d.tenant.app(app).(platform.App)
	if !ok || c.Automation || c.Tenant != d.tenant.ID || s.GetAuthority() != app || s.GetPrincipalId() != c.ID {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The approval needs its original authorised member")
	}
	if d.probing {
		return d.Decide(c, a, s, now)
	}
	return d.requestApproval(a, c.Member, s, now)
}

func (d *stagedDecision) requestApproval(a platform.App, m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	// Permission/attribute/rule checks occur privately. Even a rule that uses
	// another app cannot leave anything in the actual request transaction.
	c := platform.NewCaller(d, m, a.Manifest().ID, false, false)
	if err := d.ProbeDecision(c, func(probe platform.Caller) *kernel.Error {
		_, err := platform.Decide(probe, a, s, now)
		return err
	}); err != nil {
		return nil, err
	}
	owner, ok := d.tenant.app(platform.WorkApp).(platform.ApprovalOwner)
	if !ok {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The approval owner is unavailable")
	}
	return owner.RequestApproval(c, s, now)
}

// Internal protocol requests and replies are child decisions of the input,
// never separately confirmed journal writes. They run after application
// locks are released and before the single result is planned/committed.
func (d *stagedDecision) answerRequests(now time.Time) *kernel.Error {
	for count := 0; len(d.requests) > 0; count++ {
		if count >= resultformat.MaxHops {
			unsupportedStagedEffect()
		}
		q := d.requests[0]
		d.requests = d.requests[1:]
		if err := d.answerRequest(q, now); err != nil {
			return err
		}
	}
	return d.failure
}

func (d *stagedDecision) answerRequest(q request, now time.Time) (refusal *kernel.Error) {
	end := d.tenant.begin("request "+q.Protocol+"#"+q.Action, trace.SpanContext{}, attribute.String("platform.app", q.caller.App), attribute.String("platform.target", q.Target))
	outcome := "ok"
	defer func() {
		if refusal != nil {
			outcome = refusal.Code.String()
		}
		end(outcome)
	}()
	s := q.record.GetSubmission()
	key := fmt.Sprintf("%s:%s#%d", q.caller.App, s.GetIdempotencyKey(), q.n)
	providerKey := key
	if q.Key != "" {
		providerKey = q.Key
	}
	answer := platform.Answer{Tag: q.Tag, Call: q.Target, Action: q.Action, Outcome: "accepted"}
	var ref *pb.EntityRef
	_, err := d.Attempt(func() (*pb.ChangeRecord, *kernel.Error) {
		var record *pb.ChangeRecord
		var refusal *kernel.Error
		ref, record, refusal = d.routingTenant().invoke(q.caller, q.Protocol, q.Action, q.Target,
			platform.Raw(q.Payload), providerKey, s.GetIdempotencyKey(), now, q.Provider)
		return record, refusal
	})
	if err != nil {
		answer.Outcome, answer.Code, answer.Reason = "refused", err.Code.String(), err.Message
		outcome = answer.Code
	} else {
		answer.Ref = ref.GetType() + "/" + ref.GetId()
	}
	payload, errJSON := json.Marshal(answer)
	if errJSON != nil {
		unsupportedStagedEffect()
	}
	c := d.tenant.automated(q.caller, q.caller.App)
	reply := &pb.Submission{TenantId: d.tenant.ID, PrincipalId: c.ID, Authority: s.GetAuthority(), Target: s.GetTarget(),
		Schema: &pb.SchemaRef{Name: q.Reply, Version: 1}, IdempotencyKey: "answer:" + key,
		CorrelationId: s.GetIdempotencyKey(), Payload: payload}
	if _, err := platform.Decide(c, d.tenant.app(c.App), reply, now); err != nil {
		return err
	}
	return d.failure
}

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
	if err := save("settings", t.settings.clone()); err != nil {
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
	view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, Store: t.store(), identity: t.identity, apps: t.apps, appIndex: t.appIndex, owner: t.owner, definitions: t.definitions, records: t.records, procs: t.procs, tasks: t.tasks, linker: t.linker, directory: t.directory, agents: t.agents, ai: t.ai,
		bindings: maps.Clone(t.bindings), connectors: newConnectorRoster(t.services.Kernel), work: newWorkBoard(), works: t.forkWorkState()}
	for _, j := range t.work.jobs {
		copy := *j
		view.work.addJob(&copy)
	}
	t.opsMu.Lock()
	_, marks := t.connectors.kernel.State()
	t.opsMu.Unlock()
	view.connectors.kernel.Restore(nil, marks)
	view.i18n = translator{apps: func() []platform.App { return view.apps }, tenant: view.words.clone}
	view.i18n.installApplications(view.definitions)
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
		t.connectors.kernel = t.services.Kernel.Connectors()
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
		t.settings.restore(values)
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

// The host console owns lifecycle, bounded support authority and migration
// manifests. These images contain no ordinary tenant records or execution state.
type hostControlImage struct {
	Lifecycle        string                        `json:"lifecycle"`
	Support          []SupportGrant                `json:"support"`
	Migrations       []MigrationManifest           `json:"migrations"`
	Requests         map[string]hostControlReceipt `json:"requests"`
	RuntimeSelection map[string]string             `json:"runtimeSelection,omitempty"`
	DisabledOwners   []string                      `json:"disabledOwners,omitempty"`
}
type hostControlReceipt struct {
	RequestHash string       `json:"requestHash"`
	Answer      SupportGrant `json:"answer,omitempty"`
}
type acceptedHostControl struct {
	Authorizations []resultformat.AuthorizationEvidence `json:"authorizations,omitempty"`
	Version        int                                  `json:"version"`
	Kind           string                               `json:"kind"`
	Tenant         string                               `json:"tenant"`
	App            string                               `json:"app"`
	Member         string                               `json:"member"`
	Key            string                               `json:"key"`
	At             time.Time                            `json:"at"`
	Action         string                               `json:"action"`
	Request        json.RawMessage                      `json:"request"`
	RequestHash    string                               `json:"requestHash"`
	Digest         string                               `json:"digest"`
	Before         string                               `json:"before"`
	AuditBefore    string                               `json:"auditBefore"`
	Image          hostControlImage                     `json:"image"`
	Audit          AuditEntry                           `json:"audit"`
	Answer         SupportGrant                         `json:"answer,omitempty"`
}

func (h *hostControl) image() (hostControlImage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Encode the complete owned shape to isolate nested migration row diagnostics.
	raw, err := json.Marshal(hostControlImage{Lifecycle: h.state(), Support: h.support, Migrations: h.migrations, Requests: h.requests, RuntimeSelection: h.runtimeSelection, DisabledOwners: h.disabledOwners})
	var image hostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &image)
	}
	return image, err
}
func validateHostImage(tenant string, image hostControlImage) error {
	owners := map[string]bool{}
	for _, id := range image.DisabledOwners {
		if id == "" || len(id) > 128 || owners[id] {
			return fmt.Errorf("invalid inactive owner")
		}
		owners[id] = true
	}
	if image.RuntimeSelection != nil {
		if len(image.RuntimeSelection) != 3 {
			return fmt.Errorf("invalid runtime selection")
		}
		for _, id := range []string{"platform.rules", "platform.authorization", "platform.kernel"} {
			if _, found := image.RuntimeSelection[id]; !found {
				return fmt.Errorf("missing runtime contract %s", id)
			}
		}
	}
	if !slices.Contains([]string{"", "open", "suspend", "decommission", "suspended", "decommissioned"}, image.Lifecycle) {
		return fmt.Errorf("invalid host lifecycle")
	}
	ids := map[string]bool{}
	for _, grant := range image.Support {
		if grant.ID == "" || ids[grant.ID] || grant.Tenant != tenant || grant.Member == "" || grant.Reason == "" || grant.Opened == "" || grant.Expires.IsZero() || grant.Uses < 0 {
			return fmt.Errorf("invalid host support grant")
		}
		ids[grant.ID] = true
	}
	for _, m := range image.Migrations {
		if m.At.IsZero() || m.Key == "" || m.From == "" || m.To != tenant || m.From == m.To || m.Member == "" {
			return fmt.Errorf("invalid host migration manifest")
		}
		for _, part := range m.Types {
			if part.Type == "" || part.Rows < 0 || part.Written < 0 || part.Written > part.Rows {
				return fmt.Errorf("invalid host migration counts")
			}
		}
	}
	for key, receipt := range image.Requests {
		tag, _, found := strings.Cut(strings.TrimPrefix(key, "host:"), ":")
		if !strings.HasPrefix(key, "host:") || !found || !slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration", "composition"}, tag) || len(receipt.RequestHash) != 64 || !strings.HasSuffix(key, ":"+receipt.RequestHash) {
			return fmt.Errorf("invalid host receipt")
		}
		if tag == "support.open" || tag == "support.use" {
			i := slices.IndexFunc(image.Support, func(g SupportGrant) bool { return g.ID == receipt.Answer.ID })
			if i < 0 {
				return fmt.Errorf("host receipt names no support grant")
			}
			current := image.Support[i]
			if receipt.Answer.Uses < 0 || receipt.Answer.Uses > current.Uses {
				return fmt.Errorf("host receipt has an invalid support count")
			}
			current.Uses = receipt.Answer.Uses
			if current != receipt.Answer {
				return fmt.Errorf("host receipt rewrites its support authority")
			}
		} else if receipt.Answer != (SupportGrant{}) {
			return fmt.Errorf("host receipt has an unrelated answer")
		}
	}
	return nil
}
func hostRequestHash(r acceptedHostControl) (string, error) {
	return acceptance.Digest([]any{r.Tenant, r.Member, r.Action, r.Request, r.At})
}
func hostResultDigest(r acceptedHostControl) (string, error) {
	r.Digest = ""
	return acceptance.Digest(r)
}
func decodeAcceptedHostControl(raw []byte) (acceptedHostControl, error) {
	var r acceptedHostControl
	if len(raw) == 0 || len(raw) > acceptance.MaxResultBytes {
		return r, fmt.Errorf("host result is empty or too large")
	}
	if err := acceptance.DecodeJSON(raw, &r, "host result"); err != nil {
		return r, err
	}
	if err := resultformat.ValidateAuthorizationEvidence(r.Tenant, r.Authorizations); err != nil {
		return r, err
	}
	hash, hashErr := hostRequestHash(r)
	digest, digestErr := hostResultDigest(r)
	if hashErr != nil || digestErr != nil || r.Version != 1 || r.Kind != "host-result" || r.Tenant == "" || r.App != PlatformApp || r.Member == "" || r.At.IsZero() ||
		!slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration", "composition"}, r.Action) || r.Key != "host:"+r.Action+":"+hash || r.RequestHash != hash || r.Digest != digest || r.Before == "" || r.AuditBefore == "" ||
		r.Audit.Member != r.Member || r.Audit.App != PlatformApp || !r.Audit.At.Equal(r.At) || r.Audit.Action == "" {
		return r, fmt.Errorf("host result identity differs")
	}
	if err := validateHostImage(r.Tenant, r.Image); err != nil {
		return r, err
	}
	receipt, ok := r.Image.Requests[r.Key]
	if !ok || receipt.RequestHash != hash || receipt.Answer != r.Answer {
		return r, fmt.Errorf("host result has no matching receipt")
	}
	return r, nil
}

// acceptHostControl holds Tenant.mu from private image capture through append
// and application. The accepted clock is part of identity because these APIs
// predate caller supplied keys; repeating that same request returns its receipt.
func (t *Tenant) acceptHostControl(action, member string, request any, now time.Time, audit AuditEntry, decide func(*hostControlImage) (SupportGrant, error), evidence ...resultformat.AuthorizationEvidence) (SupportGrant, error) {
	r := acceptedHostControl{Version: 1, Kind: "host-result", Tenant: t.ID, App: PlatformApp, Member: member, Action: action, At: now.UTC(), Request: platform.Raw(request)}
	r.Authorizations = slices.Clone(evidence)
	var err error
	r.RequestHash, err = hostRequestHash(r)
	if err != nil {
		return SupportGrant{}, err
	}
	r.Key = "host:" + action + ":" + r.RequestHash
	image, err := t.console.image()
	if err != nil {
		return SupportGrant{}, err
	}
	if prior, ok := image.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash {
			return SupportGrant{}, fmt.Errorf("host request key changed")
		}
		return prior.Answer, nil
	}
	r.Before, err = acceptance.Digest(image)
	if err != nil {
		return SupportGrant{}, err
	}
	r.AuditBefore, err = acceptance.Digest(t.audit.all())
	if err != nil {
		return SupportGrant{}, err
	}
	r.Answer, err = decide(&image)
	if err != nil {
		return SupportGrant{}, err
	}
	if image.Requests == nil {
		image.Requests = map[string]hostControlReceipt{}
	}
	image.Requests[r.Key] = hostControlReceipt{r.RequestHash, r.Answer}
	r.Image = image
	audit.At, audit.Member, audit.App = r.At, member, PlatformApp
	r.Audit = audit
	r.Digest, err = hostResultDigest(r)
	if err != nil {
		return SupportGrant{}, err
	}
	raw, err := json.Marshal(r)
	if err != nil {
		return SupportGrant{}, err
	}
	if _, err = decodeAcceptedHostControl(raw); err != nil {
		return SupportGrant{}, err
	}
	principal, _ := json.Marshal(platform.Member{ID: member, Tenant: t.ID})
	committed, err := t.acceptResult(Entry{App: PlatformApp, Kind: "accepted-result", Principal: principal, Body: raw, At: r.At}, r.Key, r.RequestHash)
	if err != nil {
		return SupportGrant{}, err
	}
	saved, err := decodeAcceptedHostControl(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed host result differs: %v", err))
		return SupportGrant{}, fmt.Errorf("committed host result differs: %v", err)
	}
	if _, err = t.applyAcceptedHostControl(committed); err != nil {
		t.quarantine(err)
		return SupportGrant{}, err
	}
	t.acceptedResultApplied()
	t.changed()
	return saved.Answer, nil
}
func (t *Tenant) applyAcceptedHostControl(raw []byte) (acceptedHostControl, error) {
	r, err := decodeAcceptedHostControl(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID {
		return r, fmt.Errorf("host result belongs to another tenant")
	}
	current, err := t.console.image()
	if err != nil {
		return r, err
	}
	if prior, ok := current.Requests[r.Key]; ok {
		if prior.RequestHash != r.RequestHash || prior.Answer != r.Answer {
			return r, fmt.Errorf("host result receipt differs")
		}
		return r, nil
	}
	before, err := acceptance.Digest(current)
	auditBefore, auditErr := acceptance.Digest(t.audit.all())
	if err != nil || auditErr != nil || before != r.Before || auditBefore != r.AuditBefore {
		return r, fmt.Errorf("host result predecessor differs")
	}
	if err := validateHostTransition(current, r); err != nil {
		return r, err
	}
	t.console.mu.Lock()
	t.console.setState(r.Image.Lifecycle)
	t.console.support, t.console.migrations, t.console.requests = r.Image.Support, r.Image.Migrations, maps.Clone(r.Image.Requests)
	t.console.runtimeSelection = maps.Clone(r.Image.RuntimeSelection)
	t.console.disabledOwners = slices.Clone(r.Image.DisabledOwners)
	t.console.mu.Unlock()
	t.audit.remember(r.Audit)
	return r, nil
}

// A saved image may only perform the one requested host-owned transition. This
// checks frozen bytes and predecessors; it does not rerun current authorization.
func validateHostTransition(current hostControlImage, r acceptedHostControl) error {
	raw, err := json.Marshal(current)
	var expected hostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &expected)
	}
	if err != nil {
		return err
	}
	var answer SupportGrant
	audit := AuditEntry{At: r.At, Member: r.Member, App: PlatformApp}
	switch r.Action {
	case "composition":
		var body CompositionRequest
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if body.Reason == "" || body.At.IsZero() || !body.At.Equal(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("invalid runtime maintenance request")
		}
		expected.RuntimeSelection = maps.Clone(body.Selection)
		expected.DisabledOwners = slices.Clone(body.DisabledOwners)
		expected.Lifecycle = "suspend"
		audit.Action, audit.Target = "host.composition", r.Tenant+":"+body.Reason
	case "lifecycle":
		var body struct{ Action, Reason string }
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if !slices.Contains([]string{"suspend", "decommission", "resume", "open"}, body.Action) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host lifecycle request differs")
		}
		expected.Lifecycle = ""
		if body.Action == "suspend" || body.Action == "decommission" {
			expected.Lifecycle = body.Action
		}
		audit.Action, audit.Target = "host.lifecycle."+body.Action, r.Tenant+":"+body.Reason
	case "support.open":
		var body struct {
			Member, Reason string
			Minutes        int
		}
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		if body.Member == "" || body.Reason == "" || body.Minutes < 1 || body.Minutes > 1440 || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support request differs")
		}
		answer = r.Answer
		if answer.Tenant != r.Tenant || answer.Member != body.Member || answer.Reason != body.Reason || answer.Opened != strings.TrimPrefix(r.Member, "host:") || answer.Uses != 0 || !answer.Expires.Equal(r.At.Add(time.Duration(body.Minutes)*time.Minute)) {
			return fmt.Errorf("host support grant differs from its accepted request")
		}
		expected.Support = append(expected.Support, answer)
		audit.Action, audit.Target = "host.support.open", body.Member+":"+body.Reason
	case "support.use":
		var body struct{ ID string }
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		i := slices.IndexFunc(expected.Support, func(g SupportGrant) bool { return g.ID == body.ID })
		if i < 0 || !expected.Support[i].Expires.After(r.At) || !strings.HasPrefix(r.Member, "host:") {
			return fmt.Errorf("host support use predecessor differs")
		}
		expected.Support[i].Uses++
		answer = expected.Support[i]
		audit.Action, audit.Target = "host.support.use", body.ID
	case "migration":
		var body MigrationResult
		if err := decodeControl(r.Request, &body); err != nil {
			return err
		}
		expected.Migrations = append(expected.Migrations, MigrationManifest{At: r.At, From: body.From, To: body.To, Key: body.Key, Member: r.Member, Types: body.Types})
		audit.Action, audit.Target = "host.migration", body.Key
	}
	if expected.Requests == nil {
		expected.Requests = map[string]hostControlReceipt{}
	}
	expected.Requests[r.Key] = hostControlReceipt{r.RequestHash, answer}
	want, err := acceptance.Digest(expected)
	got, gotErr := acceptance.Digest(r.Image)
	wantAudit, auditErr := acceptance.Digest(audit)
	gotAudit, gotAuditErr := acceptance.Digest(r.Audit)
	if err != nil || gotErr != nil || auditErr != nil || gotAuditErr != nil || want != got || answer != r.Answer || wantAudit != gotAudit {
		return fmt.Errorf("host result changes unrelated state or audit")
	}
	return nil
}

// resultformat.Release commits immutable candidate bytes or an activation. Format 2
// carries frozen owner publication images beside the pointer; neither is
// reconstructed from a later mutable draft.

func (t *Tenant) applyAcceptedRelease(raw []byte) (resultformat.Release, error) {
	saved, err := resultformat.DecodeRelease(raw)
	if err != nil {
		return saved, err
	}
	if saved.Tenant != t.ID || t.app(construction.ID) == nil {
		return saved, fmt.Errorf("release result belongs to another tenant or unavailable builder")
	}
	if prior := t.releases.raw(saved.CandidateID); prior != nil && !bytes.Equal(prior, saved.Bytes) {
		return saved, fmt.Errorf("immutable release candidate %s changed", saved.CandidateID)
	}
	if prior, ok := t.releases.appliedDigest(saved.Key); ok {
		if prior != saved.Digest {
			return saved, fmt.Errorf("release result idempotency key changed")
		}
		return saved, nil
	}
	if len(saved.Installations) > 0 {
		records := t.records
		var bootstrap *preparedAcceptedBatch
		if len(saved.Bootstrap) > 0 {
			bootstrap, err = t.prepareAcceptedBatch(t.app(saved.App).AcceptedLedger(), saved.Bootstrap)
			if err != nil || bootstrap == nil {
				return saved, fmt.Errorf("release bootstrap cannot prepare: %v", err)
			}
			records = bootstrap.records
		}
		view, err := t.assetInstallationView(records)
		if err != nil {
			return saved, err
		}
		view.records.readRelease = saved.CandidateID
		draft, err := view.stageReleaseInstallationLocked(saved.Installations, false)
		if err != nil {
			return saved, err
		}
		if saved.Version == 4 {
			candidate, err := platform.ReadCandidate(saved.CandidateID, saved.Bytes)
			if err != nil {
				return saved, err
			}
			running, err := draft.acceptedInstallationReleaseLocked(candidate)
			if err != nil || running.ID != candidate.ID {
				return saved, fmt.Errorf("accepted installation differs from its frozen candidate: %v", err)
			}
		}
		if err := t.anchorReleaseRecords(draft.records); err != nil {
			return saved, err
		}
		if bootstrap != nil {
			bootstrap.records = draft.records
			if _, err := t.promoteAcceptedBatch(bootstrap); err != nil {
				return saved, err
			}
		} else if err := t.records.promoteRecords(draft.records); err != nil {
			return saved, err
		}
		t.setDefinitions(draft.definitions)
		for key, app := range draft.owner {
			draft.owner[key] = t.app(app.Manifest().ID)
		}
		t.owner = draft.owner
		registry, err := t.assetRegistryLocked()
		if err != nil {
			return saved, err
		}
		for _, installation := range saved.Installations {
			plan, err := t.installationPlan(registry, installation)
			if err != nil {
				return saved, err
			}
			if err := registry.ApplyInstallation(plan, platform.AssetApplyContext{}); err != nil {
				return saved, err
			}
		}
		if bootstrap != nil {
			t.publishAcceptedBatch(bootstrap.result)
			t.enqueue(saved.At)
		}
	}
	if err := t.releases.commit(saved.Key, saved.Digest, saved.CandidateID, saved.Bytes, saved.Active); err != nil {
		return saved, err
	}
	if saved.Active {
		t.records.readRelease = saved.CandidateID
	}
	t.releases.rememberRequest(saved.Key, saved.RequestHash)
	if saved.Version >= 3 {
		t.committed.saveAnswer("release:"+saved.Key, raw)
	}
	if saved.From != "" {
		t.releases.rememberOrigin(saved.CandidateID, ReleaseOrigin{From: saved.From, Member: saved.Member, At: saved.At})
		t.audit.remember(AuditEntry{At: saved.At, Member: saved.Member, App: PlatformApp, Action: "host.promotion",
			Target: saved.From + "→" + t.ID + ":" + saved.CandidateID})
	}
	return saved, nil
}

// SaveReleaseCandidate freezes a previously previewed saved draft. The
// builder's private definitions are read under the tenant lock; a stale
// preview cannot be persisted by submitting its ID alone. A retry of an
// already-saved ID remains possible even after the editor changes the draft.
func (t *Tenant) SaveReleaseCandidate(m platform.Member, kind platform.AssetKind, draftID, candidateID, key string, now time.Time) (string, error) {
	return t.SaveReleaseCandidates(m, []construction.JointDraftRef{{Kind: kind, ID: draftID}}, candidateID, key, now)
}

// SaveReleaseCandidates persists one or several saved drafts as one immutable
// candidate. The bytes are recomputed under the tenant lock, so a joint
// selection cannot be frozen from a stale browser image.
func (t *Tenant) SaveReleaseCandidates(m platform.Member, drafts []construction.JointDraftRef, candidateID, key string, now time.Time) (string, error) {
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, construction.Builder, construction.Publisher) {
		return "", fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return "", fmt.Errorf("candidate ID and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	var candidateBytes []byte
	if prior := t.releases.raw(candidateID); prior != nil {
		candidateBytes = slices.Clone(prior)
	} else {
		preview, candidate, err := t.previewReleaseLocked(drafts, m)
		if err != nil {
			return "", err
		}
		if preview.Diagnostic != "" {
			return "", fmt.Errorf("release candidate: %s", preview.Diagnostic)
		}
		if candidate.ID != candidateID {
			return "", fmt.Errorf("release candidate changed since preview")
		}
		candidateBytes = candidate.Bytes
	}
	saved := resultformat.Release{Version: 1, Kind: "release-result", Tenant: t.ID, App: construction.ID,
		Member: m.ID, Key: "release:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: candidateBytes}
	// The idempotency key has a separate namespace from builder submissions.
	var err error
	saved.RequestHash, err = resultformat.ReleaseRequestHash(t.ID, m.ID, saved.Key, candidateID, false)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

// ActivateRelease installs the supported saved closure and moves its pointer
// in one committed result. Unsupported upgrades and changed code
// dependencies are refused before either state is exposed.
func (t *Tenant) ActivateRelease(m platform.Member, candidateID, key string, now time.Time) (string, error) {
	return t.ActivateReleaseWithUpgrade(m, candidateID, key, "", now)
}

const systemReleaseActivationDiagnostic = "System definitions activate through an approved Platform change. Publish business applications in a business workspace."

func (t *Tenant) ActivateReleaseWithUpgrade(m platform.Member, candidateID, key, upgradeID string, now time.Time) (string, error) {
	if t.ID == systemscope.Tenant {
		return "", fmt.Errorf("%s", systemReleaseActivationDiagnostic)
	}
	if err := t.admits(m); err != nil {
		return "", err
	}
	if !holdsIndependentBuildRole(m, construction.Builder, construction.Publisher) {
		return "", fmt.Errorf("builder or publisher role required")
	}
	if candidateID == "" || key == "" || len(key) > 200 || now.IsZero() {
		return "", fmt.Errorf("candidate ID and idempotency key are required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return "", fmt.Errorf("tenant is quarantined")
	}
	if prior := t.committed.answer("release:activate:" + key); prior != nil {
		saved, err := resultformat.DecodeRelease(prior)
		hash, hashErr := resultformat.ReleaseRequestHash(t.ID, m.ID, "activate:"+key, candidateID, true, upgradeID)
		if err != nil || hashErr != nil || hash != saved.RequestHash {
			return "", fmt.Errorf("activation key belongs to another request")
		}
		return saved.CandidateID, nil
	}
	raw := t.releases.raw(candidateID)
	if raw == nil {
		return "", fmt.Errorf("release candidate %s is not saved", candidateID)
	}
	var installations []resultformat.ReleaseInstallation
	var bootstrap json.RawMessage
	if upgradeID != "" {
		candidate, err := platform.ReadCandidate(candidateID, raw)
		if err != nil {
			return "", err
		}
		plan, err := t.releaseUpgradePlanLocked(candidate, m)
		if err != nil {
			return "", err
		}
		if plan == nil || plan.ID != upgradeID {
			return "", fmt.Errorf("storage upgrade plan changed; refresh and review it again")
		}
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return "", err
	}
	installations, bootstrap, err = t.prepareReleaseLifecycleLocked(candidate, m, now, upgradeID != "")
	if err != nil {
		return "", err
	}
	saved := resultformat.Release{Version: 1, Kind: "release-result", Tenant: t.ID, App: construction.ID,
		Member: m.ID, Key: "activate:" + key, At: now.UTC(), CandidateID: candidateID,
		Bytes: slices.Clone(raw), Active: true, Installations: installations, Bootstrap: bootstrap}
	if len(installations) > 0 {
		saved.Version = 4
	}
	if upgradeID != "" {
		saved.Version, saved.UpgradeID = 4, upgradeID
	}
	saved.RequestHash, err = resultformat.ReleaseRequestHash(t.ID, m.ID, saved.Key, candidateID, true, upgradeID)
	if err != nil {
		return "", err
	}
	return t.commitReleaseLocked(m, saved)
}

func (t *Tenant) runningMatchesLocked(candidateID string, raw []byte) error {
	saved, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	running, err := t.runningReleaseLocked(saved)
	if err != nil {
		return fmt.Errorf("release differs from the running definitions: %w", err)
	}
	if running.ID == candidateID {
		return nil
	}
	added, removed, changed, err := platform.CandidateDiff(saved, running)
	if err != nil {
		return err
	}
	return fmt.Errorf("release differs from the running definitions: changed %v, missing %v, extra %v", changed, removed, added)
}

// Read-side status and activation compare the same owner-resolved closure.
func (t *Tenant) runningReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	// Rebuild the same closure from what runs now: equal content yields the
	// same canonical ID; anything else is reported by asset path.
	roots := make([]platform.AssetRef, 0, len(saved.Assets))
	installed := make(map[platform.AssetRef]bool, len(available))
	for _, asset := range available {
		installed[asset.Ref] = true
	}
	for _, asset := range saved.Assets {
		// A first release has no running counterpart for its new assets.
		// Keep them absent from the comparison so CandidateDiff reports them
		// as additions; missing dependencies of installed roots still refuse.
		if installed[asset.Ref] {
			roots = append(roots, asset.Ref)
		}
	}
	if len(roots) == 0 {
		return platform.ReleaseCandidate{}, nil
	}
	return t.candidateWithBindings(roots, available, nil)
}

// Recovery checks the saved installation's controlled assets, not today's
// native prerequisites. Native assets in a release are declarations, never
// installation images: keep their original bytes for this comparison only.
// New activation still requires runtimeAssetOwner.controlled's exact match;
// old Flow execution keeps its separate code-dependency admission gate.
func (t *Tenant) acceptedInstallationReleaseLocked(saved platform.ReleaseCandidate) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	code := map[platform.AssetRef]bool{}
	for _, app := range t.apps {
		assets, err := t.codeReleaseAssetsLocked(app)
		if err != nil {
			return platform.ReleaseCandidate{}, err
		}
		for _, asset := range assets {
			code[asset.Ref] = true
		}
	}
	frozen := map[platform.AssetRef]platform.ReleaseAsset{}
	roots := []platform.AssetRef{}
	for _, asset := range saved.Assets {
		roots = append(roots, asset.Ref)
		if code[asset.Ref] {
			frozen[asset.Ref] = asset
		}
	}
	for index, asset := range available {
		if original, exists := frozen[asset.Ref]; exists {
			available[index] = original
		}
	}
	return t.candidateWithBindings(roots, available, nil)
}

// pendingWorkFitsLocked refuses an activation that would change the action a
// pending approval holds: the approval must run under the release it opened
// in. There is no migration; decide or withdraw it first.
func (t *Tenant) pendingWorkFitsLocked(candidateID string, raw []byte) error {
	if t.app(platform.WorkApp) == nil {
		return nil
	}
	candidate, err := platform.ReadCandidate(candidateID, raw)
	if err != nil {
		return err
	}
	actionIn := func(c platform.ReleaseCandidate, name string) []byte {
		for _, a := range c.Assets {
			if a.Ref.Kind == platform.AssetAction && a.Ref.Name == name {
				out, _ := json.Marshal(a)
				return out
			}
		}
		return nil
	}
	pending, _, _ := platform.Find[platform.ApprovalRequest](t.automation(platform.WorkApp, false),
		platform.Query{Domain: json.RawMessage(`[["state","=","pending"]]`), Sort: []string{"id"}})
	for _, request := range pending {
		next := actionIn(candidate, request.Action)
		if next == nil || request.Release == candidateID {
			continue
		}
		if saved := t.releases.raw(request.Release); saved != nil {
			started, err := platform.ReadCandidate(request.Release, saved)
			if err == nil && bytes.Equal(actionIn(started, request.Action), next) {
				continue
			}
		}
		return fmt.Errorf("pending approval %s holds %s under another release; decide or withdraw it before activating", request.ID, request.Action)
	}
	return nil
}

// ActiveRelease is the tenant's active release ID, empty before any activation.
func (t *Tenant) ActiveRelease() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releases.active
}

// commitReleaseLocked appends one release result and applies only what the
// journal returned; the caller holds the tenant lock.
func (t *Tenant) commitReleaseLocked(m platform.Member, saved resultformat.Release) (string, error) {
	candidateID := saved.CandidateID
	raw, err := resultformat.EncodeRelease(saved)
	if err != nil {
		return "", err
	}
	principal, err := json.Marshal(m)
	if err != nil {
		return "", err
	}
	entry := Entry{App: construction.ID, Kind: "accepted-result", Principal: principal, Body: raw, At: saved.At}
	if prior, ok := t.releases.request(saved.Key); ok {
		if prior != saved.RequestHash {
			return "", fmt.Errorf("release key %s belongs to another request", saved.Key)
		}
		return candidateID, nil
	}
	if _, applied := t.releases.appliedDigest(saved.Key); applied && t.AcceptResult == nil {
		return "", fmt.Errorf("historical release request needs its journal receipt")
	}
	committed, err := t.acceptResult(entry, saved.Key, saved.RequestHash)
	if err != nil {
		return "", err
	}
	return t.finishCommittedRelease(m, saved, committed)
}

func (t *Tenant) finishCommittedRelease(m platform.Member, saved resultformat.Release, committed []byte) (id string, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("apply committed release: %v", failure)
			t.quarantine(err)
			id = ""
		}
	}()
	candidateID := saved.CandidateID
	applied, err := t.applyAcceptedRelease(committed)
	if err != nil || applied.Key != saved.Key || applied.RequestHash != saved.RequestHash ||
		applied.Member != m.ID || applied.CandidateID != candidateID {
		// Once appended, an invalid result is a tenant recovery fault, not a
		// transient error that may permit further writes over uncertain state.
		t.quarantine(fmt.Errorf("committed release result differs: %v", err))
		return "", fmt.Errorf("committed release result differs: %v", err)
	}
	t.acceptedResultApplied()
	t.changed()
	return applied.CandidateID, nil
}
