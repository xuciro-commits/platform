package platformserver

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"net/http"
	"platformserver/journal"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/work"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Delivery is one event handed to one subscriber.
type Delivery struct {
	At         time.Time `json:"at"`
	App        string    `json:"app"`
	Action     string    `json:"action"`
	Target     string    `json:"target"`
	Subscriber string    `json:"subscriber"`
	Outcome    string    `json:"outcome"` // "ok" or an error code
	Attempt    int       `json:"attempt,omitempty"`
}

// Tenant runs a tenant's apps: routing by name, one ordered journal, and calls
// between apps only through the protocols they provide and consume.
type Tenant struct {
	ID string
	// Record, when set, makes each accepted input durable before it is answered;
	// a failure must stop the server (ADR-0007).
	Record func(Entry)
	// AcceptResult persists a supported top-level decision or owned-work
	// attempt before any authoritative changes or intents are visible.
	AcceptResult func(Entry, string, string) ([]byte, error)
	// Store keeps what is derived outside the journal: vectors and transcripts
	// (ADR-0022); without one they stay in memory.
	Store           Store
	derivedMu       sync.Mutex
	vectorMemory    map[string][]float32
	transcripts     []Transcript
	knowledge       glossary // the knowledge app: documents are searched, terms read (ADR-0022)
	index           index    // passages cut from documents and knowledge fields
	dictionaries    sync.Map // language → map[string]string, merged from the platform's and the apps' (ADR-0023)
	patternCache    sync.Map // language → []pattern
	agentRun        string   // the run whose agent is submitting, under mu: its effects name it
	mu              sync.Mutex
	fault           atomic.Pointer[tenantFault] // recovery failure stops this tenant without stopping its neighbors
	apps            []platform.App
	refusals        map[string]refusedResult   // "<app>/<key>" → committed effect-free answer
	acceptedAnswers map[string]json.RawMessage // original input → approval answer, not the held action's later receipt
	acceptedInputs  map[string]json.RawMessage // connector input identity → saved answer and owned effects
	// releaseCandidates contains exact, validated candidate bytes. activeRelease
	// is a movable pointer; neither is derived from the mutable development
	// definition registry (ADR-0039 20b).
	releaseCandidates map[string]json.RawMessage
	releaseApplied    map[string]string // committed release key -> applied result digest
	activeRelease     string
	// hostLifecycle is the host console's lifecycle for this tenant (ADR-0047
	// §6.5): "" or "open" runs, "suspended" and "decommissioned" block
	// ordinary requests; support are its authorized support sessions.
	hostLifecycle atomic.Pointer[string] // read without the lock (health, the tenant record)
	support       []SupportGrant
	// sealed are the candidate artifacts written to the file store (item 5),
	// and migrations what the host console moved in or out of this tenant.
	sealed     map[string]SealedArtifact
	migrations []MigrationManifest
	// compositeApplied are the composite commands this tenant committed, by key.
	compositeApplied map[string]string
	// staged are the per-call result channel's sealed entries, by call.
	staged      map[string]platform.StagedResult
	definitions []platform.Definition   // installed code assets; member views are derived on read
	owner       map[string]platform.App // "action:", "read:" and "input:" names → app
	// audit holds accepted top-level inputs, newest last, rebuilt by replay; its
	// own lock, because reads run inside other apps' submissions.
	auditMu    sync.Mutex
	audit      []AuditEntry
	deliveries []Delivery
	events     []caused // published during the current input, queued after it
	bindings   map[string]binding
	observers  []host.Observer // the platform's views derived from events, inside the input
	change     changes         // what clients follow to refetch (F-32)
	linker     host.Linker     // serves Caller.Link and Caller.Links
	directory  host.Directory  // the organisation, when composed
	hops       int             // of the event being handled, for the events it causes
	acted      int             // decisions published and notifications given, ever
	works      *kernel.Works
	// opsMu guards what reads and the runner share: queues, connectors,
	// notifications and settings (operations.go). It is never held while t.mu is taken.
	opsMu  sync.Mutex
	queues map[string][]*Task // subscriber → its deliveries, head first
	failed []*Task
	jobs   []*Task
	// Quota is the attempts of owned work each app may make in a minute (ADR-0027 D3); 0: no limit.
	Quota       int
	used        map[string]usedMinute // attempts per app in the current minute (volatile)
	turn        int                   // the app the next round starts at
	breakers    breakers              // per endpoint and AI provider (volatile)
	connectors  *kernel.Connectors
	descriptors map[string]*pb.ConnectorDescriptor
	lastError   map[string]ConnectorError
	notices     []platform.Notification
	noticeSeq   int
	settings    map[string]string // "<app>/<name>" → value
	seqMu       sync.Mutex
	sequences   map[string]int // "<app>/<sequence>/<year>" → the last number taken (ADR-0024)
	endpoints   []*Endpoint
	outbound    []*effect
	// Secrets resolves a secret's name (default: PLATFORM_SECRETS_DIR, then
	// PLATFORM_SECRET_<NAME>); Outbound sends an effect's request (default: a
	// client refusing private addresses). Tests replace both (ADR-0014).
	Secrets  func(name string) ([]byte, bool)
	Outbound func(req *http.Request, allowPrivate bool) (*http.Response, error)
	// AIClient sends model calls (default: a client refusing private addresses
	// unless the provider is local); tests replace it (ADR-0015).
	AIClient  func(req *http.Request) (*http.Response, error)
	ai        models          // the AI app: the models the host calls (ADR-0015)
	records   *recordStore    // the apps' entity records (ADR-0016)
	tasks     host.Tasks      // serves Caller.Assign: the work app (ADR-0017)
	procs     host.Processes  // the flow app (ADR-0020)
	listeners []string        // platform apps given other apps' events as owned work (host.Listener)
	agents    *Agents         // AI agents (ADR-0021)
	ctx       context.Context // the span of the work being done under mu (telemetry.go)
	// Files keeps file bytes (ADR-0028 D1); nil: memory, for development and tests.
	Files          FileStore
	ComputeWorker  WasmWorker
	Compiler       CodeCompiler
	computeCancels map[string]context.CancelFunc // volatile handles, never ownership
	memFiles       memoryFiles
	uploads        map[string]time.Time // hashes uploaded and when, until attached or swept (volatile)
	personal       []PersonalRead       // reads of personal data (ADR-0028 D4), volatile
	probing        bool                 // a submission for approval is being checked, not applied
	requests       []request            // accepted decisions' requests of other apps, run with their events (ADR-0026)
}

