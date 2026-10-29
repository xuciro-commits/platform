package platform

import (
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/types/known/timestamppb"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform/authz"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

type Versioning struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	ValidFrom  string `json:"validFrom"`
	ValidUntil string `json:"validUntil,omitempty"`
	Pin        string `json:"pin"`
}

type VersionPinBinding struct {
	Reference string `json:"reference"`
	Version   string `json:"version"`
}

type RecordVersionPin struct {
	Reference    string          `json:"reference"`
	VersionField string          `json:"versionField"`
	Source       string          `json:"source"`
	Version      json.RawMessage `json:"version"`
	At           time.Time       `json:"at"`
}

type BusinessTime struct {
	At  string `json:"at,omitempty"`
	All bool   `json:"all,omitempty"`
}

func withinBusinessPeriod(startComparedToPoint, pointComparedToEnd int, bounded bool) bool {
	return startComparedToPoint <= 0 && (!bounded || pointComparedToEnd < 0)
}

func ActiveOn(from, until, day string) bool {
	return withinBusinessPeriod(strings.Compare(from, day), strings.Compare(day, until), until != "")
}

func EffectiveAt(kind, from, until, at string) (bool, error) {
	layout := time.DateOnly
	if kind == "datetime" {
		layout = time.RFC3339Nano
	}
	start, err := time.Parse(layout, from)
	if err != nil {
		return false, fmt.Errorf("business valid-from must be a %s", kind)
	}
	point, err := time.Parse(layout, at)
	if err != nil {
		return false, fmt.Errorf("business time must be a %s", kind)
	}
	var end time.Time
	if until != "" {
		end, err = time.Parse(layout, until)
		if err != nil || !end.After(start) {
			return false, fmt.Errorf("business valid-until must be after valid-from")
		}
	}
	return withinBusinessPeriod(start.Compare(point), point.Compare(end), until != ""), nil
}

func CheckVersioning(info EntityInfo, lookup func(string) (EntityInfo, bool)) error {
	if v := info.Versioning; v != nil {
		key, k := info.Field(v.Key)
		version, n := info.Field(v.Version)
		from, f := info.Field(v.ValidFrom)
		if !k || !n || !f || !key.Required || !version.Required || !from.Required || !slices.Contains([]string{"text", "choice"}, key.Type) || !slices.Contains([]string{"text", "integer"}, version.Type) || !slices.Contains([]string{"date", "datetime"}, from.Type) || v.Key == v.Version || v.Key == v.ValidFrom || v.Version == v.ValidFrom {
			return fmt.Errorf("business versioning needs distinct required key, integer/text version and date/datetime valid-from fields")
		}
		if v.ValidUntil != "" {
			until, ok := info.Field(v.ValidUntil)
			if !ok || until.Type != from.Type || slices.Contains([]string{v.Key, v.Version, v.ValidFrom}, v.ValidUntil) {
				return fmt.Errorf("business valid-until must be a distinct field with the valid-from type")
			}
		}
		if v.Pin != "none" && v.Pin != "on_create" && (!strings.HasPrefix(v.Pin, "on_state:") || strings.TrimPrefix(v.Pin, "on_state:") == "") {
			return fmt.Errorf("business version pin policy must be none, on_create or on_state:<state>")
		}
	}
	seen := map[string]bool{}
	for _, pin := range info.VersionPins {
		reference, ok := info.Field(pin.Reference)
		version, exists := info.Field(pin.Version)
		if !ok || reference.Type != "reference" || reference.Ref == "" || !exists || !version.ReadOnly || pin.Reference == pin.Version || seen[pin.Reference] || seen[pin.Version] {
			return fmt.Errorf("business version pin needs a distinct reference and read-only version field")
		}
		seen[pin.Reference], seen[pin.Version] = true, true
		source, ok := lookup(reference.Ref)
		if !ok || source.App != info.App || source.Versioning == nil || source.Versioning.Pin == "none" {
			return fmt.Errorf("business version pin source must declare a same-authority pin policy")
		}
		selected, ok := source.Field(source.Versioning.Version)
		if !ok || selected.Type != version.Type {
			return fmt.Errorf("business version pin field type differs from its source")
		}
		if policy := source.Versioning.Pin; strings.HasPrefix(policy, "on_state:") && (info.Lifecycle == nil || !slices.ContainsFunc(info.Lifecycle.States, func(s State) bool { return s.Name == strings.TrimPrefix(policy, "on_state:") })) {
			return fmt.Errorf("business version pin state is not declared by the consumer")
		}
	}
	return nil
}

func VersioningConstraints(v *Versioning) []Constraint {
	if v == nil {
		return nil
	}
	return []Constraint{
		{Name: "business-version-unique", Kind: "unique", Fields: []string{v.Key, v.Version}, Scope: "tenant", Message: "The business key and version must be unique", Translations: map[string]string{"zh-CN": "业务标识和版本必须唯一"}},
	}
}

type BusinessVersionRuntime interface {
	PrepareBusinessVersions(Caller, any, time.Time) *kernel.Error
}

