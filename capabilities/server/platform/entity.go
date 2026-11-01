package platform

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	ruleexpr "platformserver/expr"
	"platformserver/platform/authz"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type Record struct {
	ID          string             `json:"id"`
	Revision    uint32             `json:"revision"`
	Created     Stamp              `json:"created"`
	Changed     Stamp              `json:"changed"`
	Archived    bool               `json:"archived,omitempty"`
	VersionPins []RecordVersionPin `json:"versionPins,omitempty"`
}

type Stamp struct {
	By     string    `json:"by,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Change string    `json:"change,omitempty"`
}

type Ref[T any] string

func (Ref[T]) refType() reflect.Type { return reflect.TypeFor[T]() }

type reference interface{ refType() reflect.Type }

type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type Entity struct {
	DataSources       []DataSourceBinding
	LocalRules        map[string]Expr
	EnterpriseUse     *Expr
	Protocols         []Provision
	CreateTxn         *BusinessTxn
	ApplyCreateTxn    func(Caller, *pb.ChangeRecord, time.Time) *kernel.Error
	Versioning        *Versioning
	VersionPins       []VersionPinBinding
	Sets              []RecordSet
	Constraints       []Constraint
	PropertyBindings  map[string]AssetBinding
	ValueTypeBindings map[string]AssetBinding
	Format            map[string][]FormatRule
	Compute           func(record any)
	ComputeAt         func(c Caller, record any, now time.Time) *kernel.Error
	Validate          func(c Caller, record any) *kernel.Error
	ValidateAt        func(c Caller, record any, now time.Time) *kernel.Error

	Type            string
	Title           string
	Plural          string
	Description     string
	Synonyms        string
	Model           any
	RecordFormat    string
	RecordImages    []RecordImageFormat
	Display         string
	ObjectView      *ObjectView `json:"objectView,omitempty"`
	Icon            string
	Color           string
	Scope           Scope
	KnowledgeFiles  bool
	Standard        Standard
	Implements      []string
	Extends         string
	ExtensionSource *ExtensionSource
	Lifecycle       *Lifecycle
	Seed            []any
	Derived         []Derivation
	Withheld        string
}

type DataSourceBinding struct {
	Kind       string             `json:"kind"`
	Source     string             `json:"source"`
	Key        string             `json:"key"`
	SourceTime string             `json:"sourceTime,omitempty"`
	Columns    []DataSourceColumn `json:"columns"`
}

type DataSourceColumn struct {
	Column    string `json:"column"`
	Attribute string `json:"attribute"`
	Strategy  string `json:"strategy,omitempty"`
}

type Derivation struct {
	From    string
	Fields  []string
	Element bool
}

type DerivationInfo struct {
	All     bool
	List    []int
	Refs    [][]int
	Fields  [][]int
	Element bool
	Names   []string
}

type Lifecycle struct {
	Field       string
	Initial     string
	States      []State
	Transitions []Transition
}

type State struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Tone        string `json:"tone,omitempty" enum:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty"`
}

