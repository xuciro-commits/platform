package delegation

import (
	"cmp"
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

//go:embed i18n
var languageFiles embed.FS

var languages = platform.LoadLanguages(languageFiles, "i18n")

const maxDelegationHops = 8

var delegationSecret struct {
	sync.RWMutex
	key []byte
}

func UseTokenKey(key []byte) {
	delegationSecret.Lock()
	defer delegationSecret.Unlock()
	if len(key) < 32 {
		delegationSecret.key = nil
		return
	}
	delegationSecret.key = append([]byte("platform-delegation-v1|"), key...)
}

func HasTokenKey() bool {
	delegationSecret.RLock()
	defer delegationSecret.RUnlock()
	return len(delegationSecret.key) > 0
}

type Service struct {
	host        runtimeprotocol.AgentHost
	mu          sync.Mutex
	tenant      string
	authority   platform.Authority
	authorities platform.Authorities
}

func New(tenant string, authorities platform.Authorities) *Service {
	actions := append([]platform.Action{}, delegationActions()...)
	actions = append(actions,
		platform.Action{
			Schema:      runtimeprotocol.SchemaRunCancel,
			Target:      runtimeprotocol.RunType,
			Capability:  "runs",
			Title:       "Stop run",
			Description: "Stop a run; what it did stays done. Anyone may stop a run on their behalf.",
			Roles:       []string{runtimeprotocol.AgentAdmin, platform.AnyMember},
			Payload:     []platform.Field{},
		},
		platform.Action{
			Schema:      runtimeprotocol.SchemaSuspend,
			Target:      runtimeprotocol.SwitchType,
			New:         true,
			Capability:  "switch",
			Title:       "Suspend agent",
			Description: "Stop an agent: active delegations pause and external decisions are refused.",
			Roles:       []string{runtimeprotocol.AgentAdmin},
			Payload:     []platform.Field{{Name: "reason", Type: "string", Description: "Why, for the audit and whom its runs were for"}},
		},
		platform.Action{
			Schema:      runtimeprotocol.SchemaResume,
			Target:      runtimeprotocol.SwitchType,
			Capability:  "switch",
			Title:       "Resume agent",
			Description: "Let a suspended agent work again.",
			Roles:       []string{runtimeprotocol.AgentAdmin},
			Payload:     []platform.Field{},
		},
	)
	return &Service{
		tenant:      tenant,
		authorities: authorities,
		authority: authorities(
			tenant,
			runtimeprotocol.AgentApp,
			platform.NewCatalog(actions...),
			runtimeprotocol.RunType,
			runtimeprotocol.SwitchType,
			runtimeprotocol.DelegationType,
			runtimeprotocol.DelegationWorkerType,
			runtimeprotocol.DelegationEventType,
			runtimeprotocol.DelegationJobType,
		),
	}
}

func (a *Service) AttachAgent(h runtimeprotocol.AgentHost)            { a.host = h }
func (a *Service) AttachOwner(bindings runtimeprotocol.OwnerBindings) { a.AttachAgent(bindings.Agent) }
func (a *Service) NewSimulationApp(tenant string) platform.Service    { return New(tenant, a.authorities) }
func (a *Service) Declarations() []*pb.AuthorityDeclaration           { return a.authority.Declarations() }
func (a *Service) Authority() platform.Authority                      { return a.authority }
func (a *Service) SnapshotFormat() string                             { return "platform.ledger.v1" }
func (a *Service) Snapshot() (json.RawMessage, error)                 { return a.authority.Snapshot() }
func (a *Service) Restore(raw json.RawMessage) error                  { return a.authority.Restore(raw) }

func (a *Service) Fork(authority platform.Authority) (platform.Service, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return &Service{tenant: a.tenant, host: a.host, authority: authority, authorities: a.authorities}, nil
}

func (a *Service) Manifest() platform.Manifest {
	return platform.Manifest{
		Languages: languages,
		ID:        runtimeprotocol.AgentApp,
		Title:     platform.BuiltinManifestTitle(runtimeprotocol.AgentApp),
		Version:   "1",
		Actions:   a.authority.Catalog(),
		Entities:  serviceEntities(),
		Reads:     []string{"runs"},
		Everyone:  []string{"runs"},
		Settings: []platform.Setting{
			{Name: runtimeprotocol.SettingAgentModel, Title: "Model for agents", Type: "text", Default: "", Description: "The enabled model agents call, <provider>/<model>; it must call tools. Empty: agents stop and hand their goal to a person."},
			{Name: runtimeprotocol.SettingAgentModel + "-options", Title: "Agent model options", Type: "text", Default: "", Description: "Input, generation and thinking overrides configured in AI configuration."},
			{Name: runtimeprotocol.SettingAgentDaily, Title: "Tokens per agent per day", Type: "integer", Default: "200000", Description: "An agent that has used this many tokens today stops its runs until tomorrow."},
			{
				Name:        runtimeprotocol.SettingTranscriptDays,
				Title:       "Days transcripts are kept",
				Type:        "integer",
				Default:     "30",
				Description: "Every model call's full request and answer are kept this many days outside the journal, for agent administrators; they may hold personal data. 0: none are kept, and what was kept is forgotten.",
			},
		},
	}
}

func (a *Service) Read(c platform.Caller, name string) (any, *platform.Error) {
	if name == "runs" {
		mine, _ := json.Marshal([]any{[]any{"onBehalf", "=", c.ID}})
		out, _, _ := platform.Find[runtimeprotocol.AgentRunRecord](a.host.Automation(c, runtimeprotocol.AgentApp), platform.Query{Domain: mine, Sort: []string{"-created"}, Limit: 50})
		return out, nil
	}
	return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}

func (a *Service) Start(c platform.Caller, _ *pb.ChangeRecord, _ platform.RunStart, _ time.Time) {
	c.FailDecision(platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "In-host agent runs are replaced by delegations"))
}

func (a *Service) Signal(_ platform.Caller, _ *pb.ChangeRecord, _ string, _ platform.RunSignal, _ time.Time) {
}
func (a *Service) Finished(_ platform.Caller, _, _ string) []string { return nil }
func (a *Service) ApplyAgentStep(_ runtimeprotocol.AgentResultStep, _ time.Time, _ bool) *platform.Error {
	return nil
}

func (a *Service) Interested(_ []string, e platform.Event) bool {
	return a.interestedDelegation(e)
}

