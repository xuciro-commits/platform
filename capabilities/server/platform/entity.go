package platform

import (
	"encoding/json"
	"fmt"
	"platformserver/platform/authz"
	"reflect"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// The application model (ADR-0016). An app declares its entity types once, as
// Go structs; the host keeps their records, and gives every type the same
// reads (filter, sort, page, history, related records), a record page and
// generated forms. The app keeps the rules: its decisions put records through
// the caller, inside the input, so replay rebuilds them.

// Record is embedded first in every entity struct.
type Record struct {
	ID       string `json:"id"`
	Revision uint32 `json:"revision"` // the kernel's revision of the record (K4 C12)
	Created  Stamp  `json:"created"`
	Changed  Stamp  `json:"changed"`
	Archived bool   `json:"archived,omitempty"`
}

// Stamp is who changed a record, when, and by which decision.
type Stamp struct {
	By     string    `json:"by,omitempty"`
	At     time.Time `json:"at,omitzero"`
	Change string    `json:"change,omitempty"`
}

// Ref is a reference to a record of an entity type of the same app (D3):
// the record's ID, typed by the struct it points to.
type Ref[T any] string

func (Ref[T]) refType() reflect.Type { return reflect.TypeFor[T]() }

type reference interface{ refType() reflect.Type }

// Money is an amount in minor units (cents) of a currency (ISO 4217).
type Money struct {
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

// Entity declares an entity type of the app.
type Entity struct {
	PropertyBindings map[string]AssetBinding
	// Compute, when set, fills the record's computed fields right before every
	// decision on it is checked; it receives a pointer to the record.
	Compute func(record any)
	// Validate, when set, may refuse a record after Compute and before the
	// store's own checks: cross-record rules such as one extension record per
	// base record (ADR-0058 A3). It receives a pointer to the record.
	Validate func(c Caller, record any) *kernel.Error

	Type   string // a data class the app is authority for, e.g. "crm.opportunity"
	Title  string // "Opportunity"
	Plural string // "Opportunities"; default: Title with the English plural rule
	// Description says what a record of this type is, for people and agents
	// (ADR-0023 D1); Synonyms are other names people use for it, comma-separated.
	Description string
	Synonyms    string
	Model       any    // the struct's zero value, e.g. Opportunity{}
	Display     string // the field naming a record; default: the first search field, else the ID
	Scope       Scope
	// KnowledgeFiles makes the text files attached to its records knowledge,
	// searched and cited like a knowledge field (ADR-0028 D3).
	KnowledgeFiles bool
	// Standard asks for generated create, edit and archive actions (D5),
	// named <type>.create, <type>.edit and <type>.archive.
	Standard Standard
	// Implements names the interfaces this type carries the fields of
	// (ADR-0058 A2); the host checks them when the tenant is composed.
	Implements []string
	// Extends names the type this one adds fields to, one record per base
	// record through its `base` reference (ADR-0058 A3); its pages merge them.
	Extends string
	// Lifecycle makes the type a document that moves through states (ADR-0017).
	Lifecycle *Lifecycle
	// Seed are the records the type starts with in a tenant (a deployment's or
	// a package's configuration, like the organisation's seed); decisions change
	// them afterwards, and replay starts from them again.
	Seed []any
	// Derived is content this type took from other records (ADR-0033), and
	// Withheld the boolean field the host sets when it left some of it out for
	// a reader. The host checks every source again at each read; the app's only
	// duty is to keep the refs on the record when it writes the content.
	Derived  []Derivation
	Withheld string
}

// Derivation says that some of a type's content came from other records
// (ADR-0033): what the reader may not read at the source is not read here
// either, however long ago it was written.
type Derivation struct {
	// From is the field holding the records the content came from: refs
	// ("<type>/<id>", or "<type>/<id>#<field>" for one field of a record), one
	// or a list of them. A path may enter one list of records the type holds
	// ("steps.sources": the field sources of each step), and its last segment
	// may join two fields as a type and an id ("draft.type/target"). "*" is
	// every source the type's other derivations name.
	From string
	// Fields are emptied for a reader who may not read one of the sources —
	// relative to the element when From enters a list. Element instead leaves
	// that element out altogether.
	Fields  []string
	Element bool
}

// DerivationInfo is a Derivation resolved to field indices for the host.
type DerivationInfo struct {
	All     bool     // From "*": every source the type names
	List    []int    // the list of records holding it, none for the record itself
	Refs    [][]int  // the fields holding the sources; two are joined as "<type>/<id>"
	Fields  [][]int  // what is emptied, within the element when List is set
	Element bool     // leave the element out instead
	Names   []string // the declared field names, for diagnostics
}

// Lifecycle declares a status field, its states and the transitions between
// them (ADR-0017 D1). Each transition is an action <type>.<name>; the status
// changes only through transitions, so a record's history is its lifecycle.
type Lifecycle struct {
	Field       string // the status field: read-only text or choice
	Initial     string // a new record's status
	States      []State
	Transitions []Transition
}

type State struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Tone        string `json:"tone,omitempty" enum:"info,success,warning,danger,neutral"`
	Description string `json:"description,omitempty"` // what a record in this state means (ADR-0023 D1)
}

// Transition moves a record from one of From to one of To. Do, when set,
// checks the transition's rules on the record (a pointer to the entity struct)
// with the caller's payload and changes its other fields; it may choose the
// status among To (the first is the default). It runs inside the decision, so
// replay runs it again; an error refuses the transition.
type Transition struct {
	Name        string
	Title       string
	Description string
	From, To    []string
	// ToInput names the required choice payload that the original action uses as its destination.
	ToInput    string
	Roles      []string
	Payload    []Field
	Capability string    // default: the entity type
	Approval   *Approval // the transition waits for these approvers (ADR-0017)
	Do         func(c Caller, record any, payload json.RawMessage, now time.Time) *kernel.Error
	// After runs once the transition is accepted and the record stored: what
	// follows from it elsewhere (another record, a notification, an effect).
	After func(c Caller, r *pb.ChangeRecord, record any, now time.Time)
}

// LifecycleInfo is a lifecycle as the UI sees it.
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

// Standard are the generated actions an entity type asks for, and who may call them.
type Standard struct {
	Create, Edit, Archive bool
	Roles                 []string
	Capability            string // default: the entity type
	// CreateRoles, EditRoles and ArchiveRoles, when set, say who may take that
	// verb instead of Roles (ADR-0037 18b): a role that reads may not create.
	CreateRoles, EditRoles, ArchiveRoles []string
}

// roles are the roles that may take verb: its own list, or Roles.
func (s Standard) roles(verb string) []string {
	own := map[string][]string{"create": s.CreateRoles, "edit": s.EditRoles, "archive": s.ArchiveRoles}[verb]
	if own != nil {
		return own
	}
	return s.Roles
}

// Scope is who sees which records in the platform's reads (D4): per role, all
// of the tenant's, those in the member's units, those in their units and
// below, or their own. Roles not listed see Default ("" is the whole tenant).
// Rules read the app's records unscoped: authorization is the action's.
type Scope struct {
	Levels    map[string]string // role → tenant, below, unit, own
	Default   string
	Structure string // organisation structure for unit and below
	Unit      string // field holding the record's unit
	// Owner is the field holding the member who owns the record, or
	// OwnerCreated: whoever's decision created it (ADR-0037 D5).
	Owner string
	// Participants are the members a record concerns — a request's requester
	// and approvers, a task's candidates: they read it whatever their role in
	// the app, so whoever is told about a record can open it (F-31).
	Participants func(record any) []string
	// Through makes a record readable exactly when the record it names
	// ("<type>/<id>") is: a file attached to a ticket, a comment on an order
	// (ADR-0028). It replaces roles and levels for the type.
	Through func(record any) string
}

const (
	ScopeTenant = "tenant"
	ScopeBelow  = "below"
	ScopeUnit   = "unit"
	ScopeOwn    = "own"
	// ScopeNone hides the type and its records from the role (ADR-0037 18b).
	ScopeNone = "none"
	// OwnerCreated as Scope.Owner makes a record its creator's.
	OwnerCreated = "created"
)

// Level is the scope a role gives.
func (s Scope) Level(role string) string {
	if l, ok := s.Levels[role]; ok {
		return l
	}
	if s.Default == "" {
		return ScopeTenant
	}
	return s.Default
}

// LevelFor is the widest level any of roles gives (ADR-0078 §3.3); none without a role.
func (s Scope) LevelFor(roles []string) string {
	levels := make([]string, 0, len(roles))
	for _, r := range roles {
		if r != "" {
			levels = append(levels, s.Level(r))
		}
	}
	return authz.Widest(levels)
}

// FieldInfo describes one field, for the host's reads and the UI kit's pages.
type FieldInfo struct {
	Property *AssetBinding `json:"property,omitempty"`
	Name     string        `json:"name"`
	Title    string        `json:"title"`
	Type     string        `json:"type" enum:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference,references,tags,lines,json"`
	Required bool          `json:"required,omitempty"`
	Search   bool          `json:"search,omitempty"`
	ReadOnly bool          `json:"readOnly,omitempty"`
	// Aside marks a field a purpose-built surface writes — a page's sections in
	// the composer (ADR-0035) — so generated forms do not ask for it. Its
	// actions still take it, and it is read and shown like any other field.
	Aside   bool     `json:"aside,omitempty"`
	Choices []string `json:"choices,omitempty"`
	// ChoiceTitles are the choices in the reader's language, beside the values
	// records hold; the host fills them when it translates (ADR-0023).
	ChoiceTitles []string `json:"choiceTitles,omitempty"`
	Ref          string   `json:"ref,omitempty"` // the entity type a reference points to
	// Stereotype narrows a reference to the enterprise model (ref:"enterprise.element")
	// to one UAF stereotype, tag stereo:"ActualLocation" (ADR-0067 D8).
	Stereotype string `json:"stereotype,omitempty"`
	// Inverse names the relation from the referenced record back to this one
	// ("opportunities" on an account), tag inverse:"…" (ADR-0040 21b D1).
	Inverse string `json:"inverse,omitempty"`
	// Knowledge marks a text field agents and members find through the
	// knowledge app (ADR-0022 D1), tag knowledge:"true".
	Knowledge bool `json:"knowledge,omitempty"`
	// Field security (ADR-0028 D3), from the tags read:"role,role" and
	// write:"role": the app's roles that read the field, and those that set it
	// through generated actions; empty: every role that reads the record.
	// Who may not read a field may not set it either.
	Read  []string `json:"read,omitempty"`
	Write []string `json:"write,omitempty"`
	// Personal marks personal data (ADR-0028 D4), tag personal:"contact",
	// "identity", "health", "finance" …: its reads are audited.
	Personal string `json:"personal,omitempty"`
	// Meaning (ADR-0023 D1), from the tags help:"…", synonyms:"a,b" and
	// example:"…": what the field holds, other names for it, a typical value.
	Help     string `json:"help,omitempty"`
	Synonyms string `json:"synonyms,omitempty"`
	Example  string `json:"example,omitempty"`
	// Fields are a lines field's columns (ADR-0024): each line is a struct of them.
	Fields []FieldInfo `json:"fields,omitempty"`
	Index  []int       `json:"-"`
}

// EntityInfo is an entity type as the host and the UI see it.
type EntityInfo struct {
	Type           string         `json:"type"`
	Title          string         `json:"title"`
	Plural         string         `json:"plural"`
	Description    string         `json:"description,omitempty"`
	Synonyms       string         `json:"synonyms,omitempty"`
	KnowledgeFiles bool           `json:"knowledgeFiles,omitempty"` // its attached text files are knowledge (ADR-0028)
	App            string         `json:"app"`
	Display        string         `json:"display"`
	Fields         []FieldInfo    `json:"fields"`
	Standard       []string       `json:"standard"`             // the generated actions' schemas
	Implements     []string       `json:"implements,omitempty"` // the interfaces it carries the fields of (ADR-0058 A2)
	Extends        string         `json:"extends,omitempty"`    // the type it adds fields to (ADR-0058 A3)
	Lifecycle      *LifecycleInfo `json:"lifecycle,omitempty"`
	Go             reflect.Type   `json:"-"`
	Scope          Scope          `json:"-"`
	// Derived and Withheld are ADR-0033's declaration, resolved: the host
	// narrows these fields to the sources the reader may still read.
	Derived  []DerivationInfo `json:"-"`
	Withheld []int            `json:"-"`
}

// Field is the named field's description.
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

// Describe reads an entity declaration. Field names are the struct's JSON
// names; the tag `field:"required,search,readonly"` marks them, `title:"…"`
// names them, `choices:"a,b"` restricts a string, and `type:"date"` (or
// longtext) refines a string. typeOf resolves a Ref's target struct to its
// entity type ("" when it is not one of the app's).
func Describe(app string, e Entity, typeOf func(reflect.Type) string) (EntityInfo, error) {
	t := reflect.TypeOf(e.Model)
	if t == nil || t.Kind() != reflect.Struct || t.NumField() == 0 || t.Field(0).Type != reflect.TypeFor[Record]() || !t.Field(0).Anonymous {
		return EntityInfo{}, fmt.Errorf("entity %s: the model must be a struct embedding platform.Record first", e.Type)
	}
	info := EntityInfo{Type: e.Type, Title: e.Title, Description: e.Description, Synonyms: e.Synonyms, KnowledgeFiles: e.KnowledgeFiles, App: app, Display: e.Display, Go: t, Scope: e.Scope, Fields: []FieldInfo{}, Standard: []string{}, Implements: e.Implements, Extends: e.Extends}
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
			if f, ok := info.Field(x[1]); !ok || f.Type != "text" {
				return EntityInfo{}, fmt.Errorf("entity %s: the scope's %s field %q is not a text field", e.Type, x[0], x[1])
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
				if index < 0 || input.Type != "string" || !input.Required || input.Ref != "" || len(input.Choices) != len(t.To) || slices.ContainsFunc(t.To, func(state string) bool { return !slices.Contains(input.Choices, state) }) || t.Do == nil {
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
	return info, nil
}

// EntityActions are the catalog entries an entity type's declaration
// generates: its standard create, edit and archive, and its transitions.
func EntityActions(e Entity) []Action {
	info, err := Describe("", e, func(reflect.Type) string { return "?" }) // only names and kinds matter here
	if err != nil {
		panic(err) // a declaration error: the app does not compose
	}
	var fields, editable []Field
	for _, f := range info.Fields {
		if f.ReadOnly || f.Aside { // a field a purpose-built editor writes is not asked for in a generated form
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money", "json": "json"}[f.Type]
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
		fields = append(fields, Field{Name: f.Name, Type: typ, Required: f.Required, Description: description})
		editable = append(editable, Field{Name: f.Name, Type: typ, Description: description})
	}
	var out []Action
	capability := e.Standard.Capability
	if capability == "" {
		capability = e.Type
	}
	if e.Standard.Create {
		out = append(out, Action{Schema: e.Type + ".create", Target: e.Type, New: true, Capability: capability, Title: "Create " + strings.ToLower(info.Title),
			Description: "Create " + article(info.Title) + ".", Payload: fields, Roles: e.Standard.roles("create")})
	}
	if e.Standard.Edit {
		out = append(out, Action{Schema: e.Type + ".edit", Target: e.Type, Capability: capability, Title: "Edit " + strings.ToLower(info.Title),
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
				Description: description, Payload: payload, Roles: t.Roles, Approval: t.Approval})
			if t.Approval != nil && t.Approval.Pending != "" { // the work app's alone: no role holds them
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

// Query selects records of one type (D7): a domain in Odoo's prefix form, as
// JSON — [["stage","=","open"], "|", ["amount",">",100], ["title","like","offsite"]] —
// conditions joined by "&" unless "|" or "!" says otherwise; a text search
// over the type's search fields; sort fields ("-" for descending); a page.
type Query struct {
	Traversal *LinkTraversal  `json:"traversal,omitempty"`
	Set       *QuerySet       `json:"set,omitempty"`
	Domain    json.RawMessage `json:"domain,omitempty"`
	Search    string          `json:"search,omitempty"`
	Sort      []string        `json:"sort,omitempty"`
	Offset    int             `json:"offset,omitempty"`
	Limit     int             `json:"limit,omitempty"`    // 0: all
	Archived  bool            `json:"archived,omitempty"` // include archived records
}

// Put stores entity (a struct embedding Record, with its ID set) as the
// result of the decision r, inside the input that made r. The host sets its
// revision and stamps and keeps the fields it changed as the record's history.
func (c Caller) Put(r *pb.ChangeRecord, entity any) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Put(c, r, entity)
}

// Get is the app's record of T with id (unscoped: the app's own data).
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

// Find is the app's records of T matching q, and how many match in all.
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

// FindOf is Find for a model type known only at run time (a defined object's).
func (c Caller) FindOf(t reflect.Type, q Query) ([]any, int, *kernel.Error) {
	if c.rt == nil {
		return nil, 0, notFound()
	}
	return c.rt.Find(c, t, q)
}

// Records is every record of T, archived ones included, ordered by ID.
func Records[T any](c Caller) []T {
	out, _, _ := Find[T](c, Query{Archived: true})
	return out
}

// PutAt stores entity as changed by an input that is not a decision (an
// observation, an effect's answer): the change is stamped with the caller and
// now, and named by cause in the record's history.
func (c Caller) PutAt(now time.Time, cause string, entity any) *kernel.Error {
	r := &pb.ChangeRecord{RecordedTime: timestamppb.New(now), Submission: &pb.Submission{PrincipalId: c.ID, Schema: &pb.SchemaRef{Name: cause}}}
	return c.Put(r, entity)
}

// Check validates entity against its declaration before a decision is
// accepted: required fields, choices, dates, and references to records that exist.
func (c Caller) Check(entity any) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Check(c, entity)
}

// article is a title in lower case with its indefinite article: "an account".
func article(title string) string {
	t := strings.ToLower(title)
	if t != "" && strings.ContainsRune("aeiou", rune(t[0])) {
		return "an " + t
	}
	return "a " + t
}

// describeFields describes the fields of an entity's struct from its field
// from on: 1 skips the embedded Record; a line's struct starts at 0 and may not
// hold lines itself.
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
			ft = ft.Elem() // optional scalar values retain their declared field kind
		}
		switch {
		case refined == "json" && (ft.Kind() == reflect.Map || ft.Kind() == reflect.Struct || ft == reflect.TypeOf(json.RawMessage{})):
			f.Type = "json" // governed structured definitions; each owner validates its schema
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
			// A reference named by the type it points at, rather than Ref[T]: how
			// a definition a tenant authored says it (ADR-0034 D1).
			f.Type, f.Ref, f.Stereotype = "reference", sf.Tag.Get("ref"), sf.Tag.Get("stereo")
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
			f.Type = "lines" // child lines kept inside the record (Frappe child tables, Odoo one2many)
			if columns, err := describeFields(e, ft.Elem(), 0, typeOf); err == nil {
				f.Fields = columns // lines whose columns the kit cannot show stay read-only
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
		out = append(out, f)
	}
	return out, nil
}

// Reads reports whether a member holding role in the field's app reads it (ADR-0028 D3).
func (f FieldInfo) Reads(role string) bool {
	return len(f.Read) == 0 || slices.Contains(f.Read, role)
}

// Writes reports whether a member holding role sets it through generated actions.
func (f FieldInfo) Writes(role string) bool {
	return f.Reads(role) && (len(f.Write) == 0 || slices.Contains(f.Write, role))
}

// ReadsAny reports whether a member holding any of roles reads it (ADR-0078 §3.3: roles add up).
func (f FieldInfo) ReadsAny(roles []string) bool {
	return len(f.Read) == 0 || slices.ContainsFunc(roles, f.Reads)
}

// WritesAny reports whether a member holding any of roles sets it.
func (f FieldInfo) WritesAny(roles []string) bool {
	return slices.ContainsFunc(roles, f.Writes) || len(roles) == 0 && f.Writes("")
}

// describeDerived resolves Entity.Derived and Entity.Withheld to field indices
// (ADR-0033), refusing a declaration that names a field the type does not have:
// a tenant with one does not start, like any other manifest error.
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
		return fail("a type that derives content names a withheld field, so a reader is told (ADR-0033 D4)")
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
				if !ok || f.Type != "lines" {
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

// elementOf is the struct type of a list field's elements.
func elementOf(t reflect.Type, index []int) reflect.Type {
	f := t.FieldByIndex(index).Type
	for f.Kind() == reflect.Slice || f.Kind() == reflect.Pointer {
		f = f.Elem()
	}
	return f
}

// pathTo is the index of the field a JSON name names in a struct.
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
