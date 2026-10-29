package platform

import (
	"encoding/json"
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"slices"
	"strings"
	"time"
)

const ConfirmationType = "build.confirmation"
const ConfirmationAnswerSchema = ConfirmationType + ".answer"

// ConfirmationTransport is provided by the caller's private decision, so
// compiled owner closures never retain a live host as their writing context.
type ConfirmationTransport interface {
	FreezeConfirmationProtocol(ConfirmationParty) (*ConfirmationProtocol, error)
	FreezeConfirmation(Caller, string, Confirmation, time.Time) (string, []ReleaseAsset, error)
	QueueConfirmation(Caller, *pb.ChangeRecord, ConfirmationRun, int, bool, time.Time) *kernel.Error
	CompleteConfirmation(Caller, *pb.ChangeRecord, ConfirmationRun, string, time.Time) (string, *kernel.Error)
	ResolveConfirmationDelivery(Caller, ConfirmationRun, int, bool, time.Time) *kernel.Error
	RetryConfirmationDelivery(Caller, ConfirmationRun, int, bool, time.Time) *kernel.Error
	CheckConfirmationRead(ConfirmationRun, int, time.Time) *kernel.Error
}

type ConfirmationRuntime interface{ ConfirmationTransport() ConfirmationTransport }

type ConfirmationCompletionRuntime interface {
	ConfirmationCompletion(Caller, string, string, string) bool
}

func (c Caller) ConfirmationCompletion(object, id, action string) bool {
	runtime, ok := c.rt.(ConfirmationCompletionRuntime)
	return ok && runtime.ConfirmationCompletion(c, object, id, action)
}

func (c Caller) ConfirmationTransport() ConfirmationTransport {
	rt, ok := c.rt.(ConfirmationRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return nil
	}
	return rt.ConfirmationTransport()
}

// Confirmation declares coordination after a single authority accepts its
// transaction. Each party's request, reply and compensation stays independent.
type Confirmation struct {
	Parties         []ConfirmationParty `json:"parties"`
	DeadlineSeconds int                 `json:"deadlineSeconds"`
	OnPartial       string              `json:"onPartial"`
	OnExpire        string              `json:"onExpire"`
	OnComplete      string              `json:"onComplete"`
	OnPartialAction string              `json:"onPartialAction,omitempty"`
	ReviewerRole    string              `json:"reviewerRole"`
}

type ConfirmationParty struct {
	Name                string            `json:"name"`
	Title               string            `json:"title"`
	Writeback           string            `json:"writeback,omitempty"`
	Protocol            string            `json:"protocol,omitempty"`
	Action              string            `json:"action,omitempty"`
	Target              string            `json:"target,omitempty"`
	CompensateWriteback string            `json:"compensateWriteback,omitempty"`
	CompensateAction    string            `json:"compensateAction,omitempty"`
	Reconcile           *ConfirmationRead `json:"reconcile,omitempty"`
}

type ConfirmationRead struct {
	KeyInput     string          `json:"keyInput,omitempty"`
	Source       string          `json:"source"`
	KeyField     string          `json:"keyField"`
	ResultField  string          `json:"resultField"`
	Expected     json.RawMessage `json:"expected"`
	EverySeconds int             `json:"everySeconds"`
}

func (c Confirmation) Check() error {
	if len(c.Parties) < 1 || len(c.Parties) > 8 || c.DeadlineSeconds < 1 || c.DeadlineSeconds > 604800 || !slices.Contains([]string{"hold", "proceed-with-flag", "reject"}, c.OnPartial) || !slices.Contains([]string{"hold", "escalate", "compensate"}, c.OnExpire) || c.ReviewerRole == "" || c.OnComplete == "" {
		return fmt.Errorf("Confirmation needs 1–8 parties, a bounded deadline, completion action, reviewer and explicit partial/expiry policies")
	}
	if c.OnPartial != "hold" && c.OnPartialAction == "" {
		return fmt.Errorf("Partial confirmation needs an explicit outcome action")
	}
	seen := map[string]bool{}
	for _, p := range c.Parties {
		if !functionName.MatchString(p.Name) || p.Title == "" || seen[p.Name] || (p.Writeback == "") == (p.Protocol == "") || p.Protocol != "" && p.Action == "" {
			return fmt.Errorf("Each confirmation party needs a unique name, title and one declared transport")
		}
		seen[p.Name] = true
		if p.Writeback != "" && p.CompensateAction != "" || p.Protocol != "" && p.CompensateWriteback != "" {
			return fmt.Errorf("Compensation uses the party's declared transport")
		}
		if c.OnExpire == "compensate" && p.CompensateWriteback == "" && p.CompensateAction == "" {
			return fmt.Errorf("Compensation must be declared for every party")
		}
		if r := p.Reconcile; r != nil {
			if r.Source == "" || r.KeyField == "" || r.ResultField == "" || !json.Valid(r.Expected) || len(r.Expected) > 4096 || r.EverySeconds < 1 || r.EverySeconds > 86400 {
				return fmt.Errorf("Reconciliation needs a source, match/result fields, expected value and bounded period")
			}
		}
	}
	return nil
}

