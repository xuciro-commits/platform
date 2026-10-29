package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"io"
	"maps"
	"math"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	BooksApp    = "core"
	JournalType = "core.journal"
)

type Journal struct {
	Record
	Number       string        `json:"number,omitempty" field:"readonly,search" title:"Document number"`
	Date         string        `json:"date" field:"required" title:"Posting date" help:"YYYY-MM-DD"`
	Period       string        `json:"period,omitempty" field:"readonly" title:"Period"`
	ClosedPeriod string        `json:"closedPeriod,omitempty" choices:"reject,next-open" title:"Closed period policy"`
	Shifted      bool          `json:"shifted,omitempty" field:"readonly" title:"Posted to a later period"`
	Text         string        `json:"text,omitempty" field:"search"`
	Currency     string        `json:"currency,omitempty" help:"ISO 4217; default the tenant's first currency"`
	Lines        []JournalLine `json:"lines" field:"required" type:"json" title:"Lines"`
	Total        float64       `json:"total,omitempty" field:"readonly" title:"Total debit"`
	Source       string        `json:"source,omitempty" field:"search" help:"The decision that posted it, set by the platform"`
	Reverses     string        `json:"reverses,omitempty" field:"readonly" title:"Reverses"`
	Reversed     string        `json:"reversed,omitempty" field:"readonly" title:"Reversed by"`
	State        string        `json:"state" field:"readonly"`
}

type JournalLine struct {
	Account string  `json:"account"`
	Debit   float64 `json:"debit,omitempty"`
	Credit  float64 `json:"credit,omitempty"`
	Text    string  `json:"text,omitempty"`
	Object  string  `json:"object,omitempty"`
	Partner string  `json:"partner,omitempty"`
}

func OwnedRelationType(app string) string { return app + ".__relation" }

func RelationEntity(app string) Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[Record](), Anonymous: true, Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, OwnedRelationType(app)))}}
	for _, field := range []string{"binding", "version", "parent", "child"} {
		fields = append(fields, reflect.StructField{Name: strings.ToUpper(field[:1]) + field[1:], Type: reflect.TypeFor[string](), Tag: reflect.StructTag(fmt.Sprintf(`json:"%s" field:"readonly"`, field))})
	}
	fields = append(fields,
		reflect.StructField{Name: "Migration", Type: reflect.TypeFor[bool](), Tag: `json:"migration,omitempty" field:"readonly"`},
		reflect.StructField{Name: "Legacy", Type: reflect.TypeFor[[]RelationLink](), Tag: `json:"legacy,omitempty" type:"json" field:"readonly"`},
	)
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	return Entity{Type: OwnedRelationType(app), Title: "Owned relationship", Description: "An immutable typed relation instance of this application's authority", Model: model, Scope: Scope{Default: ScopeNone}}
}

type TxnLink struct {
	Binding AssetBinding `json:"binding"`
	Parent  TxnRef       `json:"parent"`
	Child   TxnRef       `json:"child"`
}

type OwnedRelationRuntime interface {
	PutOwnedRelation(Caller, *pb.ChangeRecord, TxnLink, bool, time.Time) *kernel.Error
}

func (c Caller) PutOwnedRelation(receipt *pb.ChangeRecord, link TxnLink, remove bool, at time.Time) *kernel.Error {
	runtime, ok := c.rt.(OwnedRelationRuntime)
	if !ok {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Owned relation writes require a private host")
	}
	return runtime.PutOwnedRelation(c, receipt, link, remove, at)
}

func RelationValue(entity Entity, id, binding, version, parent, child string, archived bool, legacy ...RelationLink) any {
	value := reflect.New(reflect.TypeOf(entity.Model))
	_ = json.Unmarshal(Raw(map[string]any{"id": id, "binding": binding, "version": version, "parent": parent, "child": child, "archived": archived, "legacy": legacy}), value.Interface())
	return value.Elem().Interface()
}

type RelationMigrationPlan struct {
	Binding  AssetBinding   `json:"binding"`
	Links    []RelationLink `json:"links"`
	Partial  bool           `json:"partial"`
	Migrated bool           `json:"migrated"`
}

type OwnedRelationMigrationRuntime interface {
	MigrateOwnedRelations(Caller, *pb.ChangeRecord, AssetBinding, time.Time) *kernel.Error
}