func (a *Service) Listen(c platform.Caller, e platform.Event, _ []string, now time.Time) *platform.Error {
	return a.listenDelegation(c, e, now)
}

func (a *Service) automation(app string, replaying bool) platform.Caller {
	c := a.host.Automation(platform.Caller{}, app)
	c.Replaying = replaying
	return c
}

type DelegationTrigger struct {
	Kind   string          `json:"kind"`
	Object string          `json:"object,omitempty"`
	Domain json.RawMessage `json:"domain,omitempty"`
	State  string          `json:"state,omitempty"`
	Field  string          `json:"field,omitempty"`
	Every  string          `json:"every,omitempty"`
}

type Delegation struct {
	platform.Record
	Name        string            `json:"name" field:"required,search" source:"record"`
	Title       string            `json:"title" field:"required,search" source:"record"`
	Trigger     DelegationTrigger `json:"trigger" field:"required" type:"json"`
	Agent       string            `json:"agent" field:"required" source:"record"`
	Model       string            `json:"model,omitempty" source:"record"`
	Prompt      string            `json:"prompt" field:"required" type:"longtext" source:"record"`
	Cwd         string            `json:"cwd,omitempty" source:"record"`
	Program     string            `json:"program,omitempty" source:"record"`
	OnBehalf    string            `json:"onBehalf" field:"required" source:"record"`
	Actions     []string          `json:"actions" type:"json"`
	Notify      string            `json:"notify" choices:"needed,always,never"`
	DailyTokens int               `json:"dailyTokens,omitempty"`
	State       string            `json:"state" field:"readonly" choices:"active,paused"`
	PauseReason string            `json:"pauseReason,omitempty" field:"readonly" source:"record"`
	Version     string            `json:"version,omitempty" field:"readonly" source:"record"`
	LastSweep   time.Time         `json:"lastSweep,omitzero" field:"readonly"`
	SeenKeys    []string          `json:"seenKeys,omitempty" field:"readonly,internal" type:"json"`
}

type DelegationWorker struct {
	platform.Record
	Name string `json:"name" field:"required,search" source:"record"`
}

type DelegationEvent struct {
	platform.Record
	Seq        int       `json:"seq" field:"readonly"`
	ExternalID string    `json:"externalId" field:"readonly" source:"record"`
	Source     string    `json:"source" field:"readonly" source:"record"`
	SourceName string    `json:"sourceName" field:"readonly" source:"record"`
	EventType  string    `json:"eventType" field:"readonly" source:"record"`
	Subject    string    `json:"subject,omitempty" field:"readonly" source:"record"`
	Summary    string    `json:"summary,omitempty" field:"readonly" source:"record"`
	Detail     string    `json:"detail,omitempty" field:"readonly" type:"longtext" source:"user-input"`
	Link       string    `json:"link,omitempty" field:"readonly" source:"record"`
	Hops       int       `json:"hops" field:"readonly"`
	HopBlocked bool      `json:"hopBlocked,omitempty" field:"readonly"`
	Routed     []string  `json:"routed,omitempty" field:"readonly" type:"json"`
	Paused     []string  `json:"paused,omitempty" field:"readonly" type:"json"`
	ReceivedAt time.Time `json:"receivedAt" field:"readonly"`
}

type DelegationAnswer struct {
	Card    int      `json:"card"`
	Title   string   `json:"title"`
	Options []string `json:"options"`
	Choice  *int     `json:"choice"`
	Chosen  *string  `json:"chosen"`
	Text    *string  `json:"text"`
	About   string   `json:"about"`
	By      *string  `json:"by"`
	At      string   `json:"at"`
	Events  []string `json:"events"`
}

type DelegationJob struct {
	platform.Record
	Run           int                `json:"run" field:"readonly"`
	RunRecordID   string             `json:"runRecordId" field:"readonly" source:"record"`
	DelegationID  string             `json:"delegationId" field:"readonly" source:"record"`
	State         string             `json:"state" field:"readonly" choices:"queued,claimed,done,nothing,failed,cancelled"`
	Attempt       int                `json:"attempt" field:"readonly"`
	ClaimKey      string             `json:"claimKey,omitempty" field:"readonly" source:"record"`
	Token         string             `json:"token,omitempty" field:"readonly,internal" source:"record"`
	Session       string             `json:"session,omitempty" field:"readonly" source:"record"`
	Link          string             `json:"link,omitempty" field:"readonly" source:"record"`
	EventIDs      []string           `json:"eventIds,omitempty" field:"readonly" type:"json"`
	Answers       []DelegationAnswer `json:"answers,omitempty" field:"readonly" type:"json"`
	Hops          int                `json:"hops" field:"readonly"`
	ReceiptDigest string             `json:"receiptDigest,omitempty" field:"readonly" source:"record"`
	Summary       string             `json:"summary,omitempty" field:"readonly" source:"user-input"`
	ErrorText     string             `json:"errorText,omitempty" field:"readonly" source:"user-input"`
	TokensUsed    int                `json:"tokensUsed,omitempty" field:"readonly"`
	Day           string             `json:"day,omitempty" field:"readonly" source:"record"`
}

func serviceEntities() []platform.RecordType {
	return []platform.RecordType{
		{
			Type:    runtimeprotocol.RunType,
			Title:   "Agent run",
			Model:   runtimeprotocol.AgentRunRecord{},
			Display: "title",
			Derived: []platform.Derivation{
				{From: "ref", Fields: []string{"seen", "citations"}},
				{From: "steps.sources", Fields: []string{"arguments", "outcome", "calls", "content"}},
				{From: "citations.document", Element: true},
				{From: "draft.type/target", Element: true},
				{From: "*", Fields: []string{"result", "pending"}},
			},
			Withheld: "withheld",
			Scope:    platform.Scope{Participants: func(record any) []string { return []string{record.(runtimeprotocol.AgentRunRecord).OnBehalf} }},
		},
		{
			Type:        runtimeprotocol.SwitchType,
			Title:       "Agent switch",
			Model:       runtimeprotocol.Switch{},
			Description: "An agent's off switch: suspended by an administrator, or working.",
		},
		{
			Type:    runtimeprotocol.DelegationType,
			Title:   "Agent delegation",
			Plural:  "Agent delegations",
			Model:   Delegation{},
			Display: "title",
			Scope: platform.Scope{
				Default: platform.ScopeOwn,
				Owner:   "onBehalf",
				Levels:  map[string]string{runtimeprotocol.AgentAdmin: platform.ScopeTenant},
				Participants: func(record any) []string {
					d := record.(Delegation)
					return []string{d.OnBehalf, d.Created.By}
				},
			},
		},
		{
			Type:    runtimeprotocol.DelegationWorkerType,
			Title:   "Delegation worker",
			Plural:  "Delegation workers",
			Model:   DelegationWorker{},
			Display: "name",
			Scope:   platform.Scope{Levels: map[string]string{runtimeprotocol.AgentAdmin: platform.ScopeTenant}},
		},
		{
			Type:    runtimeprotocol.DelegationEventType,
			Title:   "Delegation event",
			Plural:  "Delegation events",
			Model:   DelegationEvent{},
			Display: "summary",
			Scope:   platform.Scope{Levels: map[string]string{runtimeprotocol.AgentAdmin: platform.ScopeTenant}},
		},
		{
			Type:    runtimeprotocol.DelegationJobType,
			Title:   "Delegation job",
			Plural:  "Delegation jobs",
			Model:   DelegationJob{},
			Display: "runRecordId",
			Scope:   platform.Scope{Levels: map[string]string{runtimeprotocol.AgentAdmin: platform.ScopeTenant}},
		},
	}
}

