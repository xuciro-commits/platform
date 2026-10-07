package platformserver

import (
	"encoding/json"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// stagedDecision owns the private record, ledger and intent views for supported
// ADR-0038 transactions. Unlike runtime, it never falls back to live mutators.
type stagedDecision struct {
	tenant           *Tenant
	records          *recordStore
	logs             map[*platform.Ledger]*kernel.ChangeLog
	events           []platform.Event
	sequences        map[string]int // private counters, copied before any decision code
	allocated        map[string]int // counters actually consumed by this decision
	sequenceBases    map[string]int
	failure          *kernel.Error
	active           map[string]bool // reject cycles before re-entering an app/ledger lock
	at               time.Time
	probing          bool
	notices          []platform.Notification
	noticeBase       int
	noticeSeq        int
	noticeBefore     string
	noticeEffects    []platform.Effect
	noticeChanged    bool
	writers          map[string][]string
	changed          map[string][]string
	publications     map[string]*acceptedPublication
	intents          []platform.Effect
	observations     []acceptedObservation
	observed         bool
	hops             int
	requests         []request
	requestCounts    map[string]int
	states           map[string]platform.AcceptedStateApp
	stateBases       map[string]json.RawMessage
	connectors       *kernel.Connectors
	deliveries       []acceptedConnectorDelivery
	operationAnswers map[string]platform.OperationResult
	operationCancels map[string]bool
}

var _ platform.Runtime = (*stagedDecision)(nil)

func (*stagedDecision) StagedDecision() {}

func (t *Tenant) newStagedDecision() *stagedDecision {
	sequences := t.sequences.clone()
	t.opsMu.Lock()
	notices, noticeSeq := slices.Clone(t.notices), t.noticeSeq
	descriptors, marks := t.connectors.State()
	t.opsMu.Unlock()
	connectors := kernel.NewConnectors()
	connectors.Restore(descriptors, marks)
	before, _ := canonicalDigest(notices)
	return &stagedDecision{tenant: t, records: t.records.forkRecords(),
		logs: map[*platform.Ledger]*kernel.ChangeLog{}, sequences: sequences,
		allocated: map[string]int{}, sequenceBases: map[string]int{}, active: map[string]bool{},
		notices: notices, noticeBase: noticeSeq, noticeSeq: noticeSeq, noticeBefore: before,
		writers: map[string][]string{}, changed: map[string][]string{}, connectors: connectors}
}

func (d *stagedDecision) Decide(c platform.Caller, app platform.App, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if _, ok := app.(platform.ResultApp); !ok || !d.tenant.acceptsGenerated(app, s) {
		unsupportedStagedEffect()
	}
	id := app.Manifest().ID
	if d.active[id] || len(d.active) >= maxHops {
		unsupportedStagedEffect()
	}
	d.active[id] = true
	defer delete(d.active, id)
	if d.at.IsZero() {
		d.at = at
	}
	c.Roles = maps.Clone(c.Roles)
	// Callers may reuse/edit their request object for a later nested action.
	// Receipts and their staged idempotency entries must own the input bytes.
	r, err := d.stateApp(app).Submit(c, proto.Clone(s).(*pb.Submission), at)
	if err != nil && d.failure == nil {
		d.failure = err
	}
	if d.failure != nil {
		return nil, d.failure
	}
	return r, nil
}

// DraftChanges is called under the ledger's lock. It keeps both the kernel
// receipt and the host record image private until a durable result exists.
func (d *stagedDecision) DraftChanges(l *platform.Ledger, fork func() *kernel.ChangeLog) *kernel.ChangeLog {
	if d.logs[l] == nil {
		d.logs[l] = fork()
		d.bindPrivateFacts(l)
	}
	return d.logs[l]
}

func (d *stagedDecision) bindPrivateFacts(l *platform.Ledger) {
	for _, owner := range d.states {
		ledger, ok := owner.(platform.ResultApp)
		if !ok || ledger.AcceptedLedger() != l {
			continue
		}
		if facts, ok := owner.(platform.AcceptedFactApp); ok {
			d.logs[l].Facts = facts.HasAcceptedFact
		}
	}
}

func (d *stagedDecision) Put(c platform.Caller, r *pb.ChangeRecord, entity any) *kernel.Error {
	err := d.records.put(c, r, entity)
	if err != nil && d.failure == nil {
		d.failure = err
	}
	if err == nil {
		d.records.mu.Lock()
		et := d.records.of(c, reflect.TypeOf(entity))
		id := recordOf(copyOf(et.info.Go, entity)).ID
		ref := et.info.Type + "/" + id
		d.records.mu.Unlock()
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
	return d.records.check(c, entity)
}

func (d *stagedDecision) Publish(c platform.Caller, record *pb.ChangeRecord) {
	changed := slices.Clone(d.changed[c.App+"/"+record.GetChangeId()])
	d.events = append(d.events, platform.Event{App: c.App, Record: record, Changed: changed})
	schema := record.GetSubmission().GetSchema().GetName()
	if publisher, ok := d.tenant.app(c.App).(platform.AcceptedPublisher); ok &&
		slices.Contains(publisher.AcceptedPublicationSchemas(), schema) {
		ref := record.GetSubmission().GetTarget()
		d.records.mu.Lock()
		et := d.records.types[ref.GetType()]
		var raw []byte
		var err error
		if et != nil && et.rows[ref.GetId()] != nil {
			raw, err = json.Marshal(et.rows[ref.GetId()].value.Interface())
		}
		d.records.mu.Unlock()
		if len(raw) == 0 || err != nil || publisher.ValidateAcceptedPublication(schema, raw) != nil {
			unsupportedStagedEffect()
		}
		if err := d.tenant.installPublicationLinkConstraint(d.records, schema, raw); err != nil {
			d.failure = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, linkCardinalityFailure)
			return
		}
		if d.publications == nil {
			d.publications = map[string]*acceptedPublication{}
		}
		d.publications[c.App+"/"+record.GetChangeId()] = &acceptedPublication{Schema: schema, Image: raw}
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
	return d.tenant.probe(c, protocol, action, id, payload, at)
}
func (d *stagedDecision) Request(c platform.Caller, r *pb.ChangeRecord, q platform.Request) {
	if r.GetSubmission().GetTenantId() != d.tenant.ID || r.GetSubmission().GetAuthority() != c.App {
		unsupportedStagedEffect()
	}
	if q.Protocol == "" {
		d.intents = append(d.intents, d.tenant.planModelRequest(c, r, q, d.intents))
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
func (d *stagedDecision) Query(c platform.Caller, protocol, read string) ([]platform.ProviderResult, *kernel.Error) {
	return d.tenant.query(c, protocol, read)
}
func (d *stagedDecision) Setting(c platform.Caller, name string) string {
	return d.tenant.setting(c, name)
}
func (d *stagedDecision) Emit(c platform.Caller, kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	planned, err := d.tenant.planAppEffects(c, kind, key, entity, data, now, d.intents)
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
func (d *stagedDecision) Units(c platform.Caller, structure string, now time.Time) []string {
	if d.tenant.directory == nil {
		return nil
	}
	return d.tenant.directory.Units("member:"+c.ID, structure, c.Today(now))
}
func (d *stagedDecision) Element(c platform.Caller, id string, now time.Time) (platform.ElementInfo, bool) {
	if d.tenant.directory == nil {
		return platform.ElementInfo{}, false
	}
	return d.tenant.directory.Element(id, now.UTC().Format(time.DateOnly))
}
func (d *stagedDecision) Related(c platform.Caller, element, stereotype string, outgoing bool, now time.Time) []string {
	if d.tenant.directory == nil {
		return nil
	}
	return d.tenant.directory.Related(element, stereotype, outgoing, now.UTC().Format(time.DateOnly))
}
func (d *stagedDecision) Links(c platform.Caller, entity string) []string {
	if d.tenant.linker == nil {
		return nil
	}
	return d.stateApp(d.tenant.linker.(platform.App)).(interface {
		Links(platform.Caller, string) []string
	}).Links(c, entity)
}
func (d *stagedDecision) Link(c platform.Caller, from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if d.tenant.linker == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	owner := d.stateApp(d.tenant.linker.(platform.App))
	err := owner.(interface {
		Link(platform.Caller, *pb.EntityRef, *pb.EntityRef, string, time.Time) *kernel.Error
	}).Link(c, from, to, key, now)
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
	before, err := canonicalDigest(prior)
	if err != nil {
		unsupportedStagedEffect()
	}
	if refusal := d.connectors.Deliver(c.Tenant, c.ID, dataClass, from, to, now); refusal != nil {
		if d.failure == nil {
			d.failure = refusal
		}
		return refusal
	}
	d.deliveries = append(d.deliveries, acceptedConnectorDelivery{
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
	page, total, err := d.records.find(et, q, nil)
	out := make([]any, len(page))
	for i, v := range page {
		out[i] = copyOf(typ, v.Interface()).Interface()
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
	d.sequences[key]++
	d.allocated[key] = d.sequences[key]
	return s.Format(date.Year(), d.sequences[key]), nil
}
