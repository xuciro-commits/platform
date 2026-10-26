package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/records"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// stagedDecision owns the private record, ledger and intent views for supported
// accepted-result transactions. Unlike runtime, it never falls back to live mutators.
type stagedDecision struct {
	currentSubmission    *pb.Submission
	businessRefusals     map[*kernel.Error]platform.BusinessRefusalFact
	businessFacts        []platform.BusinessRefusalFact
	simulation           bool
	definitions          map[string]*resultformat.DecisionDefinition
	schemaVersion        *platform.SchemaVersion
	retirements          []migrationRetirement
	migrationDefinitions []resultformat.ReleaseInstallation
	authorizations       []resultformat.AuthorizationEvidence
	iterations           []platform.TxnIteration
	constraintWrites     map[string]constraintWrite
	callbackOrigin       *platform.Effect
	callbackWrites       map[string]callbackWrite
	control              *controlDecision
	tenant               *Tenant
	records              *recordStore
	logs                 map[*platform.Ledger]platform.ChangeLog
	events               []platform.Event
	sequences            map[string]int // private counters, copied before any decision code
	allocated            map[string]int // counters actually consumed by this decision
	sequenceBases        map[string]int
	inputIssues          map[*kernel.Error][]platform.FieldIssue
	failure              *kernel.Error
	active               map[string]bool // reject cycles before re-entering an app/ledger lock
	at                   time.Time
	probing              bool
	notices              []platform.Notification
	noticeBase           int
	noticeSeq            int
	noticeBefore         string
	noticeEffects        []platform.Effect
	noticeChanged        bool
	writers              map[string][]string
	changed              map[string][]string
	publications         map[string]*resultformat.Publication
	intents              []platform.Effect
	transactionSteps     int
	observations         []resultformat.Observation
	observed             bool
	hops                 int
	requests             []request
	requestCounts        map[string]int
	receiving            map[*platform.Ledger]bool
	privateApps          map[string]platform.App
	states               map[string]platform.AcceptedStateApp
	stateBases           map[string]json.RawMessage
	connectors           platform.Connectors
	deliveries           []resultformat.ConnectorDelivery
	operationAnswers     map[string]platform.OperationResult
	operationCancels     map[string]bool
	operationInputs      map[string][]*preparedOperationInput
	observationInput     *resultformat.ObservationDecisionEvidence
}

// HoldChanges tracks the stack that owns a private ledger receive. Savepoints
// must retain its captured log and receiver until that receive returns.
func (d *stagedDecision) HoldChanges(ledger *platform.Ledger) (func(), *kernel.Error) {
	if d.receiving[ledger] {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The decision cannot re-enter its held ledger")
	}
	if d.receiving == nil {
		d.receiving = map[*platform.Ledger]bool{}
	}
	d.receiving[ledger] = true
	return func() { delete(d.receiving, ledger) }, nil
}

func (d *stagedDecision) InputRefused(err *kernel.Error, issues []platform.FieldIssue) {
	if d.inputIssues == nil {
		d.inputIssues = map[*kernel.Error][]platform.FieldIssue{}
	}
	d.inputIssues[err] = issues
}

var _ platform.StagedRuntime = (*stagedDecision)(nil)

func (d *stagedDecision) FailDecision(err *kernel.Error) {
	if d.failure == nil {
		d.failure = err
	}
}

func (t *Tenant) newStagedDecision() *stagedDecision {
	sequences := t.sequences.clone()
	notices, noticeSeq := t.notices.state()
	t.opsMu.Lock()
	descriptors, marks := t.connectors.kernel.State()
	t.opsMu.Unlock()
	for i, descriptor := range descriptors {
		descriptors[i] = proto.Clone(descriptor).(*pb.ConnectorDescriptor)
	}
	connectors := t.services.Kernel.Connectors()
	connectors.Restore(descriptors, marks)
	before, _ := acceptance.Digest(notices)
	return &stagedDecision{simulation: t.simulation, tenant: t, records: t.records.forkRecords(),
		logs: map[*platform.Ledger]platform.ChangeLog{}, sequences: sequences,
		allocated: map[string]int{}, sequenceBases: map[string]int{}, active: map[string]bool{},
		notices: notices, noticeBase: noticeSeq, noticeSeq: noticeSeq, noticeBefore: before,
		writers: map[string][]string{}, changed: map[string][]string{}, connectors: connectors}
}

func (d *stagedDecision) Decide(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return d.decideApp(c, d.stateApp(app), s, at)
}