func (c Caller) MigrateOwnedRelations(receipt *pb.ChangeRecord, binding AssetBinding, at time.Time) *kernel.Error {
	runtime, ok := c.rt.(OwnedRelationMigrationRuntime)
	if !ok {
		return Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Relationship migration requires a private decision")
	}
	return runtime.MigrateOwnedRelations(c, receipt, binding, at)
}

type LegacyRelationSource interface{ LegacyRelations() []RelationLink }
type OwnedRelationReadRuntime interface {
	OwnedRelations(Caller, string) []RelationLink
	LegacyRelationActive(Caller, RelationLink) bool
	LegacyRelationWritable(Caller, string, string) bool
}

func (c Caller) OwnedRelations(entity string) []RelationLink {
	if runtime, ok := c.rt.(OwnedRelationReadRuntime); ok {
		return runtime.OwnedRelations(c, entity)
	}
	return nil
}
func (c Caller) LegacyRelationActive(link RelationLink) bool {
	if runtime, ok := c.rt.(OwnedRelationReadRuntime); ok {
		return runtime.LegacyRelationActive(c, link)
	}
	return true
}
func (c Caller) LegacyRelationWritable(from, to string) bool {
	if runtime, ok := c.rt.(OwnedRelationReadRuntime); ok {
		return runtime.LegacyRelationWritable(c, from, to)
	}
	return true
}

type RecordSetPredicate struct {
	Domain json.RawMessage `json:"domain,omitempty"`
	Search string          `json:"search,omitempty"`
	Set    *QuerySet       `json:"set,omitempty"`
}

type QuerySet struct {
	Op     string                `json:"op"`
	Inputs []*RecordSetPredicate `json:"inputs"`
}

const (
	QuerySetMaxConditions  = 32
	QuerySetMaxSearchBytes = 4096
	QuerySetMaxNodes       = 31
	QuerySetMaxDepth       = 4
	QuerySetMaxBytes       = 64 * 1024
)

