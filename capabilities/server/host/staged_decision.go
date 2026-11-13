package host

import (
	"maps"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"
	"platformserver/services/actions"
	"platformserver/services/objects"

	"platformserver/kernel/records"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/platform"
)

type stagedDecision struct {
	runtimekernel.Decision
	sourceProjection       *runtimekernel.ClaimSet
	confirmationCompleting *confirmationCompletion
	control                *controlDecision
	tenant                 *Tenant
	requests               []actions.PendingRequest
	operationInputs        map[string][]*preparedOperationInput
}

func (d *stagedDecision) StagedDecision() platform.StagedDecision            { return d }
func (d *stagedDecision) ReadScoped() bool                                   { return true }
func (d *stagedDecision) DecisionCatalog(*platform.Ledger) *platform.Catalog { return nil }

func (t *Tenant) newStagedDecision() *stagedDecision {
	return t.newStagedDecisionOn(t.Projection.ForkRecords())
}

func (t *Tenant) newStagedDecisionOn(base *records.Store) *stagedDecision {
	sequences := t.sequences.Clone()
	connectors := t.connectors.Clone()
	return &stagedDecision{tenant: t, Decision: runtimekernel.Decision{Logs: map[*platform.Ledger]*kernel.ChangeLog{}, Simulation: t.simulation, Records: base,
		Sequences: sequences, Allocated: map[string]int{}, SequenceBases: map[string]int{}, Active: map[string]bool{},
		Notices: t.Notices.Draft(), Writers: map[string][]string{}, Changed: map[string][]string{}, Connectors: connectors, BusinessQuery: objects.BusinessQuery,
		Rules: runtimekernel.ConstraintRules{Definitions: func() []platform.Definition { return t.definitions }, RuleQuery: hostView{t: t}.RuleQuery, Binding: t.recordSetBinding,
			Trace: func() func(platform.DecisionTrace) { return t.simulationTrace }},
		DirectRows: func(rows []acceptedformat.InputRow, member string, at time.Time) error {
			_, err := t.prepareAcceptedDirectRows(rows, member, at)
			return err
		}}}
}

func (d *stagedDecision) Decide(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return d.decideApp(c, d.stateApp(app), s, at)
}

func (d *stagedDecision) decideApp(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if d.Simulation {
		if candidate, err := d.tenant.releases.Candidate(d.tenant.releases.Active()); err == nil {
			return d.decideAppFrom(c, app, s, at, d.routingTenant(), candidate.ID, candidate.Assets)
		}
	}
	return d.decideAppFrom(c, app, s, at, d.routingTenant(), d.tenant.releases.Active())
}

func (d *stagedDecision) decideAppFrom(c platform.Caller, app platform.App, s *pb.Submission, at time.Time, view *Tenant, release string, retained ...[]platform.ReleaseAsset) (*pb.ChangeRecord, *kernel.Error) {
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
	d.Records.Lock()
	defer d.Records.Unlock()
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return d.tenant.readableAs(d.Records, c, ref, at)
}

type stagedEffectPanic struct{}

func unsupportedStagedEffect() { panic(stagedEffectPanic{}) }

func isStagedEffect(failure any) bool {
	_, ok := failure.(stagedEffectPanic)
	return ok
}

func (d *stagedDecision) Probe(c platform.Caller, protocol, action, id string, payload []byte, at time.Time) *kernel.Error {
	return d.routingTenant().router().Probe(c, protocol, action, id, payload, at)
}
func (d *stagedDecision) Request(c platform.Caller, r *pb.ChangeRecord, q platform.Request) {
	if r.GetSubmission().GetTenantId() != d.tenant.ID || r.GetSubmission().GetAuthority() != c.App {
		unsupportedStagedEffect()
	}
	if q.Protocol == "" {
		d.Intents = append(d.Intents, d.routingTenant().planModelRequest(c, r, q, d.Intents))
		return
	}
	q.Payload = slices.Clone(platform.Raw(q.Payload))
	d.requests = append(d.requests, actions.PendingRequest{Request: q, Caller: c,
		Record: proto.Clone(r).(*pb.ChangeRecord), N: d.NextRequest(c.App + "/" + r.GetChangeId())})
}
func (d *stagedDecision) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *kernel.Error) {
	return d.routingTenant().router().Query(c, protocol, read, provider...)
}
func (d *stagedDecision) Setting(c platform.Caller, name string) string {
	return d.routingTenant().setting(c, name)
}
func (d *stagedDecision) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	planned, err := d.routingTenant().planAppEffects(c, kind, key, entity, data, now, d.Intents)
	if err != nil {
		d.FailDecision(err)
		return 0, err
	}
	d.Intents = append(d.Intents, planned...)
	d.tenant.askHeldEffects(c, kind, entity, planned, now)
	return len(planned), nil
}
func (d *stagedDecision) CheckEnterpriseUsage(c platform.Caller, id, until string, deleting bool, now time.Time) *kernel.Error {
	return d.enterpriseUsageTenant().checkEnterpriseUsage(c, d.Records, id, until, deleting, now)
}
func (d *stagedDecision) CheckEnterpriseTenantConnection(c platform.Caller, id string, now time.Time) *kernel.Error {
	return d.routingTenant().checkTenantConnection(c, id, now)
}

