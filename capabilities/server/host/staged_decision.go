package host

import (
	"encoding/json"
	"errors"
	"maps"
	"platformserver/hostroles"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/kernel/transport"
	"platformserver/services/actions"
	automation "platformserver/services/automate/runtime"
	"platformserver/services/objects"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"

	"platformserver/kernel/records"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	"platformserver/platform"
)

type stagedDecision struct {
	runtimekernel.Decision
	sourceProjection       *runtimekernel.ClaimSet
	confirmationCompleting *confirmationCompletion
	control                *runtimekernel.ControlStage[*Tenant]
	tenant                 *Tenant
	requests               []actions.PendingRequest
	operationInputs        map[string][]*preparedOperationInput
	privateRoles           hostroles.PrivateRoles
}

func (d *stagedDecision) StagedDecision() platform.StagedDecision              { return d }
func (d *stagedDecision) ReadScoped() bool                                     { return true }
func (d *stagedDecision) DecisionCatalog(platform.Authority) *platform.Catalog { return nil }

func (t *Tenant) newStagedDecision() *stagedDecision {
	return t.newStagedDecisionOn(t.Projection.ForkRecords())
}

func (t *Tenant) newStagedDecisionOn(base *records.Store) *stagedDecision {
	decision := runtimekernel.NewDecision(base, &t.Sequence, t.simulation)
	decision.BusinessQuery = objects.BusinessQuery
	decision.Rules = runtimekernel.ConstraintRules{Definitions: func() []platform.Definition { return t.definitions }, RuleQuery: hostView{t: t}.RuleQuery, Binding: t.recordSetBinding,
		Trace: func() func(platform.DecisionTrace) { return t.simulationTrace }}
	decision.DirectRows = func(rows []acceptedformat.InputRow, member string, at time.Time) error {
		_, err := t.prepareAcceptedDirectRows(rows, member, at)
		return err
	}
	staged := &stagedDecision{tenant: t, Decision: decision}
	if t.Directory != nil {
		staged.DirectoryOf = staged.directoryView
	}
	return staged
}

func (d *stagedDecision) Decide(c platform.Caller, app platform.Service, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *platform.Error) {
	app = d.stateApp(app)
	if d.Simulation {
		if candidate, err := d.tenant.releases.Candidate(d.tenant.releases.Active()); err == nil {
			return d.decideAppFrom(c, app, s, at, d.routingTenant(), candidate.ID, candidate.Assets)
		}
	}
	return d.decideAppFrom(c, app, s, at, d.routingTenant(), d.tenant.releases.Active())
}

func (d *stagedDecision) decideAppFrom(c platform.Caller, app platform.Service, s *pb.Submission, at time.Time, view *Tenant, release string, retained ...[]platform.ReleaseAsset) (*pb.ChangeRecord, *platform.Error) {
	r, err, ok := d.DecideApp(c, app, s, at, func() (*platform.DecisionDefinition, error) {
		return view.decisionDefinition(app.Manifest().ID, s, release, retained...)
	})
	if !ok {
		unsupportedStagedEffect()
	}
	return r, err
}

func (d *stagedDecision) Publish(c platform.Caller, record *pb.ChangeRecord) {
	if !d.PublishRecord(d.tenant.App(c.App), c, record, func(schema string, raw []byte) error {
		return d.tenant.installPublicationLinkConstraint(d.Records, schema, raw)
	}) {
		unsupportedStagedEffect()
	}
}

func (d *stagedDecision) Readable(c platform.Caller, ref string) bool {
	return d.ReadableAt(func(store *records.Store, at time.Time) bool { return d.tenant.readableAs(store, c, ref, at) })
}

var errStagedEffect = errors.New("staged effect is unavailable")

func unsupportedStagedEffect() { panic(errStagedEffect) }

