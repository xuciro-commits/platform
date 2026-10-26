package platform

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/proto"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

const TxnMaxSteps = 256
const TxnMaxIterations = 1000
const TxnMaxExecutionSteps = TxnMaxSteps * TxnMaxIterations

// BusinessTxn is a bounded, single-authority decision. Guards, assignments and
// iteration predicates all use the original  evaluator.
type BusinessTxn struct {
	Subject      string        `json:"subject"`
	Steps        []TxnStep     `json:"steps"`
	Guard        *Expr         `json:"guard,omitempty"`
	Message      string        `json:"message,omitempty"`
	Reads        *RuleReadSpec `json:"reads,omitempty"`
	Confirmation *Confirmation `json:"confirmation,omitempty"`
	Dispatch     *Dispatch     `json:"dispatch,omitempty"`
	Refusals     []Refusal     `json:"refusals,omitempty"`
}

type TxnStep struct {
	Name      string        `json:"name"`
	Kind      string        `json:"kind"`
	Target    TxnRef        `json:"target,omitempty"`
	Changes   []TxnSet      `json:"changes,omitempty"`
	Sequence  *Sequence     `json:"sequence,omitempty"`
	When      *Expr         `json:"when,omitempty"`
	Link      *TxnLink      `json:"link,omitempty"`
	Post      *TxnPost      `json:"post,omitempty"`
	Journal   *TxnJournal   `json:"journal,omitempty"`
	Effect    *TxnEffect    `json:"effect,omitempty"`
	Work      *TxnWork      `json:"work,omitempty"`
	Call      *TxnCall      `json:"call,omitempty"`
	ForEach   *TxnForEach   `json:"forEach,omitempty"`
	Intervals *TxnIntervals `json:"intervals,omitempty"`
	Refusals  []Refusal     `json:"refusals,omitempty"`
}

// TxnIntervals projects the accepted, typed observation context into an
// ordinary declared object. Its original collection and relation own history.
type TxnIntervals struct {
	Source       string            `json:"source"`
	Set          string            `json:"set"`
	Predecessors AssetBinding      `json:"predecessors"`
	Fields       TxnIntervalFields `json:"fields"`
}

type TxnIntervalFields struct {
	Subject           string `json:"subject"`
	Source            string `json:"source"`
	SourceVersion     string `json:"sourceVersion"`
	SourceRevision    string `json:"sourceRevision"`
	ProcessingVersion string `json:"processingVersion"`
	Since             string `json:"since"`
	Until             string `json:"until"`
	OpenStart         string `json:"openStart"`
	OpenEnd           string `json:"openEnd"`
	Retired           string `json:"retired"`
	Review            string `json:"review"`
}

func (t *TxnIntervals) UnmarshalJSON(raw []byte) error {
	type plain TxnIntervals
	return strictTxn(raw, (*plain)(t))
}

type TxnPost struct {
	Match    []TxnSet `json:"match"`
	Field    string   `json:"field"`
	Amount   Expr     `json:"amount"`
	Subtract bool     `json:"subtract,omitempty"`
	Floor    bool     `json:"floor,omitempty"`
}

// Journal records the intent inside Build's accepted result. Core keeps its
// independent authority and decides whether the balanced entry can be posted.
type TxnJournal struct {
	Reverses string `json:"reverses,omitempty"`
	// Set applies the line templates to the subject's complete, ordered collection.
	Set          string           `json:"set,omitempty"`
	ClosedPeriod string           `json:"closedPeriod,omitempty" enum:"reject,next-open"`
	Date         *Expr            `json:"date,omitempty"`
	Text         *Expr            `json:"text,omitempty"`
	Currency     *Expr            `json:"currency,omitempty"`
	Lines        []TxnJournalLine `json:"lines"`
}

type TxnJournalLine struct {
	// When selects a line using the same typed rule context as its amounts.
	When    *Expr `json:"when,omitempty"`
	Account Expr  `json:"account"`
	Debit   *Expr `json:"debit,omitempty"`
	Credit  *Expr `json:"credit,omitempty"`
	Text    *Expr `json:"text,omitempty"`
	Object  *Expr `json:"object,omitempty"`
	Partner *Expr `json:"partner,omitempty"`
}

