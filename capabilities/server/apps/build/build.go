// Package build is the platform's builder app (ADR-0034), on the app API and
// internal/host (ADR-0025 D4): objects a tenant defines, published into the
// running host so they behave like an app's own — the same records, generated
// actions and forms, search, aggregates, journal and replay. The definition is
// data; nothing about it is a second runtime.
package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
	"unicode"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

const (
	ID         = "build"
	ObjectType = "build.object"
	// Builder defines and publishes objects; User works with what is published.
	Builder       = "builder"
	User          = "user"
	SchemaPublish = ObjectType + ".publish"
)

// Object is an object someone defines in this tenant: what it is called and
// which fields it holds. Publishing it installs it (ADR-0034 D2).
type Object struct {
	platform.Record
	Name        string  `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visit"`
	Title       string  `json:"title" field:"required,search" title:"What people call it" example:"Visit"`
	Plural      string  `json:"plural,omitempty" title:"What people call several" example:"Visits"`
	Description string  `json:"description,omitempty" type:"longtext" help:"What one record of it is, for people and agents"`
	Fields      []Field `json:"fields" title:"Fields"`
	// States and Actions make its records a small process (ADR-0037): the
	// object editor owns them, so generated forms do not ask for them.
	States    []State  `json:"states,omitempty" field:"aside" title:"States"`
	Actions   []Action `json:"actions,omitempty" field:"aside" title:"Actions"`
	State     string   `json:"state" field:"readonly" choices:"draft,published"`
	Installed string   `json:"installed,omitempty" field:"readonly" title:"Installed as" help:"The type records of it are stored under"`
	// Published is the definition as it was last published, which is what is
	// installed and what a restore installs again — not the draft beside it.
	Published string `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

// Field is one field of a defined object, as a person describes it.
type Field struct {
	Name     string `json:"name" field:"required" help:"Lower-case letters and digits" example:"visited"`
	Title    string `json:"title" field:"required" title:"Label" example:"Visited on"`
	Type     string `json:"type" field:"required" choices:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference" example:"date"`
	Choices  string `json:"choices,omitempty" help:"For a choice: the values, comma-separated" example:"open,done"`
	Ref      string `json:"ref,omitempty" title:"Refers to" help:"For a reference: the object it points at" example:"crm.account"`
	Required bool   `json:"required,omitempty"`
	Search   bool   `json:"search,omitempty" title:"Searchable"`
}

// Build is the builder app of one tenant.
type Build struct {
	host   host.Host
	ledger *platform.Ledger
	// installed are the objects published in this tenant, by entity type: their
	// declarations, so generated actions of a defined object reach the record store.
	installed map[string]platform.Entity
}

// Attach is called by the host when a tenant is composed.
func (b *Build) Attach(h host.Host) { b.host = h }

// New is a tenant's builder app.
func New(tenant string) *Build {
	b := &Build{installed: map[string]platform.Entity{}}
	actions := append(platform.EntityActions(b.objectEntity()), platform.EntityActions(b.pageEntity())...)
	actions = append(actions, platform.EntityActions(b.applicationEntity())...)
	b.ledger = platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), ObjectType, PageType, AppType)
	return b
}

func (b *Build) objectEntity() platform.Entity {
	return platform.Entity{Type: ObjectType, Title: "Object", Plural: "Objects", Model: Object{}, Display: "title",
		Description: "An object this organisation defines itself: its name, what people call it and its fields. Publishing it installs it, and records of it are kept like any other app's.",
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "objects"},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft",
			States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning", Description: "Being defined; nothing is installed yet."},
				{Name: "published", Title: "Published", Tone: "success", Description: "Installed in this tenant: people work with its records."}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", From: []string{"draft", "published"}, To: []string{"published"},
				Roles: []string{Builder}, Capability: "objects", Payload: []platform.Field{},
				Description: "Install the object as it stands. Published again, it takes its new fields, and records keep the values of the fields that remain.",
				Do: func(c platform.Caller, record any, _ json.RawMessage, now time.Time) *kernel.Error {
					object, ok := record.(*Object)
					if !ok {
						return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
					}
					if err := b.install(*object); err != nil {
						return err
					}
					object.Installed = TypeOf(object.Name) // what its records are kept as, for people to see
					object.Published = published(*object)
					return nil
				}}}}}
}