func (d *stagedDecision) Probe(c platform.Caller, protocol, action, id string, payload []byte, at time.Time) *platform.Error {
	return d.routingTenant().router().Probe(c, protocol, action, id, payload, at)
}
func (d *stagedDecision) Request(c platform.Caller, r *pb.ChangeRecord, q platform.Request) {
	if r.GetSubmission().GetTenantId() != d.tenant.ID || r.GetSubmission().GetAuthority() != c.App {
		unsupportedStagedEffect()
	}
	if q.Protocol == "" {
		d.Intents = append(d.Intents, d.routingTenant().gatewayRuntime().Plan(c, r, q, d.Intents))
		return
	}
	q.Payload = slices.Clone(platform.Raw(q.Payload))
	d.requests = append(d.requests, actions.PendingRequest{Request: q, Caller: c,
		Record: proto.Clone(r).(*pb.ChangeRecord), N: d.NextRequest(c.App + "/" + r.GetChangeId())})
}
func (d *stagedDecision) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *platform.Error) {
	return d.routingTenant().router().Query(c, protocol, read, provider...)
}
func (d *stagedDecision) Setting(c platform.Caller, name string) string {
	return d.routingTenant().setting(c, name)
}
func (d *stagedDecision) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *platform.Error) {
	planned, err := d.routingTenant().planAppEffects(c, kind, key, entity, data, now, d.Intents)
	if err != nil {
		d.FailDecision(err)
		return 0, err
	}
	d.Intents = append(d.Intents, planned...)
	d.tenant.askHeldEffects(c, kind, entity, planned, now)
	return len(planned), nil
}
func (d *stagedDecision) CheckEnterpriseUsage(c platform.Caller, id, until string, deleting bool, now time.Time) *platform.Error {
	original := d.routingTenant()
	apps := make([]platform.Service, len(original.apps))
	for i, app := range original.apps {
		apps[i] = d.stateApp(app)
	}
	view := &Tenant{services: original.services, disabledOwners: original.disabledOwners, ID: original.ID, ownerSet: original.with(apps), ownerRoles: original.ownerRoles,
		Projection: d.Records, Secrets: original.Secrets, releases: original.releases}
	view.settings.Mirror(&original.settings)
	return view.checkEnterpriseUsage(c, d.Records, id, until, deleting, now)
}
func (d *stagedDecision) CheckEnterpriseTenantConnection(c platform.Caller, id string, now time.Time) *platform.Error {
	return d.routingTenant().checkTenantConnection(c, id, now)
}

func (d *stagedDecision) Links(c platform.Caller, entity string) []string {
	var out []string
	if d.tenant.Linker != nil {
		out = d.stateApp(d.tenant.Linker.(platform.Service)).(platform.Linker).Links(c, entity)
	}
	return records.AppendRelationNeighbors(out, c.OwnedRelations(entity), entity)
}
func (d *stagedDecision) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *platform.Error {
	if d.tenant.Linker == nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	owner := d.stateApp(d.tenant.Linker.(platform.Service))
	err := owner.(platform.Linker).Link(c, from, to, key, now)
	d.FailDecision(err)
	return err
}
func (d *stagedDecision) Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *platform.Error {
	if d.tenant.Tasks == nil {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	err := d.tenant.Tasks.Assign(c, r, a)
	d.FailDecision(err)
	return err
}
func (d *stagedDecision) Next(c platform.Caller, rec *pb.ChangeRecord, name string, date time.Time) (string, *platform.Error) {
	return d.AllocateNumber(c, d.tenant.ID, d.tenant.App, rec, name, date)
}

func (d *stagedDecision) BoundProvider(c platform.Caller, protocol string) (string, *platform.Error) {
	return d.routingTenant().router().BoundProvider(c, protocol)
}

func (d *stagedDecision) directoryView() platform.Directory {
	if d.tenant.DirectoryOwner == "" {
		return nil
	}
	return d.privateRoles.Directories[d.stateApp(d.tenant.App(d.tenant.DirectoryOwner)).Manifest().ID]
}

func (d *stagedDecision) stateApp(app platform.Service) platform.Service {
	fork, ok := d.PrivateApp(app, d.bindPrivate)
	if !ok {
		unsupportedStagedEffect()
	}
	return fork
}

func (d *stagedDecision) bindPrivate(app platform.Service) {
	(hostView{t: d.tenant, app: app, decision: d}).bindOwner(app)
	d.privateRoles.Add(app)
}

func (d *stagedDecision) attachPrivateHosts() {
	for _, app := range d.PrivateApps {
		d.bindPrivate(app)
	}
}

func (d *stagedDecision) Attempt(decide func() (*pb.ChangeRecord, *platform.Error)) (*pb.ChangeRecord, *platform.Error) {
	events := len(d.Events)
	return runtimekernel.Attempt(d, d.privateFork, d.attachPrivateHosts, decide, func(r *pb.ChangeRecord) *platform.Error { return d.CheckAttempt(r, events) })
}

func (d *stagedDecision) privateFork() *stagedDecision {
	fork := *d
	forked, ok := d.Decision.Fork(d.tenant.App)
	if !ok {
		unsupportedStagedEffect()
	}
	fork.Decision = forked
	fork.privateRoles = hostroles.PrivateRoles{}
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
	}
	fork.requests = slices.Clone(d.requests)
	for id, app := range fork.PrivateApps {
		if !d.Shared(id, app) {
			fork.bindPrivate(app)
		}
	}
	fork.forkControlDecision(d.control)
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *platform.Error) *platform.Error {
	probe := d.privateFork()
	err := probe.runProbe(c, decide)
	if err != nil {
		d.AdoptRefusal(&probe.Decision, err)
	}
	return err
}