// AuditEntry is one accepted input: who, when, through which app, what.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Action string    `json:"action"` // an action's schema, or "input:<name>"
	Target string    `json:"target,omitempty"`
}

const auditKept = 1000

// Audit is the tenant's recent accepted inputs, oldest first.
func (t *Tenant) Audit() []AuditEntry {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	return slices.Clone(t.audit)
}

func (t *Tenant) remember(e AuditEntry) {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	t.audit = append(t.audit, e)
	if len(t.audit) > auditKept {
		t.audit = t.audit[len(t.audit)-auditKept:]
	}
}

// NewTenant enables apps for a tenant; it refuses duplicate names, consumed
// protocols no earlier app provides, and manifests the host could not honour.
func NewTenant(id string, apps ...platform.App) (*Tenant, error) {
	t := &Tenant{ID: id, apps: apps, refusals: map[string]refusedResult{}, owner: map[string]platform.App{}, bindings: map[string]binding{}, works: kernel.NewWorks(), queues: map[string][]*Task{},
		connectors: kernel.NewConnectors(), records: newRecordStore(), descriptors: map[string]*pb.ConnectorDescriptor{}, lastError: map[string]ConnectorError{}, settings: map[string]string{}, sequences: map[string]int{}}
	claim := func(name string, a platform.App) error {
		if other := t.owner[name]; other != nil {
			return fmt.Errorf("tenant %s: %q is declared by %s and %s", id, name, other.Manifest().ID, a.Manifest().ID)
		}
		t.owner[name] = a
		return nil
	}
	for i, a := range apps {
		m := a.Manifest()
		if err := t.bind(i, a); err != nil {
			return nil, err
		}
		// Roles the host's own apps take (ADR-0025 D4), by the interfaces they implement.
		if x, ok := a.(host.Attached); ok {
			x.Attach(hostView{t: t, app: a})
		}
		if x, ok := a.(host.Observer); ok {
			t.observers = append(t.observers, x)
		}
		if x, ok := a.(host.Linker); ok {
			t.linker = x
		}
		if x, ok := a.(host.Directory); ok {
			t.directory = x
		}
		if d, ok := a.(*Console); ok {
			d.t = t
			defer d.applyAccess() // once every app is known
		}
		if x, ok := a.(models); ok {
			t.ai = x
		}
		if x, ok := a.(host.Tasks); ok {
			t.tasks = x
		}
		if x, ok := a.(host.Processes); ok {
			t.procs = x
		}
		if _, ok := a.(host.Listener); ok {
			t.listeners = append(t.listeners, m.ID)
		}
		if x, ok := a.(*Agents); ok {
			t.agents, x.t = x, t
		}
		if x, ok := a.(glossary); ok {
			t.knowledge = x
		}
		for _, action := range m.Subscribes {
			if protocol, _, ok := strings.Cut(action, "#"); ok {
				if !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
					return nil, fmt.Errorf("tenant %s: %s subscribes to %s of a protocol it does not consume", id, m.ID, action)
				}
				continue
			}
			if _, own := m.Actions.Action(action); !own {
				return nil, fmt.Errorf("tenant %s: %s subscribes to %s, neither its own action nor a protocol event", id, m.ID, action)
			}
			if _, ok := a.(platform.Subscriber); !ok {
				return nil, fmt.Errorf("tenant %s: %s subscribes but has no Handle", id, m.ID)
			}
		}
		if err := checkManifest(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %s: %v", id, m.ID, err)
		}
		if err := t.records.declare(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %s: %v", id, m.ID, err)
		}
		for _, j := range m.Jobs {
			t.jobs = append(t.jobs, &Task{ID: "job:" + m.ID + "/" + j.Name, Kind: "job", App: m.ID, Title: j.Title, State: "scheduled", job: j})
		}
		var names []string
		for _, r := range m.Reads {
			names = append(names, "read:"+r)
		}
		for _, action := range m.Actions.All() {
			names = append(names, "action:"+action.Schema)
		}
		for input := range m.Inputs {
			names = append(names, "input:"+input)
		}
		for _, n := range names {
			if err := claim(n, a); err != nil {
				return nil, err
			}
		}
	}
	for _, a := range apps { // agents, once every app is composed (ADR-0021); flows may give them steps
		if len(a.Manifest().Agents) == 0 {
			continue
		}
		if t.agents == nil {
			return nil, fmt.Errorf("tenant %s: %s declares agents, and the tenant runs no agent app", id, a.Manifest().ID)
		}
		if err := t.agents.declare(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	for _, a := range apps { // flows, once every app is composed (ADR-0020)
		if len(a.Manifest().Flows) == 0 {
			continue
		}
		if t.procs == nil {
			return nil, fmt.Errorf("tenant %s: %s declares flows, and the tenant runs no flow app", id, a.Manifest().ID)
		}
		if err := t.procs.Declare(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	{ // interfaces, once every entity is described (ADR-0058 A2)
		manifests := make([]platform.Manifest, len(apps))
		infos := map[string]platform.EntityInfo{}
		for k, a := range apps {
			manifests[k] = a.Manifest()
		}
		for _, et := range t.records.sortedTypes() {
			infos[et.info.Type] = et.info
		}
		if err := platform.CheckInterfaces(manifests, infos); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	if err := t.registerDefinitions(); err != nil {
		return nil, fmt.Errorf("tenant %s: %v", id, err)
	}
	return t, nil
}

func (t *Tenant) app(id string) platform.App {
	i := slices.IndexFunc(t.apps, func(a platform.App) bool { return a.Manifest().ID == id })
	if i < 0 {
		return nil
	}
	return t.apps[i]
}

func (t *Tenant) caller(m platform.Member, app platform.App, replaying bool) platform.Caller {
	return platform.NewCaller(runtime{t}, m, app.Manifest().ID, replaying, false)
}

func unknown() *kernel.Error { return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA} }

// Submit routes a submission to the app declaring its action and records it when accepted.
func (t *Tenant) Submit(m platform.Member, s *pb.Submission, now time.Time) (record *pb.ChangeRecord, err *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	a := t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return nil, unknown()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	// A builder role held only through projects is bounded to their assets,
	// from this submission's target under the tenant lock, before any app decides it.
	if err = t.delegatedBound(m, s); err != nil {
		return nil, err
	}
	end := t.begin("submit "+s.GetSchema().GetName(), trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID),
		attribute.String("platform.target", target(s)), attribute.String("platform.member", m.ID))
	defer func() { end(outcomeOf(err)) }()
	if t.AcceptResult != nil {
		if resultApp, ok := a.(platform.ResultApp); ok && t.acceptsGenerated(a, s) {
			return t.submitAccepted(resultApp, m, s, now, false)
		}
	}
	if m.Agent && t.suspended(m.ID) { // an agent an administrator switched off (ADR-0029 D4)
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID)
	}
	defer t.enqueue(now)
	if declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName()); declared.Approval != nil && t.owner["action:"+work.SchemaRequest] != nil {
		return t.request(m, a, s, now)
	}
	record, err = a.Submit(t.caller(m, a, false), s, now)
	if err == nil {
		t.journal(a, m, s, now)
	}
	return record, explained(err, a, s.GetSchema().GetName(), target(s))
}