func (b *Build) Manifest() platform.Manifest {
	entities := []platform.Entity{b.objectEntity(), b.pageEntity(), b.applicationEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	return platform.Manifest{ID: ID, Title: "Builder", Version: "1", Actions: b.ledger.Catalog, Entities: entities,
		Pages: []platform.Page{{Name: "objects", Title: "Objects", Description: "The objects this organisation defines. Publish one to install it.",
			Layout: "list-detail", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: ObjectType},
			ListFields:   []string{"title", "name", "state", "installed"},
			DetailFields: []string{"title", "name", "plural", "description", "fields", "state", "installed"},
			Actions: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".create"},
				{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: SchemaPublish}}},
			{Name: "applications", Title: "Applications", Description: "The applications this organisation hands to its people. Each holds pages and appears in their launcher.",
				Layout: "list-detail", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: AppType},
				ListFields:   []string{"title", "name", "icon", "state"},
				DetailFields: []string{"title", "name", "description", "icon", "pages", "groups", "state"},
				Actions: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: AppType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: AppType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: SchemaHandOver}}},
			{Name: "pages", Title: "Pages", Description: "The pages this organisation composes over the objects it may read. Publish one to put it in the workspace.",
				Layout: "list-detail", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: PageType},
				ListFields:   []string{"title", "name", "object", "state"},
				DetailFields: []string{"title", "name", "description", "object", "list", "detail", "actions", "state"},
				Actions: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: PageType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: PageType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: SchemaRelease}}}}}
}

// sortedTypes are the installed types, in a fixed order: a manifest and a
// submission must not depend on a map's order.
func sortedTypes(installed map[string]platform.Entity) []string {
	out := make([]string, 0, len(installed))
	for typ := range installed {
		out = append(out, typ)
	}
	slices.Sort(out)
	return out
}