type Transition struct {
	ConfirmationOnly bool
	LegacyReplay     *Transition       `json:"-"`
	Adoption         *ProposalAdoption `json:"-"`
	Criteria         *RuleCriteria     `json:"-"`
	Name             string
	Title            string
	Description      string
	From, To         []string
	ToInput          string
	Roles            []string
	Payload          []Field
	DeferRequired    bool `json:"-"`
	Capability       string
	Approval         *Approval
	Do               func(c Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error
	After            func(c Caller, r *pb.ChangeRecord, record any, now time.Time)
	Txn              *BusinessTxn                                                    `json:"-"`
	ApplyTxn         func(c Caller, r *pb.ChangeRecord, now time.Time) *kernel.Error `json:"-"`
}

type LifecycleInfo struct {
	Field       string           `json:"field"`
	Initial     string           `json:"initial"`
	States      []State          `json:"states"`
	Transitions []TransitionInfo `json:"transitions"`
}

type TransitionInfo struct {
	ToInput string   `json:"toInput,omitempty"`
	Name    string   `json:"name"`
	Schema  string   `json:"schema"`
	Title   string   `json:"title"`
	From    []string `json:"from"`
	To      []string `json:"to"`
}

func transactionDestination(subject, field string, txn *BusinessTxn, inputs []Field) ([]string, string) {
	if txn == nil || txn.Subject != subject {
		return nil, ""
	}
	var assignment *Expr
	writes, direct := 0, false
	var visit func([]TxnStep, bool)
	visit = func(steps []TxnStep, nested bool) {
		for _, step := range steps {
			if step.Kind == "update" && step.Target.Type == subject {
				for _, change := range step.Changes {
					if change.Field == field {
						writes++
						assignment = change.Expr
						direct = !nested && step.When == nil && step.Target.ID == "$subject" && step.Target.Via == ""
					}
				}
			}
			if step.ForEach != nil {
				visit(step.ForEach.Steps, true)
			}
		}
	}
	visit(txn.Steps, false)
	if writes != 1 || !direct || assignment == nil || assignment.Op != "" {
		return nil, ""
	}
	if assignment.Lit != nil {
		var state string
		if json.Unmarshal(assignment.Lit.Value, &state) == nil {
			return []string{state}, ""
		}
	}
	for _, input := range inputs {
		if assignment.Ref == "input."+input.Name && input.Type == "string" && input.Required && len(input.Choices) > 0 {
			return slices.Clone(input.Choices), input.Name
		}
	}
	return nil, ""
}

type Standard struct {
	Create, Edit, Archive                bool
	Roles                                []string
	Capability                           string
	CreateRoles, EditRoles, ArchiveRoles []string
}

func (s Standard) roles(verb string) []string {
	own := map[string][]string{"create": s.CreateRoles, "edit": s.EditRoles, "archive": s.ArchiveRoles}[verb]
	if own != nil {
		return own
	}
	return s.Roles
}

type Scope struct {
	Levels       map[string]string
	Default      string
	Structure    string
	Unit         string
	Owner        string
	Participants func(record any) []string
	Through      func(record any) string
}

const (
	ScopeTenant  = "tenant"
	ScopeBelow   = "below"
	ScopeUnit    = "unit"
	ScopeOwn     = "own"
	ScopeNone    = "none"
	OwnerCreated = "created"
)

func (s Scope) Level(role string) string {
	if l, ok := s.Levels[role]; ok {
		return l
	}
	if s.Default == "" {
		return ScopeTenant
	}
	return s.Default
}

func (s Scope) LevelFor(roles []string) string {
	levels := make([]string, 0, len(roles))
	for _, r := range roles {
		if r != "" {
			levels = append(levels, s.Level(r))
		}
	}
	return authz.Widest(levels)
}

type FieldInfo struct {
	AIInput      *bool             `json:"aiInput,omitempty"`
	SourceType   string            `json:"sourceType,omitempty" enum:"record,user-input,external-input,generated"`
	Constraints  *InputConstraints `json:"constraints,omitempty"`
	Range        *DateRange        `json:"range,omitempty"`
	Group        string            `json:"group,omitempty"`
	Property     *AssetBinding     `json:"property,omitempty"`
	ValueType    *AssetBinding     `json:"valueType,omitempty"`
	Format       []FormatRule      `json:"format,omitempty"`
	Name         string            `json:"name"`
	Title        string            `json:"title"`
	Type         string            `json:"type" enum:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference,references,tags,lines,json"`
	Required     bool              `json:"required,omitempty"`
	Search       bool              `json:"search,omitempty"`
	ReadOnly     bool              `json:"readOnly,omitempty"`
	AliasOf      string            `json:"aliasOf,omitempty"`
	Aside        bool              `json:"aside,omitempty"`
	Choices      []string          `json:"choices,omitempty"`
	ChoiceTitles []string          `json:"choiceTitles,omitempty"`
	Ref          string            `json:"ref,omitempty"`
	Stereotype   string            `json:"stereotype,omitempty"`
	Stereotypes  []string          `json:"stereotypes,omitempty"`
	Inverse      string            `json:"inverse,omitempty"`
	Knowledge    bool              `json:"knowledge,omitempty"`
	Read         []string          `json:"read,omitempty"`
	Write        []string          `json:"write,omitempty"`
	Personal     string            `json:"personal,omitempty"`
	Help         string            `json:"help,omitempty"`
	Synonyms     string            `json:"synonyms,omitempty"`
	Example      string            `json:"example,omitempty"`
	When         *FieldCondition   `json:"when,omitempty"`
	Fields       []FieldInfo       `json:"fields,omitempty"`
	Index        []int             `json:"-"`
}

type FieldCondition struct {
	Field string   `json:"field"`
	In    []string `json:"in"`
}

func (c FieldCondition) Expr() Expr {
	items := make([]string, len(c.In))
	copy(items, c.In)
	return Expr{Op: "in", Kind: "boolean", Args: []Expr{
		ruleexpr.RefOf("text", c.Field),
		{Kind: "set", Lit: &ruleexpr.Literal{Value: Raw(items), Element: "text"}},
	}}
}

type conditionValue string

func (v conditionValue) Ref(string) (ruleexpr.Value, bool) {
	return ruleexpr.Present("text", string(v)), true
}

var conditionPrograms sync.Map

func (c *FieldCondition) Holds(value string) bool {
	if c == nil {
		return true
	}
	key := c.Field + "=" + strings.Join(c.In, "\x00")
	cached, ok := conditionPrograms.Load(key)
	if !ok {
		program, err := ruleexpr.CompileRow(c.Expr(), ruleexpr.Schema{Refs: map[string]ruleexpr.Type{c.Field: {Kind: "text"}}})
		if err != nil {
			return false
		}
		cached, _ = conditionPrograms.LoadOrStore(key, program)
	}
	return cached.(*ruleexpr.RowProgram).Match(conditionValue(value))
}

func ParseWhen(tag string) (*FieldCondition, error) {
	if tag == "" {
		return nil, nil
	}
	field, values, ok := strings.Cut(tag, "=")
	if !ok || field == "" || values == "" {
		return nil, fmt.Errorf("a condition is field=value,value, not %q", tag)
	}
	return &FieldCondition{Field: field, In: strings.Split(values, ",")}, nil
}

func checkConditions(entity string, fields []FieldInfo) error {
	for _, f := range fields {
		if f.When == nil {
			continue
		}
		i := slices.IndexFunc(fields, func(x FieldInfo) bool { return x.Name == f.When.Field })
		if i < 0 || i == slices.IndexFunc(fields, func(x FieldInfo) bool { return x.Name == f.Name }) {
			return fmt.Errorf("entity %s: field %s depends on %s, which is not another field", entity, f.Name, f.When.Field)
		}
		on := fields[i]
		if on.When != nil {
			return fmt.Errorf("entity %s: field %s depends on %s, which is conditional itself", entity, f.Name, on.Name)
		}
		allowed := on.Choices
		if on.Type == "boolean" {
			allowed = []string{"true", "false"}
		} else if on.Type != "choice" {
			return fmt.Errorf("entity %s: field %s depends on %s, which is neither a choice nor a boolean", entity, f.Name, on.Name)
		}
		for _, v := range f.When.In {
			if !slices.Contains(allowed, v) {
				return fmt.Errorf("entity %s: field %s depends on %s being %q, which it never is", entity, f.Name, on.Name, v)
			}
		}
	}
	return nil
}

func (f FieldInfo) Active(valueOf func(field string) string) bool {
	return f.When == nil || f.When.Holds(valueOf(f.When.Field))
}

type EntityInfo struct {
	DataSources     []DataSourceBinding `json:"datasources,omitempty"`
	LocalRules      map[string]Expr     `json:"localRules,omitempty"`
	EnterpriseUse   *Expr               `json:"enterpriseUse,omitempty"`
	Versioning      *Versioning         `json:"versioning,omitempty"`
	VersionPins     []VersionPinBinding `json:"versionPins,omitempty"`
	Sets            []RecordSet         `json:"sets,omitempty"`
	Constraints     []Constraint        `json:"constraints,omitempty"`
	Type            string              `json:"type"`
	Title           string              `json:"title"`
	Plural          string              `json:"plural"`
	Description     string              `json:"description,omitempty"`
	Synonyms        string              `json:"synonyms,omitempty"`
	KnowledgeFiles  bool                `json:"knowledgeFiles,omitempty"`
	App             string              `json:"app"`
	Display         string              `json:"display"`
	ObjectView      *ObjectView         `json:"objectView,omitempty"`
	Icon            string              `json:"icon,omitempty"`
	Color           string              `json:"color,omitempty"`
	Fields          []FieldInfo         `json:"fields"`
	Standard        []string            `json:"standard"`
	Implements      []string            `json:"implements,omitempty"`
	Extends         string              `json:"extends,omitempty"`
	ExtensionSource *ExtensionSource    `json:"-"`
	Lifecycle       *LifecycleInfo      `json:"lifecycle,omitempty"`
	Go              reflect.Type        `json:"-"`
	RecordFormat    string              `json:"-"`
	RecordImages    []RecordImageFormat `json:"-"`
	Scope           Scope               `json:"-"`
	Derived         []DerivationInfo    `json:"-"`
	Withheld        []int               `json:"-"`
}

func (e EntityInfo) Field(name string) (FieldInfo, bool) {
	i := slices.IndexFunc(e.Fields, func(f FieldInfo) bool { return f.Name == name })
	if i < 0 {
		return FieldInfo{}, false
	}
	return e.Fields[i], true
}

var (
	timeType  = reflect.TypeFor[time.Time]()
	moneyType = reflect.TypeFor[Money]()
	refIface  = reflect.TypeFor[reference]()
)

type Query struct {
	BusinessTime *BusinessTime        `json:"businessTime,omitempty"`
	Traversal    *LinkTraversal       `json:"traversal,omitempty"`
	Set          *QuerySet            `json:"set,omitempty"`
	Domain       json.RawMessage      `json:"domain,omitempty"`
	Filter       *Expr                `json:"filter,omitempty"`
	Parameters   map[string]ExprValue `json:"parameters,omitempty"`
	Search       string               `json:"search,omitempty"`
	Sort         []string             `json:"sort,omitempty"`
	Offset       int                  `json:"offset,omitempty"`
	Limit        int                  `json:"limit,omitempty"`
	Archived     bool                 `json:"archived,omitempty"`
}

func (c Caller) Put(r *pb.ChangeRecord, entity any) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Put(c, r, entity)
}

func Get[T any](c Caller, id string) (T, bool) {
	var zero T
	if c.rt == nil {
		return zero, false
	}
	v, ok := c.rt.Get(c, reflect.TypeFor[T](), id)
	if !ok {
		return zero, false
	}
	return v.(T), true
}

func Find[T any](c Caller, q Query) ([]T, int, *kernel.Error) {
	if c.rt == nil {
		return nil, 0, notFound()
	}
	values, total, err := c.rt.Find(c, reflect.TypeFor[T](), q)
	out := make([]T, len(values))
	for i, v := range values {
		out[i] = v.(T)
	}
	return out, total, err
}

func (c Caller) FindOf(t reflect.Type, q Query) ([]any, int, *kernel.Error) {
	if c.rt == nil {
		return nil, 0, notFound()
	}
	return c.rt.Find(c, t, q)
}

func (c Caller) PutAt(now time.Time, cause string, entity any) *kernel.Error {
	r := &pb.ChangeRecord{RecordedTime: timestamppb.New(now), Submission: &pb.Submission{PrincipalId: c.ID, Schema: &pb.SchemaRef{Name: cause}}}
	return c.Put(r, entity)
}

func (c Caller) Check(entity any) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Check(c, entity)
}