func (d *stagedDecision) runProbe(c platform.Caller, decide func(platform.Caller) *platform.Error) *platform.Error {
	return d.RunProbe(d, c, decide, func(failure any) bool { return failure == errStagedEffect })
}

func (d *stagedDecision) stageObservers() *platform.Error {
	refusal, ok := d.StageObservers(runtimekernel.ObserverHooks{Apps: d.tenant.LiveApps, Private: d.stateApp,
		Caller: d.appCaller,
		Events: func(e platform.Event) []string { return d.tenant.router().Events(e) }})
	if !ok {
		unsupportedStagedEffect()
	}
	return refusal
}

func (d *stagedDecision) sources() runtimekernel.ResultSources {
	return runtimekernel.ResultSources{Tenant: d.tenant.ID, App: d.tenant.App, Control: d.savedControl, Controlled: d.control != nil, Pending: len(d.requests) > 0,
		Observers: len(d.tenant.Observers) > 0, Operations: func() ([]acceptedformat.OperationCancellation, error) {
			return d.tenant.operationCancellations().Saved(d.OperationCancels)
		}, Plan: d.routingTenant().planAcceptedEvent,
		Pure: build.CallbackFreeTransition}
}

func (d *stagedDecision) approvals() runtimekernel.ApprovalRequests {
	return runtimekernel.ApprovalRequests{Tenant: d.tenant.ID, Runtime: d, App: d.tenant.App, Decide: d.Decide, Probe: d.ProbeDecision}
}

func (d *stagedDecision) RequestApproval(c platform.Caller, app string, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return d.approvals().Request(d.ProbeOnly, c, app, s, now)
}

func (d *stagedDecision) batchResult(app string, request *pb.Submission, receipt *pb.ChangeRecord, at time.Time) ([]byte, error) {
	return d.BatchResult(d.sources(), app, request, receipt, at)
}

func (t *Tenant) captureControl() (map[string]json.RawMessage, error) {
	t.opsMu.Lock()
	defer t.opsMu.Unlock()
	return t.CaptureControl(func() (any, error) {
		t.work.Lock()
		defer t.work.Unlock()
		return t.work.ControlImage()
	}, func() map[string]any {
		return map[string]any{"settings": t.settings.Snapshot(), "bindings": t.bindings.All(),
			"endpoints": t.endpoints.Saved(), "effects": t.outbound.States(), "words": t.words.Clone()}
	})
}

func (t *Tenant) newControlFork() *Tenant {
	view := &Tenant{services: t.services, disabledOwners: t.disabledOwners, ID: t.ID, Store: t.Log(), ownerSet: t.ownerSet, ownerRoles: t.ownerRoles, definitions: t.definitions, Projection: t.Projection, releases: t.releases,
		bindings: t.bindings.Clone(), ConnectorRoster: runtimekernel.NewConnectorRoster(t.ID), work: automation.NewWorkBoard(), Works: t.forkWorkState()}
	t.work.CopyJobs(&view.work)
	return view
}