type ConfirmationRun struct {
	Record
	Subject         string                 `json:"subject" field:"readonly,search" source:"record"`
	Action          string                 `json:"action" field:"readonly" source:"record"`
	Member          string                 `json:"member" field:"readonly" source:"record"`
	State           string                 `json:"state" field:"readonly" choices:"pending,partial,confirmed,expired,rejected,compensating,compensated" source:"record"`
	Deadline        time.Time              `json:"deadline" field:"readonly"`
	Partial         bool                   `json:"partial" field:"readonly"`
	Escalated       bool                   `json:"escalated,omitempty" field:"readonly"`
	Definition      Confirmation           `json:"definition" field:"readonly,aside" type:"json"`
	Release         string                 `json:"release,omitempty" field:"readonly" source:"record"`
	Assets          []ReleaseAsset         `json:"assets" field:"readonly,aside" type:"json"`
	Input           json.RawMessage        `json:"input" field:"readonly,aside" type:"json"`
	Parties         []ConfirmationProgress `json:"parties" field:"readonly" type:"json"`
	Reviewers       []string               `json:"reviewers" field:"readonly"`
	Completion      string                 `json:"completion,omitempty" field:"readonly" source:"record"`
	CompletionError string                 `json:"completionError,omitempty" field:"readonly" source:"record"`
	Withheld        bool                   `json:"withheld,omitempty" field:"readonly"`
}

type ConfirmationProgress struct {
	Name                string                   `json:"name"`
	Title               string                   `json:"title"`
	State               string                   `json:"state"`
	Intent              string                   `json:"intent"`
	Key                 string                   `json:"key"`
	At                  time.Time                `json:"at,omitzero"`
	Answer              json.RawMessage          `json:"answer,omitempty"`
	Reason              string                   `json:"reason,omitempty"`
	Request             json.RawMessage          `json:"request,omitempty"`
	RequestVersion      int                      `json:"requestVersion,omitempty"`
	Source              json.RawMessage          `json:"source,omitempty"`
	Connection          json.RawMessage          `json:"connection,omitempty"`
	SourceVersion       string                   `json:"sourceVersion,omitempty"`
	Protocol            *ConfirmationProtocol    `json:"protocol,omitempty"`
	CompensationRequest json.RawMessage          `json:"compensationRequest,omitempty"`
	Compensation        string                   `json:"compensation,omitempty"`
	CompensationState   string                   `json:"compensationState,omitempty"`
	CompensationAt      time.Time                `json:"compensationAt,omitzero"`
	CompensationAnswer  json.RawMessage          `json:"compensationAnswer,omitempty"`
	CompensationReason  string                   `json:"compensationReason,omitempty"`
	ReconcileDue        time.Time                `json:"reconcileDue,omitzero"`
	Differences         []ConfirmationDifference `json:"differences,omitempty"`
}

// ConfirmationProtocol freezes the selected provider and its declared mapping.
type ConfirmationProtocol struct {
	Provider         string `json:"provider"`
	Version          string `json:"version"`
	Action           string `json:"action"`
	CompensateAction string `json:"compensateAction,omitempty"`
}

type ConfirmationDifference struct {
	At       time.Time       `json:"at"`
	Matched  bool            `json:"matched"`
	Observed json.RawMessage `json:"observed,omitempty"`
	Error    string          `json:"error,omitempty"`
}

// PendingConfirmations reads every unfinished run through the original paged
// record reader. Historical completed runs cannot hide due work past page one.
func PendingConfirmations(c Caller) ([]ConfirmationRun, *kernel.Error) {
	var all []ConfirmationRun
	for {
		rows, count, err := Find[ConfirmationRun](c, Query{Domain: json.RawMessage(`[["state","!=","compensated"],["completion","=",""],["completionError","=",""]]`), Sort: []string{"id"}, Limit: 500, Offset: len(all)})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The confirmation inventory changed while being read")
		}
	}
}

