package platformserver

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
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

	"google.golang.org/protobuf/proto"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

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
	observationWindows map[string]observationWindowAttempt
	confirmationReads  map[string]statusReadAttempt
	dispatchReads      map[string]statusReadAttempt
	simulation         bool                         // isolated builder execution, never authoritative state
	simulationTrace    func(platform.DecisionTrace) // private trial observation; never snapshotted or installed live
	// Private control views route callbacks back into their owning decision.
	controlAnswer func(platform.Effect, platform.Outcome, time.Time) *kernel.Error
	identity      host.IdentityDirectory
	ID            string
	// Record, when set, makes each accepted input durable before it is answered;
	// a failure must stop the server (ADR-0010).
	Record func(Entry)
	// AcceptResult persists a supported top-level decision or owned-work
	// attempt before any authoritative changes or intents are visible.
	AcceptResult func(Entry, string, string) ([]byte, error)
	// Store keeps what is derived outside the journal: vectors and transcripts
	// (ADR-0009); without one they stay in memory.
	Store         Store
	memStore      journal.Memory // the Store without one
	knowledge     host.Glossary  // the knowledge app: documents are searched, terms read (ADR-0009)
	index         index          // passages cut from documents and knowledge fields
	i18n          translator     // says declarations and messages in a language (ADR-0005)
	agentRun      string         // the run whose agent is submitting, under mu: its effects name it
	mu            sync.Mutex
	streamMu      sync.Mutex                  // one outside-lane source intake pass per tenant
	flowArtifacts flowArtifactGC              // transient preparation/collection exclusion; no authoritative state
	fault         atomic.Pointer[tenantFault] // recovery failure stops this tenant without stopping its neighbors
	apps          []platform.App
	committed     committed               // answers by idempotency key (committed.go)
	releases      releaseStore            // release_store.go
	console       hostControl             // the host console's lifecycle, support sessions, migrations (host_control.go)
	staged        stagedChannel           // the per-call result channel (compute_channel.go)
	definitions   []platform.Definition   // installed code assets; member views are derived on read
	owner         map[string]platform.App // "action:", "read:" and "input:" names → app
	// audit holds accepted top-level inputs, newest last, rebuilt by replay; its
	// own lock, because reads run inside other apps' submissions.
	audit     auditLog // recent inputs, deliveries, personal reads (audit_log.go)
	events    []caused // published during the current input, queued after it
	bindings  map[string]binding
	observers []host.Observer // the platform's views derived from events, inside the input
	change    changes         // what clients follow to refetch (F-32)
	linker    host.Linker     // serves Caller.Link and Caller.Links
	directory host.Directory  // the organisation, when composed
	hops      int             // of the event being handled, for the events it causes
	acted     int             // decisions published and notifications given, ever
	works     *kernel.Works
	// opsMu guards what reads and the runner share: queues, connectors,
	// notifications and settings (operations.go). It is never held while t.mu is taken.
	opsMu sync.Mutex
	work  workBoard // queues, failed deliveries and jobs (work_board.go)
	// Quota is the attempts of owned work each app may make in a minute (ADR-0007 D3); 0: no limit.
	Quota      int
	quota      quota           // attempts per app per minute and the round-robin turn (quota.go)
	breakers   breakers        // per endpoint and AI provider (volatile)
	connectors connectorRoster // registry, descriptor index, last refusals (connector_roster.go)
	notices    noticeBoard     // notifications (notices.go)
	settings   settingValues   // "<app>/<name>" → value (settings.go)
	words      tenantWords     // the tenant's own translations (words.go, ADR-0005)
	sequences  sequences       // number counters (ADR-0026)
	endpoints  []*Endpoint
	outbound   []*effect
	// Secrets resolves a secret's name (default: PLATFORM_SECRETS_DIR, then
	// PLATFORM_SECRET_<NAME>); Outbound sends an effect's request (default: a
	// client refusing private addresses). Tests replace both (ADR-0004).
	Secrets  func(name string) ([]byte, bool)
	Outbound func(req *http.Request, allowPrivate bool) (*http.Response, error)
	// AIClient sends model calls (default: a client refusing private addresses
	// unless the provider is local); tests replace it (ADR-0008).
	AIClient  func(req *http.Request) (*http.Response, error)
	ai        models          // the AI app: the models the host calls (ADR-0008)
	records   *recordStore    // the apps' entity records (ADR-0046)
	tasks     host.Tasks      // serves Caller.Assign: the work app (ADR-0007)
	procs     host.Processes  // the flow app (ADR-0007)
	listeners []string        // platform apps given other apps' events as owned work (host.Listener)
	agents    AgentRuntime    // AI agents (ADR-0054)
	ctx       context.Context // the span of the work being done under mu (telemetry.go)
	// Files keeps file bytes (ADR-0038); nil: memory, for development and tests.
	Files         FileStore
	ComputeWorker WasmWorker
	Compiler      CodeCompiler
	cancels       cancels // running operations' cancel handles
	memFiles      memoryFiles
	uploads       uploads // hashes uploaded and not yet attached (uploads.go)
}