func delegationActions() []platform.Action {
	return []platform.Action{
		{
			Schema:      runtimeprotocol.SchemaDelegationCreate,
			Target:      runtimeprotocol.DelegationType,
			New:         true,
			Capability:  "delegations",
			Title:       "Create delegation",
			Description: "Delegate matching events, due records or schedules to an external agent within selected actions.",
			Roles:       []string{platform.AnyMember, runtimeprotocol.AgentAdmin},
			Payload: []platform.Field{
				{Name: "name", Type: "string", Required: true, Description: "Stable rule identifier"},
				{Name: "title", Type: "string", Required: true, Description: "Human-readable title"},
				{Name: "trigger", Type: "json", Required: true, Description: "When this delegation runs"},
				{Name: "agent", Type: "string", Required: true, Description: "Target agent CLI or identifier"},
				{Name: "model", Type: "string", Description: "Optional model identifier"},
				{Name: "prompt", Type: "string", Required: true, Description: "Instructions for the agent"},
				{Name: "cwd", Type: "string", Description: "Working directory on the worker"},
				{Name: "program", Type: "string", Description: "Optional CLI executable path"},
				{Name: "onBehalf", Type: "string", Description: "Member whose permissions bound the run"},
				{Name: "actions", Type: "json", Description: "Allowed action schemas"},
				{Name: "notify", Type: "string", Description: "Notification policy: needed, always or never"},
				{Name: "dailyTokens", Type: "integer", Description: "Optional daily token cap"},
			},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationEdit,
			Target:      runtimeprotocol.DelegationType,
			Capability:  "delegations",
			Title:       "Edit delegation",
			Description: "Update a delegation's trigger, instructions, allowed actions or notification policy.",
			Roles:       []string{platform.AnyMember, runtimeprotocol.AgentAdmin},
			Payload: []platform.Field{
				{Name: "title", Type: "string"},
				{Name: "trigger", Type: "json"},
				{Name: "agent", Type: "string"},
				{Name: "model", Type: "string"},
				{Name: "prompt", Type: "string"},
				{Name: "cwd", Type: "string"},
				{Name: "program", Type: "string"},
				{Name: "actions", Type: "json"},
				{Name: "notify", Type: "string"},
				{Name: "dailyTokens", Type: "integer"},
			},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationPause,
			Target:      runtimeprotocol.DelegationType,
			Capability:  "delegations",
			Title:       "Pause delegation",
			Description: "Pause new dispatches for this delegation while retaining recorded events.",
			Roles:       []string{platform.AnyMember, runtimeprotocol.AgentAdmin},
			Payload:     []platform.Field{{Name: "reason", Type: "string"}},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationResume,
			Target:      runtimeprotocol.DelegationType,
			Capability:  "delegations",
			Title:       "Resume delegation",
			Description: "Resume an active delegation once the delegator holds the required permissions.",
			Roles:       []string{platform.AnyMember, runtimeprotocol.AgentAdmin},
			Payload:     []platform.Field{},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationSweep,
			Target:      runtimeprotocol.DelegationType,
			Capability:  "delegations",
			Title:       "Evaluate due delegations",
			Description: "Evaluate due-date and scheduled triggers for a delegation and enqueue matching work.",
			Roles:       []string{platform.AnyMember, runtimeprotocol.AgentAdmin},
			Automation:  true,
			Payload:     []platform.Field{},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationWorkerKey,
			Target:      runtimeprotocol.DelegationType,
			New:         true,
			Capability:  "delegations",
			Title:       "Register delegation worker",
			Description: "Authorize an outbound autoloop worker to claim jobs and submit receipts.",
			Roles:       []string{runtimeprotocol.AgentAdmin},
			Payload:     []platform.Field{{Name: "name", Type: "string", Required: true}},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationIngest,
			Target:      runtimeprotocol.DelegationEventType,
			New:         true,
			Capability:  "delegations",
			Title:       "Record delegation event",
			Description: "Record an inbound or downstream delegation event and route matching active delegations.",
			Automation:  true,
			Payload:     []platform.Field{{Name: "event", Type: "json", Required: true}},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationClaim,
			Target:      runtimeprotocol.DelegationJobType,
			Capability:  "delegations",
			Title:       "Claim delegation job",
			Description: "Assign the next queued delegation run to an authenticated outbound worker.",
			Automation:  true,
			Payload:     []platform.Field{{Name: "claim", Type: "string", Required: true}},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationStarted,
			Target:      runtimeprotocol.DelegationJobType,
			Capability:  "delegations",
			Title:       "Mark delegation run started",
			Description: "Record the worker session and resume link for an active delegation attempt.",
			Automation:  true,
			Payload: []platform.Field{
				{Name: "token", Type: "string", Required: true},
				{Name: "session", Type: "string"},
				{Name: "link", Type: "string"},
			},
		},
		{
			Schema:      runtimeprotocol.SchemaDelegationReceipt,
			Target:      runtimeprotocol.DelegationJobType,
			Capability:  "delegations",
			Title:       "Accept delegation receipt",
			Description: "Atomically settle one turn receipt from an outbound worker.",
			Automation:  true,
			Payload: []platform.Field{
				{Name: "token", Type: "string", Required: true},
				{Name: "digest", Type: "string", Required: true},
				{Name: "receipt", Type: "json", Required: true},
			},
		},
	}
}