const DispatchType = "build.dispatch"
const DispatchAnswerSchema = DispatchType + ".answer"
const DispatchReviewSchema = DispatchType + ".review"
const DispatchTickSchema = DispatchType + ".tick"

// DispatchCommand belongs to the object's declared command table. Its
// writeback supplies the request format; addresses and credentials stay in
// the selected connection. ReceiverDeduplicates is an explicit dependency
// on the receiver, never a guarantee made by the platform.
type DispatchCommand struct {
	Name                 string `json:"name"`
	Title                string `json:"title"`
	EndpointKind         string `json:"endpointKind"`
	Writeback            string `json:"writeback"`
	Idempotent           bool   `json:"idempotent"`
	ReceiverDeduplicates bool   `json:"receiverDeduplicates,omitempty"`
	Compensate           string `json:"compensate,omitempty"`
}

func (c DispatchCommand) SafeRetry() bool { return c.Idempotent || c.ReceiverDeduplicates }

func (c DispatchCommand) Check() error {
	if !functionName.MatchString(c.Name) || strings.TrimSpace(c.Title) == "" || !functionName.MatchString(c.EndpointKind) || c.Writeback == "" || c.Compensate != "" && !functionName.MatchString(c.Compensate) {
		return fmt.Errorf("Dispatch commands need a name, title, endpoint kind and declared writeback")
	}
	return nil
}

type Dispatch struct {
	Command         string       `json:"command"`
	Target          string       `json:"target"`
	Payload         []TxnSet     `json:"payload,omitempty"`
	Key             string       `json:"key"`
	DeadlineSeconds int          `json:"deadlineSeconds"`
	Readback        DispatchRead `json:"readback"`
	OnStuck         string       `json:"onStuck"`
	OnFail          string       `json:"onFail"`
	ReviewerRole    string       `json:"reviewerRole"`
}

// Exactly one published source or observation supplies the status. States
// maps the external value to accepted/running/completed/stuck/failed/unknown.
type DispatchRead struct {
	Name         string            `json:"name"`
	Source       string            `json:"source,omitempty"`
	Observation  string            `json:"observation,omitempty"`
	Record       string            `json:"record,omitempty"`
	KeyField     string            `json:"keyField,omitempty"`
	ResultField  string            `json:"resultField,omitempty"`
	ReasonField  string            `json:"reasonField,omitempty"`
	States       map[string]string `json:"states"`
	EverySeconds int               `json:"everySeconds"`
}

func (d Dispatch) Check(command DispatchCommand) error {
	if err := command.Check(); err != nil {
		return err
	}
	if d.Command != command.Name || d.Target == "" || d.ReviewerRole == "" || d.DeadlineSeconds < 1 || d.DeadlineSeconds > 604800 {
		return fmt.Errorf("Dispatch needs a declared command, external target, bounded deadline and reviewer role")
	}
	if d.Key != "$subject" && !strings.HasPrefix(d.Key, "record.") {
		return fmt.Errorf("Dispatch business key must come from the subject record")
	}
	for _, policy := range []string{d.OnStuck, d.OnFail} {
		if !slices.Contains([]string{"manual", "escalate", "compensate", "retry"}, policy) {
			return fmt.Errorf("Dispatch needs explicit stuck and failure policies")
		}
		if policy == "retry" && !command.SafeRetry() {
			return fmt.Errorf("A command that cannot be safely resent cannot use automatic retry")
		}
		if policy == "compensate" && command.Compensate == "" {
			return fmt.Errorf("Dispatch compensation must name a declared reverse command")
		}
	}
	r := d.Readback
	methods := 0
	for _, source := range []string{r.Source, r.Observation, r.Record} {
		if source != "" {
			methods++
		}
	}
	if !functionName.MatchString(r.Name) || methods != 1 || (r.Source != "" || r.Record != "") && (r.KeyField == "" || r.ResultField == "") || r.EverySeconds < 1 || r.EverySeconds > 86400 || len(r.States) == 0 || len(r.States) > 64 || len(d.Payload) > 64 {
		return fmt.Errorf("Dispatch needs one named status read, status mapping and bounded period")
	}
	for value, state := range r.States {
		if value == "" || !slices.Contains([]string{"accepted", "running", "completed", "stuck", "failed", "unknown"}, state) {
			return fmt.Errorf("Dispatch status mapping contains an unknown state")
		}
	}
	return nil
}