func (q Query) CheckSet() error {
	if q.Set == nil {
		return nil
	}
	if q.Offset < 0 || q.Limit < 0 {
		return fmt.Errorf("invalid record set window")
	}
	nodes := 0
	var check func(RecordSetPredicate, int) error
	check = func(p RecordSetPredicate, depth int) error {
		nodes++
		if nodes > QuerySetMaxNodes || depth > QuerySetMaxDepth || len(p.Search) > QuerySetMaxSearchBytes {
			return fmt.Errorf("record set budget exceeded")
		}
		if len(p.Domain) > 0 {
			var tokens []json.RawMessage
			if json.Unmarshal(p.Domain, &tokens) != nil || len(tokens) > QuerySetMaxConditions {
				return fmt.Errorf("record set domain budget exceeded")
			}
		}
		if p.Set != nil {
			if (p.Set.Op != "union" && p.Set.Op != "intersect" && p.Set.Op != "subtract") || len(p.Set.Inputs) != 2 {
				return fmt.Errorf("record set needs a binary supported operation")
			}
			for _, input := range p.Set.Inputs {
				if input == nil {
					return fmt.Errorf("record set input is null")
				}
				if err := check(*input, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	root := RecordSetPredicate{Domain: q.Domain, Search: q.Search, Set: q.Set}
	if err := check(root, 0); err != nil {
		return err
	}
	encoded, err := json.Marshal(root)
	if err != nil || len(encoded) > QuerySetMaxBytes {
		return fmt.Errorf("record set encoding budget exceeded")
	}
	return nil
}

type Sequence struct {
	Name       string
	Pattern    string
	Yearly     bool
	SeedRecord string `json:",omitempty"`
	SeedField  string `json:",omitempty"`
}

var sequencePattern = regexp.MustCompile(`\{(n|n:[1-9][0-9]*|year)\}`)

func (s Sequence) Check() error {
	rest := sequencePattern.ReplaceAllString(s.Pattern, "")
	if s.Name == "" || strings.ContainsAny(rest, "{}") || s.Pattern != "" && !strings.Contains(s.Pattern, "{n") || (s.SeedRecord == "") != (s.SeedField == "") || s.SeedRecord != "" && strings.Count(s.Pattern, "{n") != 1 {
		return fmt.Errorf("sequence %q: a name, and a pattern with {n} and no unknown placeholder", s.Name)
	}
	for _, part := range sequencePattern.FindAllString(s.Pattern, -1) {
		if strings.HasPrefix(part, "{n:") {
			if _, err := strconv.Atoi(part[3 : len(part)-1]); err != nil {
				return fmt.Errorf("sequence %q: invalid number width", s.Name)
			}
		}
	}
	return nil
}

func (s Sequence) Format(year, n int) string {
	pattern := s.Pattern
	if pattern == "" {
		pattern = "{n}"
	}
	return sequencePattern.ReplaceAllStringFunc(pattern, func(p string) string {
		switch {
		case p == "{year}":
			return strconv.Itoa(year)
		case p == "{n}":
			return strconv.Itoa(n)
		}
		width, _ := strconv.Atoi(p[3 : len(p)-1])
		return fmt.Sprintf("%0*d", width, n)
	})
}

func (c Caller) Next(r *pb.ChangeRecord, sequence string, date time.Time) (string, *kernel.Error) {
	if c.rt == nil {
		return "", notFound()
	}
	return c.rt.Next(c, r, sequence, date)
}

const TxnMaxSteps = 256
const TxnMaxIterations = 1000
const TxnMaxExecutionSteps = TxnMaxSteps * TxnMaxIterations

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

type TxnJournal struct {
	Reverses     string           `json:"reverses,omitempty"`
	Set          string           `json:"set,omitempty"`
	ClosedPeriod string           `json:"closedPeriod,omitempty" enum:"reject,next-open"`
	Date         *Expr            `json:"date,omitempty"`
	Text         *Expr            `json:"text,omitempty"`
	Currency     *Expr            `json:"currency,omitempty"`
	Lines        []TxnJournalLine `json:"lines"`
}

type TxnJournalLine struct {
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
	QueueTxnEffect(Caller, *pb.ChangeRecord, string, TxnRef, json.RawMessage, time.Time) *kernel.Error
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
	Of    string    `json:"of,omitempty"`
	Set   string    `json:"set,omitempty"`
	Range *TxnRange `json:"range,omitempty"`
	Steps []TxnStep `json:"steps"`
}

type TxnRange struct {
	From  Expr   `json:"from"`
	Until Expr   `json:"until"`
	Unit  string `json:"unit" enum:"day,hour,item"`
	Step  int    `json:"step"`
	Limit int    `json:"limit"`
}

type TxnIteration struct {
	Decision   string   `json:"decision,omitempty"`
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
	Type   string `json:"type"`
	Action string `json:"action,omitempty"`
	ID     string `json:"id,omitempty"`
	Via    string `json:"via,omitempty"`
}

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

type BooksChecks interface {
	CheckPosting(Caller, Journal) (Outcome, *kernel.Error)
	CheckReversal(Caller, string, json.RawMessage, time.Time) (Outcome, *kernel.Error)
	HistoricalPostingMessage(string) string
}

type TransactionHost interface {
	CheckTxnCall(TxnCall) error
}

type TransformStep struct {
	Kind        string             `json:"kind"`
	Columns     []string           `json:"columns,omitempty"`
	From        string             `json:"from,omitempty"`
	To          string             `json:"to,omitempty"`
	Column      string             `json:"column,omitempty"`
	Type        string             `json:"type,omitempty"`
	Op          string             `json:"op,omitempty"`
	Value       string             `json:"value,omitempty"`
	FormulaExpr *Expr              `json:"formulaExpr,omitempty" type:"json" field:"aside"`
	Formula     string             `json:"formula,omitempty"`
	Dataset     string             `json:"dataset,omitempty"`
	Match       string             `json:"match,omitempty"`
	As          string             `json:"as,omitempty"`
	Outer       bool               `json:"outer,omitempty"`
	Desc        bool               `json:"desc,omitempty"`
	Measures    []TransformMeasure `json:"measures,omitempty"`
}

type TransformMeasure struct {
	Fn     string `json:"fn"`
	Column string `json:"column,omitempty"`
	To     string `json:"to"`
}

type TransformExpectation struct {
	Column string `json:"column"`
	Rule   string `json:"rule"`
	Value  string `json:"value,omitempty"`
}

type QuarantinedRow struct {
	Reason string         `json:"reason"`
	Row    map[string]any `json:"row"`
}

func Literal(kind, s string) (any, error) {
	switch kind {
	case "integer":
		return strconv.Atoi(s)
	case "decimal":
		return strconv.ParseFloat(s, 64)
	case "boolean":
		return strconv.ParseBool(s)
	case "date":
		_, err := time.Parse(time.DateOnly, s)
		return s, err
	case "datetime":
		return time.Parse(time.RFC3339, s)
	case "money":
		return nil, fmt.Errorf("an amount is not set to a fixed value")
	}
	return s, nil
}

func AssignmentExpr(kind string, set TxnSet, spec RuleReadSpec) (Expr, error) {
	kind = RuleFieldKind(kind)
	targetKind := kind
	if set.Expr != nil {
		if set.From != "" {
			return Expr{}, fmt.Errorf("use one assignment expression")
		}
		if !RuleAssignmentFits(kind, set.Expr.Kind) {
			return Expr{}, fmt.Errorf("assignment result type differs from field")
		}
		return *set.Expr, nil
	}
	switch {
	case set.From == "$me":
		return ExprReference("text", "caller.id"), nil
	case set.From == "$subject":
		return ExprReference("reference", "input.$subject"), nil
	case set.From == "$now":
		op := "$now"
		if kind == "date" {
			op = "$today"
		}
		return Expr{Op: op, Kind: kind}, nil
	case strings.HasPrefix(set.From, "="):
		v, err := Literal(kind, strings.TrimPrefix(set.From, "="))
		return ExprLiteralValue(kind, v), err
	default:
		name := set.From
		if !strings.HasPrefix(name, "step.") && !strings.HasPrefix(name, "row.") {
			name = "input." + name
		}
		input := strings.TrimPrefix(name, "input.")
		if typ, ok := spec.Inputs[input]; ok {
			kind = typ.Kind
		}
		if !RuleAssignmentFits(targetKind, kind) {
			return Expr{}, fmt.Errorf("assignment source differs")
		}
		return ExprReference(kind, name), nil
	}
}

func RuleAssignment(e Expr, s RuleReadSpec, c Caller, record any, inputs map[string]any, now time.Time, lookup func(string) (EntityInfo, bool), provenance ...*[]string) (any, *kernel.Error) {
	r, err := EvaluateRule(e, s, c, record, inputs, now, lookup)
	for _, refs := range provenance {
		*refs = append(*refs, r.Sources...)
	}
	if err != nil {
		return nil, err
	}
	v, failure := ExprGoValue(r.Value)
	if failure != nil {
		return nil, Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, failure.Error())
	}
	return v, nil
}

func RuleAssignmentFits(dst, src string) bool {
	dst = RuleFieldKind(dst)
	src = RuleFieldKind(src)
	return dst == src || dst == "text" && src == "choice" || (dst == "decimal" || dst == "number") && (src == "integer" || src == "decimal" || src == "number")
}

func DispatchParameter(set TxnSet, scope RuleReadSpec) (Expr, error) {
	kind := "text"
	if set.Expr != nil {
		kind = set.Expr.Kind
	} else if input, found := scope.Inputs[strings.TrimPrefix(set.From, "input.")]; found {
		kind = input.Kind
	}
	if set.Expr == nil && strings.HasPrefix(set.From, "input.") {
		set.From = strings.TrimPrefix(set.From, "input.")
	}
	if slices.Contains([]string{"set", "object", "json", ""}, kind) {
		return Expr{}, fmt.Errorf("Dispatch parameters require scalar expressions")
	}
	return AssignmentExpr(kind, set, scope)
}

func BalanceID(object string, values []string) string {
	sum := sha256.Sum256([]byte(object + "\x00" + strings.Join(values, "\x00")))
	return "bal-" + hex.EncodeToString(sum[:12])
}

func CastCell(cell, typ string) (any, error) {
	text := strings.TrimSpace(cell)
	switch typ {
	case "number":
		n, err := strconv.ParseFloat(text, 64)
		if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("%q is not a number", text)
		}
		return n, nil
	case "boolean":
		switch strings.ToLower(text) {
		case "true", "yes":
			return true, nil
		case "false", "no":
			return false, nil
		}
		return nil, fmt.Errorf("%q is not true or false", text)
	}
	return text, nil
}

func CastValue(value any, typ string) (any, error) {
	switch x := value.(type) {
	case string:
		return CastCell(x, typ)
	case float64:
		if typ == "number" {
			return x, nil
		}
		return CastCell(strconv.FormatFloat(x, 'f', -1, 64), typ)
	case bool:
		if typ == "boolean" {
			return x, nil
		}
		return CastCell(strconv.FormatBool(x), typ)
	case json.Number:
		return CastCell(x.String(), typ)
	}
	return nil, fmt.Errorf("no value")
}

func TableInput(reads RuleReadSpec, legacy bool, values map[string]any) ExprContext {
	facts := ExprContext{Refs: map[string]ExprValue{}}
	for name, col := range reads.Inputs {
		v, ok := values[name]
		if legacy {
			v, _ = CastValue(v, col.Kind)
		}
		if ok {
			facts.Refs["input."+name] = ExprPresent(col.Kind, v)
			facts.Refs[name] = facts.Refs["input."+name]
		}
	}
	return facts
}

func ConditionValue(kind, raw string) (any, error) {
	switch kind {
	case "integer":
		return strconv.ParseInt(raw, 10, 64)
	case "decimal":
		x, err := strconv.ParseFloat(raw, 64)
		if err == nil && (math.IsInf(x, 0) || math.IsNaN(x)) {
			err = fmt.Errorf("a finite number is required")
		}
		return x, err
	case "date":
		if day, err := time.Parse(time.DateOnly, raw); err == nil {
			return day, nil
		}
		instant, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return time.Time{}, err
		}
		return time.Parse(time.DateOnly, instant.UTC().Format(time.DateOnly))
	case "datetime":
		return time.Parse(time.RFC3339, raw)
	case "boolean":
		return strconv.ParseBool(raw)
	default:
		return raw, nil
	}
}

func Holds(got any, kind, op, want string) bool {
	kind = RuleFieldKind(kind)
	v := got
	if kind == "date" {
		if t, ok := got.(time.Time); ok {
			v = t.UTC().Format(time.DateOnly)
		}
	}
	e := Expr{Kind: "boolean", Op: op, Args: []Expr{ExprReference(kind, "value")}}
	if op == "not empty" {
		e.Op = "not-empty"
	}
	if op != "empty" && op != "not empty" {
		x, err := ConditionValue(kind, want)
		if err != nil {
			return false
		}
		if t, ok := x.(time.Time); ok {
			if kind == "date" {
				x = t.Format(time.DateOnly)
			} else {
				x = t.Format(time.RFC3339Nano)
			}
		}
		e.Args = append(e.Args, ExprLiteralValue(kind, x))
	}
	p, err := CompileExpr(e, ExprSchema{Refs: map[string]ExprType{"value": {Kind: kind}}})
	if err != nil {
		return false
	}
	r, err := p.Eval(ExprContext{Refs: map[string]ExprValue{"value": ExprPresent(kind, v)}})
	return err == nil && string(r.Value.Data) == "true"
}

func TextOf(v any) string {
	if v != nil {
		rv := reflect.ValueOf(v)
		for rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return ""
			}
			rv = rv.Elem()
		}
		v = rv.Interface()
	}
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case time.Time:
		if x.IsZero() {
			return ""
		}
		return x.UTC().Format(time.RFC3339)
	case Money:
		if x.Currency == "" && x.Amount == 0 {
			return ""
		}
		return strconv.FormatInt(x.Amount, 10)
	}
	return fmt.Sprint(v)
}