func (b *Build) Declarations() []*pb.AuthorityDeclaration { return b.ledger.Declarations() }
func (b *Build) Snapshot() (json.RawMessage, error)       { return b.ledger.Snapshot() }
func (b *Build) Restore(raw json.RawMessage) error        { return b.ledger.Restore(raw) }
func (b *Build) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (b *Build) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Submit takes the builder's own actions and those generated for every object
// it has installed: a defined object's records are decided like any other's.
func (b *Build) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if name := s.GetSchema().GetName(); (name == ObjectType+".create" || name == ObjectType+".edit") && !c.Replaying {
		// What the payload carries is checked while it is still a draft, so the
		// person is told at once; publishing checks the whole definition again.
		var p struct {
			Name   *string  `json:"name"`
			Fields *[]Field `json:"fields"`
		}
		if json.Unmarshal(s.GetPayload(), &p) == nil {
			var err error
			if p.Name != nil {
				err = b.checkName(*p.Name, s.GetTarget().GetId())
			}
			if err == nil && p.Fields != nil {
				err = checkFields(*p.Fields, b.host)
			}
			if err != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
			}
		}
	}
	if name := s.GetSchema().GetName(); (name == PageType+".create" || name == PageType+".edit") && !c.Replaying {
		var p Page
		if json.Unmarshal(s.GetPayload(), &p) == nil && p.Object != "" {
			if _, known := b.host.Entity(p.Object); !known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: fmt.Sprintf("this tenant has no object %q", p.Object)}
			}
		}
	}
	entities := []platform.Entity{b.objectEntity(), b.pageEntity(), b.applicationEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	if record, err, ok := b.ledger.Generated(c, s, now, nil, entities...); ok {
		return record, err
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Installed reports the entity type an object is installed as.
func TypeOf(name string) string { return ID + "." + name }

// check refuses a definition the host could not install, with the reason, while
// it is still a draft: a name that is not a name, a field the platform has no
// type for, a choice without values, a reference to an object that is not there.
func (b *Build) check(o Object, id string) error {
	if err := b.checkName(o.Name, id); err != nil {
		return err
	}
	if err := checkFields(o.Fields, b.host); err != nil {
		return err
	}
	return checkProcess(o)
}

// checkName refuses a name that is not a name, or one already taken.
func (b *Build) checkName(name, id string) error {
	if !named(name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", name)
	}
	if other, taken := b.taken(name, id); taken {
		return fmt.Errorf("the name %q is already %s", name, other)
	}
	return nil
}

// checkFields refuses a field the platform has no type for, a choice without
// values, or a reference to an object this tenant has not.
func checkFields(fields []Field, h host.Host) error {
	seen := map[string]bool{}
	for _, f := range fields {
		switch {
		case !named(f.Name):
			return fmt.Errorf("the field name %q is not lower-case letters and digits", f.Name)
		case seen[f.Name]:
			return fmt.Errorf("the field %q is declared twice", f.Name)
		case f.Name == "id" || f.Name == "state" || f.Name == "created" || f.Name == "changed" || f.Name == "revision" || f.Name == "archived":
			return fmt.Errorf("the field %q is the platform's own", f.Name)
		case f.Title == "":
			return fmt.Errorf("the field %q has no label", f.Name)
		case !slices.Contains(fieldTypes, f.Type):
			return fmt.Errorf("the field %q has no type %q", f.Name, f.Type)
		case f.Type == "choice" && strings.TrimSpace(f.Choices) == "":
			return fmt.Errorf("the choice field %q has no values", f.Name)
		case f.Type == "reference" && f.Ref == "":
			return fmt.Errorf("the reference field %q says nothing it refers to", f.Name)
		case f.Type == "reference" && h != nil && !h.Declares(f.Ref):
			return fmt.Errorf("the field %q refers to %q, which this tenant has no object for", f.Name, f.Ref)
		}
		seen[f.Name] = true
	}
	return nil
}

var fieldTypes = []string{"text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference"}

// taken says whether another object, or an app's own type, holds the name.
func (b *Build) taken(name, id string) (string, bool) {
	if b.host == nil {
		return "", false
	}
	if _, mine := b.installed[TypeOf(name)]; !mine && b.host.Declares(TypeOf(name)) {
		return "an app's own type", true
	}
	objects, _, _ := platform.Find[Object](b.host.Automation(ID, false), platform.Query{Limit: 500})
	for _, o := range objects {
		if o.Name == name && o.ID != id && !o.Archived {
			return "the object " + o.Title, true
		}
	}
	return "", false
}

func named(s string) bool {
	if s == "" || !unicode.IsLetter(rune(s[0])) {
		return false
	}
	for _, r := range s {
		if !unicode.IsLower(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

// install declares the object in the running host: the record store keeps its
// records, the ledger takes its generated actions, and the asset registry
// offers it and its page (ADR-0034 D2). Installed again, it takes its new
// fields and its records come with it (D3).
func (b *Build) install(o Object) *kernel.Error {
	if err := b.check(o, o.ID); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	entity := Entity(o)
	actions := platform.EntityActions(entity)
	if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.host.Install(entity, actions, page(o)); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	b.installed[entity.Type] = entity
	return nil
}

// published is a definition as it was published: what is installed, kept on the
// record so a restore installs that and not a draft written since (ADR-0034 D3).
func published[T any](definition T) string {
	raw, _ := json.Marshal(definition)
	return string(raw)
}

// wasPublished is the definition that was published, or false when none was.
func wasPublished[T any](raw string) (T, bool) {
	var out T
	if raw == "" || json.Unmarshal([]byte(raw), &out) != nil {
		return out, false
	}
	return out, true
}

// Reinstall installs every published object and page again, objects first: the
// host calls it when a tenant is restored from a snapshot, before the records
// of the types they define (ADR-0034 D4). It installs what was published, so a
// draft written since stays a draft.
func (b *Build) Reinstall() error {
	objects, _, _ := platform.Find[Object](b.host.Automation(ID, false), platform.Query{Limit: 1000, Sort: []string{"id"}})
	for _, o := range objects {
		was, ok := wasPublished[Object](o.Published)
		if !ok || o.Archived {
			continue
		}
		if err := b.install(was); err != nil {
			return fmt.Errorf("object %s: %v", o.Name, err)
		}
	}
	pages, _, _ := platform.Find[Page](b.host.Automation(ID, false), platform.Query{Limit: 1000, Sort: []string{"id"}})
	for _, p := range pages { // after the objects they show
		was, ok := wasPublished[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.release(was); err != nil {
			return fmt.Errorf("page %s: %v", p.Name, err)
		}
	}
	applications, _, _ := platform.Find[Application](b.host.Automation(ID, false), platform.Query{Limit: 1000, Sort: []string{"id"}})
	for _, a := range applications { // after the pages they hold
		was, ok := wasPublished[Application](a.Published)
		if !ok || a.Archived {
			continue
		}
		if err := b.hand(was); err != nil {
			return fmt.Errorf("application %s: %v", a.Name, err)
		}
	}
	return nil
}

// Entity is the declaration a defined object amounts to: a Go type built now,
// with the tags a developer would have written (ADR-0034 D1).
func Entity(o Object) platform.Entity {
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[platform.Record](), Anonymous: true}}
	for _, f := range o.Fields {
		tag := fmt.Sprintf(`json:"%s,omitempty" title:"%s"`, f.Name, f.Title)
		switch f.Type {
		case "longtext", "date", "datetime":
			tag += fmt.Sprintf(` type:"%s"`, f.Type)
		case "choice":
			tag += fmt.Sprintf(` choices:"%s"`, strings.Join(choices(f.Choices), ","))
		case "reference":
			tag += fmt.Sprintf(` ref:"%s"`, f.Ref)
		}
		var marks []string
		if f.Required {
			marks = append(marks, "required")
		}
		if f.Search {
			marks = append(marks, "search")
		}
		if len(marks) > 0 {
			tag += fmt.Sprintf(` field:"%s"`, strings.Join(marks, ","))
		}
		fields = append(fields, reflect.StructField{Name: goName(f.Name), Type: goType(f.Type), Tag: reflect.StructTag(tag)})
	}
	if len(o.States) > 0 { // where a record stands, which only its actions move (ADR-0037 D2)
		names := make([]string, 0, len(o.States))
		for _, st := range o.States {
			names = append(names, st.Name)
		}
		fields = append(fields, reflect.StructField{Name: "State", Type: reflect.TypeFor[string](),
			Tag: reflect.StructTag(fmt.Sprintf(`json:"state,omitempty" title:"State" field:"readonly" choices:"%s"`, strings.Join(names, ",")))})
	}
	model := reflect.New(reflect.StructOf(fields)).Elem().Interface()
	display := ""
	for _, f := range o.Fields {
		if display == "" && f.Search {
			display = f.Name
		}
	}
	roles := []string{Builder, User}
	return platform.Entity{Type: TypeOf(o.Name), Title: o.Title, Plural: o.Plural, Description: o.Description, Model: model, Display: display,
		Standard:  platform.Standard{Create: true, Edit: true, Archive: true, Roles: roles, Capability: o.Name},
		Lifecycle: lifecycle(o, roles)}
}

// page is the list and detail page a defined object comes with: the same
// descriptor a code page has (ADR-0034 D4).
func page(o Object) platform.Page {
	names := []string{}
	for _, f := range o.Fields {
		names = append(names, f.Name)
	}
	list := slices.Clone(names)
	if len(list) > 4 {
		list = list[:4]
	}
	if len(o.States) > 0 { // where each record stands, in the list and on its page
		list, names = append(list, "state"), append(names, "state")
	}
	typ := TypeOf(o.Name)
	actions := []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: typ + ".create"}, {App: ID, Kind: platform.AssetAction, Name: typ + ".edit"}}
	for _, a := range o.Actions {
		actions = append(actions, platform.AssetRef{App: ID, Kind: platform.AssetAction, Name: typ + "." + a.Name})
	}
	return platform.Page{Name: o.Name, Title: o.Plural, Description: o.Description, Layout: "list-detail",
		Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: typ}, ListFields: list, DetailFields: names, Actions: actions}
}

func choices(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func goType(kind string) reflect.Type {
	switch kind {
	case "integer":
		return reflect.TypeFor[int]()
	case "decimal":
		return reflect.TypeFor[float64]()
	case "money":
		return reflect.TypeFor[platform.Money]()
	case "datetime":
		return reflect.TypeFor[time.Time]()
	case "boolean":
		return reflect.TypeFor[bool]()
	}
	return reflect.TypeFor[string]()
}

// goName is a field's exported Go name: the platform never shows it.
func goName(name string) string {
	return strings.ToUpper(name[:1]) + name[1:]
}