func signToken(parts ...string) (string, bool) {
	delegationSecret.RLock()
	defer delegationSecret.RUnlock()
	if len(delegationSecret.key) == 0 {
		return "", false
	}
	mac := hmac.New(sha256.New, delegationSecret.key)
	mac.Write([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(mac.Sum(nil))[:24], true
}

func validSignature(sig string, parts ...string) bool {
	want, ok := signToken(parts...)
	return ok && hmac.Equal([]byte(sig), []byte(want))
}

func signedToken(prefix string, parts ...string) string {
	sig, ok := signToken(parts...)
	if !ok {
		return ""
	}
	return prefix + strings.Join(parts[1:], "_") + "_" + sig
}

func eventRecordID(tenant, id string) string {
	sum := sha256.Sum256([]byte(tenant + "|" + id))
	return "ev-in-" + hex.EncodeToString(sum[:])[:16]
}

func (a *Service) MintWorkerToken(id string) string {
	if strings.TrimSpace(id) == "" {
		return ""
	}
	c := a.automation(runtimeprotocol.AgentApp, false)
	if _, ok := platform.Get[DelegationWorker](c, id); !ok {
		return ""
	}
	return signedToken("dwkr_", "worker", a.tenant, id)
}

func (a *Service) verifyWorkerToken(token string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(token), "dwkr_"+a.tenant+"_")
	if !ok {
		return "", false
	}
	idx := strings.LastIndexByte(rest, '_')
	if idx <= 0 {
		return "", false
	}
	id, sig := rest[:idx], rest[idx+1:]
	if !validSignature(sig, "worker", a.tenant, id) {
		return "", false
	}
	c := a.automation(runtimeprotocol.AgentApp, false)
	if _, exists := platform.Get[DelegationWorker](c, id); !exists {
		return "", false
	}
	return id, true
}

func (a *Service) mintAttemptToken(run int, attempt int) string {
	r, att := strconv.Itoa(run), strconv.Itoa(attempt)
	return signedToken("drun_", "attempt", a.tenant, r, att)
}

func (a *Service) MintRunMCPToken(run int, attempt int) string {
	r, att := strconv.Itoa(run), strconv.Itoa(attempt)
	return signedToken("dmcp_", "mcp", a.tenant, r, att)
}

func (a *Service) ResolveDelegationMCP(token string, now time.Time) (platform.Member, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(token), "dmcp_"+a.tenant+"_")
	if !ok {
		return platform.Member{}, false
	}
	parts := strings.Split(rest, "_")
	if len(parts) != 3 {
		return platform.Member{}, false
	}
	runStr, attStr, sig := parts[0], parts[1], parts[2]
	if !validSignature(sig, "mcp", a.tenant, runStr, attStr) {
		return platform.Member{}, false
	}
	runNum, err1 := strconv.Atoi(runStr)
	attNum, err2 := strconv.Atoi(attStr)
	if err1 != nil || err2 != nil || runNum < 1 || attNum < 1 {
		return platform.Member{}, false
	}
	c := a.automation(runtimeprotocol.AgentApp, false)
	job, exists := platform.Get[DelegationJob](c, jobRecordID(runNum))
	if !exists || job.Attempt != attNum || (job.State != "claimed" && job.State != "queued") {
		return platform.Member{}, false
	}
	dlg, exists := platform.Get[Delegation](c, job.DelegationID)
	if !exists || dlg.State != "active" || (a.host != nil && a.host.AgentSuspended(dlg.OnBehalf)) {
		return platform.Member{}, false
	}
	member, permitted, _ := a.checkDelegatorPermits(dlg)
	if !permitted {
		return platform.Member{}, false
	}
	scopes := make([]string, len(dlg.Actions))
	copy(scopes, dlg.Actions)
	member.Scopes = scopes
	eventID := ""
	if len(job.EventIDs) > 0 {
		eventID = job.EventIDs[0]
	}
	member = runtimeprotocol.WithAICaller(member, &runtimeprotocol.AICaller{
		Kind:    "delegation",
		Name:    dlg.Name,
		Version: dlg.Version,
		Model:   cmp.Or(dlg.Model, "external"),
		Event:   eventID,
		Run:     job.RunRecordID,
		Hops:    job.Hops + 1,
	})
	_ = now
	return member, true
}

func jobRecordID(run int) string { return fmt.Sprintf("job-%d", run) }
func runRecordID(run int) string { return fmt.Sprintf("dlg-%d", run) }

func delegationVersion(d Delegation) string {
	digest, _ := platform.CanonicalDigest([]any{d.Name, d.Title, d.Trigger, d.Agent, d.Model, d.Prompt, d.Cwd, d.Program, d.OnBehalf, d.Actions, d.Notify, d.DailyTokens})
	return "dlg.sha256." + digest[:16]
}

func (a *Service) checkDelegatorPermits(d Delegation) (platform.Member, bool, string) {
	if a.host == nil {
		return platform.Member{}, false, "host unavailable"
	}
	member, ok := a.host.Member(d.OnBehalf)
	if !ok || !member.Active() {
		return platform.Member{}, false, "The delegator account is no longer active"
	}
	if a.host.AgentSuspended("agent:" + d.Agent) {
		return platform.Member{}, false, "The target agent is suspended"
	}
	permitted := a.host.PermittedActions(member)
	for _, schema := range d.Actions {
		if !slices.Contains(permitted, schema) {
			return member, false, "The delegator no longer holds permission for " + schema
		}
	}
	return member, true, ""
}