type DispatchRun struct {
	Record
	Subject             string            `json:"subject" field:"readonly,search" source:"record"`
	Action              string            `json:"action" field:"readonly" source:"record"`
	Member              string            `json:"member" field:"readonly" source:"record"`
	Command             DispatchCommand   `json:"command" field:"readonly" type:"json"`
	Definition          Dispatch          `json:"definition" field:"readonly,aside" type:"json"`
	Target              string            `json:"target" field:"readonly" source:"record"`
	Key                 string            `json:"key" field:"readonly" source:"record"`
	State               string            `json:"state" field:"readonly" choices:"pending,accepted,running,completed,stuck,failed,unknown,cancelled,compensating,compensated" source:"record"`
	Reason              string            `json:"reason,omitempty" field:"readonly" source:"record"`
	StartedAt           time.Time         `json:"startedAt" field:"readonly"`
	StateAt             time.Time         `json:"stateAt" field:"readonly"`
	Deadline            time.Time         `json:"deadline" field:"readonly"`
	ReadDue             time.Time         `json:"readDue" field:"readonly"`
	Readback            []DispatchReading `json:"readback" field:"readonly" type:"json"`
	Attempts            []DispatchAttempt `json:"attempts" field:"readonly" type:"json"`
	Attempt             int               `json:"attempt" field:"readonly"`
	Intent              string            `json:"intent" field:"readonly" source:"record"`
	Request             json.RawMessage   `json:"request" field:"readonly,aside" type:"json"`
	Source              json.RawMessage   `json:"source,omitempty" field:"readonly,aside" type:"json"`
	Connection          json.RawMessage   `json:"connection,omitempty" field:"readonly,aside" type:"json"`
	Observation         json.RawMessage   `json:"observation,omitempty" field:"readonly,aside" type:"json"`
	Device              string            `json:"device,omitempty" field:"readonly" source:"record"`
	CompensationCommand *DispatchCommand  `json:"compensationCommand,omitempty" field:"readonly,aside" type:"json"`
	CompensationRequest json.RawMessage   `json:"compensationRequest,omitempty" field:"readonly,aside" type:"json"`
	Compensating        bool              `json:"compensating,omitempty" field:"readonly"`
	Release             string            `json:"release,omitempty" field:"readonly" source:"record"`
	Assets              []ReleaseAsset    `json:"assets" field:"readonly,aside" type:"json"`
	Input               json.RawMessage   `json:"input" field:"readonly,aside" type:"json"`
	Reviewers           []string          `json:"reviewers" field:"readonly"`
	Escalated           bool              `json:"escalated,omitempty" field:"readonly"`
	PolicyApplied       bool              `json:"policyApplied,omitempty" field:"readonly"`
	Withheld            bool              `json:"withheld,omitempty" field:"readonly"`
}

type DispatchAttempt struct {
	Number       int             `json:"number"`
	Intent       string          `json:"intent"`
	Target       string          `json:"target"`
	State        string          `json:"state"`
	At           time.Time       `json:"at"`
	Reason       string          `json:"reason,omitempty"`
	Answer       json.RawMessage `json:"answer,omitempty"`
	Compensation bool            `json:"compensation,omitempty"`
}

type DispatchReading struct {
	At       time.Time       `json:"at"`
	State    string          `json:"state"`
	Value    string          `json:"value,omitempty"`
	Reason   string          `json:"reason,omitempty"`
	Observed json.RawMessage `json:"observed,omitempty"`
}

type DispatchTransport interface {
	FreezeDispatch(Caller, string, string, Dispatch, time.Time) (string, []ReleaseAsset, error)
	QueueDispatch(Caller, *pb.ChangeRecord, DispatchRun, time.Time) *kernel.Error
	ResolveDispatchDelivery(Caller, DispatchRun, time.Time) *kernel.Error
	CheckDispatchRead(DispatchRun, time.Time) *kernel.Error
	RetargetDispatch(Caller, DispatchRun, string, time.Time) (json.RawMessage, json.RawMessage, *kernel.Error)
}

type DispatchRuntime interface{ DispatchTransport() DispatchTransport }

func (c Caller) DispatchTransport() DispatchTransport {
	rt, ok := c.rt.(DispatchRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return nil
	}
	return rt.DispatchTransport()
}