type TxnEffect struct {
	Writeback string   `json:"writeback"`
	Payload   []TxnSet `json:"payload,omitempty"`
}

type TxnWork struct {
	Title     string   `json:"title"`
	Body      *Expr    `json:"body,omitempty"`
	Role      string   `json:"role,omitempty"`
	Structure string   `json:"structure,omitempty"`
	Unit      *Expr    `json:"unit,omitempty"`
	Member    *Expr    `json:"member,omitempty"`
	Due       *Expr    `json:"due,omitempty"`
	Answers   []string `json:"answers,omitempty"`
	Key       *Expr    `json:"key,omitempty"`
	Close     bool     `json:"close,omitempty"`
}

// Call selects the existing synchronous pure query owner. Snapshot reads the
// transaction's initial private record view; current sees preceding steps.
type TxnCall struct {
	Ref        AssetRef `json:"ref"`
	Version    int      `json:"version"`
	View       string   `json:"view"`
	Inputs     []TxnSet `json:"inputs,omitempty"`
	ResultPath []string `json:"resultPath,omitempty"`
	ResultType ExprType `json:"resultType"`
}

type TxnCalls interface {
	Call(TxnCall, json.RawMessage, time.Time) (json.RawMessage, *kernel.Error)
}

type TxnTransport interface {
	TakeTxnStep() *kernel.Error
	BeginTxnCalls(Caller, time.Time) (TxnCalls, *kernel.Error)
	QueueTxnJournal(Caller, *pb.ChangeRecord, string, Journal, string, time.Time) *kernel.Error
}

type TxnRuntime interface{ TxnTransport() TxnTransport }

func (c Caller) TxnTransport() TxnTransport {
	rt, ok := c.rt.(TxnRuntime)
	if !ok || !c.Staging() || c.Replaying {
		return nil
	}
	return rt.TxnTransport()
}

type TxnForEach struct {
	Of    string    `json:"of,omitempty"` // declared reference path from the transaction subject
	Set   string    `json:"set,omitempty"`
	Range *TxnRange `json:"range,omitempty"`
	Steps []TxnStep `json:"steps"`
}

// TxnRange supplies bounded integer or temporal [start, end) values to the original iterator.
// Generated values expose row.start, row.end and row.index, never a record ID.
type TxnRange struct {
	From  Expr   `json:"from"`
	Until Expr   `json:"until"`
	Unit  string `json:"unit" enum:"day,hour,item"`
	Step  int    `json:"step"`
	Limit int    `json:"limit"`
}

// TxnIteration is a captured ordered read, saved with the decision's rows.
// Recovery validates this witness without re-reading or re-running the set.
type TxnIteration struct {
	Decision   string   `json:"decision,omitempty"` // authority/change ID of the captured child receipt
	Step       string   `json:"step"`
	Set        string   `json:"set"`
	Object     string   `json:"object"`
	Definition string   `json:"definition"`
	IDs        []string `json:"ids"`
}

type TxnIterationRuntime interface {
	CaptureTxnIteration(Caller, *pb.ChangeRecord, TxnIteration) *kernel.Error
}

func (c Caller) CaptureTxnIteration(receipt *pb.ChangeRecord, iteration TxnIteration) *kernel.Error {
	runtime, ok := c.rt.(TxnIterationRuntime)
	if !ok {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction iteration requires a private decision")
	}
	return runtime.CaptureTxnIteration(c, receipt, iteration)
}

type TxnRef struct {
	Type string `json:"type"`
	// Action names an existing transaction only in a refusal's route.
	Action string `json:"action,omitempty"`
	// ID is a fixed ID, $subject, $item, input.<name> or an earlier step.<name>.
	// A create leaves it empty for a stable ID generated by the platform.
	ID string `json:"id,omitempty"`
	// Via resolves a declared reference path from the subject for an update.
	Via string `json:"via,omitempty"`
}