func validateDelegation(d *Delegation) *platform.Error {
	d.Name = strings.TrimSpace(d.Name)
	d.Title = strings.TrimSpace(d.Title)
	d.Agent = strings.TrimSpace(d.Agent)
	d.Prompt = strings.TrimSpace(d.Prompt)
	d.Cwd = cmp.Or(strings.TrimSpace(d.Cwd), "/tmp")
	d.Notify = cmp.Or(strings.TrimSpace(d.Notify), "needed")
	if d.Name == "" || d.Title == "" || d.Agent == "" || d.Prompt == "" || d.OnBehalf == "" {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Delegation requires name, title, agent, prompt and onBehalf")
	}
	if !slices.Contains([]string{"needed", "always", "never"}, d.Notify) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Delegation notify must be needed, always or never")
	}
	switch d.Trigger.Kind {
	case "object_set":
		if strings.TrimSpace(d.Trigger.Object) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "object_set trigger requires object")
		}
	case "state":
		if strings.TrimSpace(d.Trigger.Object) == "" || strings.TrimSpace(d.Trigger.State) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "state trigger requires object and state")
		}
	case "due":
		if strings.TrimSpace(d.Trigger.Object) == "" || strings.TrimSpace(d.Trigger.Field) == "" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "due trigger requires object and field")
		}
	case "schedule":
		dur, err := time.ParseDuration(strings.TrimSpace(d.Trigger.Every))
		if err != nil || dur < time.Minute {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "schedule trigger requires a duration of at least 1m")
		}
	default:
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Unknown delegation trigger kind")
	}
	for _, act := range d.Actions {
		if platform.StaticControlAction(platform.Action{Schema: act, Target: strings.TrimSuffix(act, "."+lastSegment(act))}) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Delegations cannot include static platform control actions")
		}
	}
	if d.Actions == nil {
		d.Actions = []string{}
	}
	d.Version = delegationVersion(*d)
	return nil
}

func lastSegment(s string) string {
	if idx := strings.LastIndexByte(s, '.'); idx >= 0 {
		return s[idx+1:]
	}
	return s
}

func (a *Service) nextEventSeq(c platform.Caller) int {
	all, _, _ := platform.Find[DelegationEvent](c, platform.Query{})
	best := 0
	for _, ev := range all {
		if ev.Seq > best {
			best = ev.Seq
		}
	}
	return best + 1
}

func (a *Service) nextRunNum(c platform.Caller) int {
	all, _, _ := platform.Find[DelegationJob](c, platform.Query{})
	best := 0
	for _, j := range all {
		if j.Run > best {
			best = j.Run
		}
	}
	return best + 1
}

func (a *Service) tokensUsedToday(c platform.Caller, delegationID, day string) int {
	all, _, _ := platform.Find[DelegationJob](c, platform.Query{})
	total := 0
	for _, j := range all {
		if j.DelegationID == delegationID && j.Day == day {
			total += j.TokensUsed
		}
	}
	return total
}

func (a *Service) pauseDelegationForPermission(c platform.Caller, r *pb.ChangeRecord, d Delegation, reason string, now time.Time) {
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	d.State = "paused"
	d.PauseReason = reason
	ac.Put(r, d)
	ac.Notify(platform.Notification{
		Category: runtimeprotocol.NotificationSystem,
		Title:    "Delegation paused: " + d.Title,
		Body:     reason,
		Ref:      runtimeprotocol.DelegationType + "/" + d.ID,
		Key:      "delegation-paused:" + d.ID,
	}, now, platform.Recipient{Member: d.OnBehalf})
}

func (a *Service) enqueueDelegationEvent(c platform.Caller, r *pb.ChangeRecord, d Delegation, ev DelegationEvent, answers []DelegationAnswer, session string, now time.Time) (routed bool, paused bool, hopBlocked bool) {
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	if d.State != "active" {
		return false, true, false
	}
	member, ok, reason := a.checkDelegatorPermits(d)
	if !ok {
		a.pauseDelegationForPermission(c, r, d, reason, now)
		return false, true, false
	}
	if ev.Hops > maxDelegationHops {
		ac.Notify(platform.Notification{
			Category: runtimeprotocol.NotificationSystem,
			Title:    "Delegation hop limit exceeded: " + d.Title,
			Body:     fmt.Sprintf("Event %s stopped at %d hops (limit %d)", cmp.Or(ev.Subject, ev.ExternalID), ev.Hops, maxDelegationHops),
			Ref:      runtimeprotocol.DelegationType + "/" + d.ID,
			Key:      "delegation-hop:" + d.ID + ":" + ev.ExternalID,
		}, now, platform.Recipient{Member: d.OnBehalf})
		return false, false, true
	}
	day := member.Today(now)
	if d.DailyTokens > 0 && a.tokensUsedToday(ac, d.ID, day) >= d.DailyTokens {
		ac.Notify(platform.Notification{
			Category: runtimeprotocol.NotificationSystem,
			Title:    "Delegation daily token cap reached: " + d.Title,
			Body:     fmt.Sprintf("Used daily budget of %d tokens on %s", d.DailyTokens, day),
			Ref:      runtimeprotocol.DelegationType + "/" + d.ID,
			Key:      "delegation-budget:" + d.ID + ":" + day,
		}, now, platform.Recipient{Member: d.OnBehalf})
		return false, true, false
	}
	runNum := a.nextRunNum(ac)
	runID := runRecordID(runNum)
	scopes := make([]string, len(d.Actions))
	copy(scopes, d.Actions)
	runRec := runtimeprotocol.AgentRunRecord{
		Record:            platform.Record{ID: runID},
		Agent:             d.Agent,
		Title:             d.Title,
		Goal:              cmp.Or(ev.Summary, d.Title),
		Ref:               ev.Subject,
		OnBehalf:          d.OnBehalf,
		Scopes:            scopes,
		State:             "running",
		Model:             d.Model,
		DefinitionVersion: d.Version,
		Release:           a.host.ActiveRelease(),
		Steps:             []runtimeprotocol.RunStep{},
	}
	job := DelegationJob{
		Record:       platform.Record{ID: jobRecordID(runNum)},
		Run:          runNum,
		RunRecordID:  runID,
		DelegationID: d.ID,
		State:        "queued",
		Attempt:      0,
		Session:      session,
		EventIDs:     []string{ev.ID},
		Answers:      answers,
		Hops:         ev.Hops,
		Day:          day,
	}
	ac.Put(r, runRec)
	ac.Put(r, job)
	return true, false, false
}

func formatRecordDataDetail(rawRecord map[string]any) string {
	clean := map[string]any{}
	for k, v := range rawRecord {
		if k == "withheld" || k == "internal" {
			continue
		}
		clean[k] = v
	}
	b, _ := json.Marshal(clean)
	return "资料:\n" + string(b)
}

