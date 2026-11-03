package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type RecordSetPredicate struct {
	Domain     json.RawMessage      `json:"domain,omitempty"`
	Filter     *Expr                `json:"filter,omitempty"`
	Parameters map[string]ExprValue `json:"parameters,omitempty"`
	Search     string               `json:"search,omitempty"`
	Set        *QuerySet            `json:"set,omitempty"`
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
	if err := q.ObjectSet("query", nil).Check(); err != nil {
		return err
	}
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
	root := RecordSetPredicate{Domain: q.Domain, Filter: q.Filter, Parameters: q.Parameters, Search: q.Search, Set: q.Set}
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