func PendingDispatches(c Caller) ([]DispatchRun, *kernel.Error) {
	var all []DispatchRun
	for {
		rows, count, err := Find[DispatchRun](c, Query{Domain: json.RawMessage(`[["state","!=","completed"],["state","!=","cancelled"],["state","!=","compensated"]]`), Sort: []string{"id"}, Limit: 500, Offset: len(all)})
		if err != nil {
			return nil, err
		}
		all = append(all, rows...)
		if len(all) >= count {
			return all, nil
		}
		if len(rows) == 0 {
			return nil, Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "The dispatch inventory changed while being read")
		}
	}
}

// EffectKind declares an effect an app sends to whatever endpoint the tenant
// binds to it.
type EffectKind struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Irreversible marks what cannot be recalled once received (a posting, a
	// payment, an email to people outside): caused by an agent, it is held until
	// a person approves it (D6).
	Irreversible bool `json:"irreversible,omitempty"`
}

// Effect is one intent for one endpoint and what became of it.
type Effect struct {
	Compensates   string          `json:"compensates,omitempty" source:"record"`
	Applications  []AssetBinding  `json:"applications,omitempty" field:"readonly" type:"json"`
	ID            string          `json:"id" source:"record"` // the idempotency key: <tenant>:<app>:<change id or key>:<endpoint>
	Endpoint      string          `json:"endpoint" source:"record"`
	Event         string          `json:"event" source:"record"`         // the event or effect kind
	App           string          `json:"app,omitempty" source:"record"` // the app that emitted it; empty for webhooks
	Key           string          `json:"key,omitempty" source:"record"` // the app's key for it
	Target        string          `json:"target" source:"record"`
	At            time.Time       `json:"at"`
	State         string          `json:"state" source:"record"`           // held, pending, retrying, unknown, delivered, rejected, failed, discarded
	Agent         string          `json:"agent,omitempty" source:"record"` // the AI agent that caused a held effect
	Run           string          `json:"run,omitempty" source:"record"`   // and its run
	Attempts      int             `json:"attempts"`
	Last          time.Time       `json:"last,omitzero"`
	Due           time.Time       `json:"due,omitzero"`
	Error         string          `json:"error,omitempty"`
	PublicMessage string          `json:"publicMessage,omitempty"` // owner-approved delivery text, safe without the request or answer
	CallbackError string          `json:"callbackError,omitempty"`
	Digest        string          `json:"digest,omitempty" source:"record"` // sha256 of the body last sent
	Body          string          `json:"body,omitempty"`                   // kept 30 days for support (D8)
	Generation    uint32          `json:"generation,omitempty"`             // K9 generation of an internal operation attempt
	Output        json.RawMessage `json:"output,omitempty"`                 // accepted internal computation output
	Millis        int64           `json:"millis,omitempty"`
	Withheld      bool            `json:"withheld,omitempty"`
}

// Outcome is what an attempt learned; it is the journal entry of the attempt.
type Outcome struct {
	Effect        string          `json:"effect"`
	Result        string          `json:"result"` // delivered, rejected, retry, unknown
	Detail        string          `json:"detail,omitempty"`
	PublicMessage string          `json:"publicMessage,omitempty"` // safe delivery text supplied by the owner; Detail remains restricted
	Digest        string          `json:"digest,omitempty"`
	Answer        json.RawMessage `json:"answer,omitempty"` // the receiver's body, for app effects (up to 64 KiB of JSON)
	// Staged references the sealed per-call result channel when an operation's
	// result exceeds its inline budget: Answer then carries
	// the handle, and the bytes live in the host's sealed artifact.
	Staged     *StagedResult `json:"staged,omitempty"`
	Generation uint32        `json:"generation,omitempty"` // rejects stale internal compute completions
	Millis     int64         `json:"millis,omitempty"`
}

// StagedResult is one call's entry in the per-call result channel: where the
// sealed bytes are, what schema they claim and the digest that proves them.
type StagedResult struct {
	Tenant string `json:"tenant"`
	Call   string `json:"call"`
	Schema string `json:"schema"`
	Key    string `json:"key"`
	Digest string `json:"digest"`
	Size   int    `json:"size"`
}