type BusinessRuntimeContext struct {
	App      string
	Entities map[string]Entity
	Ledger   *Ledger
	Lookup   func(string) (EntityInfo, bool)
	Prepare  func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) (TransactionProposal, *kernel.Error)
	Link     func(AssetBinding) (LinkType, error)
	Effect   func(Caller, string) (TransactionEffectDefinition, *kernel.Error)
	Dispatch func(Caller, *pb.ChangeRecord, *BusinessTxn, RuleReadSpec, map[string]any, map[string]any, time.Time) *kernel.Error
	Confirm  func(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error
}

type TransactionProposal struct {
	Apply  func(Entity, EntityInfo, map[string]any, []string) (Entity, map[string]any)
	Finish func(Caller, *pb.ChangeRecord, time.Time) *kernel.Error
}

type TransactionEffectDefinition struct {
	Request func(string, json.RawMessage) (json.RawMessage, *kernel.Error)
}

func WalkTxnSteps(steps []TxnStep, visit func(TxnStep)) {
	for _, step := range steps {
		visit(step)
		if step.ForEach != nil {
			WalkTxnSteps(step.ForEach.Steps, visit)
		}
		for _, refusal := range step.Refusals {
			if refusal.Record != nil {
				WalkTxnSteps([]TxnStep{*refusal.Record}, visit)
			}
		}
	}
}

