package platform

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

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
	Type    string // a data class the app is authority for, e.g. "crm.opportunity"
	Title   string // "Opportunity"
	Plural  string // "Opportunities"; default: Title with the English plural rule
	Model   any    // the struct's zero value, e.g. Opportunity{}
	Display string // the field naming a record; default: the first search field, else the ID
	Scope   Scope
	// Standard asks for generated create, edit and archive actions (D5),
	// named <type>.create, <type>.edit and <type>.archive.
	Standard Standard
	// Seed are the records the type starts with in a tenant (a deployment's or
	// a package's configuration, like the organisation's seed); decisions change
	// them afterwards, and replay starts from them again.
	Seed []any
}

// Standard are the generated actions an entity type asks for, and who may call them.
type Standard struct {
	Create, Edit, Archive bool
	Roles                 []string
	Capability            string // default: the entity type
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
	Owner     string // field holding the member who owns the record
}

const (
	ScopeTenant = "tenant"
	ScopeBelow  = "below"
	ScopeUnit   = "unit"
	ScopeOwn    = "own"
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

// FieldInfo describes one field, for the host's reads and the UI kit's pages.
type FieldInfo struct {
	Name     string   `json:"name"`
	Title    string   `json:"title"`
	Type     string   `json:"type"` // text, longtext, integer, decimal, money, date, datetime, boolean, choice, reference, references, tags
	Required bool     `json:"required,omitempty"`
	Search   bool     `json:"search,omitempty"`
	ReadOnly bool     `json:"readOnly,omitempty"`
	Choices  []string `json:"choices,omitempty"`
	Ref      string   `json:"ref,omitempty"` // the entity type a reference points to
	Index    []int    `json:"-"`
}

// EntityInfo is an entity type as the host and the UI see it.
type EntityInfo struct {
	Type     string       `json:"type"`
	Title    string       `json:"title"`
	Plural   string       `json:"plural"`
	App      string       `json:"app"`
	Display  string       `json:"display"`
	Fields   []FieldInfo  `json:"fields"`
	Standard []string     `json:"standard"` // the generated actions' schemas
	Go       reflect.Type `json:"-"`
	Scope    Scope        `json:"-"`
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
	info := EntityInfo{Type: e.Type, Title: e.Title, App: app, Display: e.Display, Go: t, Scope: e.Scope, Fields: []FieldInfo{}, Standard: []string{}}
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
	for i := 1; i < t.NumField(); i++ {
		sf := t.Field(i)
		name, _, _ := strings.Cut(sf.Tag.Get("json"), ",")
		if !sf.IsExported() || name == "-" {
			continue
		}
		if name == "" {
			return EntityInfo{}, fmt.Errorf("entity %s: field %s needs a json name", e.Type, sf.Name)
		}
		f := FieldInfo{Name: name, Title: sf.Tag.Get("title"), Index: sf.Index}
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
			case "":
			default:
				return EntityInfo{}, fmt.Errorf("entity %s: field %s has an unknown flag %q", e.Type, name, flag)
			}
		}
		refined := sf.Tag.Get("type")
		switch ft := sf.Type; {
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
		default:
			return EntityInfo{}, fmt.Errorf("entity %s: field %s has a type the kit does not know (%s)", e.Type, name, ft)
		}
		if (f.Type == "reference" || f.Type == "references") && f.Ref == "" {
			return EntityInfo{}, fmt.Errorf("entity %s: field %s refers to a struct that is not an entity type of the app", e.Type, name)
		}
		info.Fields = append(info.Fields, f)
	}
	if info.Display == "" {
		info.Display = "id"
		if i := slices.IndexFunc(info.Fields, func(f FieldInfo) bool { return f.Search }); i >= 0 {
			info.Display = info.Fields[i].Name
		}
	}
	for _, x := range [][2]string{{"owner", e.Scope.Owner}, {"unit", e.Scope.Unit}} {
		if x[1] != "" {
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
		if l != "" && l != ScopeTenant && l != ScopeBelow && l != ScopeUnit && l != ScopeOwn ||
			(l == ScopeOwn && e.Scope.Owner == "") || ((l == ScopeUnit || l == ScopeBelow) && (e.Scope.Unit == "" || e.Scope.Structure == "")) {
			return EntityInfo{}, fmt.Errorf("entity %s: scope level %q without the field it needs", e.Type, l)
		}
	}
	for _, x := range [][2]any{{e.Standard.Create, ".create"}, {e.Standard.Edit, ".edit"}, {e.Standard.Archive, ".archive"}} {
		if x[0].(bool) {
			info.Standard = append(info.Standard, e.Type+x[1].(string))
		}
	}
	return info, nil
}

// StandardActions are the catalog entries of an entity type's generated actions.
func StandardActions(e Entity) []Action {
	info, err := Describe("", e, func(reflect.Type) string { return "?" }) // only names and kinds matter here
	if err != nil {
		panic(err) // a declaration error: the app does not compose
	}
	var fields, editable []Field
	for _, f := range info.Fields {
		if f.ReadOnly {
			continue
		}
		typ := map[string]string{"integer": "integer", "decimal": "number", "boolean": "boolean", "date": "date", "datetime": "datetime",
			"references": "string[]", "tags": "string[]", "money": "money"}[f.Type]
		if typ == "" {
			typ = "string"
		}
		description := f.Title
		if len(f.Choices) > 0 {
			description += ": " + strings.Join(f.Choices, ", ")
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
		out = append(out, Action{Schema: e.Type + ".create", Target: e.Type, Capability: capability, Title: "Create " + strings.ToLower(info.Title),
			Description: "Create a " + strings.ToLower(info.Title) + ".", Payload: fields, Roles: e.Standard.Roles})
	}
	if e.Standard.Edit {
		out = append(out, Action{Schema: e.Type + ".edit", Target: e.Type, Capability: capability, Title: "Edit " + strings.ToLower(info.Title),
			Description: "Change fields of a " + strings.ToLower(info.Title) + "; fields left out keep their value.", Payload: editable, Roles: e.Standard.Roles})
	}
	if e.Standard.Archive {
		out = append(out, Action{Schema: e.Type + ".archive", Target: e.Type, Capability: capability, Title: "Archive " + strings.ToLower(info.Title),
			Description: "Archive a " + strings.ToLower(info.Title) + ": it leaves lists but stays referenced and in history.", Payload: []Field{}, Roles: e.Standard.Roles})
	}
	return out
}

// Query selects records of one type (D7): a domain in Odoo's prefix form, as
// JSON — [["stage","=","open"], "|", ["amount",">",100], ["title","like","offsite"]] —
// conditions joined by "&" unless "|" or "!" says otherwise; a text search
// over the type's search fields; sort fields ("-" for descending); a page.
type Query struct {
	Domain   json.RawMessage `json:"domain,omitempty"`
	Search   string          `json:"search,omitempty"`
	Sort     []string        `json:"sort,omitempty"`
	Offset   int             `json:"offset,omitempty"`
	Limit    int             `json:"limit,omitempty"`    // 0: all
	Archived bool            `json:"archived,omitempty"` // include archived records
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

// Records is every record of T, archived ones included, ordered by ID.
func Records[T any](c Caller) []T {
	out, _, _ := Find[T](c, Query{Archived: true})
	return out
}

// Check validates entity against its declaration before a decision is
// accepted: required fields, choices, dates, and references to records that exist.
func (c Caller) Check(entity any) *kernel.Error {
	if c.rt == nil {
		return notFound()
	}
	return c.rt.Check(c, entity)
}