// AuditEntry is one accepted input: who, when, through which app, what.
type AuditEntry struct {
	At     time.Time `json:"at"`
	Member string    `json:"member"`
	App    string    `json:"app"`
	Action string    `json:"action"` // an action's schema, or "input:<name>"
	Target string    `json:"target,omitempty"`
}

// Audit is the tenant's recent accepted inputs, oldest first.
func (t *Tenant) Audit() []AuditEntry { return t.audit.all() }

// NewTenant enables apps for a tenant; it refuses duplicate names, consumed
// protocols no earlier app provides, and manifests the host could not honour.
func NewTenant(id string, apps ...platform.App) (*Tenant, error) {
	t := &Tenant{ID: id, apps: apps, committed: committed{refusals: map[string]refusedResult{}}, owner: map[string]platform.App{}, bindings: map[string]binding{}, works: kernel.NewWorks(), work: newWorkBoard(),
		connectors: newConnectorRoster(), records: newRecordStore()}
	t.staged = stagedChannel{tenant: id, files: t.files}
	t.i18n = translator{apps: func() []platform.App { return t.apps }, tenant: t.words.clone}
	claim := func(name string, a platform.App) error {
		if other := t.owner[name]; other != nil {
			return fmt.Errorf("tenant %s: %q is declared by %s and %s", id, name, other.Manifest().ID, a.Manifest().ID)
		}
		t.owner[name] = a
		return nil
	}
	for i, a := range apps {
		m := a.Manifest()
		if a.AcceptedLedger() == nil {
			return nil, fmt.Errorf("tenant %s: app %s has no decision ledger", id, m.ID)
		}
		if err := t.bind(i, a); err != nil {
			return nil, err
		}
		// Roles the host's own apps take (ADR-0016), by the interfaces they implement.
		host.Attach(a, hostView{t: t, app: a})
		if x, ok := a.(host.Observer); ok {
			t.observers = append(t.observers, x)
		}
		if x, ok := a.(host.Linker); ok {
			t.linker = x
		}
		if x, ok := a.(host.Directory); ok {
			t.directory = x
		}
		if identity, ok := a.(host.IdentityDirectory); ok {
			if t.identity != nil {
				return nil, fmt.Errorf("tenant %s has two identity owners", id)
			}
			t.identity = identity
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
		if x, ok := a.(AgentRuntime); ok {
			if t.agents != nil {
				return nil, fmt.Errorf("tenant %s has two agent owners", id)
			}
			t.agents = x
		}
		if x, ok := a.(host.Glossary); ok {
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
			t.work.addJob(&Task{ID: "job:" + m.ID + "/" + j.Name, Kind: "job", App: m.ID, Title: j.Title, State: "scheduled", job: j})
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
	for _, a := range apps { // agents, once every app is composed (ADR-0054); flows may give them steps
		if len(a.Manifest().Agents) == 0 {
			continue
		}
		if t.agents == nil {
			return nil, fmt.Errorf("tenant %s: %s declares agents, and the tenant runs no agent app", id, a.Manifest().ID)
		}
		if err := t.agents.DeclareAgentApp(a); err != nil {
			return nil, fmt.Errorf("tenant %s: %v", id, err)
		}
	}
	for _, a := range apps { // flows, once every app is composed (ADR-0007)
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
	{ // interfaces, once every entity is described (ADR-0020 A2)
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
	for _, et := range t.records.sortedTypes() {
		if err := t.checkRecordSetDeclarations(et.info); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.info.Type, err)
		}
		if err := platform.CheckConstraints(et.info, t.entity, true); err != nil {
			return nil, fmt.Errorf("tenant %s object %s: %w", id, et.info.Type, err)
		}
	}
	var assetOwners []platform.AssetLifecycleOwner
	for _, app := range apps {
		if owner, ok := app.(platform.AssetLifecycleOwner); ok {
			assetOwners = append(assetOwners, owner)
		}
	}
	if _, err := platform.NewAssetRegistry(assetOwners...); err != nil {
		return nil, fmt.Errorf("tenant %s: %v", id, err)
	}
	for _, app := range apps {
		if ready, ok := app.(host.CompositionReady); ok {
			if err := ready.Ready(); err != nil {
				return nil, fmt.Errorf("tenant %s owner %s readiness: %w", id, app.Manifest().ID, err)
			}
		}
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
func (t *Tenant) Submit(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return t.submit(m, s, now, nil)
}

// submitDiagnosed captures one invocation, including its original durable refusal.
func (t *Tenant) submitDiagnosed(m platform.Member, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error, []platform.FieldIssue) {
	var issues []platform.FieldIssue
	record, err := t.submit(m, s, now, func(_ *kernel.Error, found []platform.FieldIssue) { issues = found })
	return record, err, t.readableFieldIssues(m, issues, now)
}

func (t *Tenant) submit(m platform.Member, s *pb.Submission, now time.Time, report func(*kernel.Error, []platform.FieldIssue), contentVersion ...string) (record *pb.ChangeRecord, err *kernel.Error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	a := t.owner["action:"+s.GetSchema().GetName()]
	retired := a == nil
	if a == nil {
		// Retirement removes an exposed action, not its original receipts.
		a = t.app(s.GetAuthority())
		if a == nil {
			return nil, unknown()
		}
	}
	if s.GetSchema().GetName() == "flow.dead-letter.replay" {
		if record, refusal, handled := t.prepareContinuousDeadLetterReplay(m, s, now, contentVersion...); handled {
			return record, refusal
		}
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	if retired {
		key := a.Manifest().ID + "/" + s.GetIdempotencyKey()
		_, refused := t.committed.refusals[key]
		if !refused && len(t.committed.answers[key]) == 0 && a.AcceptedLedger().AcceptedFor(t.ID, s.GetIdempotencyKey()) == nil {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
		}
	}
	// A builder role held only through projects is bounded to their assets,
	// from this submission's target under the tenant lock, before any app decides it.
	if err = t.delegatedBound(m, s); err != nil {
		return nil, err
	}
	var binding *platform.SchemaVersion
	var schemaRefusal *kernel.Error
	if len(contentVersion) > 0 {
		binding, schemaRefusal = t.submissionSchemaVersion(a, s, contentVersion[0])
		if schemaRefusal != nil && schemaRefusal.Code == pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT {
			return nil, schemaRefusal
		}
	} else {
		s = t.currentSubmissionSchema(a, s)
	}
	if prior, ok := t.committed.refusals[s.GetAuthority()+"/"+s.GetIdempotencyKey()]; ok {
		hash, _ := submissionHash(s, binding)
		if hash != prior.RequestHash || s.GetPrincipalId() != m.ID {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		if report != nil {
			report(&prior.Error, prior.Issues)
		}
		answer := prior.Error
		return nil, &answer
	}
	end := t.begin("submit "+s.GetSchema().GetName(), trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID),
		attribute.String("platform.target", target(s)), attribute.String("platform.member", m.ID))
	defer func() { end(outcomeOf(err)) }()
	record, err = t.submitAcceptedVersion(a, m, s, now, false, binding, schemaRefusal, nil)
	if report != nil && err != nil {
		if prior, ok := t.committed.refusals[a.Manifest().ID+"/"+s.GetIdempotencyKey()]; ok {
			hash, _ := submissionHash(s, binding)
			if prior.RequestHash == hash {
				report(err, prior.Issues)
			}
		}
	}
	return record, err
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

func (t *Tenant) submitAccepted(a platform.App, m platform.Member, s *pb.Submission, now time.Time, automation bool, input ...*preparedOperationInput) (record *pb.ChangeRecord, refusal *kernel.Error) {
	return t.submitAcceptedVersion(a, m, t.currentSubmissionSchema(a, s), now, automation, nil, nil, nil, input...)
}

func (t *Tenant) submitAcceptedVersion(a platform.App, m platform.Member, s *pb.Submission, now time.Time, automation bool, binding *platform.SchemaVersion, schemaRefusal *kernel.Error, observation *ObservationDecisionEvidence, input ...*preparedOperationInput) (record *pb.ChangeRecord, refusal *kernel.Error) {
	// A malformed transport identity cannot reserve a durable key for another
	// tenant, authority or member. The kernel still supplies its ordinary
	// refusal, but no result under this tenant can represent that submission.
	if s.GetTenantId() != t.ID || s.GetPrincipalId() != m.ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if s.GetAuthority() != a.Manifest().ID {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_AUTHORITY}
	}
	if s.GetIdempotencyKey() == "" || s.GetTarget() == nil || s.GetSchema() == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission has an invalid identity")
	}
	ledger := a.AcceptedLedger()
	if saved := t.committed.answers[a.Manifest().ID+"/"+s.GetIdempotencyKey()]; len(saved) > 0 {
		result, receipt, err := decodeAcceptedBatch(saved)
		hash, hashErr := submissionHash(s, binding)
		if err != nil || hashErr != nil || result.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return receipt, nil
	}
	if prior := ledger.AcceptedFor(t.ID, s.GetIdempotencyKey()); prior != nil {
		if !proto.Equal(prior.GetSubmission(), s) {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		saved, bound := t.committed.schemaVersions[a.Manifest().ID+"/"+s.GetIdempotencyKey()]
		if bound != (binding != nil) || binding != nil && saved != *binding {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return prior, nil
	}
	hash, err := submissionHash(s, binding)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "The submission cannot be encoded")
	}
	if prior, ok := t.committed.refusals[a.Manifest().ID+"/"+s.GetIdempotencyKey()]; ok {
		if prior.RequestHash != hash {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		answer := prior.Error
		return nil, &answer
	}
	if err := t.checkBusinessCorrection(m, s, binding); err != nil {
		return nil, err
	}
	if m.Agent && t.suspended(m.ID) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The agent {agent} is suspended", m.ID)
	}
	draft := t.newStagedDecision()
	draft.observationInput = observation
	draft.schemaVersion = binding
	if len(input) > 0 {
		draft.operationInputs = make(map[string][]*preparedOperationInput, len(input))
		for _, prepared := range input {
			if prepared != nil {
				draft.operationInputs[prepared.requestHash] = append(draft.operationInputs[prepared.requestHash], prepared)
			}
		}
	}
	if schemaRefusal != nil {
		refusal = schemaRefusal
	} else {
		record, refusal = decideAcceptedAs(a, draft, m, s, now, automation)
	}
	if draft.failure != nil {
		refusal = draft.failure
		record = nil
	}
	if refusal == nil && pendingOperationInputs(draft.operationInputs) > 0 {
		record = nil
		refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A prepared Compute input was not consumed by this accepted Flow decision")
	}
	var raw []byte
	if refusal != nil {
		refusal = explained(refusal, a, s.GetSchema().GetName(), target(s))
		fact, business := draft.businessRefusals[refusal]
		var businessFact *platform.BusinessRefusalFact
		if business {
			businessFact = &fact
		}
		raw, err = encodeBusinessRefusedResult(s, now, refusal, binding, businessFact, draft.inputIssues[refusal]...)
	} else {
		raw, err = draft.sealResult(a.Manifest().ID, s, record, now)
		if err != nil {
			refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
			var budget *acceptedResultBudgetError
			if errors.As(err, &budget) {
				refusal = platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Accepted result needs {bytes} bytes; limit is {limit} bytes / 接受结果超出字节预算", budget.Bytes, budget.Limit)
			}
			raw, err = encodeSchemaRefusedResult(s, now, refusal, binding)
		}
	}
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "This action cannot produce a bounded accepted result")
	}
	member, _ := json.Marshal(m)
	var versions map[string]int
	if t.procs != nil {
		versions = draft.flowVersions()
	}
	committed, err := t.acceptResult(Entry{App: a.Manifest().ID, Kind: "accepted-result",
		Principal: member, Body: raw, At: now, Versions: versions}, s.GetIdempotencyKey(), hash)
	if err != nil {
		if err == errAcceptedConflict {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_IDEMPOTENCY_CONFLICT}
		}
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The accepted result could not be committed")
	}
	return t.finishCommittedResult(a, m, hash, committed)
}

func (t *Tenant) finishCommittedResult(a platform.App, m platform.Member, hash string, committed []byte) (record *pb.ChangeRecord, refusal *kernel.Error) {
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
		t.committed.refusals[saved.App+"/"+sub.GetIdempotencyKey()] = saved
		t.changedOwner(saved.App)
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
			t.audit.remember(submitted(m.ID, a, sub, saved.At))
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
		t.audit.remember(submitted(m.ID, a, receipt.GetSubmission(), saved.At))
		t.publishAccepted(platform.Event{App: a.Manifest().ID, Record: receipt, Changed: saved.Event.Changed}, saved.Event, saved.Version)
		t.changedOwner(saved.App)
	}
	t.enqueue(saved.At)
	return receipt, nil
}

// Only decision evaluation may turn an unsupported-effect panic into a
// refusal. A panic after the journal commit must escape so the tenant stops.
func decideAccepted(a platform.App, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time) (record *pb.ChangeRecord, refusal *kernel.Error) {
	return decideAcceptedAs(a, draft, m, s, now, false)
}

func decideAcceptedAs(a platform.App, draft *stagedDecision, m platform.Member, s *pb.Submission, now time.Time, automation bool) (record *pb.ChangeRecord, refusal *kernel.Error) {
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
		draft.tenant.owner["action:"+platform.WorkApprovalRequestSchema] != nil {
		record, refusal = draft.requestApproval(a, m, s, now)
	} else {
		record, refusal = platform.Decide(platform.NewCaller(draft, m, a.Manifest().ID, false, automation), a, s, now)
	}
	if refusal == nil {
		refusal = draft.finishMigrationRetirements(now)
	}
	if refusal == nil {
		refusal = draft.answerRequests(now)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal == nil {
		refusal = draft.checkFinalConstraints()
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
// Input routes a connector input (push batch, poll page, heartbeat) to its app.
func (t *Tenant) Input(m platform.Member, name string, body []byte, now time.Time) (out any, err *kernel.Error) {
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	a := t.owner["input:"+name]
	if a == nil {
		return nil, unknown()
	}
	if refusal := authorizationRefusal(t.authorizeEntry(m, a.Manifest().ID, "input:"+name, "action", now)); refusal != nil {
		t.refused(m.ID, name, refusal, now)
		return nil, refusal
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.quarantined() {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	end := t.begin("input "+name, trace.SpanContext{}, attribute.String("platform.app", a.Manifest().ID), attribute.String("platform.member", m.ID))
	defer func() { end(outcomeOf(err)) }()
	if a.Manifest().Inputs[name] {
		accepted, ok := a.(platform.AcceptedInputApp)
		if !ok || !slices.Contains(accepted.AcceptedInputs(), name) {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT,
				"This connector input has no accepted-result boundary")
		}
		return t.inputAccepted(accepted, m, name, body, now)
	}
	defer t.enqueue(now)
	out, err = inputApp(a, t.caller(m, a, false), name, body, now)
	if err != nil {
		t.refused(m.ID, name, err, now)
	}
	return out, err
}

// Read serves a named read of the app that declares it, to members holding a
// role in that app or to everyone when the manifest says so; the app may refuse further.
func (t *Tenant) Read(m platform.Member, name string) (any, *kernel.Error) {
	return t.readFrom(m, t.owner["read:"+name], name)
}

// ReadFrom keeps a composed form's reads on the action's declared owner.
// Authorization and result projection stay on Read's path.
func (t *Tenant) ReadFrom(m platform.Member, app, name string) (any, *kernel.Error) {
	a := t.app(app)
	if a == nil || !slices.Contains(a.Manifest().Reads, name) {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	return t.readFrom(m, a, name)
}

func (t *Tenant) readFrom(m platform.Member, a platform.App, name string) (any, *kernel.Error) {
	m = t.authorizationMember(m)
	if err := t.admits(m); err != nil {
		return nil, err
	}
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	entry := t.authorizeEntry(m, a.Manifest().ID, name, "read", Now())
	if name == "packages" && !m.Holds(PlatformApp, Admin) && !m.Holds(PlatformApp, Auditor) {
		caller := t.caller(m, a, false)
		entry = caller.May("build", "build.app.create", []string{"builder"})
		if !m.InScope("build.app.create") {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
		}
	}
	if refusal := authorizationRefusal(entry); refusal != nil {
		return nil, refusal
	}
	out, err := readApp(a, t.caller(m, a, false), name)
	if err != nil {
		return nil, err
	}
	// An app chooses which records its read answers with; the host keeps what
	// leaves no wider than the member's authority over the fields and sources
	// behind them (#130), however the app collected them.
	return t.narrowedSource(m, out, Now(), false, a.Manifest().ReadSources[name]), nil
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
func (t *Tenant) Deliveries() []Delivery { return t.audit.recentDeliveries() }

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
				if version, err := t.actionSchemaVersion(a, action); err == nil {
					action.SchemaVersion = &version
				}
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

// Apps describes the enabled apps from their manifests (ADR-0016): discovery,
// the protocol graph and the capability matrix read this.
func (t *Tenant) Apps() []AppInfo {
	out := []AppInfo{}
	for _, a := range t.apps {
		m := a.Manifest()
		info := AppInfo{ID: m.ID, Title: m.Title, Version: m.Version, Reads: append([]string{}, m.Reads...), Provides: []string{}, Consumes: []string{},
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

// AppEntry is an app a member may open in the workspace (ADR-0019): the
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
	Title        string                    `json:"title"` // the manifest's, in the caller's language
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
	Interfaces   []platform.Interface      `json:"interfaces"` // the shapes this app declares (ADR-0020 A2)
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
	for name, source := range m.ReadSources {
		if !slices.Contains(m.Reads, name) || !slices.Contains([]string{"record", "user-input", "external-input"}, source) {
			return fmt.Errorf("read %q has an undeclared name or invalid original source type %q", name, source)
		}
	}
	return nil
}

// Member is a member of the tenant's directory, as the console holds it now:
// for tests and tools that act as a member.
func (t *Tenant) Member(id string) (platform.Member, bool) {
	if t.identity == nil {
		return platform.Member{}, false
	}
	return t.identity.MemberByID(id)
}

func readApp(app platform.App, c platform.Caller, name string) (any, *kernel.Error) {
	if reader, ok := app.(platform.Reader); ok {
		return reader.Read(c, name)
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func inputApp(app platform.App, c platform.Caller, name string, body []byte, at time.Time) (any, *kernel.Error) {
	if receiver, ok := app.(platform.InputReceiver); ok {
		return receiver.Input(c, name, body, at)
	}
	return nil, unknown()
}