func (c Caller) PrepareBusinessVersions(record any, at time.Time) *kernel.Error {
	if c.Replaying {
		return nil
	}
	if r, ok := c.rt.(BusinessVersionRuntime); ok {
		return r.PrepareBusinessVersions(c, record, at)
	}
	return nil
}

type Constraint struct {
	Name         string            `json:"name"`
	Kind         string            `json:"kind" enum:"required,unique,range,pattern,expression,cardinality,immutable,temporal"`
	Fields       []string          `json:"fields"`
	Expr         *Expr             `json:"expr,omitempty"`
	Message      string            `json:"message"`
	Translations map[string]string `json:"translations,omitempty"`
	Scope        string            `json:"scope,omitempty"`
	Level        string            `json:"level,omitempty" enum:"record,txn"`
	Reads        *RuleReadSpec     `json:"reads,omitempty"`
}

func (c *Constraint) UnmarshalJSON(raw []byte) error {
	type plain Constraint
	return strictTxn(raw, (*plain)(c))
}

func ConstraintSpec(info EntityInfo, constraint Constraint) RuleReadSpec {
	spec := RuleReadSpec{Object: info.Type}
	if constraint.Reads != nil {
		spec.Collections = constraint.Reads.Collections
	}
	return spec
}

func ConstraintEntity(info EntityInfo) EntityInfo {
	info.Fields = slices.Clone(info.Fields)
	info.Fields = append(info.Fields, FieldInfo{Name: "id", Type: "text"}, FieldInfo{Name: "archived", Type: "boolean"})
	return info
}