func WalkBusinessTxn(txn *BusinessTxn, visit func(TxnStep)) {
	WalkTxnSteps(txn.Steps, visit)
	for _, refusal := range txn.Refusals {
		if refusal.Record != nil {
			WalkTxnSteps([]TxnStep{*refusal.Record}, visit)
		}
	}
}

func WithTxnRow(spec RuleReadSpec, info EntityInfo) RuleReadSpec {
	spec.Inputs = maps.Clone(spec.Inputs)
	for _, field := range ConstraintEntity(info).Fields {
		spec.Inputs["row."+field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
	}
	spec.Inputs["row.$subject"] = ExprType{Kind: "reference", Object: info.Type}
	return spec
}

func TxnResult(spec *RuleReadSpec, step TxnStep) {
	if step.Call != nil {
		spec.Inputs["step."+step.Name] = step.Call.ResultType
	}
	if step.Kind == "sequence" {
		spec.Inputs["step."+step.Name] = ExprType{Kind: "text"}
	}
	if step.Kind == "create" || step.Kind == "post" {
		spec.Inputs["step."+step.Name] = ExprType{Kind: "reference", Object: step.Target.Type}
	}
}

func IntervalFields(f TxnIntervalFields) map[string]string {
	return map[string]string{f.Subject: "reference", f.Source: "text", f.SourceVersion: "text", f.SourceRevision: "text", f.ProcessingVersion: "text", f.Since: "datetime", f.Until: "datetime", f.OpenStart: "boolean", f.OpenEnd: "boolean", f.Retired: "boolean", f.Review: "boolean"}
}

func WithTxnRange(scope RuleReadSpec, kind string) RuleReadSpec {
	scope.Inputs = maps.Clone(scope.Inputs)
	scope.Inputs["row.start"] = ExprType{Kind: kind}
	scope.Inputs["row.end"] = ExprType{Kind: kind}
	scope.Inputs["row.index"] = ExprType{Kind: "integer"}
	return scope
}

func TxnIterationSource(owner, subject, of string, lookup func(string) (EntityInfo, bool)) (EntityInfo, error) {
	typ := subject
	if of != "" {
		field, err := RecordPathField(subject, strings.Split(of, "."), lookup)
		if err != nil || field.Type != "reference" || field.Ref == "" {
			return EntityInfo{}, fmt.Errorf("Iteration source must be a declared record reference")
		}
		typ = field.Ref
	}
	info, ok := lookup(typ)
	if !ok || info.App != owner {
		return EntityInfo{}, fmt.Errorf("Iteration source must belong to its transaction owner")
	}
	return info, nil
}

type TransactionExecutor interface {
	Transact(Caller, *pb.ChangeRecord, *BusinessTxn, time.Time) *kernel.Error
}

func IntegrationTypeOf(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case bool:
		return "boolean"
	case float64:
		return "number"
	case string:
		if _, err := time.Parse(time.RFC3339, x); err == nil {
			return "date"
		}
		if len(x) == 10 {
			if _, err := time.Parse("2006-01-02", x); err == nil {
				return "date"
			}
		}
		return "string"
	default:
		return "json"
	}
}