// TxnSet retains the existing typed assignment sources. A transaction also
// accepts step.<name>, referring only to an earlier create ID or sequence.
type TxnSet struct {
	Field string `json:"field" field:"required" help:"A field of the object"`
	Expr  *Expr  `json:"expr,omitempty" type:"json" field:"aside"`
	From  string `json:"from,omitempty" field:"required" help:"An input's name, $me, $now, or =<a fixed value>" example:"to"`
}

func strictTxn(raw []byte, into any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(into); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "One transaction definition is required / 须提供一份事务定义")
	}
	return nil
}
func (t *BusinessTxn) UnmarshalJSON(raw []byte) error {
	type plain BusinessTxn
	return strictTxn(raw, (*plain)(t))
}
func (t *TxnStep) UnmarshalJSON(raw []byte) error {
	type plain TxnStep
	return strictTxn(raw, (*plain)(t))
}
func (t *TxnRef) UnmarshalJSON(raw []byte) error {
	type plain TxnRef
	return strictTxn(raw, (*plain)(t))
}
func (t *TxnForEach) UnmarshalJSON(raw []byte) error {
	type plain TxnForEach
	return strictTxn(raw, (*plain)(t))
}
func (t *TxnLink) UnmarshalJSON(raw []byte) error {
	type plain TxnLink
	return strictTxn(raw, (*plain)(t))
}

// ApplyTxnRecord uses the original generated action's catalog, row scope,
// field permissions, compute and validation, without another kernel decision.
// All rows carry the parent receipt and remain private until the host accepts
// its complete result. This is not an authoritative write entry point.
func (l *Ledger) ApplyTxnRecord(c Caller, r *pb.ChangeRecord, e Entity, verb, id string, payload json.RawMessage, now time.Time) (refusal *kernel.Error) {
	if c.TraceEnabled() {
		before, _ := c.ReadRecord(e.Type, id, now)
		defer func() {
			event := DecisionTrace{Kind: "transaction-change", Type: e.Type, ID: id, Operation: verb, Before: before}
			if refusal != nil {
				event.Refusal = refusal.Message
			} else {
				event.After, _ = c.ReadRecord(e.Type, id, now)
			}
			c.Trace(event)
		}()
	}
	if !c.Staging() || c.Replaying || c.App != l.authority || r.GetSubmission().GetAuthority() != c.App ||
		r.GetSubmission().GetTenantId() != l.tenant || r.GetSubmission().GetPrincipalId() != c.ID || r.GetChangeId() == "" || id == "" || !strings.HasPrefix(e.Type, c.App+".") ||
		(verb != "create" && verb != "edit" && verb != "archive") {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Transaction step is outside its private authority / 事务步骤超出私有权威范围")
	}
	s := proto.Clone(r.GetSubmission()).(*pb.Submission)
	s.Schema = &pb.SchemaRef{Name: e.Type + "." + verb, Version: 1}
	s.Target = &pb.EntityRef{Type: e.Type, Id: id}
	s.Payload = payload
	catalog := l.catalogFor(c)
	a, ok := catalog.Action(s.Schema.Name)
	if !ok || a.Target != e.Type || verb == "create" && !e.Standard.Create || verb == "edit" && !e.Standard.Edit || verb == "archive" && !e.Standard.Archive {
		return Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Transaction step action is unavailable / 事务步骤动作不可用")
	}
	c.Automation = false
	authorization := s
	invocation := c.TransactionInput(r)
	if verb == "edit" && e.Type == invocation.GetTarget().GetType() && id == invocation.GetTarget().GetId() {
		original := invocation.GetSchema().GetName()
		declared := original == e.Type+".create" && e.CreateTxn != nil
		if e.Lifecycle != nil {
			declared = declared || slices.ContainsFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Txn != nil && original == e.Type+"."+t.Name })
		}
		if declared {
			authorization = invocation
		}
	}
	decision := catalog.DecideSubmission(c, authorization, now)
	if !decision.Allow {
		layer := "verb"
		if len(decision.Layers) > 0 {
			layer = decision.Layers[len(decision.Layers)-1].Name
		}
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Authorization denied at {layer}: {reason}", layer, decision.Reason)
	}
	// The ordinary reference validator checks existence. Transaction assignment
	// also requires the referenced row to be readable in this private view.
	info, _ := Describe(c.App, e, func(_ reflect.Type) string { return "" })
	var given map[string]json.RawMessage
	if json.Unmarshal(payload, &given) != nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction fields / 事务字段无效")
	}
	for name, raw := range given {
		if f, found := info.Field(name); found && f.Type == "reference" && f.Ref != "" {
			var ref string
			if json.Unmarshal(raw, &ref) != nil {
				return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid transaction reference / 事务引用无效")
			}
			if ref != "" && !c.rt.Readable(c, f.Ref+"/"+ref) {
				return notFound()
			}
		}
	}
	if raw, exists := given["state"]; exists {
		var state string
		if e.Type != invocation.GetTarget().GetType() || id != invocation.GetTarget().GetId() || e.Lifecycle == nil || e.Lifecycle.Field != "state" || json.Unmarshal(raw, &state) != nil || !slices.ContainsFunc(e.Lifecycle.States, func(s State) bool { return s.Name == state }) {
			return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Transaction state must be a declared state of its subject")
		}
		delete(given, "state")
		payload = Raw(given)
		s.Payload = payload
		compute := e.Compute
		e.Compute = func(value any) {
			if compute != nil {
				compute(value)
			}
			reflect.ValueOf(value).Elem().FieldByName("State").SetString(state)
		}
	}
	apply, err := l.checked(c, s, now, catalog, func() (func(*pb.ChangeRecord), *kernel.Error) { return standard(c, e, verb, s, now) })()
	if err != nil {
		return err
	}
	apply(r)
	return nil
}