func (a *Service) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	id := s.GetTarget().GetId()
	schema := s.GetSchema().GetName()

	switch schema {
	case runtimeprotocol.SchemaSuspend, runtimeprotocol.SchemaResume:
		var p struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(s.GetPayload(), &p)
		return a.authority.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
			if id == "" {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			sw, _ := platform.Get[runtimeprotocol.Switch](ac, id)
			suspend := schema == runtimeprotocol.SchemaSuspend
			if sw.Suspended == suspend {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			sw.ID, sw.Suspended, sw.By, sw.Reason = id, suspend, c.ID, strings.TrimSpace(p.Reason)
			return func(r *pb.ChangeRecord) { ac.Put(r, sw) }, nil
		})

	case runtimeprotocol.SchemaRunCancel:
		run, known := platform.Get[runtimeprotocol.AgentRunRecord](ac, id)
		allowed := func() bool {
			return c.Roles[runtimeprotocol.AgentApp] == runtimeprotocol.AgentAdmin || c.Holds("platform", "admin") || (known && run.OnBehalf == c.ID)
		}
		return a.authority.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			if !known || run.State == "done" || run.State == "stopped" {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			run.State = "stopped"
			run.Stopped = "stopped by " + c.ID
			return func(r *pb.ChangeRecord) {
				ac.Put(r, run)
				if numStr, ok := strings.CutPrefix(id, "dlg-"); ok {
					if job, exists := platform.Get[DelegationJob](ac, "job-"+numStr); exists && (job.State == "queued" || job.State == "claimed") {
						job.State = "cancelled"
						ac.Put(r, job)
					}
				}
			}, nil
		})

	case runtimeprotocol.SchemaDelegationWorkerKey:
		var p struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil || strings.TrimSpace(p.Name) == "" {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Worker name is required")
		}
		if !c.Replaying && !HasTokenKey() {
			return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Delegation tokens require a host token signing key")
		}
		allowed := func() bool {
			return c.Holds(runtimeprotocol.AgentApp, runtimeprotocol.AgentAdmin) || c.Holds("platform", "admin")
		}
		return a.authority.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			w := DelegationWorker{Record: platform.Record{ID: id}, Name: strings.TrimSpace(p.Name)}
			return func(r *pb.ChangeRecord) { ac.Put(r, w) }, nil
		})

	case runtimeprotocol.SchemaDelegationCreate:
		var p struct {
			Name        string            `json:"name"`
			Title       string            `json:"title"`
			Trigger     DelegationTrigger `json:"trigger"`
			Agent       string            `json:"agent"`
			Model       string            `json:"model"`
			Prompt      string            `json:"prompt"`
			Cwd         string            `json:"cwd"`
			Program     string            `json:"program"`
			OnBehalf    string            `json:"onBehalf"`
			Actions     []string          `json:"actions"`
			Notify      string            `json:"notify"`
			DailyTokens int               `json:"dailyTokens"`
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		}
		onBehalf := cmp.Or(strings.TrimSpace(p.OnBehalf), c.ID)
		allowed := func() bool {
			return !c.IsAI() && (onBehalf == c.ID || c.Holds(runtimeprotocol.AgentApp, runtimeprotocol.AgentAdmin) || c.Holds("platform", "admin"))
		}
		return a.authority.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			if _, exists := platform.Get[Delegation](ac, id); exists {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
			}
			d := Delegation{
				Record:      platform.Record{ID: id},
				Name:        p.Name,
				Title:       p.Title,
				Trigger:     p.Trigger,
				Agent:       p.Agent,
				Model:       p.Model,
				Prompt:      p.Prompt,
				Cwd:         p.Cwd,
				Program:     p.Program,
				OnBehalf:    onBehalf,
				Actions:     p.Actions,
				Notify:      p.Notify,
				DailyTokens: p.DailyTokens,
				State:       "active",
			}
			if err := validateDelegation(&d); err != nil {
				return nil, err
			}
			if !c.Replaying {
				if _, ok, reason := a.checkDelegatorPermits(d); !ok {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{why}", reason)
				}
			}
			return func(r *pb.ChangeRecord) { ac.Put(r, d) }, nil
		})

	case runtimeprotocol.SchemaDelegationEdit:
		existing, found := platform.Get[Delegation](ac, id)
		allowed := func() bool {
			return found && !c.IsAI() && (existing.OnBehalf == c.ID || c.Holds(runtimeprotocol.AgentApp, runtimeprotocol.AgentAdmin) || c.Holds("platform", "admin"))
		}
		return a.authority.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			if !found {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			var patch map[string]json.RawMessage
			if json.Unmarshal(s.GetPayload(), &patch) != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			raw, _ := json.Marshal(existing)
			var merged map[string]json.RawMessage
			_ = json.Unmarshal(raw, &merged)
			for k, v := range patch {
				if slices.Contains([]string{"title", "trigger", "agent", "model", "prompt", "cwd", "program", "actions", "notify", "dailyTokens"}, k) {
					merged[k] = v
				}
			}
			updatedRaw, _ := json.Marshal(merged)
			var d Delegation
			if json.Unmarshal(updatedRaw, &d) != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			if err := validateDelegation(&d); err != nil {
				return nil, err
			}
			if !c.Replaying {
				if _, ok, reason := a.checkDelegatorPermits(d); !ok {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{why}", reason)
				}
			}
			return func(r *pb.ChangeRecord) { ac.Put(r, d) }, nil
		})

	case runtimeprotocol.SchemaDelegationPause, runtimeprotocol.SchemaDelegationResume:
		existing, found := platform.Get[Delegation](ac, id)
		allowed := func() bool {
			return found && !c.IsAI() && (existing.OnBehalf == c.ID || c.Holds(runtimeprotocol.AgentApp, runtimeprotocol.AgentAdmin) || c.Holds("platform", "admin"))
		}
		return a.authority.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			if !found {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			var p struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(s.GetPayload(), &p)
			if schema == runtimeprotocol.SchemaDelegationPause {
				existing.State = "paused"
				existing.PauseReason = strings.TrimSpace(p.Reason)
			} else {
				if !c.Replaying {
					if _, ok, reason := a.checkDelegatorPermits(existing); !ok {
						return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "{why}", reason)
					}
				}
				existing.State = "active"
				existing.PauseReason = ""
			}
			return func(r *pb.ChangeRecord) { ac.Put(r, existing) }, nil
		})

	case runtimeprotocol.SchemaDelegationSweep:
		existing, found := platform.Get[Delegation](ac, id)
		allowed := func() bool {
			return found && (c.Automation || existing.OnBehalf == c.ID || c.Holds(runtimeprotocol.AgentApp, runtimeprotocol.AgentAdmin) || c.Holds("platform", "admin"))
		}
		return a.authority.Receive(c, s, now, allowed, func() (func(*pb.ChangeRecord), *platform.Error) {
			if !found {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
			}
			return func(r *pb.ChangeRecord) {
				a.sweepOneDelegation(c, r, existing, now)
			}, nil
		})

	case runtimeprotocol.SchemaDelegationIngest:
		return a.receiveIngest(c, s, now)
	case runtimeprotocol.SchemaDelegationClaim:
		return a.receiveClaim(c, s, now)
	case runtimeprotocol.SchemaDelegationStarted:
		return a.receiveStarted(c, s, now)
	case runtimeprotocol.SchemaDelegationReceipt:
		return a.receiveReceipt(c, s, now)
	}
	return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (a *Service) sweepOneDelegation(c platform.Caller, r *pb.ChangeRecord, d Delegation, now time.Time) {
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	if d.State != "active" {
		return
	}
	member, ok, reason := a.checkDelegatorPermits(d)
	if !ok {
		a.pauseDelegationForPermission(c, r, d, reason, now)
		return
	}
	today := member.Today(now)
	switch d.Trigger.Kind {
	case "due":
		rows := a.host.LoadDelegationRecords(member, d.Trigger.Object, d.Trigger.Domain, now)
		changed := false
		for _, row := range rows {
			rowID, _ := row["id"].(string)
			dueVal := fmt.Sprint(cmp.Or(row[d.Trigger.Field], ""))
			if rowID == "" || dueVal == "" || dueVal == "<nil>" {
				continue
			}
			dueDay := dueVal
			if len(dueDay) >= 10 {
				dueDay = dueDay[:10]
			}
			if dueDay > today {
				continue
			}
			seenKey := today + ":" + rowID
			if slices.Contains(d.SeenKeys, seenKey) {
				continue
			}
			d.SeenKeys = append(d.SeenKeys, seenKey)
			if len(d.SeenKeys) > 500 {
				d.SeenKeys = d.SeenKeys[len(d.SeenKeys)-500:]
			}
			changed = true
			seq := a.nextEventSeq(ac)
			ev := DelegationEvent{
				Record:     platform.Record{ID: fmt.Sprintf("ev-%d", seq)},
				Seq:        seq,
				ExternalID: fmt.Sprintf("due:%s:%s:%s", d.ID, rowID, today),
				Source:     "platform",
				SourceName: "platform",
				EventType:  "delegation." + d.Name,
				Subject:    d.Trigger.Object + "/" + rowID,
				Summary:    fmt.Sprintf("Overdue %s %s (%s=%s)", d.Trigger.Object, rowID, d.Trigger.Field, dueDay),
				Detail:     formatRecordDataDetail(row),
				Hops:       0,
				Routed:     []string{d.Name},
				ReceivedAt: now,
			}
			ac.Put(r, ev)
			a.enqueueDelegationEvent(c, r, d, ev, nil, "", now)
		}
		d.LastSweep = now
		if changed || !d.LastSweep.IsZero() {
			ac.Put(r, d)
		}
	case "schedule":
		every, err := time.ParseDuration(d.Trigger.Every)
		if err != nil || (!d.LastSweep.IsZero() && now.Before(d.LastSweep.Add(every))) {
			return
		}
		d.LastSweep = now
		ac.Put(r, d)
		seq := a.nextEventSeq(ac)
		ev := DelegationEvent{
			Record:     platform.Record{ID: fmt.Sprintf("ev-%d", seq)},
			Seq:        seq,
			ExternalID: fmt.Sprintf("schedule:%s:%d", d.ID, now.Unix()),
			Source:     "platform",
			SourceName: "platform",
			EventType:  "delegation." + d.Name,
			Summary:    "Scheduled delegation " + d.Title,
			Detail:     "资料:\n{}",
			Hops:       0,
			Routed:     []string{d.Name},
			ReceivedAt: now,
		}
		ac.Put(r, ev)
		a.enqueueDelegationEvent(c, r, d, ev, nil, "", now)
	}
}