// Emit sends data as an effect of kind (declared in the app's manifest) to every
// endpoint bound to it; key names the effect within the app and kind, so the
// same fact of the business is sent once whatever retries or replays do. It is
// called inside an input, so replay rebuilds the intent and never sends it.
// It returns how many endpoints will receive it. An irreversible kind emitted
// for an agent is held until a person approves it (D6).
func (c Caller) Emit(kind, key, entity string, data any, now time.Time) (int, *kernel.Error) {
	if c.rt == nil {
		return 0, notFound()
	}
	return c.rt.Emit(c, kind, key, entity, data, now)
}

// Protocol is a named, versioned interface apps provide and consume:
// actions (payload fields and meaning), reads (result shape, by convention the
// protocol package's types) and events. Consumers depend on a protocol, never on
// the app that provides it.
type Protocol struct {
	Name    string
	Version int
	Actions []Action // Schema is the protocol action's short name; Roles stay the provider's
	Reads   []string
	Events  []ProtocolEvent
}

type ProtocolEvent struct {
	Name  string `json:"name"`
	Title string `json:"title"` // how the timeline tells it, e.g. "Booking canceled"
}

// ProtocolBinding publishes an object's existing actions and record read as a
// versioned protocol. It never introduces an execution or authorization path.
type ProtocolBinding struct {
	Contract *Protocol         `json:"contract,omitempty"`
	Name     string            `json:"name"`
	Version  int               `json:"version"`
	Actions  map[string]string `json:"actions"`
	Reads    []string          `json:"reads,omitempty"`
	Events   map[string]string `json:"events,omitempty"`
}

// CheckProtocolActions checks the wire payload, not the provider's roles or
// presentation. The contract and binding remain the original declarations.
func CheckProtocolActions(contract Protocol, actions []Action) error {
	for _, required := range contract.Actions {
		index := slices.IndexFunc(actions, func(a Action) bool { return a.Schema == required.Schema })
		if index < 0 || len(actions[index].Payload) != len(required.Payload) {
			return fmt.Errorf("protocol %s action %s has an incompatible payload", contract.ID(), required.Schema)
		}
		kind := func(typ string) string {
			switch typ {
			case "string", "text", "longtext":
				return "text"
			case "number", "decimal":
				return "decimal"
			}
			return typ
		}
		for _, field := range required.Payload {
			matched := slices.ContainsFunc(actions[index].Payload, func(actual Field) bool {
				return actual.Name == field.Name && kind(actual.Type) == kind(field.Type) && actual.Required == field.Required && actual.Ref == field.Ref && slices.Equal(actual.Choices, field.Choices)
			})
			if !matched {
				return fmt.Errorf("protocol %s action %s field %s differs", contract.ID(), required.Schema, field.Name)
			}
		}
	}
	if len(actions) != len(contract.Actions) {
		return fmt.Errorf("protocol %s action set differs", contract.ID())
	}
	return nil
}

func CheckProtocol(contract, actual Protocol) error {
	if contract.ID() != actual.ID() {
		return fmt.Errorf("protocol contract identity differs")
	}
	if err := CheckProtocolActions(contract, actual.Actions); err != nil {
		return err
	}
	names := func(p Protocol) []string {
		out := append([]string{}, p.Reads...)
		for _, event := range p.Events {
			out = append(out, "event:"+event.Name)
		}
		slices.Sort(out)
		return out
	}
	if !slices.Equal(names(contract), names(actual)) {
		return fmt.Errorf("protocol %s read or event declarations differ", contract.ID())
	}
	return nil
}

// ID is "<name>/<version>", e.g. "orders.confirm/1".
func (p Protocol) ID() string { return fmt.Sprintf("%s/%d", p.Name, p.Version) }

// Provision maps a protocol onto the provider's own actions, reads and events
// (each keyed by the protocol's name, valued by the provider's).
type Provision struct {
	Protocol Protocol
	Actions  map[string]string // protocol action → provider action schema (same payload)
	Reads    map[string]string // protocol read → provider read (returning the protocol's types)
	Events   map[string]string // protocol event → provider action schema whose decisions are that event
}

// Consumption declares a protocol an app uses; an optional one may have no provider.
type Consumption struct {
	Protocol string // ID
	Optional bool
}

// ProtocolAction names a protocol action or event: "<protocol id>#<name>".
func ProtocolAction(protocol, action string) string { return protocol + "#" + action }

// ProviderResult is one provider's result of a protocol read, with the type of
// the entities it holds, so consumers match their links exactly.
type ProviderResult struct {
	Provider string
	Type     string // e.g. "build.orders"
	Result   any
}