func (d *stagedDecision) decideApp(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if d.simulation {
		if candidate, err := d.tenant.releases.candidate(d.tenant.releases.active); err == nil {
			return d.decideAppFrom(c, app, s, at, d.routingTenant(), candidate.ID, candidate.Assets)
		}
	}
	return d.decideAppFrom(c, app, s, at, d.routingTenant(), d.tenant.releases.active)
}

func (d *stagedDecision) decideAppFrom(c platform.Caller, app platform.App, s *pb.Submission, at time.Time, view *Tenant, release string, retained ...[]platform.ReleaseAsset) (*pb.ChangeRecord, *kernel.Error) {
	if app.AcceptedLedger() == nil {
		unsupportedStagedEffect()
	}
	id := app.Manifest().ID
	if d.receiving[app.AcceptedLedger()] {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The decision cannot re-enter its held owner")
	}
	if d.active[id] || len(d.active) >= resultformat.MaxHops {
		unsupportedStagedEffect()
	}
	d.active[id] = true
	defer func() { delete(d.active, id) }()
	if d.at.IsZero() {
		d.at = at
	}
	c.Roles = maps.Clone(c.Roles)
	definition, definitionErr := view.decisionDefinition(app, s, release, retained...)
	if definitionErr != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The execution definition and dependencies are unavailable")
	}
	// Callers may reuse/edit their request object for a later nested action.
	// Receipts and their staged idempotency entries must own the input bytes.
	previous := d.currentSubmission
	d.currentSubmission = proto.Clone(s).(*pb.Submission)
	defer func() { d.currentSubmission = previous }()
	r, err := app.Submit(c, proto.Clone(s).(*pb.Submission), at)
	if err != nil && d.failure == nil {
		d.failure = err
	}
	if d.failure != nil {
		return nil, d.failure
	}
	if r != nil && definition != nil {
		if d.observationInput != nil {
			definition.Observation = d.observationInput
		}
		definition.Contract.Family = r.GetSubmission().GetSchema().GetVersion()
		if d.definitions == nil {
			d.definitions = map[string]*resultformat.DecisionDefinition{}
		}
		d.definitions[id+"/"+r.GetChangeId()] = definition
	}
	return r, nil
}

// DraftChanges is called under the ledger's lock. It keeps both the kernel
// receipt and the host record image private until a durable result exists.
func (d *stagedDecision) DraftChanges(l *platform.Ledger, fork func() platform.ChangeLog) platform.ChangeLog {
	if d.logs[l] == nil {
		d.logs[l] = fork()
		d.bindPrivateFacts(l)
	}
	return d.logs[l]
}

func (d *stagedDecision) bindPrivateFacts(l *platform.Ledger) {
	for _, owner := range d.states {
		ledger, ok := owner.(platform.App)
		if !ok || ledger.AcceptedLedger() != l {
			continue
		}
		if facts, ok := owner.(platform.AcceptedFactApp); ok {
			d.logs[l].SetFacts(facts.HasAcceptedFact)
		}
	}
}

func (d *stagedDecision) Put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	var before *records.Row
	var info platform.EntityInfo
	var ref string
	func() {
		d.records.mu.Lock()
		defer d.records.mu.Unlock()
		et := d.records.of(c, reflect.TypeOf(entity))
		if et != nil {
			info = et.info
			id := records.Header(records.Copy(et.info.Go, entity)).ID
			before, ref = et.rows.Get(id), et.info.Type+"/"+id
		}
	}()
	prepared, err := d.prepareBusinessRecord(c, entity, d.at)
	if err == nil {
		err = d.records.put(c, r, prepared)
	}
	if err == nil {
		if d.constraintWrites == nil {
			d.constraintWrites = map[string]constraintWrite{}
		}
		if _, exists := d.constraintWrites[ref]; !exists {
			d.constraintWrites[ref] = constraintWrite{caller: c, before: before}
		}
		err = d.checkRecordConstraints(c, info, d.records.types[info.Type].rows.Get(strings.TrimPrefix(ref, info.Type+"/")), before)
	}
	if err != nil && d.failure == nil {
		d.failure = err
	}
	if err == nil {
		var history int
		func() {
			d.records.mu.Lock()
			defer d.records.mu.Unlock()
			et := d.records.of(c, reflect.TypeOf(entity))
			id := records.Header(records.Copy(et.info.Go, entity)).ID
			history = et.rows.Get(id).HistoryLength()
		}()
		if origin := d.callbackOrigin; origin != nil && origin.App == c.App && c.ID == "app:"+origin.App &&
			r.GetChangeId() == "" && r.GetSubmission().GetAuthority() == "" && r.GetSubmission().GetPrincipalId() == c.ID &&
			r.GetSubmission().GetSchema().GetName() != "" && r.GetRecordedTime().AsTime().Equal(d.at) {
			if d.callbackWrites == nil {
				d.callbackWrites = map[string]callbackWrite{}
			}
			d.callbackWrites[ref] = callbackWrite{Source: *origin, History: history}
		}
		app := r.GetSubmission().GetAuthority()
		d.writers[ref] = append(d.writers[ref], app)
		key := app + "/" + r.GetChangeId()
		if !slices.Contains(d.changed[key], ref) {
			d.changed[key] = append(d.changed[key], ref)
		}
	}
	return err
}