func (a *Service) interestedDelegation(e platform.Event) bool {
	if e.App == runtimeprotocol.AgentApp {
		return false
	}
	s := e.Record.GetSubmission()
	if e.App == runtimeprotocol.WorkApp && s.GetSchema().GetName() == "work.question.answer" {
		return true
	}
	if len(e.Changed) == 0 || a.host == nil {
		return false
	}
	c := a.automation(runtimeprotocol.AgentApp, false)
	delegations, _, _ := platform.Find[Delegation](c, platform.Query{})
	for _, d := range delegations {
		if d.Trigger.Kind != "object_set" && d.Trigger.Kind != "state" {
			continue
		}
		prefix := d.Trigger.Object + "/"
		for _, ref := range e.Changed {
			if strings.HasPrefix(ref, prefix) {
				return true
			}
		}
	}
	return false
}

func (a *Service) listenDelegation(c platform.Caller, e platform.Event, now time.Time) *platform.Error {
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	s := e.Record.GetSubmission()
	if e.App == runtimeprotocol.WorkApp && s.GetSchema().GetName() == "work.question.answer" {
		qid := s.GetTarget().GetId()
		q, exists := platform.Get[runtimeprotocol.AgentQuestion](a.host.Automation(c, runtimeprotocol.WorkApp), qid)
		if !exists || q.Delegation == "" || q.Submission != "" {
			return nil
		}
		dlg, found := platform.Get[Delegation](ac, q.Delegation)
		if !found {
			return nil
		}
		var choiceIdx *int
		var chosenText *string
		if idx := slices.Index(q.Options, q.Answer); idx >= 0 {
			i := idx
			choiceIdx = &i
			val := q.Answer
			chosenText = &val
		}
		by := q.AnsweredBy
		note := q.Outcome
		ans := DelegationAnswer{
			Card:    1,
			Title:   q.Title,
			Options: q.Options,
			Choice:  choiceIdx,
			Chosen:  chosenText,
			Text:    &note,
			About:   q.About,
			By:      &by,
			At:      now.UTC().Format(time.RFC3339),
			Events:  []string{},
		}
		sub := &pb.Submission{
			TenantId:       a.tenant,
			PrincipalId:    c.ID,
			Authority:      runtimeprotocol.AgentApp,
			IdempotencyKey: "delegation-answer:" + qid + ":" + e.Record.GetChangeId(),
			Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationEventType, Id: "ans-" + qid},
			Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationIngest, Version: 1},
			Payload:        platform.Raw(map[string]any{"answerFor": dlg.ID, "answer": ans, "session": q.Key}),
		}
		_, err := a.receiveAnswerResume(c, sub, dlg, ans, q.Key, now)
		return err
	}

	delegations, _, _ := platform.Find[Delegation](ac, platform.Query{Sort: []string{"id"}})
	hops := a.host.EventHops()
	for _, d := range delegations {
		if d.Trigger.Kind != "object_set" && d.Trigger.Kind != "state" {
			continue
		}
		prefix := d.Trigger.Object + "/"
		for _, ref := range e.Changed {
			rowID, ok := strings.CutPrefix(ref, prefix)
			if !ok || rowID == "" {
				continue
			}
			sub := &pb.Submission{
				TenantId:       a.tenant,
				PrincipalId:    c.ID,
				Authority:      runtimeprotocol.AgentApp,
				IdempotencyKey: fmt.Sprintf("delegation-listen:%s:%s:%s", d.ID, e.Record.GetChangeId(), rowID),
				Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationEventType, Id: fmt.Sprintf("ev-%s-%s", d.ID, e.Record.GetChangeId())},
				Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationIngest, Version: 1},
				Payload:        []byte("{}"),
			}
			_, err := a.authority.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
				currentDlg, exists := platform.Get[Delegation](ac, d.ID)
				if !exists || currentDlg.State != "active" {
					return func(*pb.ChangeRecord) {}, nil
				}
				member, permitted, reason := a.checkDelegatorPermits(currentDlg)
				if !permitted {
					return func(r *pb.ChangeRecord) {
						a.pauseDelegationForPermission(c, r, currentDlg, reason, now)
					}, nil
				}
				var domain json.RawMessage
				idCond := []any{"id", "=", rowID}
				if len(currentDlg.Trigger.Domain) > 0 {
					var rawTerms []any
					if json.Unmarshal(currentDlg.Trigger.Domain, &rawTerms) == nil && len(rawTerms) > 0 {
						domain, _ = json.Marshal(append([]any{idCond}, rawTerms...))
					}
				}
				if len(domain) == 0 {
					domain, _ = json.Marshal([]any{idCond})
				}
				rows := a.host.LoadDelegationRecords(member, currentDlg.Trigger.Object, domain, now)
				if len(rows) == 0 {
					return func(*pb.ChangeRecord) {}, nil
				}
				row := rows[0]
				if currentDlg.Trigger.Kind == "state" && fmt.Sprint(row["state"]) != currentDlg.Trigger.State {
					return func(*pb.ChangeRecord) {}, nil
				}
				return func(r *pb.ChangeRecord) {
					seq := a.nextEventSeq(ac)
					ev := DelegationEvent{
						Record:     platform.Record{ID: fmt.Sprintf("ev-%d", seq)},
						Seq:        seq,
						ExternalID: e.Record.GetChangeId() + ":" + rowID,
						Source:     "platform",
						SourceName: "platform",
						EventType:  "delegation." + currentDlg.Name,
						Subject:    ref,
						Summary:    fmt.Sprintf("%s changed on %s", s.GetSchema().GetName(), ref),
						Detail:     formatRecordDataDetail(row),
						Hops:       hops,
						Routed:     []string{currentDlg.Name},
						ReceivedAt: now,
					}
					_, _, hopBlocked := a.enqueueDelegationEvent(c, r, currentDlg, ev, nil, "", now)
					ev.HopBlocked = hopBlocked
					if hopBlocked {
						ev.Routed = []string{}
					}
					ac.Put(r, ev)
				}, nil
			})
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Service) receiveAnswerResume(c platform.Caller, sub *pb.Submission, d Delegation, ans DelegationAnswer, session string, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	ac := a.host.Automation(c, runtimeprotocol.AgentApp)
	return a.authority.Receive(c, sub, now, nil, func() (func(*pb.ChangeRecord), *platform.Error) {
		return func(r *pb.ChangeRecord) {
			seq := a.nextEventSeq(ac)
			ev := DelegationEvent{
				Record:     platform.Record{ID: fmt.Sprintf("ev-%d", seq)},
				Seq:        seq,
				ExternalID: sub.GetIdempotencyKey(),
				Source:     "platform",
				SourceName: "platform",
				EventType:  "delegation." + d.Name + ".answer",
				Summary:    "Answered: " + ans.Title,
				Detail:     "资料:\n{}",
				Hops:       0,
				Routed:     []string{d.Name},
				ReceivedAt: now,
			}
			ac.Put(r, ev)
			a.enqueueDelegationEvent(c, r, d, ev, []DelegationAnswer{ans}, session, now)
		}, nil
	})
}