// Probe asks the tenant's provider, in a decision's rules, whether it would
// accept a protocol action from the member now: its policy and rules run, and
// nothing is recorded or applied. NOT_FOUND says no provider is bound. In a
// replay it holds, as it did, when a provider is bound. Rules never change another app:
// what they need of one is a Request, made once the decision is accepted.
func (c Caller) Probe(protocol, action, id string, payload any, now time.Time) *kernel.Error {
	if c.rt == nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.Probe(c, protocol, action, id, Raw(payload), now)
}

// Request is a protocol action an accepted decision asks of the tenant's provider.
// The host submits it after the decision, as the member, and
// submits the provider's Answer to the app's Reply action on the decision's target.
//
// A request of a model names no protocol: Model is
// "<provider>/<model>", or empty for the tenant's model for apps resolved when
// the decision is accepted (later settings cannot change it); Payload is a
// Prompt; Target is what the answer is about. The host asks the model after
// the decision, outside its lock and as the app, within the app's limits, and
// submits the Answer — its Text the model's — to Reply; a replay asks nothing.
type Request struct {
	Tag                string
	Provider           string // optional original provider; rebinding cannot move an in-flight request
	Key                string // optional stable intent identity across explicit retries
	Protocol, Action   string
	Model              string
	Target             string // the provider's record the action is about
	Payload            any
	Reply              string      // the app's own action that receives the Answer; a member may take it too (D5)
	Evaluation         bool        // this model answer belongs to a measured function evaluation
	EvaluationConfig   string      // accepted digest of the provider/model configuration, for evaluation only
	EvaluationFunction *AIFunction // pinned function contract for evaluation output and budget checks
}

// Prompt is what an app asks a model: instructions and the question.
type Prompt struct {
	System    string `json:"system,omitempty"`
	User      string `json:"user"`
	MaxTokens int    `json:"maxTokens,omitempty"`
}

// Answer is how the provider answered a Request: the payload of the Reply action.
// A member who records an answer themselves gives the same fields.
type Answer struct {
	Tag     string `json:"tag,omitempty"`
	Call    string `json:"call"`             // the request's target at the provider
	Action  string `json:"action,omitempty"` // the protocol action
	Outcome string `json:"outcome"`          // accepted or refused; a protocol's events may add their own, e.g. released
	Code    string `json:"code,omitempty"`   // the refusal's code
	Reason  string `json:"reason,omitempty"` // why, as the provider said it (F-23): a person reads it at the consumer
	Ref     string `json:"ref,omitempty"`    // "<type>/<id>" of the provider's record
	Text    string `json:"text,omitempty"`   // a model's answer
	// Model effect callbacks alone supply these measurements. A missing meter
	// is distinct from a measured zero; providers may omit tokens or price.
	Metered        bool    `json:"metered,omitempty"`
	TokensReported bool    `json:"tokensReported,omitempty"`
	InputTokens    int     `json:"inputTokens,omitempty"`
	OutputTokens   int     `json:"outputTokens,omitempty"`
	CostReported   bool    `json:"costReported,omitempty"`
	CostUSD        float64 `json:"costUsd,omitempty"`
	LatencyMillis  int64   `json:"latencyMillis,omitempty"`
	ServedModel    string  `json:"servedModel,omitempty"`
}

// AnswerFields are the payload fields of a Reply action.
func AnswerFields() []Field {
	return []Field{{Name: "call", Type: "string", Required: true, Description: "What was asked of the provider"},
		{Name: "action", Type: "string", Description: "The protocol action"},
		{Name: "outcome", Type: "string", Required: true, Description: "accepted or refused", Choices: []string{"accepted", "refused", "released"}},
		{Name: "code", Type: "string", Description: "The refusal's code"},
		{Name: "reason", Type: "string", Description: "Why it was refused, as the provider said"},
		{Name: "ref", Type: "string", Description: "The provider's record"},
		{Name: "text", Type: "string", Description: "A model's answer"}}
}

// FunctionAnswerFields adds measured model data only to AI function replies;
// ordinary protocol and manually recorded answers keep their smaller shape.
func FunctionAnswerFields() []Field {
	return append(AnswerFields(),
		Field{Name: "metered", Type: "boolean", Description: "The provider was called"},
		Field{Name: "tokensReported", Type: "boolean", Description: "The provider reported token counts"},
		Field{Name: "inputTokens", Type: "integer", Description: "Reported input tokens"},
		Field{Name: "outputTokens", Type: "integer", Description: "Reported output tokens"},
		Field{Name: "costReported", Type: "boolean", Description: "The provider reported a USD cost"},
		Field{Name: "costUsd", Type: "decimal", Description: "Reported USD cost"},
		Field{Name: "latencyMillis", Type: "integer", Description: "Model call latency in milliseconds"},
		Field{Name: "servedModel", Type: "string", Description: "Provider-reported model when different"})
}