// ApplyTxnRoute checks the target's original action, state, inputs and row scope
// without re-entering its held ledger or creating a second kernel decision.
// The target's writes retain the original decision identity and atomic boundary.
func (l *Ledger) ApplyTxnRoute(c Caller, r *pb.ChangeRecord, e Entity, target TxnRef, now time.Time, run func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error) *kernel.Error {
	if !c.Staging() || c.Replaying || c.App != l.authority || target.Type != e.Type || target.Action == "" || target.ID == "" || e.Lifecycle == nil || run == nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Refusal route requires its original transaction owner / 拒绝改路须经原事务归属方")
	}
	index := slices.IndexFunc(e.Lifecycle.Transitions, func(t Transition) bool { return t.Name == target.Action })
	if index < 0 {
		return Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "Refusal route transaction is unavailable / 拒绝改路目标事务不可用")
	}
	tr := e.Lifecycle.Transitions[index]
	if tr.Txn == nil || tr.Approval != nil || tr.Adoption != nil {
		return Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Refusal routes require an ordinary declared transaction / 拒绝改路须指向已声明的普通事务")
	}
	c.Automation = false
	routed := proto.Clone(r).(*pb.ChangeRecord)
	routed.Submission.Schema = &pb.SchemaRef{Name: target.Type + "." + target.Action, Version: 1}
	routed.Submission.Target = &pb.EntityRef{Type: target.Type, Id: target.ID}
	catalog := l.catalogFor(c)
	decision := catalog.DecideSubmission(c, routed.Submission, now)
	if !decision.Allow {
		return Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Refusal route authorization denied: {reason} / 拒绝改路授权被拒：{reason}", decision.Reason, decision.Reason)
	}
	var failure *kernel.Error
	tr.ApplyTxn = func(c Caller, receipt *pb.ChangeRecord, at time.Time) *kernel.Error {
		failure = run(c.WithTransactionInput(routed.Submission), r, tr.Txn, at)
		return failure
	}
	apply, err := l.checked(c, routed.Submission, now, catalog, func() (func(*pb.ChangeRecord), *kernel.Error) {
		return transition(c, e, tr, routed.Submission, now)
	})()
	if err != nil {
		return err
	}
	apply(r)
	return failure
}