func CheckConstraints(info EntityInfo, lookup func(string) (EntityInfo, bool), transactionBoundary bool, engines ...RuleEngine) error {
	if len(info.Constraints) > 64 {
		return fmt.Errorf("object supports at most 64 constraints")
	}
	seen := map[string]bool{}
	ownerLookup := func(typ string) (EntityInfo, bool) {
		if typ == info.Type {
			return ConstraintEntity(info), true
		}
		other, ok := lookup(typ)
		return ConstraintEntity(other), ok && other.App == info.App
	}
	for _, c := range info.Constraints {
		if !pageNodeID.MatchString(c.Name) || len(c.Name) > 64 || seen[c.Name] || strings.TrimSpace(c.Message) == "" || len(c.Message) > 512 || len(c.Fields) == 0 || len(c.Fields) > 16 {
			return fmt.Errorf("constraint needs a unique stable name, message and 1-16 fields")
		}
		seen[c.Name] = true
		if !slices.Contains([]string{"required", "unique", "range", "pattern", "expression", "cardinality", "immutable", "temporal"}, c.Kind) {
			return fmt.Errorf("constraint %s has unknown kind %s", c.Name, c.Kind)
		}
		if c.Level != "" && c.Level != "record" && c.Level != "txn" || c.Level == "txn" && !transactionBoundary {
			return fmt.Errorf("constraint %s needs a known accepted transaction boundary", c.Name)
		}
		for i, name := range c.Fields {
			if _, ok := ConstraintEntity(info).Field(name); !ok || slices.Contains(c.Fields[:i], name) {
				return fmt.Errorf("constraint %s names an unknown or repeated field %s", c.Name, name)
			}
		}
		for language, message := range c.Translations {
			if language != "zh-CN" || strings.TrimSpace(message) == "" || len(message) > 512 {
				return fmt.Errorf("constraint %s has an invalid message translation", c.Name)
			}
		}
		if c.Kind != "unique" && c.Scope != "" {
			return fmt.Errorf("only unique constraints have a scope")
		}
		if c.Kind == "unique" && c.Scope != "" && c.Scope != "tenant" {
			path, err := ConstraintScopePath(info, c.Scope)
			if err != nil {
				return err
			}
			if _, err := RecordPathField(info.Type, path, ownerLookup); err != nil {
				return fmt.Errorf("constraint %s scope: %w", c.Name, err)
			}
		}
		if c.Kind == "immutable" && (c.Expr == nil && info.Lifecycle == nil || c.Level == "txn") {
			return fmt.Errorf("immutable constraint %s needs a lifecycle and record level", c.Name)
		}
		needsExpr := slices.Contains([]string{"range", "pattern", "expression", "cardinality", "temporal"}, c.Kind)
		if c.Kind != "immutable" && needsExpr != (c.Expr != nil) {
			return fmt.Errorf("constraint %s has an invalid expression shape", c.Name)
		}
		if c.Reads != nil {
			if c.Reads.Object != "" && c.Reads.Object != info.Type || len(c.Reads.Inputs) > 0 || c.Expr == nil {
				return fmt.Errorf("constraint reads cannot accept invocation inputs")
			}
			for _, set := range c.Reads.Collections {
				object, ok := ownerLookup(set.Object)
				if !ok || object.App != info.App || set.App != info.App || set.Policy != "complete" || set.Of != "" && set.Of != "$subject" {
					return fmt.Errorf("constraint collections must be complete and owned by the application")
				}
			}
		}
		if c.Expr != nil {
			if c.Expr.Kind != "boolean" {
				return fmt.Errorf("constraint %s must return boolean", c.Name)
			}
			if _, err := RuleSchema(*c.Expr, ConstraintSpec(info, c), ownerLookup, engines...); err != nil {
				return fmt.Errorf("constraint %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

func ConstraintScopePath(info EntityInfo, scope string) ([]string, error) {
	if scope == "parent" {
		var refs []string
		for _, field := range info.Fields {
			if field.Type == "reference" {
				refs = append(refs, field.Name)
			}
		}
		if len(refs) != 1 {
			return nil, fmt.Errorf("parent scope needs one reference; choose an explicit path")
		}
		return refs, nil
	}
	path := strings.Split(scope, ".")
	if len(path) > 16 {
		return nil, fmt.Errorf("constraint scope exceeds 16 segments")
	}
	return path, nil
}

func (c Caller) RefuseConstraint(constraint Constraint, record string, values map[string]json.RawMessage, reason string) *kernel.Error {
	issue := FieldIssue{Code: "constraint_violation", Constraint: constraint.Name, Record: record, Message: constraint.Message, Translations: constraint.Translations, Values: values, Reason: reason, Path: []string{constraint.Fields[0]}}
	for _, field := range constraint.Fields[1:] {
		issue.RelatedPaths = append(issue.RelatedPaths, []string{field})
	}
	err := Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Constraint {name}: {message}", constraint.Name, constraint.Message)
	if c.inputRefused != nil {
		c.inputRefused(err, []FieldIssue{issue})
	}
	if c.rt != nil {
		c.rt.InputRefused(err, []FieldIssue{issue})
	}
	return err
}

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
	LocalRules       map[string]Expr
	EnterpriseUse    *Expr
	Protocols        []Provision
	CreateTxn        *BusinessTxn
	ApplyCreateTxn   func(Caller, *pb.ChangeRecord, time.Time) *kernel.Error
	Versioning       *Versioning
	VersionPins      []VersionPinBinding
	Sets             []RecordSet
	Constraints      []Constraint
	PropertyBindings map[string]AssetBinding
	Compute          func(record any)
	ComputeAt        func(c Caller, record any, now time.Time) *kernel.Error
	Validate         func(c Caller, record any) *kernel.Error
	ValidateAt       func(c Caller, record any, now time.Time) *kernel.Error

	Type            string
	Title           string
	Plural          string
	Description     string
	Synonyms        string
	Model           any
	RecordFormat    string
	RecordImages    []RecordImageFormat
	Display         string
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

func (c *FieldCondition) Holds(value string) bool { return c == nil || slices.Contains(c.In, value) }

func parseWhen(tag string) (*FieldCondition, error) {
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

func Describe(app string, e Entity, typeOf func(reflect.Type) string) (EntityInfo, error) {
	t := reflect.TypeOf(e.Model)
	if t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 || t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous {
		return EntityInfo{}, fmt.Errorf("entity %s: the model must be a struct embedding platform.Record first", e.Type)
	}
	info := EntityInfo{LocalRules: e.LocalRules, EnterpriseUse: e.EnterpriseUse, Type: e.Type, Title: e.Title, Description: e.Description, Synonyms: e.Synonyms, KnowledgeFiles: e.KnowledgeFiles, App: app, Display: e.Display, Go: t, Scope: e.Scope, Fields: []FieldInfo{}, Standard: []string{}, Implements: e.Implements, Extends: e.Extends, ExtensionSource: e.ExtensionSource, Constraints: append(slices.Clone(e.Constraints), VersioningConstraints(e.Versioning)...), Sets: e.Sets, Versioning: e.Versioning, VersionPins: slices.Clone(e.VersionPins)}
	info.RecordFormat, info.RecordImages = e.RecordFormat, slices.Clone(e.RecordImages)
	if err := checkRecordImageFormats(info); err != nil {
		return EntityInfo{}, err
	}
	if info.Title == "" {
		info.Title = e.Type
	}
	info.Plural = e.Plural
	switch t := info.Title; {
	case info.Plural != "":
	case strings.HasSuffix(t, "y") && !strings.HasSuffix(t, "ay") && !strings.HasSuffix(t, "ey") && !strings.HasSuffix(t, "oy"):
		info.Plural = t[:len(t)-1] + "ies"
	case strings.HasSuffix(t, "s") || strings.HasSuffix(t, "x") || strings.HasSuffix(t, "ch") || strings.HasSuffix(t, "sh"):
		info.Plural = t + "es"
	default:
		info.Plural = t + "s"
	}
	fields, err := describeFields(e, t, 1, typeOf)
	if err != nil {
		return EntityInfo{}, err
	}
	info.Fields = fields
	for name, binding := range e.PropertyBindings {
		i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == name })
		if i < 0 || binding.Ref.Kind != AssetPropertyType || binding.Ref.Check() != nil || binding.SourceVersion == "" {
			return EntityInfo{}, fmt.Errorf("invalid property field binding")
		}
		selected := binding
		info.Fields[i].Property = &selected
	}
	if info.Display == "" {
		info.Display = "id"
		if i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Search }); i >= 0 {
			info.Display = info.Fields[i].Name
		}
	}
	for _, x := range [][2]string{{"owner", e.Scope.Owner}, {"unit", e.Scope.Unit}} {
		if x[1] != "" && !(x[0] == "owner" && x[1] == OwnerCreated) {
			f, ok := info.Field(x[1])
			unitReference := x[0] == "unit" && f.Type == "reference" && f.Ref == "enterprise.element"
			if !ok || f.Type != "text" && !unitReference {
				return EntityInfo{}, fmt.Errorf("entity %s: the scope's %s field %q needs text or the unit's enterprise reference", e.Type, x[0], x[1])
			}
		}
	}
	for _, l := range append(func() []string {
		var out []string
		for _, l := range e.Scope.Levels {
			out = append(out, l)
		}
		return out
	}(), e.Scope.Default) {
		if l != "" && l != ScopeTenant && l != ScopeBelow && l != ScopeUnit && l != ScopeOwn && l != ScopeNone ||
			(l == ScopeOwn && e.Scope.Owner == "") || ((l == ScopeUnit || l == ScopeBelow) && (e.Scope.Unit == "" || e.Scope.Structure == "")) {
			return EntityInfo{}, fmt.Errorf("entity %s: scope level %q without the field it needs", e.Type, l)
		}
	}
	if l := e.Lifecycle; l != nil {
		f, ok := info.Field(l.Field)
		states := map[string]bool{}
		for _, s := range l.States {
			states[s.Name] = true
		}
		if !ok || !f.ReadOnly || (f.Type != "text" && f.Type != "choice") || !states[l.Initial] || len(states) != len(l.States) {
			return EntityInfo{}, fmt.Errorf("entity %s: the lifecycle needs a read-only text or choice status field, unique states and an initial state among them", e.Type)
		}
		info.Lifecycle = &LifecycleInfo{Field: l.Field, Initial: l.Initial, States: l.States, Transitions: []TransitionInfo{}}
		for i, t := range l.Transitions {
			if to, input := transactionDestination(e.Type, l.Field, t.Txn, t.Payload); len(to) > 0 {
				t.To, t.ToInput = to, input
			}
			ends := append(slices.Clone(t.From), t.To...)
			if t.Name == "" || len(t.From) == 0 || len(t.To) == 0 || slices.ContainsFunc(ends, func(s string) bool { return !states[s] }) ||
				slices.ContainsFunc(l.Transitions[:i], func(x Transition) bool { return x.Name == t.Name }) || t.Name == "create" || t.Name == "edit" || t.Name == "archive" {
				return EntityInfo{}, fmt.Errorf("entity %s: transition %q needs a unique name and states of the lifecycle", e.Type, t.Name)
			}
			if t.ToInput != "" {
				index := slices.IndexFunc(t.Payload, func(f Field) bool { return f.Name == t.ToInput })
				var input Field
				if index >= 0 {
					input = t.Payload[index]
				}
				if index < 0 || input.Type != "string" || !input.Required || input.Ref != "" || len(input.Choices) != len(t.To) || slices.ContainsFunc(t.To, func(state string) bool { return !slices.Contains(input.Choices, state) }) || t.Do == nil && (t.Txn == nil || t.ApplyTxn == nil) {
					return EntityInfo{}, fmt.Errorf("entity %s: transition %s needs its required destination choice and original action", e.Type, t.Name)
				}
			}
			title := t.Title
			if title == "" {
				title = strings.ToUpper(t.Name[:1]) + t.Name[1:]
			}
			info.Lifecycle.Transitions = append(info.Lifecycle.Transitions, TransitionInfo{Name: t.Name, Schema: e.Type + "." + t.Name, Title: title, From: t.From, To: t.To, ToInput: t.ToInput})
		}
	}
	for _, x := range [][2]any{{e.Standard.Create, ".create"}, {e.Standard.Edit, ".edit"}, {e.Standard.Archive, ".archive"}} {
		if x[0].(bool) {
			info.Standard = append(info.Standard, e.Type+x[1].(string))
		}
	}
	if err := describeDerived(&info, e); err != nil {
		return EntityInfo{}, err
	}
	for _, alias := range info.Fields {
		if alias.AliasOf == "" {
			continue
		}
		target, found := info.Field(alias.AliasOf)
		if !found || target.Name == alias.Name || target.AliasOf != "" || !alias.ReadOnly || alias.Type != target.Type || alias.Ref != target.Ref || alias.Required != target.Required || !slices.Equal(alias.Choices, target.Choices) || info.Go.FieldByIndex(alias.Index).Type != info.Go.FieldByIndex(target.Index).Type {
			return EntityInfo{}, fmt.Errorf("field %s needs one compatible direct read alias target", alias.Name)
		}
	}
	if _, err := info.CompileEnterpriseUse(); err != nil {
		return EntityInfo{}, fmt.Errorf("entity %s: %w", e.Type, err)
	}
	return info, nil
}

func (info EntityInfo) CompileEnterpriseUse(engines ...RuleEngine) (ExprProgram, error) {
	if info.EnterpriseUse == nil {
		return nil, nil
	}
	if info.EnterpriseUse.Kind != "boolean" {
		return nil, fmt.Errorf("current enterprise use must return boolean")
	}
	refs := map[string]ExprType{"archived": {Kind: "boolean"}}
	for _, field := range info.Fields {
		refs[field.Name] = ExprType{Kind: RuleFieldKind(field.Type), Object: field.Ref}
	}
	return ruleEngineOf(engines).CompileExpr(*info.EnterpriseUse, ExprSchema{Refs: refs})
}

func ApplyFieldAliases(info EntityInfo, value reflect.Value) {
	for _, field := range info.Fields {
		if field.AliasOf != "" {
			target, _ := info.Field(field.AliasOf)
			value.FieldByIndex(field.Index).Set(value.FieldByIndex(target.Index))
		}
	}
}

func (info EntityInfo) HasReadSources() bool {
	return len(info.Derived) > 0 || slices.ContainsFunc(info.Fields, func(field FieldInfo) bool { return field.AliasOf != "" })
}

func payloadColumns(columns []FieldInfo) []Field {
	if len(columns) == 0 {
		return nil
	}
	out := make([]Field, 0, len(columns))
	for _, column := range columns {
		if column.ReadOnly {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "json": "json", "lines": "lines"}[column.Type]
		if typ == "" {
			typ = "string"
		}
		field := Field{Name: column.Name, Type: typ, Required: column.Required && column.When == nil, Description: column.Title,
			Choices: column.Choices, Constraints: column.Constraints, Range: column.Range, Fields: payloadColumns(column.Fields)}
		if column.Ref == "enterprise.element" {
			field.Ref, field.Stereotype, field.Stereotypes = column.Ref, column.Stereotype, slices.Clone(column.Stereotypes)
		}
		out = append(out, field)
	}
	return out
}

func EntityActions(e Entity) []Action {
	info, err := Describe("", e, func(reflect.Type) string { return "?" })
	if err != nil {
		panic(err)
	}
	var fields, editable []Field
	for _, f := range info.Fields {
		if f.ReadOnly || f.Aside {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "json": "json", "lines": "lines"}[f.Type]
		if typ == "" {
			typ = "string"
		}
		description := f.Title
		if f.Help != "" {
			description += ": " + f.Help
		}
		if len(f.Choices) > 0 {
			description += " (" + strings.Join(f.Choices, ", ") + ")"
		}
		if f.Example != "" {
			description += ", e.g. " + f.Example
		}
		field := Field{Name: f.Name, Type: typ, Required: f.Required && f.When == nil, Description: description, Choices: f.Choices, Constraints: f.Constraints, Range: f.Range, Group: f.Group,
			Fields: payloadColumns(f.Fields)}
		if f.Ref == "enterprise.element" {
			field.Ref, field.Stereotype, field.Stereotypes = f.Ref, f.Stereotype, slices.Clone(f.Stereotypes)
		}
		fields = append(fields, field)
		field.Required = false
		editable = append(editable, field)
	}
	var out []Action
	capability := e.Standard.Capability
	if capability == "" {
		capability = e.Type
	}
	if e.Standard.Create {
		if e.CreateTxn != nil {
			for _, step := range e.CreateTxn.Steps {
				if step.Kind == "update" && step.Target.Type == e.Type && step.Target.ID == "$subject" && step.When == nil {
					for _, assignment := range step.Changes {
						for i := range fields {
							if fields[i].Name == assignment.Field {
								fields[i].Required = false
							}
						}
					}
				}
			}
		}
		out = append(out, Action{Schema: e.Type + ".create", Target: e.Type, New: true, Capability: capability, Title: "Create " + strings.ToLower(info.Title),
			Description: "Create " + article(info.Title) + ".", Payload: fields, Roles: e.Standard.roles("create")})
	}
	if e.Standard.Edit {
		out = append(out, Action{recordEdit: true, Schema: e.Type + ".edit", Target: e.Type, Capability: capability, Title: "Edit " + strings.ToLower(info.Title),
			Description: "Change fields of " + article(info.Title) + "; fields left out keep their value.", Payload: editable, Roles: e.Standard.roles("edit")})
	}
	if e.Standard.Archive {
		out = append(out, Action{Schema: e.Type + ".archive", Target: e.Type, Capability: capability, Title: "Archive " + strings.ToLower(info.Title),
			Description: "Archive " + article(info.Title) + ": it leaves lists but stays referenced and in history.", Payload: []Field{}, Roles: e.Standard.roles("archive")})
	}
	if e.Lifecycle != nil {
		for i, t := range e.Lifecycle.Transitions {
			c := t.Capability
			if c == "" {
				c = e.Type
			}
			payload := t.Payload
			if payload == nil {
				payload = []Field{}
			}
			description := t.Description
			if description == "" {
				description = fmt.Sprintf("Move %s from %s to %s.", article(info.Title), strings.Join(t.From, " or "), strings.Join(t.To, " or "))
			}
			out = append(out, Action{Schema: e.Type + "." + t.Name, Target: e.Type, Capability: c, Title: info.Lifecycle.Transitions[i].Title,
				Description: description, Payload: payload, Roles: t.Roles, Adoption: t.Adoption, Approval: t.Approval, DeferRequired: t.DeferRequired, ConfirmationOnly: t.ConfirmationOnly})
			if t.Approval != nil && t.Approval.Pending != "" {
				for _, m := range [][2]string{{ApprovalHeld, "Hold for approval"}, {ApprovalRejected, "Mark rejected"}, {ApprovalReturned, "Return from approval"}} {
					suffix, title := m[0], m[1]
					out = append(out, Action{Schema: e.Type + "." + t.Name + suffix, Target: e.Type, Capability: c, Title: title,
						Description: "Moves the record while its approval runs; only the approval takes it.", Payload: []Field{}, Automation: true})
				}
			}
		}
	}
	return out
}

type Query struct {
	BusinessTime *BusinessTime   `json:"businessTime,omitempty"`
	Traversal    *LinkTraversal  `json:"traversal,omitempty"`
	Set          *QuerySet       `json:"set,omitempty"`
	Domain       json.RawMessage `json:"domain,omitempty"`
	Search       string          `json:"search,omitempty"`
	Sort         []string        `json:"sort,omitempty"`
	Offset       int             `json:"offset,omitempty"`
	Limit        int             `json:"limit,omitempty"`
	Archived     bool            `json:"archived,omitempty"`
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

func article(title string) string {
	t := strings.ToLower(title)
	if t != "" && strings.ContainsRune("aeiou", rune(t[0])) {
		return "an " + t
	}
	return "a " + t
}

func describeFields(e Entity, t reflect.Type, from int, typeOf func(reflect.Type) string) ([]FieldInfo, error) {
	out := []FieldInfo{}
	for i := from; i < t.NumField(); i++ {
		sf := t.Field(i)
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if !sf.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			return nil, fmt.Errorf("entity %s: field %s needs a json name", e.Type, sf.Name)
		}
		f := FieldInfo{Name: name, Title: sf.Tag.Get("title"), Index: sf.Index, Knowledge: sf.Tag.Get("knowledge") == "true",
			Help: sf.Tag.Get("help"), Synonyms: sf.Tag.Get("synonyms"), Example: sf.Tag.Get("example"), Personal: sf.Tag.Get("personal")}
		f.SourceType = sf.Tag.Get("source")
		if policy := sf.Tag.Get("ai"); policy != "" {
			allowed := policy == "allow"
			f.AIInput = &allowed
		}
		f.AliasOf = sf.Tag.Get("alias")
		if f.SourceType != "" && !slices.Contains([]string{"record", "user-input", "external-input", "generated"}, f.SourceType) {
			return nil, fmt.Errorf("field %s has an unknown source type", name)
		}
		if raw := sf.Tag.Get("constraints"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &f.Constraints); err != nil {
				return nil, fmt.Errorf("%s.%s constraints: %w", e.Type, name, err)
			}
		}
		if raw := sf.Tag.Get("range"); raw != "" {
			if err := json.Unmarshal([]byte(raw), &f.Range); err != nil {
				return nil, fmt.Errorf("%s.%s range: %w", e.Type, name, err)
			}
		}
		f.Group = sf.Tag.Get("group")
		if roles := sf.Tag.Get("read"); roles != "" {
			f.Read = strings.Split(roles, ",")
		}
		if roles := sf.Tag.Get("write"); roles != "" {
			f.Write = strings.Split(roles, ",")
		}
		if f.Title == "" {
			f.Title = strings.ToUpper(name[:1]) + name[1:]
		}
		for _, flag := range strings.Split(sf.Tag.Get("field"), ",") {
			switch flag {
			case "required":
				f.Required = true
			case "search":
				f.Search = true
			case "readonly":
				f.ReadOnly = true
			case "aside":
				f.Aside = true
			case "":
			default:
				return nil, fmt.Errorf("entity %s: field %s has an unknown flag %q", e.Type, name, flag)
			}
		}
		refined := sf.Tag.Get("type")
		ft := sf.Type
		if ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		switch {
		case refined == "json" && (ft.Kind() == reflect.Map || ft.Kind() == reflect.Struct || ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array):
			f.Type = "json"
		case ft.Implements(refIface):
			f.Type = "reference"
			f.Ref = typeOf(reflect.Zero(ft).Interface().(reference).refType())
		case ft.Kind() == reflect.Slice && ft.Elem().Implements(refIface):
			f.Type = "references"
			f.Ref = typeOf(reflect.Zero(ft.Elem()).Interface().(reference).refType())
		case ft == timeType:
			f.Type = "datetime"
		case ft == moneyType:
			f.Type = "money"
		case ft.Kind() == reflect.String && sf.Tag.Get("ref") != "":
			f.Type, f.Ref, f.Stereotype = "reference", sf.Tag.Get("ref"), sf.Tag.Get("stereo")
			if values := sf.Tag.Get("stereos"); values != "" {
				f.Stereotypes = strings.Split(values, ",")
			}
		case ft.Kind() == reflect.String && refined == "date", ft.Kind() == reflect.String && refined == "longtext":
			f.Type = refined
		case ft.Kind() == reflect.String && sf.Tag.Get("choices") != "":
			f.Type, f.Choices = "choice", strings.Split(sf.Tag.Get("choices"), ",")
		case ft.Kind() == reflect.String:
			f.Type = "text"
		case ft.Kind() == reflect.Bool:
			f.Type = "boolean"
		case ft.Kind() >= reflect.Int && ft.Kind() <= reflect.Uint64:
			f.Type = "integer"
		case ft.Kind() == reflect.Float64:
			f.Type = "decimal"
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.String:
			f.Type = "tags"
		case ft.Kind() == reflect.Slice && ft.Elem().Kind() == reflect.Struct && from > 0:
			f.Type = "lines"
			if columns, err := describeFields(e, ft.Elem(), 0, typeOf); err == nil {
				f.Fields = columns
			}
		default:
			return nil, fmt.Errorf("entity %s: field %s has a type the kit does not know (%s)", e.Type, name, ft)
		}
		if (f.Type == "reference" || f.Type == "references") && f.Ref == "" {
			return nil, fmt.Errorf("entity %s: field %s refers to a struct that is not an entity type of the app", e.Type, name)
		}
		if f.Inverse = sf.Tag.Get("inverse"); f.Inverse != "" && f.Type != "reference" {
			return nil, fmt.Errorf("entity %s: field %s names an inverse but is not a reference", e.Type, name)
		}
		when, err := parseWhen(sf.Tag.Get("when"))
		if err != nil {
			return nil, fmt.Errorf("entity %s: field %s: %w", e.Type, name, err)
		}
		f.When = when
		out = append(out, f)
	}
	if err := checkConditions(e.Type, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (f FieldInfo) Reads(role string) bool {
	return !slices.Contains(f.Read, "-") && (len(f.Read) == 0 || slices.Contains(f.Read, role))
}

func (f FieldInfo) Writes(role string) bool {
	return f.Reads(role) && (len(f.Write) == 0 || slices.Contains(f.Write, role))
}

func (f FieldInfo) ReadsAny(roles []string) bool {
	return len(f.Read) == 0 || slices.ContainsFunc(roles, f.Reads)
}

func (f FieldInfo) WritesAny(roles []string) bool {
	return slices.ContainsFunc(roles, f.Writes) || len(roles) == 0 && f.Writes("")
}

func describeDerived(info *EntityInfo, e Entity) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("entity %s: derived content: "+format, append([]any{e.Type}, args...)...)
	}
	if e.Withheld != "" {
		f, ok := info.Field(e.Withheld)
		if !ok || f.Type != "boolean" {
			return fail("the withheld field %q is not a boolean field", e.Withheld)
		}
		info.Withheld = f.Index
	}
	if len(e.Derived) == 0 {
		return nil
	}
	if e.Withheld == "" {
		return fail("a type that derives content names a withheld field, so a reader is told")
	}
	for _, d := range e.Derived {
		out := DerivationInfo{Element: d.Element, Names: append([]string{d.From}, d.Fields...)}
		if d.From == "" {
			return fail("a derivation names no source field")
		}
		holder, at := info.Go, ""
		if d.From == "*" {
			out.All = true
		} else {
			list, last, entered := strings.Cut(d.From, ".")
			if entered {
				f, ok := info.Field(list)
				jsonList := false
				if ok && f.Type == "json" {
					fieldType := info.Go.FieldByIndex(f.Index).Type
					jsonList = fieldType.Kind() == reflect.Slice && elementOf(info.Go, f.Index).Kind() == reflect.Struct
				}
				if !ok || f.Type != "lines" && !jsonList {
					return fail("%q enters %q, which is not a list of records the type holds", d.From, list)
				}
				out.List, holder, at = f.Index, elementOf(info.Go, f.Index), list+"."
			} else {
				last = list
			}
			for _, name := range strings.Split(last, "/") {
				path, err := pathTo(holder, name)
				if err != nil {
					return fail("%q: %v", at+name, err)
				}
				out.Refs = append(out.Refs, path)
			}
			if len(out.Refs) > 2 {
				return fail("%q joins more than a type and an id", d.From)
			}
		}
		if out.Element && len(out.List) == 0 {
			return fail("%q drops an element, but names no list", d.From)
		}
		if !out.Element && len(d.Fields) == 0 {
			return fail("%q empties no field and drops no element", d.From)
		}
		for _, name := range d.Fields {
			path, err := pathTo(holder, name)
			if err != nil {
				return fail("%q: %v", at+name, err)
			}
			out.Fields = append(out.Fields, path)
		}
		info.Derived = append(info.Derived, out)
	}
	return nil
}

func elementOf(t reflect.Type, index []int) reflect.Type {
	f := t.FieldByIndex(index).Type
	for f.Kind() == reflect.Slice || f.Kind() == reflect.Pointer {
		f = f.Elem()
	}
	return f
}

func pathTo(t reflect.Type, name string) ([]int, error) {
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%s holds no fields", t)
	}
	for i := range t.NumField() {
		f := t.Field(i)
		if json, _, _ := strings.Cut(f.Tag.Get("json"), ","); json == name && f.IsExported() {
			return []int{i}, nil
		}
	}
	return nil, fmt.Errorf("%s has no field %q", t, name)
}

