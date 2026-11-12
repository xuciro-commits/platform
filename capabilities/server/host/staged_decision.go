package host

import (
	"encoding/json"
	"fmt"
	"maps"
	runtimekernel "platformserver/kernel"
	acceptedformat "platformserver/kernel/acceptedformat"

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
	requests               []request
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
		Notices: t.notices.Draft(), Writers: map[string][]string{}, Changed: map[string][]string{}, Connectors: connectors, BusinessQuery: businessQuery,
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
	if app.AcceptedLedger() == nil {
		unsupportedStagedEffect()
	}
	id := app.Manifest().ID
	if d.Receiving[app.AcceptedLedger()] {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The decision cannot re-enter its held owner")
	}
	if d.Active[id] || len(d.Active) >= acceptedformat.MaxHops {
		unsupportedStagedEffect()
	}
	d.Active[id] = true
	defer func() { delete(d.Active, id) }()
	if d.At.IsZero() {
		d.At = at
	}
	c.Roles = maps.Clone(c.Roles)
	definition, definitionErr := view.decisionDefinition(id, s, release, retained...)
	if definitionErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The execution definition and dependencies are unavailable")
	}
	previous := d.CurrentSubmission
	d.CurrentSubmission = proto.Clone(s).(*pb.Submission)
	defer func() { d.CurrentSubmission = previous }()
	r, err := app.Submit(c, proto.Clone(s).(*pb.Submission), at)
	if err != nil && d.Failure == nil {
		d.Failure = err
	}
	if d.Failure != nil {
		return nil, d.Failure
	}
	if r != nil && definition != nil {
		if d.ObservationInput != nil {
			definition.Observation = d.ObservationInput
		}
		definition.Contract.Family = r.GetSubmission().GetSchema().GetVersion()
		if d.Definitions == nil {
			d.Definitions = map[string]*platform.DecisionDefinition{}
		}
		d.Definitions[id+"/"+r.GetChangeId()] = definition
	}
	return r, nil
}

func (d *stagedDecision) Publish(c platform.Caller, record *pb.ChangeRecord) {
	if tracker, ok := d.tenant.App(c.App).(interface {
		TrackResource(platform.Caller, *pb.ChangeRecord, time.Time)
	}); ok {
		tracker.TrackResource(c, record, d.At)
	}
	changed := slices.Clone(d.Changed[c.App+"/"+record.GetChangeId()])
	d.Events = append(d.Events, platform.Event{App: c.App, Record: record, Changed: changed})
	schema := record.GetSubmission().GetSchema().GetName()
	if publisher, ok := d.tenant.App(c.App).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
		ref := record.GetSubmission().GetTarget()
		var raw []byte
		var err error
		func() {
			d.Records.Lock()
			defer d.Records.Unlock()
			et := d.Records.Type(ref.GetType())
			if et != nil && et.Rows().Get(ref.GetId()) != nil {
				raw, err = json.Marshal(et.Rows().Get(ref.GetId()).Value.Interface())
			}
		}()
		if len(raw) == 0 || err != nil || publisher.ValidateAcceptedPublication(schema, raw) != nil {
			unsupportedStagedEffect()
		}
		if err := d.tenant.installPublicationLinkConstraint(d.Records, schema, raw); err != nil {
			d.Failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, records.LinkCardinalityFailure)
			return
		}
		if d.Publications == nil {
			d.Publications = map[string]*acceptedformat.Publication{}
		}
		d.Publications[c.App+"/"+record.GetChangeId()] = &acceptedformat.Publication{Schema: schema, Image: raw}
	}
}

func (d *stagedDecision) Readable(c platform.Caller, ref string) bool {
	d.Records.Lock()
	defer d.Records.Unlock()
	at := d.At
	if at.IsZero() {
		at = time.Now()
	}
	return d.tenant.readableIn(d.Records, c.Member, ref, at)
}

type stagedEffectPanic struct{}

func unsupportedStagedEffect() { panic(stagedEffectPanic{}) }

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
	if d.RequestCounts == nil {
		d.RequestCounts = map[string]int{}
	}
	key := c.App + "/" + r.GetChangeId()
	n := d.RequestCounts[key]
	d.RequestCounts[key]++
	d.requests = append(d.requests, request{Request: q, caller: c,
		record: proto.Clone(r).(*pb.ChangeRecord), n: n})
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
		if d.Failure == nil {
			d.Failure = err
		}
		return 0, err
	}
	d.Intents = append(d.Intents, planned...)
	d.tenant.askHeldEffects(c, kind, entity, planned, now)
	return len(planned), nil
}
func (d *stagedDecision) CheckEnterpriseUsage(c platform.Caller, id, until string, deleting bool, now time.Time) *kernel.Error {
	view := d.enterpriseUsageTenant()
	return view.checkEnterpriseUsage(c, d.Records, id, until, deleting, now)
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
	return appendOwnedRelationNeighbors(out, c.OwnedRelations(entity), entity)
}
func (d *stagedDecision) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if d.tenant.linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	owner := d.stateApp(d.tenant.linker.(platform.App))
	err := owner.(platform.Linker).Link(c, from, to, key, now)
	if err != nil && d.Failure == nil {
		d.Failure = err
	}
	return err
}
func (d *stagedDecision) Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if d.tenant.tasks == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	err := d.tenant.tasks.Assign(c, r, a)
	if err != nil && d.Failure == nil {
		d.Failure = err
	}
	return err
}
func (d *stagedDecision) Next(c platform.Caller, rec *pb.ChangeRecord, name string, date time.Time) (number string, err *kernel.Error) {
	defer func() {
		if err != nil && d.Failure == nil {
			d.Failure = err
		}
	}()
	if rec.GetChangeId() == "" || rec.GetSubmission().GetAuthority() != c.App ||
		rec.GetSubmission().GetTenantId() != d.tenant.ID {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	app := d.tenant.App(c.App)
	if app == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	i := slices.IndexFunc(app.Manifest().Sequences, func(s platform.Sequence) bool { return s.Name == name })
	if i < 0 {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	s, year := app.Manifest().Sequences[i], 0
	if s.Yearly {
		year = date.Year()
	}
	key := fmt.Sprintf("%s/%s/%d", c.App, name, year)
	if _, taken := d.Allocated[key]; !taken {
		d.SequenceBases[key] = d.Sequences[key]
	}
	if _, started := d.Sequences[key]; !started && s.SeedRecord != "" {
		waterline, problem := d.SequenceWaterline(c.App, s, date.Year())
		if problem != nil {
			return "", problem
		}
		d.Sequences[key] = waterline
	}
	d.Sequences[key]++
	d.Allocated[key] = d.Sequences[key]
	return s.Format(date.Year(), d.Sequences[key]), nil
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