func (t *Tenant) acceptsGenerated(a platform.App, s *pb.Submission) bool {
	schema := s.GetSchema().GetName()
	if selected, ok := a.(platform.AcceptedActionApp); ok &&
		slices.Contains(selected.AcceptedActionSchemas(), schema) {
		return true
	}
	if published, ok := a.(platform.AcceptedPublisher); ok &&
		slices.Contains(published.AcceptedPublicationSchemas(), schema) {
		return true
	}
	if acceptsPureTransition(a, schema) {
		return true
	}
	// Generated callbacks run through the same private Runtime. Any capability
	// not yet representable fails closed instead of changing the live tenant.
	for _, entity := range a.Manifest().Entities {
		verb, match := strings.CutPrefix(schema, entity.Type+".")
		if !match || entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if verb == transition.Name {
				return true
			}
			if transition.Approval != nil && transition.Approval.Pending != "" &&
				slices.Contains([]string{transition.Name + platform.ApprovalHeld,
					transition.Name + platform.ApprovalRejected, transition.Name + platform.ApprovalReturned}, verb) {
				return true
			}
		}
	}
	if !strings.HasSuffix(schema, ".create") && !strings.HasSuffix(schema, ".edit") &&
		!strings.HasSuffix(schema, ".archive") {
		return false
	}
	t.records.mu.Lock()
	defer t.records.mu.Unlock()
	// Select by the declared action, not the submitted target. A malformed
	// target must be refused inside the isolated decision, not silently routed
	// to the legacy direct mutator.
	for _, et := range t.records.types {
		if et.info.App == a.Manifest().ID && slices.Contains(et.info.Standard, schema) &&
			(schema == et.info.Type+".create" || schema == et.info.Type+".edit" ||
				schema == et.info.Type+".archive") {
			return true
		}
	}
	return false
}

// Callback-free transitions retain the smaller version-4 result format.
// Callback and approval transactions use the validated multi-decision batch.
func acceptsPureTransition(a platform.App, schema string) bool {
	for _, entity := range a.Manifest().Entities {
		verb, match := strings.CutPrefix(schema, entity.Type+".")
		if !match || entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if transition.Name == verb && transition.Do == nil && transition.After == nil && transition.Approval == nil {
				return true
			}
		}
	}
	return false
}

func (t *Tenant) submitAccepted(a platform.ResultApp, m platform.Member, s *pb.Submission, now time.Time, automation bool) (record *pb.ChangeRecord, refusal *kernel.Error) {
	// A malformed transport identity cannot reserve a durable key for another
	// tenant, authority or member. The kernel still supplies its ordinary
	// refusal, but no result under this tenant can represent that submission.
	if s.GetTenantId() != t.ID || s.GetAuthority() != a.Manifest().ID ||
		s.GetPrincipalId() != m.ID || s.GetIdempotencyKey() == "" ||
		s.GetTarget() == nil || s.GetSchema() == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission has an invalid identity")
	}
	ledger := a.AcceptedLedger()
	if saved := t.acceptedAnswers[a.Manifest().ID+"/"+s.GetIdempotencyKey()]; len(saved) > 0 {
		result, receipt, err := decodeAcceptedBatch(saved)
		hash, hashErr := submissionHash(s)
		if err != nil || hashErr != nil || result.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return receipt, nil
	}
	if prior := ledger.AcceptedFor(t.ID, s.GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior.GetSubmission(), s) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return prior, nil
	}
	hash, err := submissionHash(s)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission cannot be encoded")
	}
	if prior, ok := t.refusals[a.Manifest().ID+"/"+s.GetIdempotencyKey()]; ok {
		if prior.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		answer := prior.Error
		return nil, &answer
	}
	if m.Agent && t.suspended(m.ID) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID)
	}
	draft := t.newStagedDecision()
	record, refusal = decideAcceptedAs(a, draft, m, s, now, automation)
	var raw []byte
	if refusal != nil {
		refusal = explained(refusal, a, s.GetSchema().GetName(), target(s))
		raw, err = encodeRefusedResult(s, now, refusal)
	} else {
		selected, explicit := a.(platform.AcceptedActionApp)
		schema := s.GetSchema().GetName()
		single := strings.HasSuffix(schema, ".create") || strings.HasSuffix(schema, ".edit") ||
			strings.HasSuffix(schema, ".archive") || acceptsPureTransition(a, schema)
		if publisher, ok := a.(platform.AcceptedPublisher); ok {
			single = single || slices.Contains(publisher.AcceptedPublicationSchemas(), schema)
		}
		if len(draft.events) > 1 || len(draft.records.writes) > 1 || len(draft.allocated) > 0 || draft.noticeChanged || len(draft.intents) > 0 || len(draft.deliveries) > 0 ||
			!proto.Equal(record.GetSubmission(), s) || !single || draft.observed && len(t.observers) > 0 ||
			explicit && slices.Contains(selected.AcceptedActionSchemas(), s.GetSchema().GetName()) {
			raw, err = draft.batchResult(a.Manifest().ID, s, record, now)
		} else {
			raw, err = draft.result(a.Manifest().ID, record, now)
		}
		if err != nil {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
			raw, err = encodeRefusedResult(s, now, refusal)
		}
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
	}
	member, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = t.procs.Versions()
	}
	committed, err := t.AcceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: member, Body: raw, At: now, Versions: versions}, s.GetIdempotencyKey(), hash)
	if err != nil {
		if err == errAcceptedConflict {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted result could not be committed")
	}
	return t.finishCommittedResult(a, m, hash, committed)
}