type ExtensionSource struct {
	Base   AssetBinding `json:"base"`
	Fields []FieldInfo  `json:"fields"`
}

func (s ExtensionSource) Check(typ string) error {
	if s.Base.Ref.Check() != nil || s.Base.Ref.Kind != AssetObject || s.Base.Ref.Name != typ || s.Base.SourceVersion == "" || len(s.Fields) == 0 {
		return fmt.Errorf("extension %s needs its original base package version and shape", typ)
	}
	seen := map[string]bool{}
	for _, field := range s.Fields {
		if field.Name == "" || field.Type == "" || seen[field.Name] {
			return fmt.Errorf("extension %s has an invalid original base shape", typ)
		}
		seen[field.Name] = true
	}
	return nil
}

func CheckExtension(extension, base EntityInfo) error {
	if extension.Extends == "" || extension.Extends != base.Type || extension.Type == base.Type {
		return fmt.Errorf("extension %s has an unavailable base %s", extension.Type, extension.Extends)
	}
	link, ok := extension.Field("base")
	if !ok || link.Type != "reference" || link.Ref != base.Type || !link.Required {
		return fmt.Errorf("extension %s needs a required base reference to %s", extension.Type, base.Type)
	}
	for _, field := range extension.Fields {
		if field.Name == "base" || field.ReadOnly && field.AliasOf != "" {
			continue
		}
		if _, exists := base.Field(field.Name); exists {
			return fmt.Errorf("extension %s field %s conflicts with base %s; declare a migration to rename the extension field", extension.Type, field.Name, base.Type)
		}
	}
	if source := extension.ExtensionSource; source != nil {
		if err := source.Check(base.Type); err != nil {
			return err
		}
		if base.App != "" && source.Base.Ref.App != base.App {
			return fmt.Errorf("extension %s base authority differs from its source package", extension.Type)
		}
		for _, field := range source.Fields {
			next, exists := base.Field(field.Name)
			if !exists || field.Type != next.Type || field.Ref != next.Ref || field.Required != next.Required || field.ReadOnly != next.ReadOnly || field.AliasOf != next.AliasOf || !slices.Equal(field.Choices, next.Choices) || !reflect.DeepEqual(field.Property, next.Property) {
				return fmt.Errorf("extension %s source %s@%s has an incompatible base field %s; review and republish the extension against the new package", extension.Type, base.Type, source.Base.SourceVersion, field.Name)
			}
		}
	}
	return nil
}