func (d *stagedDecision) Get(c platform.Caller, typ reflect.Type, id string) (any, bool) {
	return d.records.get(c, typ, id)
}

func (d *stagedDecision) Check(c platform.Caller, entity any) *kernel.Error {
	at := d.at
	if at.IsZero() {
		at = time.Now()
	}
	return d.records.check(c, entity, at)
}

func (d *stagedDecision) Publish(c platform.Caller, record *pb.ChangeRecord) {
	changed := slices.Clone(d.changed[c.App+"/"+record.GetChangeId()])
	d.events = append(d.events, platform.Event{App: c.App, Record: record, Changed: changed})
	schema := record.GetSubmission().GetSchema().GetName()
	if publisher, ok := d.tenant.app(c.App).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
		ref := record.GetSubmission().GetTarget()
		var raw []byte
		var err error
		func() {
			d.records.mu.Lock()
			defer d.records.mu.Unlock()
			et := d.records.types[ref.GetType()]
			if et != nil && et.rows.Get(ref.GetId()) != nil {
				raw, err = json.Marshal(et.rows.Get(ref.GetId()).Value.Interface())
			}
		}()
		if len(raw) == 0 || err != nil || publisher.ValidateAcceptedPublication(schema, raw) != nil {
			unsupportedStagedEffect()
		}
		if err := d.tenant.installPublicationLinkConstraint(d.records, schema, raw); err != nil {
			d.failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkCardinalityFailure)
			return
		}
		if d.publications == nil {
			d.publications = map[string]*resultformat.Publication{}
		}
		d.publications[c.App+"/"+record.GetChangeId()] = &resultformat.Publication{Schema: schema, Image: raw}
	}
}

func (d *stagedDecision) Readable(c platform.Caller, ref string) bool {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	at := d.at
	if at.IsZero() {
		at = time.Now() // direct app-only tests have no routed input clock
	}
	return d.tenant.readableIn(d.records, c.Member, ref, at)
}

func (d *stagedDecision) Probing() bool { return d.probing }

// Capabilities not represented by a saved result fail closed. Neither a nested
// decision nor an owned-work attempt may silently acquire a live mutator.
type stagedEffectPanic struct{}

func unsupportedStagedEffect() { panic(stagedEffectPanic{}) }

func (d *stagedDecision) Probe(c platform.Caller, protocol, action, id string, payload []byte, at time.Time) *kernel.Error {
	return d.routingTenant().probe(c, protocol, action, id, payload, at)
}
func (d *stagedDecision) Request(c platform.Caller, r *pb.ChangeRecord, q platform.Request) {
	if r.GetSubmission().GetTenantId() != d.tenant.ID || r.GetSubmission().GetAuthority() != c.App {
		unsupportedStagedEffect()
	}
	if q.Protocol == "" {
		d.intents = append(d.intents, d.routingTenant().planModelRequest(c, r, q, d.intents))
		return
	}
	// Own both the payload and receipt. An application may reuse either after
	// returning; requests run only once its decision/ledger locks are released.
	q.Payload = slices.Clone(platform.Raw(q.Payload))
	if d.requestCounts == nil {
		d.requestCounts = map[string]int{}
	}
	key := c.App + "/" + r.GetChangeId()
	n := d.requestCounts[key]
	d.requestCounts[key]++
	d.requests = append(d.requests, request{Request: q, caller: c,
		record: proto.Clone(r).(*pb.ChangeRecord), n: n})
}
func (d *stagedDecision) Query(c platform.Caller, protocol, read string, provider ...string) ([]platform.ProviderResult, *kernel.Error) {
	return d.routingTenant().query(c, protocol, read, provider...)
}
func (d *stagedDecision) Setting(c platform.Caller, name string) string {
	return d.routingTenant().setting(c, name)
}
func (d *stagedDecision) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	planned, err := d.routingTenant().planAppEffects(c, kind, key, entity, data, now, d.intents)
	if err != nil {
		if d.failure == nil {
			d.failure = err
		}
		return 0, err
	}
	d.intents = append(d.intents, planned...)
	d.tenant.askHeldEffects(c, kind, entity, planned, now)
	return len(planned), nil
}