func IntegrationLookupPath(in map[string]any, path string) (any, bool) {
	var cur any = in
	for _, part := range strings.Split(path, ".") {
		switch value := cur.(type) {
		case map[string]any:
			var ok bool
			if cur, ok = value[part]; !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(value) {
				return nil, false
			}
			cur = value[index]
		default:
			return nil, false
		}
	}
	return cur, true
}

func IntegrationMatches(v any, op, want string) bool {
	text := IntegrationScalar(v)
	switch op {
	case "empty":
		return v == nil || text == ""
	case "notempty":
		return v != nil && text != ""
	case "contains":
		return strings.Contains(strings.ToLower(text), strings.ToLower(want))
	case "=":
		return text == want || (IntegrationTypeOf(v) == "number" && IntegrationNumeric(v) == IntegrationNumeric(want))
	case "!=":
		return !(text == want || (IntegrationTypeOf(v) == "number" && IntegrationNumeric(v) == IntegrationNumeric(want)))
	}
	if IntegrationTypeOf(v) == "number" {
		a, b := IntegrationNumeric(v), IntegrationNumeric(want)
		switch op {
		case "<":
			return a < b
		case "<=":
			return a <= b
		case ">":
			return a > b
		case ">=":
			return a >= b
		}
	}
	switch op {
	case "<":
		return text < want
	case "<=":
		return text <= want
	case ">":
		return text > want
	case ">=":
		return text >= want
	}
	return false
}