// Request asks for a protocol action once the decision r is accepted: made in
// the decision's apply, run by the host after it in order, and run again by a replay.
func (c Caller) Request(r *pb.ChangeRecord, q Request) {
	if c.rt != nil && !c.rt.Probing() {
		c.rt.Request(c, r, q)
	}
}

// Raw is a payload as JSON: bytes as they are, anything else marshalled.
func Raw(v any) []byte {
	if b, ok := v.([]byte); ok {
		return b
	}
	if b, ok := v.(json.RawMessage); ok {
		return b
	}
	out, _ := json.Marshal(v)
	return out
}

// Query reads a protocol read from every provider of the tenant, the bound one
// first: switching the binding sends new calls elsewhere, but what the other
// providers hold stays visible (#99). Only a consumer of the protocol may query it.
func (c Caller) Query(protocol, read string, provider ...string) ([]ProviderResult, *kernel.Error) {
	if c.rt == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.Query(c, protocol, read, provider...)
}

// BoundProvider identifies the selected provider without reading its records.
func (c Caller) BoundProvider(protocol string) (string, *kernel.Error) {
	if c.rt == nil {
		return "", &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	return c.rt.BoundProvider(c, protocol)
}

// current reviewer membership. There is no independent decision or scheduler.
type ConfirmationHost interface {
	ConfirmationTransport
	Holding(string, string) []string
}

type DispatchHost interface {
	DispatchTransport
	Holding(string, string) []string
}

type RelationsHost interface {
	OwnerOf(dataClass string) (app string, ok bool)
	ProtocolEvent(name string) (ProtocolEvent, bool)
	As(c Caller, app string) Caller
	Automation(parent Caller, app string) Caller
	Member(id string) (Member, bool)
	Readable(m Member, ref string, now time.Time) bool
}

type AttachedRelations interface{ AttachRelations(RelationsHost) }

const (
	RelationsApp     = "relations"
	RelationLinkType = "platform.link"
	CommentType      = "platform.comment"
	FollowType       = "platform.follow"
)

// Comment is a member's comment on a record; @member mentions notify them.
type RecordComment struct {
	Record
	Target   string   `json:"target" field:"readonly" title:"On" source:"record"`
	Text     string   `json:"text" field:"required,search" type:"longtext"`
	By       string   `json:"by" field:"readonly" source:"record"`
	Mentions []string `json:"mentions,omitempty" field:"readonly"`
}

// Follow is a member following a record: they hear of its decisions and comments.
type RecordFollow struct {
	Record
	Member string `json:"member" field:"readonly"`
	Target string `json:"target" field:"readonly" title:"Follows"`
}

// FollowID is the one follow of member on target.
func FollowID(member, target string) string {
	return member + "@" + strings.ReplaceAll(target, "/", "~")
}

type RelationLink struct {
	From string    `json:"from" source:"record"` // "<type>/<id>"
	To   string    `json:"to" source:"record"`
	By   string    `json:"by" source:"record"`
	At   time.Time `json:"at"`
}

// Note is one protocol event on an entity's timeline, told by the app that decided it.
type RelationNote struct {
	Entity string    `json:"entity" source:"record"`
	By     string    `json:"by" source:"record"`
	At     time.Time `json:"at"`
	Title  string    `json:"title,omitempty" source:"record" from:"Entity"` // a protocol event's title, said in the reader's language (F-37)
	Text   string    `json:"text"`
}

// Links are the entities linked to entity ("<type>/<id>") that c sees.
func (c Caller) Links(entity string) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Links(c, entity)
}

// Link relates two entities for c (an app linking what it just did, for example).
func (c Caller) Link(from, to *pb.EntityRef, key string, now time.Time) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Link(c, from, to, key, now)
}

// Breaker is a destination's breaker as Settings shows it.
type Breaker struct {
	Destination string    `json:"destination"` // endpoint:<id> or ai:<provider>
	State       string    `json:"state"`       // closed, open or probing
	Failures    int       `json:"failures"`
	Until       time.Time `json:"until,omitzero"`
}