func (t *Tenant) finishCommittedResult(a platform.ResultApp, m platform.Member, hash string, committed []byte) (record *pb.ChangeRecord, refusal *kernel.Error) {
	// Once committed, even an unexpected application panic is tenant-local:
	// never answer from a possibly half-applied memory image or take down the
	// other tenants. A repaired journal requires a fresh process to resume.
	defer func() {
		if failure := recover(); failure != nil {
			t.quarantine(fmt.Errorf("apply committed result: %v", failure))
			record = nil
			refusal = &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}()
	var envelope struct{ Kind string }
	if err := json.Unmarshal(committed, &envelope); err != nil {
		t.quarantine(fmt.Errorf("committed result is not an envelope: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if envelope.Kind == "refusal" {
		saved, sub, err := decodeRefusedResult(committed)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
			saved.RequestHash != hash || sub.GetPrincipalId() != m.ID {
			t.quarantine(fmt.Errorf("committed refusal differs from the accepted request: %v", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		t.refusals[saved.App+"/"+sub.GetIdempotencyKey()] = saved
		answer := saved.Error
		return nil, &answer
	}
	if envelope.Kind == "record-batch" {
		saved, receipt, err := decodeAcceptedBatch(committed)
		sub, subErr := batchSubmission(saved, receipt)
		if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
			saved.RequestHash != hash || subErr != nil || sub.GetPrincipalId() != m.ID {
			t.quarantine(fmt.Errorf("committed batch differs from the accepted request: %v", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		applied, err := t.applyAcceptedBatch(a.AcceptedLedger(), committed)
		if err != nil {
			t.quarantine(fmt.Errorf("committed record batch could not be applied: %w", err))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
		if applied {
			t.remember(submitted(m.ID, a, sub, saved.At))
			t.publishAcceptedBatch(saved)
			t.changedOwner(saved.App)
		}
		t.enqueue(saved.At)
		return receipt, nil
	}
	saved, receipt, err := decodeAcceptedResult(committed)
	if err != nil || saved.App != a.Manifest().ID || saved.Tenant != t.ID ||
		saved.RequestHash != hash || receipt.GetSubmission().GetPrincipalId() != m.ID {
		t.quarantine(fmt.Errorf("committed result differs from the accepted request: %v", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	for _, subscriber := range saved.Event.Subscribers {
		if t.app(subscriber) == nil {
			t.quarantine(fmt.Errorf("committed result requires missing subscriber %s", subscriber))
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		}
	}
	applied, err := t.applyAcceptedResult(a.AcceptedLedger(), committed)
	if err != nil {
		t.quarantine(fmt.Errorf("committed accepted result could not be applied: %w", err))
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if saved.At.IsZero() {
		saved.At = receipt.GetRecordedTime().AsTime()
	}
	if applied {
		t.remember(submitted(m.ID, a, receipt.GetSubmission(), saved.At))
		t.publishAccepted(platform.Event{App: a.Manifest().ID, Record: receipt, Changed: saved.Event.Changed}, saved.Event, saved.Version)
		t.changedOwner(saved.App)
	}
	t.enqueue(saved.At)
	return receipt, nil
}

// Only decision evaluation may turn an unsupported-effect panic into a
// refusal. A panic after the journal commit must escape so the tenant stops.
func decideAccepted(a platform.ResultApp, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time) (record *pb.ChangeRecord, refusal *kernel.Error) {
	return decideAcceptedAs(a, draft, m, s, now, false)
}

func decideAcceptedAs(a platform.ResultApp, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time, automation bool) (record *pb.ChangeRecord, refusal *kernel.Error) {
	defer func() {
		if p := recover(); p != nil {
			if _, ok := p.(stagedEffectPanic); !ok {
				panic(p)
			}
			record = nil
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action uses an effect outside the accepted-result boundary")
		}
	}()
	if declared, _ := a.Manifest().Actions.Action(s.GetSchema().GetName()); declared.Approval != nil &&
		draft.tenant.owner["action:"+work.SchemaRequest] != nil {
		record, refusal = draft.requestApproval(a, m, s, now)
	} else {
		record, refusal = platform.Decide(platform.NewCaller(draft, m, a.Manifest().ID, false, automation), a, s, now)
	}
	if refusal == nil {
		refusal = draft.answerRequests(now)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	return record, refusal
}

// explained gives a refusal an app returned without saying why the reason its
// code implies, naming the action and its target, so that every refusal a
// person, an agent or another app reads says something (docs/Testing.md C3).
// An app's own reason, through platform.Refuse, is always better; the known
// apps that still refuse bare are counted in scripts/escapes.sh (#129).
func explained(err *kernel.Error, a platform.App, schema, target string) *kernel.Error {
	if err == nil || err.Message != "" {
		return err
	}
	action := schema
	if declared, ok := a.Manifest().Actions.Action(schema); ok && declared.Title != "" {
		action = declared.Title
	}
	text := map[pb.ErrorCode]string{
		pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:     "{action} was refused: a value is missing or out of range",
		pb.ErrorCode_ERROR_CODE_CONFLICT:             "{action} was refused: {target} is not in a state that allows it, or changed since it was read",
		pb.ErrorCode_ERROR_CODE_NOT_FOUND:            "{action} was refused: {target} does not exist, or you may not see it",
		pb.ErrorCode_ERROR_CODE_POLICY_DENIED:        "{action} was refused: you may not do it to {target}",
		pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA:       "{action} is not an action here",
		pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT: "{action} was sent before with other values",
		pb.ErrorCode_ERROR_CODE_INVALID_REFERENCE:    "{action} names a record that does not exist",
		pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY:        "{action} was refused: another authority decides {target}",
	}[err.Code]
	if text == "" {
		return err
	}
	return platform.Refuse(err.Code, text, action, target)
}

func outcomeOf(err *kernel.Error) string {
	if err != nil {
		return err.Code.String()
	}
	return "ok"
}

// journal records an accepted submission as the member's, for the audit and the journal.
func (t *Tenant) journal(a platform.App, m platform.Member, s *pb.Submission, now time.Time) {
	t.remember(submitted(m.ID, a, s, now))
	body, _ := protojson.Marshal(s)
	t.record(a, "submission", m, body, now)
}

// request holds a submission whose action needs approval (ADR-0017): its
// policy and rules are checked now, as the requester, without effect; then the
// work app opens the request, and the submission runs when the last approver
// agrees. A resend with the same key answers with the request made.
func (t *Tenant) request(m platform.Member, a platform.App, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	id := a.Manifest().ID + "." + s.GetIdempotencyKey()
	approvals := t.owner["action:"+work.SchemaRequest] // the work app
	c := t.automation(work.ID, false)
	if _, known := platform.Get[work.ApprovalRequest](c, id); !known {
		t.probing = true
		_, err := a.Submit(t.caller(m, a, false), s, now)
		t.probing = false
		if err != nil {
			return nil, err
		}
	}
	held, _ := protojson.Marshal(s)
	payload, _ := json.Marshal(map[string]any{"requester": m.ID, "submission": json.RawMessage(held)})
	request := &pb.Submission{TenantId: t.ID, PrincipalId: c.ID, Authority: work.ID, IdempotencyKey: "approval:" + id,
		Target: &pb.EntityRef{Type: work.ApprovalType, Id: id}, Schema: &pb.SchemaRef{Name: work.SchemaRequest, Version: 1}, Payload: payload}
	record, err := approvals.Submit(c, request, now)
	if err == nil { // the journal holds the request, made by the work app; the requester is on the request
		t.journal(approvals, c.Member, request, now)
	}
	return record, err
}

// Input routes a connector input (push batch, poll page, heartbeat) to its app.
func (t *Tenant) Input(m platform.Member, name string, body []byte, now time.Time) (out any, err *kernel.Error) {
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	a := t.owner["input:"+name]
	if a == nil {
		return nil, unknown()
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	end := t.begin("input "+name, trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID), attribute.String("platform.member", m.ID))
	defer func() { end(outcomeOf(err)) }()
	if t.AcceptResult != nil {
		if a.Manifest().Inputs[name] {
			accepted, ok := a.(platform.AcceptedInputApp)
			if !ok || !slices.Contains(accepted.AcceptedInputs(), name) {
				return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT,
					"This connector input has no accepted-result boundary")
			}
			return t.inputAccepted(accepted, m, name, body, now)
		}
	}
	defer t.enqueue(now)
	out, err = a.Input(t.caller(m, a, false), name, body, now)
	if err != nil {
		t.refused(m.ID, name, err, now)
	}
	if err == nil && a.Manifest().Inputs[name] {
		t.remember(AuditEntry{At: now, Member: m.ID, App: a.Manifest().ID, Action: "input:" + name})
		t.record(a, name, m, body, now)
	}
	return out, err
}

// Read serves a named read of the app that declares it, to members holding a
// role in that app or to everyone when the manifest says so; the app may refuse further.
func (t *Tenant) Read(m platform.Member, name string) (any, *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	a := t.owner["read:"+name]
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if m.Roles[a.Manifest().ID] == "" && !slices.Contains(a.Manifest().Everyone, name) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	out, err := a.Read(t.caller(m, a, false), name)
	if err != nil {
		return nil, err
	}
	// An app chooses which records its read answers with; the host keeps what
	// leaves no wider than the member's authority over the fields and sources
	// behind them (#130), however the app collected them.
	return t.narrowed(m, out, Now(), false), nil
}

// caused is an accepted decision with how many deliveries caused it.
type caused struct {
	platform.Event
	hops int
	span trace.SpanContext // the input that caused it, whose trace its delivery continues
	plan *acceptedEvent    // durable work/effect intent; nil for legacy entries
}

// publish queues an accepted decision for its subscribers (Ledger, through Runtime).
func (t *Tenant) publish(e platform.Event) {
	e.Changed = t.Changed(e)
	t.events = append(t.events, caused{Event: e, hops: t.hops, span: t.current()})
	t.acted++
}

// planAcceptedEvent freezes the subscribers and outbound intents while the
// decision is private. Recovery does not rediscover them from a newer
// manifest or changed endpoint subscriptions.
func (t *Tenant) planAcceptedEvent(e platform.Event) acceptedEvent {
	names := append([]string{e.Record.GetSubmission().GetSchema().GetName()}, t.protocolEvents(e)...)
	plan := acceptedEvent{App: e.App, Changed: slices.Clone(e.Changed), Names: names,
		Effects: t.eventEffects(e, names)}
	for _, a := range t.apps {
		if slices.ContainsFunc(a.Manifest().Subscribes, func(x string) bool { return slices.Contains(names, x) }) {
			plan.Subscribers = append(plan.Subscribers, a.Manifest().ID)
		}
	}
	for _, id := range t.listeners {
		if id != e.App && t.app(id).(host.Listener).Interested(names, e) &&
			!slices.Contains(plan.Subscribers, id) {
			plan.Subscribers = append(plan.Subscribers, id)
		}
	}
	return plan
}

func (t *Tenant) publishAccepted(e platform.Event, plan acceptedEvent, version int) {
	t.acted++
	c := caused{Event: e, hops: t.hops, span: t.current()}
	if version >= 2 {
		c.plan = &plan
		c.hops = plan.Hops
	}
	t.events = append(t.events, c)
}

// Deliveries is the tenant's recent event deliveries, oldest first.
func (t *Tenant) Deliveries() []Delivery {
	t.auditMu.Lock()
	defer t.auditMu.Unlock()
	return slices.Clone(t.deliveries)
}

func (t *Tenant) record(a platform.App, kind string, m platform.Member, body []byte, now time.Time) {
	t.changedOwner(a.Manifest().ID)
	if t.Record == nil {
		return
	}
	member, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = t.procs.Versions()
	}
	t.Record(Entry{App: a.Manifest().ID, Kind: kind, Principal: member, Body: body, At: now, Versions: versions})
}

// Replay feeds recorded inputs through the apps that first accepted them, as the
// members they came from; a refusal means the record and the code disagree.
func (t *Tenant) Replay(entries []Entry) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.procs != nil {
		defer t.procs.Pin(nil)
	}
	for i, e := range entries {
		if t.procs != nil {
			t.procs.Pin(e.Versions)
		}
		var m platform.Member
		a := t.app(e.App)
		if a == nil || json.Unmarshal(e.Principal, &m) != nil {
			return fmt.Errorf("entry %d: app %q not enabled or member unreadable", i+1, e.App)
		}
		var err *kernel.Error
		if e.Kind == "effect" { // an outbound attempt's outcome: applied, never sent again
			var o platform.Outcome
			if json.Unmarshal(e.Body, &o) != nil || !t.apply(o, e.At, true) {
				return fmt.Errorf("entry %d: effect outcome for an effect the replay did not create", i+1)
			}
			continue
		}
		if e.Kind == "usage" && t.ai != nil { // a model call's usage: applied, the call never made again
			var u ai.Usage
			if json.Unmarshal(e.Body, &u) != nil {
				return fmt.Errorf("entry %d: bad usage", i+1)
			}
			t.ai.Meter(u)
			continue
		}
		if e.Kind == "agent" && t.agents != nil { // a step an agent's model chose: applied, the model never called again
			var b stepBody
			if json.Unmarshal(e.Body, &b) != nil {
				return fmt.Errorf("entry %d: bad agent step", i+1)
			}
			if err := t.agents.apply(b, e.At, true); err != nil {
				return fmt.Errorf("entry %d: agent step: %v", i+1, err)
			}
			t.enqueue(e.At)
			continue
		}
		if e.Kind == "delivery" || e.Kind == "job" {
			if err := t.replayWork(e.Kind, e.Body, e.At); err != nil {
				return fmt.Errorf("entry %d: %v", i+1, err)
			}
			continue
		}
		if e.Kind == "accepted-result" {
			var envelope struct{ Kind string }
			if err := json.Unmarshal(e.Body, &envelope); err != nil {
				return fmt.Errorf("entry %d: unreadable result envelope: %w", i+1, err)
			}
			if envelope.Kind == "release-result" {
				saved, err := t.applyAcceptedRelease(e.Body)
				if err != nil || saved.App != e.App || saved.Member != m.ID ||
					!journal.SameTime(saved.At, e.At) {
					return fmt.Errorf("entry %d: immutable release result: %v", i+1, err)
				}
				continue
			}
			if envelope.Kind == "composite-result" {
				saved, err := t.applyAcceptedComposite(e.Body)
				if err != nil || saved.App != e.App || saved.Member != m.ID || !journal.SameTime(saved.At, e.At) {
					return fmt.Errorf("entry %d: composite result: %v", i+1, err)
				}
				t.changed()
				continue
			}
			if envelope.Kind == "input-result" {
				saved, applied, err := t.applyAcceptedInput(e.Body)
				if err != nil || !applied || saved.App != e.App || saved.Member != m.ID || !journal.SameTime(saved.At, e.At) {
					return fmt.Errorf("entry %d: connector result: %v (applied=%t)", i+1, err, applied)
				}
				if saved.Refusal == nil {
					t.remember(AuditEntry{At: e.At, Member: m.ID, App: e.App, Action: "input:" + saved.Name})
					t.enqueue(e.At)
				}
				continue
			}
			if envelope.Kind == "effect-result" {
				saved, applied, err := t.applyAcceptedEffect(e.Body)
				if err != nil || !applied || saved.App != e.App || m.ID != "app:"+PlatformApp ||
					!journal.SameTime(saved.At, e.At) {
					return fmt.Errorf("entry %d: effect result: %v (applied=%t)", i+1, err, applied)
				}
				t.enqueue(saved.At)
				continue
			}
			if envelope.Kind == "operation-claim" {
				saved, err := t.applyOperationClaim(e.Body)
				if err != nil || saved.App != e.App || m.ID != "app:"+PlatformApp || !journal.SameTime(saved.At, e.At) {
					return fmt.Errorf("entry %d: operation claim: %v", i+1, err)
				}
				t.opsMu.Lock()
				for _, x := range t.outbound {
					if x.ID == saved.Before.ID {
						x.sending = false
					}
				}
				t.opsMu.Unlock()
				continue
			}
			ra, ok := a.(platform.ResultApp)
			if !ok {
				return fmt.Errorf("entry %d: app %s cannot apply accepted results", i+1, e.App)
			}
			if envelope.Kind == "work-result" {
				saved, decodeErr := decodeAcceptedWork(e.Body)
				if decodeErr != nil || saved.App != e.App || !journal.SameTime(saved.At, e.At) || m.ID != "app:"+e.App {
					return fmt.Errorf("entry %d: invalid work result: %v", i+1, decodeErr)
				}
				if _, err := t.applyAcceptedWork(e.Body); err != nil {
					return fmt.Errorf("entry %d: work result: %w", i+1, err)
				}
				t.enqueue(e.At)
				continue
			}
			if envelope.Kind == "refusal" {
				saved, sub, decodeErr := decodeRefusedResult(e.Body)
				if decodeErr != nil || saved.App != e.App || saved.Tenant != t.ID ||
					!journal.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
					return fmt.Errorf("entry %d: invalid refused result: %v", i+1, decodeErr)
				}
				key := e.App + "/" + sub.GetIdempotencyKey()
				if _, duplicate := t.refusals[key]; duplicate || ra.AcceptedLedger().AcceptedFor(t.ID, sub.GetIdempotencyKey()) != nil {
					return fmt.Errorf("entry %d: duplicate refused result key", i+1)
				}
				t.refusals[key] = saved
				continue
			}
			if envelope.Kind == "record-batch" {
				saved, receipt, decodeErr := decodeAcceptedBatch(e.Body)
				if decodeErr != nil {
					return fmt.Errorf("entry %d: invalid record batch: %w", i+1, decodeErr)
				}
				sub, subErr := batchSubmission(saved, receipt)
				if subErr != nil {
					return fmt.Errorf("entry %d: invalid record batch request: %w", i+1, subErr)
				}
				if saved.App != e.App || saved.Tenant != t.ID || !journal.SameTime(saved.At, e.At) || sub.GetPrincipalId() != m.ID {
					return fmt.Errorf("entry %d: record batch journal identity differs (app=%q/%q tenant=%q/%q principal=%q/%q time=%s/%s)",
						i+1, saved.App, e.App, saved.Tenant, t.ID, sub.GetPrincipalId(), m.ID, saved.At, e.At)
				}
				if _, refused := t.refusals[e.App+"/"+sub.GetIdempotencyKey()]; refused {
					return fmt.Errorf("entry %d: record batch reused a refused key", i+1)
				}
				applied, applyErr := t.applyAcceptedBatch(ra.AcceptedLedger(), e.Body)
				if applyErr != nil || !applied {
					return fmt.Errorf("entry %d: record batch: %v (applied=%t)", i+1, applyErr, applied)
				}
				t.remember(submitted(m.ID, a, sub, saved.At))
				t.publishAcceptedBatch(saved)
				t.enqueue(e.At)
				continue
			}
			saved, receipt, decodeErr := decodeAcceptedResult(e.Body)
			if decodeErr != nil {
				return fmt.Errorf("entry %d: invalid accepted result: %v", i+1, decodeErr)
			}
			if saved.App != e.App || receipt.GetSubmission().GetPrincipalId() != m.ID ||
				saved.Version >= 2 && !journal.SameTime(saved.At, e.At) {
				return fmt.Errorf("entry %d: accepted result app or input clock differs from journal entry", i+1)
			}
			if _, refused := t.refusals[e.App+"/"+receipt.GetSubmission().GetIdempotencyKey()]; refused {
				return fmt.Errorf("entry %d: accepted result reused a refused key", i+1)
			}
			applied, applyErr := t.applyAcceptedResult(ra.AcceptedLedger(), e.Body)
			if applyErr != nil || !applied {
				return fmt.Errorf("entry %d: accepted result: %v (applied=%t)", i+1, applyErr, applied)
			}
			t.remember(submitted(m.ID, a, receipt.GetSubmission(), e.At))
			t.publishAccepted(platform.Event{App: e.App, Record: receipt, Changed: saved.Event.Changed}, saved.Event, saved.Version)
			t.enqueue(e.At)
			continue
		}
		if e.Kind == "submission" {
			s := &pb.Submission{}
			if protojson.Unmarshal(e.Body, s) != nil {
				return fmt.Errorf("entry %d: bad submission", i+1)
			}
			_, err = a.Submit(t.caller(m, a, true), s, e.At)
			t.remember(submitted(m.ID, a, s, e.At))
			t.enqueue(e.At)
		} else {
			_, err = a.Input(t.caller(m, a, true), e.Kind, e.Body, e.At)
			t.remember(AuditEntry{At: e.At, Member: m.ID, App: e.App, Action: "input:" + e.Kind})
			t.enqueue(e.At)
		}
		if err != nil {
			return fmt.Errorf("entry %d (%s %s): %v", i+1, e.App, e.Kind, err)
		}
	}
	return nil
}

func submitted(member string, a platform.App, s *pb.Submission, at time.Time) AuditEntry {
	return AuditEntry{At: at, Member: member, App: a.Manifest().ID, Action: s.GetSchema().GetName(),
		Target: s.GetTarget().GetType() + "/" + s.GetTarget().GetId()}
}

// Catalog is what m may call: each app's actions for m's role there, and an
// action that uses protocol actions only when m may call the bound provider's.
func (t *Tenant) Catalog(m platform.Member) []platform.Action {
	out := []platform.Action{}
	entities := map[string]platform.EntityInfo{}
	for _, info := range t.Entities(m) {
		entities[info.Type] = info
	}
	for _, a := range t.apps {
		for _, action := range a.Manifest().Actions.ForRoles(m.RolesIn(a.Manifest().ID)) {
			if !slices.ContainsFunc(action.Uses, func(used string) bool {
				owner, schema, _ := t.provider(used)
				return owner == nil || !owner.Manifest().Actions.PermitsAny(m.RolesIn(owner.Manifest().ID), schema)
			}) {
				// Generated create/edit forms use this member's writable fields.
				// Custom actions keep their own declared inputs and policy.
				if info, ok := entities[action.Target]; ok && info.App == a.Manifest().ID &&
					slices.Contains(info.Standard, action.Schema) && (action.Schema == info.Type+".create" || action.Schema == info.Type+".edit") {
					action.Payload = slices.DeleteFunc(slices.Clone(action.Payload), func(input platform.Field) bool {
						field, visible := info.Field(input.Name)
						return !visible || field.ReadOnly || !field.Writes(m.Roles[info.App])
					})
				}
				out = append(out, action)
			}
		}
	}
	return out
}

func (t *Tenant) Declarations() []*pb.AuthorityDeclaration {
	var out []*pb.AuthorityDeclaration
	for _, a := range t.apps {
		out = append(out, a.Declarations()...)
	}
	return out
}

// Apps describes the enabled apps from their manifests (ADR-0010): discovery,
// the protocol graph and the capability matrix read this.
func (t *Tenant) Apps() []AppInfo {
	out := []AppInfo{}
	for _, a := range t.apps {
		m := a.Manifest()
		info := AppInfo{ID: m.ID, Version: m.Version, Reads: append([]string{}, m.Reads...), Provides: []string{}, Consumes: []string{},
			Roles: m.AllRoles(), Capabilities: m.Actions.Capabilities(), Inputs: []string{}, Uses: []string{}, Subscribes: append([]string{}, m.Subscribes...), Emits: append([]platform.EffectKind{}, m.Emits...), Interfaces: append([]platform.Interface{}, m.Interfaces...)}
		for input, journaled := range m.Inputs {
			info.Inputs = append(info.Inputs, input+map[bool]string{true: "", false: " (not journaled)"}[journaled])
		}
		slices.Sort(info.Inputs)
		for _, pv := range m.Provides {
			info.Provides = append(info.Provides, pv.Protocol.ID())
		}
		for _, c := range m.Consumes {
			info.Consumes = append(info.Consumes, c.Protocol+map[bool]string{true: " (optional)", false: ""}[c.Optional])
		}
		for _, action := range m.Actions.All() {
			for _, used := range action.Uses {
				info.Uses = append(info.Uses, action.Schema+" → "+used)
			}
		}
		out = append(out, info)
	}
	return out
}

// AppEntry is an app a member may open in the workspace (ADR-0018 D4): the
// tenant runs it and the member holds a role in it.
type AppEntry struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Role  string `json:"role"`
}

// AppsOf are the apps the member holds a role in, in the order the tenant runs them.
func (t *Tenant) AppsOf(m platform.Member) []AppEntry {
	out := []AppEntry{}
	for _, a := range t.apps {
		man := a.Manifest()
		if role := m.Roles[man.ID]; role != "" {
			out = append(out, AppEntry{ID: man.ID, Title: cmp.Or(man.Title, man.ID), Role: role})
		}
	}
	return out
}

type AppInfo struct {
	ID           string                    `json:"id"`
	Version      string                    `json:"version"`
	Reads        []string                  `json:"reads"`
	Roles        []string                  `json:"roles"`
	Capabilities []platform.CapabilityInfo `json:"capabilities"`
	Inputs       []string                  `json:"inputs"`
	Uses         []string                  `json:"uses"`
	Subscribes   []string                  `json:"subscribes"`
	Provides     []string                  `json:"provides"`
	Consumes     []string                  `json:"consumes"`
	Emits        []platform.EffectKind     `json:"emits"`
	Interfaces   []platform.Interface      `json:"interfaces"` // the shapes this app declares (ADR-0058 A2)
}

// checkManifest refuses a manifest the host could not honour: every app's
// declarations are validated where it is composed, so each composition's
// tests check them (#103).
func checkManifest(a platform.App) error {
	m := a.Manifest()
	if m.ID == "" || m.Actions == nil {
		return fmt.Errorf("manifest without ID or action catalog")
	}
	for _, action := range m.Actions.All() {
		if action.Title == "" || action.Description == "" || action.Target == "" || action.Payload == nil || len(action.Roles) == 0 && !action.Automation {
			return fmt.Errorf("action %s lacks a title, description, target, payload fields or roles", action.Schema)
		}
	}
	if _, ok := a.(platform.Runner); len(m.Jobs) > 0 && !ok {
		return fmt.Errorf("declares jobs but has no Run")
	}
	for _, j := range m.Jobs {
		if j.Name == "" || j.Every <= 0 {
			return fmt.Errorf("job %q needs a name and a positive interval", j.Name)
		}
	}
	for i, s := range m.Settings {
		if s.Name == "" || !s.Accepts(s.Default) || slices.ContainsFunc(m.Settings[:i], func(x platform.Setting) bool { return x.Name == s.Name }) {
			return fmt.Errorf("setting %q: unnamed, repeated, or its default is not a %s", s.Name, s.Type)
		}
	}
	for i, s := range m.Sequences {
		if err := s.Check(); err != nil || slices.ContainsFunc(m.Sequences[:i], func(x platform.Sequence) bool { return x.Name == s.Name }) {
			return fmt.Errorf("sequence %q: badly declared or repeated", s.Name)
		}
	}
	for i, e := range m.Emits {
		if e.Name == "" || strings.Contains(e.Name, "/") || slices.ContainsFunc(m.Emits[:i], func(x platform.EffectKind) bool { return x.Name == e.Name }) {
			return fmt.Errorf("effect kind %q: unnamed, repeated or containing '/'", e.Name)
		}
	}
	for _, action := range m.Actions.All() {
		for _, used := range action.Uses {
			protocol, _, ok := strings.Cut(used, "#")
			if !ok || !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
				return fmt.Errorf("action %s uses %s, not an action of a protocol it consumes", action.Schema, used)
			}
		}
	}
	for _, r := range m.Everyone {
		if !slices.Contains(m.Reads, r) {
			return fmt.Errorf("read %q is opened to everyone but not declared", r)
		}
	}
	return nil
}

// Member is a member of the tenant's directory, as the console holds it now:
// for tests and tools that act as a member.
func (t *Tenant) Member(id string) (platform.Member, bool) {
	d, ok := t.app(PlatformApp).(*Console)
	if !ok {
		return platform.Member{}, false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.members[id] // by ID, not by a subject it signs in as
	if m == nil {
		return platform.Member{}, false
	}
	return d.currentMember(m), true
}