func (t *Tenant) forkControl(images map[string]json.RawMessage) (*Tenant, error) {
	view := t.newControlFork()
	_, marks := t.ConnectorRoster.State()
	view.ConnectorRoster.RestoreMarks(marks)
	view.i18n = transport.NewTranslator(func() []platform.Service { return view.apps }, nil, view.words.Clone, PlatformLanguages)
	view.i18n.InstallApplications(view.definitions)
	if err := view.controlOwners().InstallAll(images); err != nil {
		return nil, err
	}
	return view, nil
}

func (t *Tenant) controlOwners() runtimekernel.ControlOwners {
	return runtimekernel.ControlOwners{Connectors: t.ConnectorRoster.InstallImage, Settings: t.settings.InstallImage, Endpoints: t.endpoints.InstallImage, Effects: t.outbound.InstallImage,
		Bindings: func(raw json.RawMessage) error {
			return actions.InstallBindings(raw, func() func(string, string) bool {
				t.bindings = actions.NewBindings(nil)
				return t.router().Rebind
			})
		},
		Work: func(raw json.RawMessage) error {
			t.work.Lock()
			defer t.work.Unlock()
			return t.work.InstallImage(t.ID, raw, func(app string) bool { return t.App(app) != nil })
		},
		Words: func(raw json.RawMessage) error {
			if err := t.words.InstallImage(raw); err != nil {
				return err
			}
			t.i18n.Reset()
			return nil
		}}
}

func (d *stagedDecision) controlHost() *Tenant {
	if d.control != nil {
		return d.control.Host
	}
	base, err := d.tenant.captureControl()
	if err != nil {
		unsupportedStagedEffect()
	}
	d.control = &runtimekernel.ControlStage[*Tenant]{Host: d.newControlView(base), Base: base}
	return d.control.Host
}

func (d *stagedDecision) newControlView(images map[string]json.RawMessage) *Tenant {
	view, err := d.tenant.forkControl(images)
	if err != nil {
		unsupportedStagedEffect()
	}
	view.Projection = d.Records
	view.ConnectorRoster.Use(d.Connectors)
	view.controlAnswer = func(x platform.Effect, out platform.Outcome, at time.Time) *platform.Error {
		d.answerControlEffect(x, out, at)
		return d.Failure
	}
	if d.tenant.Agents != nil {
		view.Agents = d.stateApp(d.tenant.Agents).(runtimeprotocol.AgentRuntime)
	}
	return view
}

func (d *stagedDecision) savedControl() (*acceptedformat.Control, error) {
	return d.control.Saved((*Tenant).captureControl, d.OperationCancels)
}

func (d *stagedDecision) answerControlEffect(x platform.Effect, out platform.Outcome, at time.Time) {
	if err := d.tenant.stageEffectAnswer(d, x, out, nil, at); err != nil {
		var refusal *platform.Error
		if errors.As(err, &refusal) {
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
	return runtimekernel.ApplyControl(saved, t.controlOwners().Install, t.outbound.PatchImage)
}

func (t *Tenant) controlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	return controlowner.Deciders{
		Connector: t.ConnectorRoster.ConnectorDecider(controlowner.SchemaConnectorOff),
		Setting:   t.settingDecision(), Binding: t.router().DecideBinding, Work: t.work.Decide,
		Notification: t.Notices.Decide, Endpoint: t.effectControl().EndpointDecider(controlowner.SchemaEndpointRemove),
		Effect:      t.effectControl().EffectDecider(controlowner.SchemaEffectApprove, controlowner.SchemaEffectDiscard, controlowner.SchemaEffectResolve),
		Translation: t.words.TranslationDecider(t.i18n.Reset)}.Decide(c, s, at)
}

func (r runtime) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *platform.Error) {
	if !c.Replaying {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Control actions require a private decision")
	}
	return r.t.controlDecision(c, s, at)
}

func (d *stagedDecision) ControlDecision(c platform.Caller, s *pb.Submission, at time.Time) (func(*pb.ChangeRecord), *platform.Error) {
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

func (d *stagedDecision) forkControlDecision(original *runtimekernel.ControlStage[*Tenant]) {
	forked, err := original.Fork((*Tenant).captureControl, d.newControlView)
	if err != nil {
		unsupportedStagedEffect()
	}
	d.control = forked
}

func (d *stagedDecision) routingTenant() *Tenant {
	if d.control != nil {
		return d.control.Host
	}
	return d.tenant
}