func (d *stagedDecision) enterpriseUsageTenant() *Tenant {
	original := d.routingTenant()
	apps := make([]platform.App, len(original.apps))
	for i, app := range original.apps {
		apps[i] = d.stateApp(app)
	}
	view := &Tenant{services: original.services, disabledOwners: original.disabledOwners, ID: original.ID, ownerSet: original.with(apps), Projection: d.Records, identity: original.identity, Secrets: original.Secrets, releases: original.releases}
	view.settings.Restore(original.settings.Snapshot())
	return view
}

func (d *stagedDecision) Units(c platform.Caller, structure string, now time.Time) []string {
	if d.tenant.directory == nil {
		return nil
	}
	return d.directoryView().Units("member:"+c.ID, structure, c.Today(now))
}
func (d *stagedDecision) Element(c platform.Caller, id string, now time.Time) (platform.ElementInfo, bool) {
	if d.tenant.directory == nil {
		return platform.ElementInfo{}, false
	}
	return d.directoryView().Element(id, now.UTC().Format(time.DateOnly))
}
func (d *stagedDecision) Related(c platform.Caller, element, stereotype string, outgoing bool, now time.Time) []string {
	if d.tenant.directory == nil {
		return nil
	}
	return d.directoryView().Related(element, stereotype, outgoing, now.UTC().Format(time.DateOnly))
}
func (d *stagedDecision) Links(c platform.Caller, entity string) []string {
	var out []string
	if d.tenant.linker != nil {
		out = d.stateApp(d.tenant.linker.(platform.App)).(platform.Linker).Links(c, entity)
	}
	return records.AppendRelationNeighbors(out, c.OwnedRelations(entity), entity)
}
func (d *stagedDecision) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if d.tenant.linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	owner := d.stateApp(d.tenant.linker.(platform.App))
	err := owner.(platform.Linker).Link(c, from, to, key, now)
	d.FailDecision(err)
	return err
}
func (d *stagedDecision) Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if d.tenant.tasks == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	err := d.tenant.tasks.Assign(c, r, a)
	d.FailDecision(err)
	return err
}
func (d *stagedDecision) Next(c platform.Caller, rec *pb.ChangeRecord, name string, date time.Time) (string, *kernel.Error) {
	return d.AllocateNumber(c, d.tenant.ID, d.tenant.App, rec, name, date)
}

func (d *stagedDecision) BoundProvider(c platform.Caller, protocol string) (string, *kernel.Error) {
	return d.routingTenant().router().BoundProvider(c, protocol)
}

func (d *stagedDecision) directoryView() platform.Directory {
	if owner, ok := d.tenant.directory.(platform.App); ok {
		return d.stateApp(owner).(platform.Directory)
	}
	return d.tenant.directory
}

func (d *stagedDecision) stateApp(app platform.App) platform.App {
	fork, ok := d.PrivateApp(app, func(fork platform.App) { (hostView{t: d.tenant, app: fork, decision: d}).bindOwner(fork) })
	if !ok {
		unsupportedStagedEffect()
	}
	return fork
}

func (d *stagedDecision) attachPrivateHosts() {
	for _, app := range d.PrivateApps {
		(hostView{t: d.tenant, app: app, decision: d}).bindOwner(app)
	}
}

func (d *stagedDecision) Attempt(decide func() (*pb.ChangeRecord, *kernel.Error)) (r *pb.ChangeRecord, err *kernel.Error) {
	saved := *d
	*d = *d.privateFork()
	d.attachPrivateHosts()
	defer func() {
		if p := recover(); p != nil {
			*d = saved
			d.attachPrivateHosts()
			panic(p)
		}
	}()
	r, err = decide()
	if err == nil {
		err = d.CheckAttempt(r, len(saved.Events))
	}
	if err != nil {
		*d = saved
		d.attachPrivateHosts()
	}
	return r, err
}

func (d *stagedDecision) privateFork() *stagedDecision {
	fork := *d
	forked, ok := d.Decision.Fork(d.tenant.App)
	if !ok {
		unsupportedStagedEffect()
	}
	fork.Decision = forked
	fork.operationInputs = maps.Clone(d.operationInputs)
	for hash, pending := range fork.operationInputs {
		fork.operationInputs[hash] = slices.Clone(pending)
	}
	fork.requests = slices.Clone(d.requests)
	for id, app := range fork.PrivateApps {
		if !d.Shared(id, app) {
			(hostView{t: d.tenant, app: app, decision: &fork}).bindOwner(app)
		}
	}
	fork.forkControlDecision(d.control)
	return &fork
}

func (d *stagedDecision) ProbeDecision(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	probe := d.privateFork()
	err := probe.runProbe(c, decide)
	if err != nil {
		d.AdoptRefusal(&probe.Decision, err)
	}
	return err
}

func (d *stagedDecision) runProbe(c platform.Caller, decide func(platform.Caller) *kernel.Error) *kernel.Error {
	return d.RunProbe(d, c, decide, isStagedEffect)
}

func (d *stagedDecision) stageObservers() *kernel.Error {
	refusal, ok := d.StageObservers(runtimekernel.ObserverHooks{Apps: d.tenant.LiveApps, Private: d.stateApp,
		Caller: func(id string) platform.Caller {
			return platform.NewCaller(d, platform.Member{ID: "app:" + id, Tenant: d.tenant.ID}, id, false, true)
		},
		Events: func(e platform.Event) []string { return d.tenant.router().Events(e) }})
	if !ok {
		unsupportedStagedEffect()
	}
	return refusal
}