func (a *Service) SweepDelegations(now time.Time, scope func(func()), submit func(platform.Member, *pb.Submission, time.Time) (*pb.ChangeRecord, *platform.Error)) {
	if scope == nil {
		scope = func(read func()) { read() }
	}
	if a.host == nil {
		return
	}
	var delegations []Delegation
	var findErr error
	scope(func() {
		c := a.automation(runtimeprotocol.AgentApp, false)
		delegations, _, findErr = platform.Find[Delegation](c, platform.Query{Sort: []string{"id"}})
	})
	if findErr != nil {
		return
	}
	for _, d := range delegations {
		if d.State != "active" || (d.Trigger.Kind != "due" && d.Trigger.Kind != "schedule") {
			continue
		}
		if !d.LastSweep.IsZero() && now.Sub(d.LastSweep) < time.Minute {
			continue
		}
		actor := platform.Member{ID: "app:" + runtimeprotocol.AgentApp, Tenant: a.tenant, Roles: map[string]string{runtimeprotocol.AgentApp: runtimeprotocol.AgentAdmin}}
		sub := &pb.Submission{
			TenantId:       a.tenant,
			PrincipalId:    actor.ID,
			Authority:      runtimeprotocol.AgentApp,
			IdempotencyKey: fmt.Sprintf("auto-sweep:%s:%d", d.ID, now.Unix()/60),
			Target:         &pb.EntityRef{Type: runtimeprotocol.DelegationType, Id: d.ID},
			Schema:         &pb.SchemaRef{Name: runtimeprotocol.SchemaDelegationSweep, Version: 1},
			Payload:        []byte("{}"),
		}
		_, _ = submit(actor, sub, now)
	}
}