func IntegrationNumeric(v any) float64 {
	switch x := v.(type) {
	case json.Number:
		n, _ := x.Float64()
		return n
	case float64:
		return x
	case bool:
		if x {
			return 1
		}
	case string:
		n, _ := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return n
	}
	return 0
}

func IntegrationKeyOf(r map[string]any, cols []string) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = IntegrationScalar(r[c])
	}
	return strings.Join(parts, "\x1f")
}

func IntegrationNormalize(v, how string) string {
	switch how {
	case "folded":
		return strings.Join(strings.Fields(strings.ToLower(v)), " ")
	case "digits":
		var b strings.Builder
		for _, r := range v {
			if unicode.IsDigit(r) {
				b.WriteRune(r)
			}
		}
		return strings.TrimLeft(b.String(), "0")
	}
	return v
}

func IntegrationCSVRows(body []byte) ([]map[string]any, error) {
	r := csv.NewReader(bytes.NewReader(body))
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("the answer is not CSV: %v", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	head := records[0]
	rows := make([]map[string]any, 0, len(records)-1)
	for _, rec := range records[1:] {
		row := map[string]any{}
		for i, name := range head {
			if i < len(rec) {
				row[strings.TrimSpace(name)] = rec[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func IntegrationRowsAt(body []byte, path string) ([]map[string]any, error) {
	var node any
	if err := json.Unmarshal(body, &node); err != nil {
		return nil, fmt.Errorf("the answer is not JSON")
	}
	if path != "" {
		for _, part := range strings.Split(path, ".") {
			object, ok := node.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("the answer has no %s", path)
			}
			node = object[part]
		}
	}
	items, ok := node.([]any)
	if !ok {
		return nil, fmt.Errorf("the rows at %q are not an array", path)
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("a row is not an object")
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func IntegrationScalar(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		raw, _ := json.Marshal(x)
		return string(raw)
	}
}

func IntegrationConvert(v any, to string) (any, error) {
	if v == nil {
		return nil, nil
	}
	switch to {
	case "":
		return v, nil
	case "string":
		return IntegrationScalar(v), nil
	case "number":
		switch x := v.(type) {
		case json.Number:
			if _, err := x.Float64(); err == nil {
				return x, nil
			}
		case float64:
			return x, nil
		case string:
			n, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
			if err != nil {
				return nil, fmt.Errorf("%q is not a number", x)
			}
			return n, nil
		}
	case "boolean":
		switch x := v.(type) {
		case bool:
			return x, nil
		case string:
			switch strings.ToLower(strings.TrimSpace(x)) {
			case "true", "yes", "1", "y":
				return true, nil
			case "false", "no", "0", "n", "":
				return false, nil
			}
		case float64:
			return x != 0, nil
		}
	case "date":
		text := IntegrationScalar(v)
		for _, layout := range []string{time.RFC3339, "2006-01-02", "2006-01-02 15:04:05", "2006/01/02", "02.01.2006"} {
			if t, err := time.Parse(layout, text); err == nil {
				return t.UTC().Format(time.RFC3339), nil
			}
		}
		return nil, fmt.Errorf("%q is not a date", text)
	}
	return nil, fmt.Errorf("%v cannot become %s", v, to)
}