func (d *stagedDecision) enterpriseUsageTenant() *Tenant {
	original := d.routingTenant()
	view := &Tenant{services: original.services, disabledOwners: original.disabledOwners, ID: original.ID, apps: slices.Clone(original.apps), appIndex: original.appIndex, records: d.records, identity: original.identity, Secrets: original.Secrets, releases: original.releases}
	view.settings.restore(original.settings.clone())
	for i, app := range view.apps {
		view.apps[i] = d.stateApp(app)
	}
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
		out = d.stateApp(d.tenant.linker.(platform.App)).(host.Linker).Links(c, entity)
	}
	return appendOwnedRelationNeighbors(out, c.OwnedRelations(entity), entity)
}
func (d *stagedDecision) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if d.tenant.linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	owner := d.stateApp(d.tenant.linker.(platform.App))
	err := owner.(host.Linker).Link(c, from, to, key, now)
	if err != nil && d.failure == nil {
		d.failure = err
	}
	return err
}
func (d *stagedDecision) Deliver(c platform.Caller, dataClass, from, to string, now time.Time) *kernel.Error {
	if c.Tenant != d.tenant.ID || c.ID == "" || dataClass == "" || now.IsZero() {
		unsupportedStagedEffect()
	}
	_, marks := d.connectors.State()
	var prior kernel.ConnectorMark
	for _, mark := range marks {
		if mark.Tenant == c.Tenant && mark.Connector == c.ID {
			prior = mark
			break
		}
	}
	before, err := acceptance.Digest(prior)
	if err != nil {
		unsupportedStagedEffect()
	}
	if refusal := d.connectors.Deliver(c.Tenant, c.ID, dataClass, from, to, now); refusal != nil {
		if d.failure == nil {
			d.failure = refusal
		}
		return refusal
	}
	d.deliveries = append(d.deliveries, resultformat.ConnectorDelivery{
		Connector: c.ID, DataClass: dataClass, From: from, To: to, At: now, Before: before})
	return nil
}
func (d *stagedDecision) Find(c platform.Caller, typ reflect.Type, q platform.Query) ([]any, int, *kernel.Error) {
	d.records.mu.Lock()
	defer d.records.mu.Unlock()
	et := d.records.of(c, typ)
	if et == nil {
		return nil, 0, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if len(q.Sort) == 0 {
		q.Sort = []string{"id"}
	}
	q = businessQuery(et.info, q, c.Member, d.at)
	page, total, err := d.records.find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = records.Copy(typ, v.Interface()).Interface()
	}
	return out, total, err
}
func (d *stagedDecision) Assign(c platform.Caller, r *pb.ChangeRecord, a platform.Assignment) *kernel.Error {
	if d.tenant.tasks == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	err := d.tenant.tasks.Assign(c, r, a)
	if err != nil && d.failure == nil {
		d.failure = err
	}
	return err
}
func (d *stagedDecision) Next(c platform.Caller, rec *pb.ChangeRecord, name string, date time.Time) (number string, err *kernel.Error) {
	defer func() {
		if err != nil && d.failure == nil {
			d.failure = err
		}
	}()
	if rec.GetChangeId() == "" || rec.GetSubmission().GetAuthority() != c.App ||
		rec.GetSubmission().GetTenantId() != d.tenant.ID {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	app := d.tenant.app(c.App)
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
	if _, taken := d.allocated[key]; !taken {
		d.sequenceBases[key] = d.sequences[key]
	}
	if _, started := d.sequences[key]; !started && s.SeedRecord != "" {
		waterline, problem := d.sequenceWaterline(c.App, s, date.Year())
		if problem != nil {
			return "", problem
		}
		d.sequences[key] = waterline
	}
	d.sequences[key]++
	d.allocated[key] = d.sequences[key]
	return s.Format(date.Year(), d.sequences[key]), nil
}

func (d *stagedDecision) BoundProvider(c platform.Caller, protocol string) (string, *kernel.Error) {
	return d.routingTenant().boundProvider(c, protocol)
}

func (d *stagedDecision) directoryView() host.Directory {
	if owner, ok := d.tenant.directory.(platform.App); ok {
		return d.stateApp(owner).(host.Directory)
	}
	return d.tenant.directory
}