type Interface struct {
	Name        string           `json:"name"`
	Title       string           `json:"title"`
	Description string           `json:"description,omitempty"`
	Fields      []InterfaceField `json:"fields"`
}

type InterfaceField struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Title string `json:"title"`
}

var interfaceName = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9]*$`)

func (i Interface) Check(app string) error {
	if !interfaceName.MatchString(i.Name) || i.Name[:len(app)+1] != app+"." {
		return fmt.Errorf("interface %q: not <%s>.<name> in lower-case letters and digits", i.Name, app)
	}
	if i.Title == "" || i.Description == "" || len(i.Fields) == 0 {
		return fmt.Errorf("interface %s lacks a title, a description or fields", i.Name)
	}
	for k, f := range i.Fields {
		if f.Name == "" || f.Type == "" || slices.ContainsFunc(i.Fields[:k], func(x InterfaceField) bool { return x.Name == f.Name }) {
			return fmt.Errorf("interface %s: field %q unnamed, untyped or repeated", i.Name, f.Name)
		}
	}
	return nil
}

func (i Interface) Implements(info EntityInfo) error {
	for _, want := range i.Fields {
		k := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Name == want.Name })
		if k < 0 {
			return fmt.Errorf("%s implements %s but has no field %s", info.Type, i.Name, want.Name)
		}
		if info.Fields[k].Type != want.Type {
			return fmt.Errorf("%s implements %s but its field %s is %s, not %s", info.Type, i.Name, want.Name, info.Fields[k].Type, want.Type)
		}
	}
	return nil
}

func CheckInterfaces(manifests []Manifest, infos map[string]EntityInfo) error {
	declared := map[string]Interface{}
	for _, m := range manifests {
		for _, i := range m.Interfaces {
			if err := i.Check(m.ID); err != nil {
				return err
			}
			if _, dup := declared[i.Name]; dup {
				return fmt.Errorf("interface %s is declared twice", i.Name)
			}
			declared[i.Name] = i
		}
	}
	for _, m := range manifests {
		for _, e := range m.Entities {
			for k, name := range e.Implements {
				i, ok := declared[name]
				if !ok {
					return fmt.Errorf("%s implements %s, which no app declares", e.Type, name)
				}
				if slices.Contains(e.Implements[:k], name) {
					return fmt.Errorf("%s implements %s twice", e.Type, name)
				}
				info, ok := infos[e.Type]
				if !ok {
					return fmt.Errorf("%s implements %s but was not described", e.Type, name)
				}
				if err := i.Implements(info); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func Interfaces(manifests []Manifest) []Interface {
	var out []Interface
	for _, m := range manifests {
		out = append(out, m.Interfaces...)
	}
	slices.SortFunc(out, func(a, b Interface) int { return compareStrings(a.Name, b.Name) })
	return out
}

func compareStrings(a, b string) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}
