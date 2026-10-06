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
	ID                = "build"
	definitionVersion = "1"
	ObjectType        = "build.object"
	// Builder defines and publishes objects; Publisher saves and activates
	// release candidates without editing definitions; User works with what is
	// published (ADR-0047 §11).
	Builder       = "builder"
	Publisher     = "publisher"
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
	States  []State  `json:"states,omitempty" field:"aside" title:"States"`
	Actions []Action `json:"actions,omitempty" field:"aside" title:"Actions"`
	// Access is who may do what with it (ADR-0037 18b); empty: builder and
	// user do everything, as before.
	Access []Access `json:"access,omitempty" field:"aside" title:"Access"`
	// Implements names the interfaces this object carries the fields of; Extends
	// names an installed object type this one adds fields to, one record per base
	// record through its `base` reference (ADR-0058 A2, A3).
	Implements []string `json:"implements,omitempty" field:"aside" title:"Implements" help:"Interfaces whose fields this object has, e.g. core.coded"`
	Extends    string   `json:"extends,omitempty" field:"aside" title:"Extends" help:"The installed object type this object adds fields to, e.g. core.person; records pair one to one through the base field"`
	State      string   `json:"state" field:"readonly" choices:"draft,published"`
	Installed  string   `json:"installed,omitempty" field:"readonly" title:"Installed as" help:"The type records of it are stored under"`
	// Published is the definition as it was last published, which is what is
	// installed and what a restore installs again — not the draft beside it.
	Published string `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

// Field is one field of a defined object, as a person describes it.
type Field struct {
	Property *platform.AssetBinding `json:"property,omitempty" type:"json" field:"aside"`
	Name     string                 `json:"name" field:"required" help:"Lower-case letters and digits" example:"visited"`
	Title    string                 `json:"title" field:"required" title:"Label" example:"Visited on"`
	Type     string                 `json:"type" field:"required" choices:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference" example:"date"`
	Choices  string                 `json:"choices,omitempty" help:"For a choice: the values, comma-separated" example:"open,done"`
	Ref      string                 `json:"ref,omitempty" title:"Refers to" help:"For a reference: the object it points at" example:"crm.account"`
	// Inverse names the relation seen from the referenced record (ADR-0040 21b).
	Inverse  string `json:"inverse,omitempty" title:"Seen from there as" help:"For a reference: what the referenced record calls these records" example:"visits"`
	Required bool   `json:"required,omitempty"`
	Search   bool   `json:"search,omitempty" title:"Searchable"`
	// Read and Write, when set, are the roles that read and set it (ADR-0028 D3).
	Read  []string `json:"read,omitempty" title:"Read by" help:"Roles that read it; empty: every role that reads the object"`
	Write []string `json:"write,omitempty" title:"Set by" help:"Roles that set it; empty: every role that edits the object"`
}

// Build is the builder app of one tenant.
type Build struct {
	host   host.Host
	ledger *platform.Ledger
	// installed are the objects published in this tenant, by entity type: their
	// declarations, so generated actions of a defined object reach the record store.
	installed map[string]platform.Entity
	// jointEntities are the object drafts of the candidate being built, so the
	// same validation answers entity lookups as the candidate would install
	// them (ADR-0048). Empty outside a joint build.
	jointEntities map[string]platform.EntityInfo
	// jointMode is set while a candidate holds more than one draft: the host's
	// installed-state install checks then move to the candidate's own install
	// dry run, because no single draft's image is installed yet (ADR-0048 D2).
	jointMode     bool
	linkTypes     map[string]LinkType
	propertyTypes map[string]PropertyType
	queries       map[string]Query
	functions     map[string]Function
	codes         map[string]Code
	tables        map[string]Table // published decision tables, by name (ADR-0062)
}

// Attach is called by the host when a tenant is composed.
func (b *Build) Attach(h host.Host) { b.host = h }

// New is a tenant's builder app.
func New(tenant string) *Build {
	b := &Build{installed: map[string]platform.Entity{}, linkTypes: map[string]LinkType{}, propertyTypes: map[string]PropertyType{}, queries: map[string]Query{}, functions: map[string]Function{}, codes: map[string]Code{}, tables: map[string]Table{}}
	actions := append(platform.EntityActions(b.objectEntity()), platform.EntityActions(b.pageEntity())...)
	actions = append(actions, platform.EntityActions(b.applicationEntity())...)
	actions = append(actions, platform.EntityActions(b.testPlanEntity())...)
	actions = append(actions, platform.EntityActions(b.processEntity())...)
	actions = append(actions, platform.EntityActions(b.functionEntity())...)
	actions = append(actions, platform.EntityActions(b.linkTypeEntity())...)
	actions = append(actions, platform.EntityActions(b.propertyTypeEntity())...)
	actions = append(actions, platform.EntityActions(b.queryEntity())...)
	actions = append(actions, functionCallActions([]string{Builder, User})...)
	actions = append(actions, evaluationActions()...)
	actions = append(actions, platform.EntityActions(b.codeEntity())...)
	actions = append(actions, platform.EntityActions(b.sourceEntity())...)
	actions = append(actions, sourceActions()...)
	actions = append(actions, codeActions()...)
	b.ledger = platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), ObjectType, PageType, AppType, TestPlanType, ProcessType, FunctionType, PropertyTypeType, LinkTypeType, QueryType, FunctionCallType, EvaluationType, CodeType)
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
					if c.Staging() {
						if err := b.check(*object, object.ID); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
						if err := b.validateObjectInstallation(*object); err != nil {
							return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
						}
					} else if err := b.install(c, *object); err != nil {
						return err
					}
					object.Installed = TypeOf(object.Name) // what its records are kept as, for people to see
					object.Published = published(*object)
					return nil
				}}}}}
}

// releaseProfile is declared, not hidden: an operator sets it through the
// platform's settings surface (ADR-0048 D5b).
func (b *Build) settings() []platform.Setting {
	return []platform.Setting{{Name: SettingReleaseProfile, Title: "Release profile", Type: "choice",
		Choices: []string{ProfileProduction, ProfileDevelopment}, Default: ProfileDevelopment,
		Description: "production: definitions are delivered only through a saved release candidate; development: direct install stays available for authoring, import and probes."}}
}

func (b *Build) Manifest() platform.Manifest {
	entities := []platform.Entity{b.objectEntity(), b.pageEntity(), b.applicationEntity(), b.testPlanEntity(), b.processEntity(), b.propertyTypeEntity(), b.linkTypeEntity(), b.queryEntity(), b.functionEntity(), b.functionCallEntity(), b.evaluationEntity(), b.codeEntity(), b.sourceEntity(), b.tableEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	// Every role an object names is a role of this app, which the Console
	// grants — one that only reads grants no action (ADR-0037 D4). Builder and
	// Publisher are the two roles releases need (ADR-0047 §11).
	roles := []string{User, Builder, Publisher}
	for _, typ := range sortedTypes(b.installed) {
		for role := range b.installed[typ].Scope.Levels {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	slices.Sort(roles)
	return platform.Manifest{ID: ID, Title: "Builder", Version: definitionVersion, Actions: b.ledger.Catalog, Entities: entities, Roles: roles, Settings: b.settings(), Reads: []string{ReadReleaseProfile}, Queries: b.queryDeclarations(), Functions: b.functionDeclarations(), Operations: b.operationDeclarations(),
		Pages: []platform.Page{{Name: "objects", Title: "Objects", Description: "The objects this organisation defines. Publish one to install it.",
			Layout: "list-detail", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: ObjectType},
			ListFields:   []string{"title", "name", "state", "installed"},
			DetailFields: []string{"title", "name", "plural", "description", "fields", "state", "installed"},
			Actions: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".create"},
				{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: ObjectType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaPublish}}},
			{Name: "applications", Title: "Applications", Description: "The applications this organisation hands to its people. Each holds pages and appears in their launcher.",
				Layout: "list-detail", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: AppType},
				ListFields:   []string{"title", "name", "icon", "state"},
				DetailFields: []string{"title", "name", "description", "icon", "pages", "groups", "state"},
				Actions: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: AppType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: AppType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: AppType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaHandOver}}},
			{Name: "pages", Title: "Pages", Description: "The pages this organisation composes over the objects it may read. Publish one to put it in the workspace.",
				Layout: "list-detail", Object: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: PageType},
				ListFields:   []string{"title", "name", "object", "state"},
				DetailFields: []string{"title", "name", "description", "object", "list", "detail", "actions", "state"},
				Actions: []platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: PageType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: PageType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: PageType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaRelease}}}}}
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

// SettingReleaseProfile declares how definitions reach operators (ADR-0048
// D5b): a tenant that declares "production" delivers only through a saved joint
// candidate, and the direct install its authoring surfaces used to offer is
// refused by the owner. Any other value — including the default below — is the
// development/import/probe profile, where the same compile, install and
// recovery implementation stays available, exactly as before.
const SettingReleaseProfile = "releaseProfile"

// ReadReleaseProfile serves the profile to this app's authoring surfaces.
const ReadReleaseProfile = "release-profile"

const (
	// ProfileProduction delivers through saved release candidates only.
	ProfileProduction = "production"
	// ProfileDevelopment is authoring, import and probe: direct install stays.
	ProfileDevelopment = "development"
)

// directInstallSchemas are the builder's direct install entries: each installs a
// definition for operators at once, without a saved candidate.
var directInstallSchemas = []string{SchemaPublish, SchemaRelease, SchemaHandOver, SchemaProcess,
	SchemaPropertyType, SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish}

// checkReleaseProfile refuses a direct install that a production tenant has
// retired, naming the schema and the route that replaces it. Replay and
// recovery never take this path, so a historical Published image, its versions
// and a restore keep working in every profile.
func (b *Build) checkReleaseProfile(c platform.Caller, schema string) *kernel.Error {
	if c.Setting(SettingReleaseProfile) != ProfileProduction || !slices.Contains(directInstallSchemas, schema) {
		return nil
	}
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: fmt.Sprintf(
		"%s installs a definition for operators at once, and this tenant delivers through a saved release candidate: review the draft and activate the candidate (POST /v1/releases/candidates). Direct install is limited to the development and import profiles.", schema)}
}

func (b *Build) Declarations() []*pb.AuthorityDeclaration { return b.ledger.Declarations() }
func (b *Build) AcceptedLedger() *platform.Ledger         { return b.ledger }
func (*Build) AcceptedPublicationSchemas() []string {
	return []string{SchemaPublish, SchemaRelease, SchemaHandOver, SchemaProcess, SchemaPropertyType, SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish}
}

// publicationImage reads and verifies the already committed record image
// without rerunning builder action rules or publishing a second decision.
func (b *Build) publicationImage(schema string, image []byte) (platform.Entity, []platform.Action, []platform.Page, *platform.Application, error) {
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := wasPublished[Object](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name || record.Installed != TypeOf(published.Name) {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted object has no matching published definition")
		}
		entity := b.entity(published)
		actions := platform.EntityActions(entity)
		return entity, actions, []platform.Page{page(published)}, nil, nil
	case SchemaRelease:
		var record Page
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := wasPublished[Page](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted page has no published definition")
		}
		return platform.Entity{}, nil, []platform.Page{descriptor(published)}, nil, nil
	case SchemaHandOver:
		var record Application
		if err := json.Unmarshal(image, &record); err != nil {
			return platform.Entity{}, nil, nil, nil, err
		}
		published, ok := wasPublished[Application](record.Published)
		if !ok || record.State != "published" || record.ID != published.ID ||
			record.Name != published.Name {
			return platform.Entity{}, nil, nil, nil, fmt.Errorf("accepted application has no published definition")
		}
		app := applicationDescriptor(published)
		return platform.Entity{}, nil, nil, &app, nil
	}
	return platform.Entity{}, nil, nil, nil, fmt.Errorf("not an accepted builder publication: %s", schema)
}

func (b *Build) ValidateAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaPropertyType {
		l, err := propertyTypeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallPropertyType(l.definition())
	}
	if schema == SchemaLinkType {
		l, err := linkTypeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallLinkType(l.definition())
	}
	if schema == SchemaQuery {
		q, err := queryImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallQuery(q.definition())
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallOperation(code.definition())
	}
	if schema == SchemaFunction {
		f, err := functionImage(image)
		if err != nil {
			return err
		}
		return b.host.ValidateInstallFunction(f.definition())
	}
	if schema == SchemaProcess {
		p, err := processImage(image)
		if err != nil {
			return err
		}
		if b.host.Processes() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return b.host.Processes().Validate(b, b.flowOf(p))
	}
	_, _, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := wasPublished[Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		return b.validateObjectInstallation(published)
	case SchemaRelease:
		return b.host.ValidateInstallPage(pages[0])
	case SchemaHandOver:
		return b.host.ValidateInstallApplication(*app)
	}
	return fmt.Errorf("unknown builder publication: %s", schema)
}

// Installation reconstructs the runtime registry from the saved published
// image, not by running the publish transition or making another journal entry.
func (b *Build) ApplyAcceptedPublication(schema string, image []byte) error {
	if schema == SchemaPropertyType {
		l, err := propertyTypeImage(image)
		if err != nil {
			return err
		}
		var record PropertyType
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		for _, raw := range record.Versions {
			version, _ := wasPublished[PropertyType](raw)
			if err := b.installPropertyType(platform.Caller{Replaying: true}, version); err != nil {
				return err
			}
		}
		return b.installPropertyType(platform.Caller{Replaying: true}, l)
	}
	if schema == SchemaLinkType {
		l, err := linkTypeImage(image)
		if err != nil {
			return err
		}
		var record LinkType
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		for _, raw := range record.Versions {
			version, _ := wasPublished[LinkType](raw)
			if err := b.installLinkType(platform.Caller{Replaying: true}, version); err != nil {
				return err
			}
		}
		return b.installLinkType(platform.Caller{Replaying: true}, l)
	}
	if schema == SchemaQuery {
		q, err := queryImage(image)
		if err != nil {
			return err
		}
		var record Query
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		for _, raw := range record.Versions {
			version, _ := wasPublished[Query](raw)
			if err := b.installQuery(platform.Caller{Replaying: true}, version); err != nil {
				return err
			}
		}
		return b.installQuery(platform.Caller{Replaying: true}, q)
	}
	if schema == SchemaCodePublish {
		code, err := codeImage(image)
		if err != nil {
			return err
		}
		return b.installCode(platform.Caller{Replaying: true}, code)
	}
	if schema == SchemaFunction {
		f, err := functionImage(image)
		if err != nil {
			return err
		}
		return b.installFunction(platform.Caller{Replaying: true}, f)
	}
	if schema == SchemaProcess {
		p, err := processImage(image)
		if err != nil {
			return err
		}
		if b.host.Processes() == nil {
			return fmt.Errorf("tenant has no flow runtime")
		}
		return b.host.Processes().Install(b, b.flowOf(p))
	}
	entity, actions, pages, app, err := b.publicationImage(schema, image)
	if err != nil {
		return err
	}
	switch schema {
	case SchemaPublish:
		var record Object
		if err := json.Unmarshal(image, &record); err != nil {
			return err
		}
		published, ok := wasPublished[Object](record.Published)
		if !ok {
			return fmt.Errorf("accepted object has no published definition")
		}
		pages, _, err = b.objectInstallationPages(published)
		if err != nil {
			return err
		}
		if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
			return fmt.Errorf("extend accepted object: %w", err)
		}
		if err := b.host.Install(platform.Caller{Replaying: true}, entity, actions, pages...); err != nil {
			return fmt.Errorf("install accepted object: %w", err)
		}
		b.installed[entity.Type] = entity
	case SchemaRelease:
		if err := b.host.InstallPage(platform.Caller{Replaying: true}, pages[0]); err != nil {
			return fmt.Errorf("install accepted page: %w", err)
		}
	case SchemaHandOver:
		if err := b.host.InstallApplication(platform.Caller{Replaying: true}, *app); err != nil {
			return fmt.Errorf("install accepted application: %w", err)
		}
	}
	return nil
}
func (b *Build) Snapshot() (json.RawMessage, error) { return b.ledger.Snapshot() }
func (b *Build) Restore(raw json.RawMessage) error  { return b.ledger.Restore(raw) }

// Read release-profile answers how this tenant delivers definitions, for the
// authoring surfaces that must not offer an entry the owner would refuse
// (ADR-0048 D5b). Every role of this app may read it: it is a declaration of
// the tenant's profile, not a record.
func (b *Build) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name != ReadReleaseProfile {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	profile := c.Setting(SettingReleaseProfile)
	if profile != ProfileProduction {
		profile = ProfileDevelopment
	}
	return ReleaseProfile{Profile: profile, DirectInstall: profile != ProfileProduction}, nil
}

// ReleaseProfile is how a tenant delivers definitions to operators.
type ReleaseProfile struct {
	Profile       string `json:"profile"`
	DirectInstall bool   `json:"directInstall"`
}

func (b *Build) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// Draft archival never retires installed definitions or retained runtime versions.
func (b *Build) checkDefinitionArchive(c platform.Caller, typ, id string) *kernel.Error {
	if c.Role() != Builder {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	published := false
	switch typ {
	case ObjectType:
		record, _ := platform.Get[Object](c, id)
		published = record.Published != ""
	case PageType:
		record, _ := platform.Get[Page](c, id)
		published = record.Published != ""
	case AppType:
		record, _ := platform.Get[Application](c, id)
		published = record.Published != ""
	case ProcessType:
		record, _ := platform.Get[Process](c, id)
		published = record.Published != ""
	case PropertyTypeType:
		record, _ := platform.Get[PropertyType](c, id)
		published = record.Published != ""
	case LinkTypeType:
		record, _ := platform.Get[LinkType](c, id)
		published = record.Published != ""
	case QueryType:
		record, _ := platform.Get[Query](c, id)
		published = record.Published != ""
	case FunctionType:
		record, _ := platform.Get[Function](c, id)
		published = record.Published != ""
	case CodeType:
		record, _ := platform.Get[Code](c, id)
		published = record.Published != ""
		if record.State == "compiling" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A compiling code function cannot be archived; wait for its build result")
		}
	}
	if !published {
		return nil
	}
	if typ == ObjectType {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published object cannot be archived; its installed definition must be retained")
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be archived; its installed version must be retained")
}

// Submit takes the builder's own actions and those generated for every object
// it has installed: a defined object's records are decided like any other's.
func (b *Build) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if !c.Replaying {
		if err := b.checkReleaseProfile(c, s.GetSchema().GetName()); err != nil {
			return nil, err
		}
	}
	if s.GetSchema().GetName() == SchemaCodeCompile {
		return b.submitCodeCompile(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaCodeCompiled {
		return b.submitCodeCompiled(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaSourcePulled {
		return b.submitSourcePulled(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == SchemaFunctionCall || name == SchemaFunctionAnswer {
		return b.submitFunctionCall(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == SchemaEvaluationStart || name == SchemaEvaluationAnswer {
		return b.submitEvaluation(c, s, now)
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive && !c.Replaying && slices.Contains([]string{ObjectType, PageType, AppType, ProcessType, PropertyTypeType, LinkTypeType, QueryType, FunctionType, CodeType}, typ) {
		if err := b.checkDefinitionArchive(c, typ, s.GetTarget().GetId()); err != nil {
			return nil, err
		}
	}
	if name := s.GetSchema().GetName(); (name == TestPlanType+".create" || name == TestPlanType+".edit") && !c.Replaying {
		if err := b.checkTestPlan(c, s); err != nil {
			return nil, err
		}
	}
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
				err = b.checkPropertyFields(*p.Fields)
				if err == nil {
					err = checkFields(*p.Fields, b.fieldKnown)
				}
			}
			if err != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
			}
		}
	}
	if name := s.GetSchema().GetName(); (name == PageType+".create" || name == PageType+".edit") && !c.Replaying {
		var p Page
		if json.Unmarshal(s.GetPayload(), &p) == nil && p.Object != "" {
			if !b.fieldKnown(p.Object) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: fmt.Sprintf("this tenant has no object %q", p.Object)}
			}
		}
	}
	entities := []platform.Entity{b.objectEntity(), b.pageEntity(), b.applicationEntity(), b.testPlanEntity(), b.processEntity(), b.propertyTypeEntity(), b.linkTypeEntity(), b.queryEntity(), b.functionEntity(), b.functionCallEntity(), b.evaluationEntity(), b.codeEntity(), b.sourceEntity(), b.tableEntity()}
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
func (b *Build) conditionLookup(typ string) (platform.EntityInfo, bool) {
	if b.host == nil {
		return platform.EntityInfo{}, false
	}
	return b.lookupEntity(typ)
}

func (b *Build) check(o Object, id string) error {
	if err := b.checkName(o.Name, id); err != nil {
		return err
	}
	if err := b.checkPropertyFields(o.Fields); err != nil {
		return err
	}
	if err := checkFields(o.Fields, b.fieldKnown); err != nil {
		return err
	}
	if err := checkProcess(o, b.conditionLookup); err != nil {
		return err
	}
	if err := b.checkCreates(o); err != nil {
		return err
	}
	if err := b.checkPosts(o); err != nil {
		return err
	}
	if err := b.checkShape(o); err != nil {
		return err
	}
	return checkAccess(o)
}

// BaseField is the reference an extension object carries to the record it extends.
const BaseField = "base"

// checkShape refuses an interface the tenant's apps do not declare or whose
// fields the object lacks, and an extension of a type that is not there, of
// itself, with a lifecycle of its own, or whose base field is not the base.
func (b *Build) checkShape(o Object) error {
	if len(o.Implements) > 0 {
		var declared []platform.Interface
		if b.host != nil {
			declared = b.host.Interfaces()
		}
		info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
		if err != nil {
			return err
		}
		for k, name := range o.Implements {
			if slices.Contains(o.Implements[:k], name) {
				return fmt.Errorf("the interface %s is listed twice", name)
			}
			i := slices.IndexFunc(declared, func(i platform.Interface) bool { return i.Name == name })
			if i < 0 {
				return fmt.Errorf("no app declares the interface %s", name)
			}
			if err := declared[i].Implements(info); err != nil {
				return err
			}
		}
	}
	if o.Extends != "" {
		if o.Extends == TypeOf(o.Name) || !b.fieldKnown(o.Extends) {
			return fmt.Errorf("the object cannot extend %s: not an object type this tenant has", o.Extends)
		}
		if len(o.States) > 0 || len(o.Actions) > 0 {
			return fmt.Errorf("an extension adds fields to %s; its lifecycle stays the base type's", o.Extends)
		}
		k := slices.IndexFunc(o.Fields, func(f Field) bool { return f.Name == BaseField })
		if k < 0 || o.Fields[k].Type != "reference" || o.Fields[k].Ref != o.Extends || !o.Fields[k].Required {
			return fmt.Errorf("an extension of %s needs a required reference field %q pointing at it", o.Extends, BaseField)
		}
	}
	return nil
}

// checkName refuses a name that is not a name, or one already taken.
func (b *Build) checkName(name, id string) error {
	if !named(name) {
		return fmt.Errorf("the name %q is not lower-case letters and digits", name)
	}
	other, taken, err := b.taken(name, id)
	if err != nil {
		return err
	}
	if taken {
		return fmt.Errorf("the name %q is already %s", name, other)
	}
	return nil
}

// fieldKnown answers whether a reference names an object this tenant can
// compose against: an installed one, one selected as a draft in the candidate
// being built, or another saved draft object (ADR-0048 D1). Delivery still
// proves the object installs.
func (b *Build) fieldKnown(ref string) bool {
	if _, known := b.lookupEntity(ref); known {
		return true
	}
	if b.host == nil {
		return false
	}
	if b.host.Declares(ref) {
		return true
	}
	name, derived := strings.CutPrefix(ref, ID+".")
	if !derived {
		return false
	}
	objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return false
	}
	return slices.ContainsFunc(objects, func(o Object) bool { return !o.Archived && o.Name == name })
}

// checkFields refuses a field the platform has no type for, a choice without
// values, or a reference to an object this tenant has not.
func checkFields(fields []Field, known func(string) bool) error {
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
		case f.Type == "reference" && known != nil && !known(f.Ref):
			return fmt.Errorf("the field %q refers to %q, which this tenant has no object for", f.Name, f.Ref)
		case f.Inverse != "" && f.Type != "reference":
			return fmt.Errorf("the field %q names how it is seen from elsewhere but refers to nothing", f.Name)
		case f.Inverse != "" && !named(f.Inverse):
			return fmt.Errorf("the relation name %q of field %q is not lower-case letters and digits", f.Inverse, f.Name)
		}
		seen[f.Name] = true
	}
	return nil
}

var fieldTypes = []string{"text", "longtext", "integer", "decimal", "money", "date", "datetime", "boolean", "choice", "reference"}

// taken says whether another object, or an app's own type, holds the name.
func (b *Build) taken(name, id string) (string, bool, error) {
	if b.host == nil {
		return "", false, nil
	}
	if _, mine := b.installed[TypeOf(name)]; !mine && b.host.Declares(TypeOf(name)) {
		return "an app's own type", true, nil
	}
	objects, err := readDefinitionInventory[Object](b.host.Automation(platform.Caller{}, ID))
	if err != nil {
		return "", false, err
	}
	for _, o := range objects {
		if o.Name == name && o.ID != id && !o.Archived {
			return "the object " + o.Title, true, nil
		}
	}
	return "", false, nil
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
func (b *Build) install(c platform.Caller, o Object) *kernel.Error {
	if c.Staging() {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	// A restore installs definitions that were already accepted, in ID order;
	// objects that refer to each other (a visit creating follow-ups that refer
	// back) cannot each pass the draft checks before the other is installed.
	if err := b.check(o, o.ID); err != nil && !c.Replaying {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	// The in-memory development path has no accepted-result staging. Validate
	// before extending the ledger, or a refused publication leaves new
	// authority/actions in the running tenant.
	if err := b.validateObjectInstallation(o); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	entity := b.entity(o)
	actions := platform.EntityActions(entity)
	pages, _, err := b.objectInstallationPages(o)
	if err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.ledger.Extend([]string{entity.Type}, actions); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	if err := b.host.Install(c, entity, actions, pages...); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	b.installed[entity.Type] = entity
	return nil
}

// published is a definition as it was published: what is installed, kept on the
// record so a restore installs that and not a draft written since (ADR-0034 D3).
func published[T any](definition T) string {
	// A published definition is one version, not a recursive copy of every
	// preceding publication. Keeping the old Published string inside the new
	// image would double its size at each edit and exhaust the bounded result.
	switch value := any(definition).(type) {
	case Object:
		value.Published = ""
		definition = any(value).(T)
	case Page:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Application:
		value.Published = ""
		definition = any(value).(T)
	case PropertyType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case LinkType:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Query:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Function:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Code:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	case Process:
		value.Published = ""
		value.Versions = nil
		definition = any(value).(T)
	}
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
	props, problem := b.propertyTypeInventory()
	if problem != nil {
		return problem
	}
	for _, record := range props {
		if record.Published != "" {
			raw, _ := json.Marshal(record)
			if err := b.ApplyAcceptedPublication(SchemaPropertyType, raw); err != nil {
				return err
			}
		}
	}

	objects, pages, applications, err := b.releaseInventory()
	if err != nil {
		return err
	}
	for _, o := range objects {
		was, ok := wasPublished[Object](o.Published)
		if !ok || o.Archived {
			continue
		}
		if err := b.install(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("object %s: %v", o.Name, err)
		}
	}
	links, err := b.linkTypeInventory()
	if err != nil {
		return err
	}
	for _, record := range links {
		if record.Published != "" {
			raw, _ := json.Marshal(record)
			if err := b.ApplyAcceptedPublication(SchemaLinkType, raw); err != nil {
				return err
			}
		}
	}
	queries, err := b.queryInventory()
	if err != nil {
		return err
	}
	for _, record := range queries {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		if err := b.ApplyAcceptedPublication(SchemaQuery, raw); err != nil {
			return err
		}
	}
	functions, err := b.functionInventory()
	if err != nil {
		return err
	}
	for _, record := range functions {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		f, err := functionImage(raw)
		if err != nil {
			return err
		}
		if err := b.installFunction(platform.Caller{Replaying: true}, f); err != nil {
			return err
		}
	}
	codes, err := b.codeInventory()
	if err != nil {
		return err
	}
	for _, record := range codes {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		code, err := codeImage(raw)
		if err != nil {
			return err
		}
		if err := b.installCode(platform.Caller{Replaying: true}, code); err != nil {
			return err
		}
	}
	for _, p := range pages { // after the objects they show
		was, ok := wasPublished[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.release(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("page %s: %v", p.Name, err)
		}
	}
	// Navigation may be reciprocal or refer to a later page ID. All original
	// descriptors must exist before the same strict live target/interface check.
	for _, p := range pages {
		was, ok := wasPublished[Page](p.Published)
		if !ok || p.Archived {
			continue
		}
		if err := b.host.ValidateInstallPage(descriptor(was)); err != nil {
			return fmt.Errorf("page %s navigation: %w", p.Name, err)
		}
	}
	if err := b.installProcesses(); err != nil {
		return err
	}
	if err := b.installTables(); err != nil {
		return err
	}
	for _, a := range applications { // after the pages they hold
		was, ok := wasPublished[Application](a.Published)
		if !ok || a.Archived {
			continue
		}
		if err := b.hand(platform.Caller{Replaying: true}, was); err != nil {
			return fmt.Errorf("application %s: %v", a.Name, err)
		}
	}
	return nil
}

// Entity is the declaration a defined object amounts to: a Go type built now,
// with the tags a developer would have written (ADR-0034 D1).
func Entity(o Object) platform.Entity { return entityWith(o, nil, nil) }

// entity is the declaration as installed: its actions may create related
// records through this builder's host (ADR-0040 21c).
func (b *Build) entity(o Object) platform.Entity { return entityWith(o, b, b.conditionLookup) }

func entityWith(o Object, creates creator, lookup func(string) (platform.EntityInfo, bool)) platform.Entity {
	// StructOf interns identical shapes. The entity identity keeps two named
	// objects distinct, just as two named Go record types are distinct; this
	// tag adds no JSON field and stays stable across draft/publication/restore.
	fields := []reflect.StructField{{Name: "Record", Type: reflect.TypeFor[platform.Record](), Anonymous: true,
		Tag: reflect.StructTag(fmt.Sprintf(`entity:"%s"`, TypeOf(o.Name)))}}
	for _, f := range o.Fields {
		tag := fmt.Sprintf(`json:"%s,omitempty" title:"%s"`, f.Name, f.Title)
		switch f.Type {
		case "longtext", "date", "datetime":
			tag += fmt.Sprintf(` type:"%s"`, f.Type)
		case "choice":
			tag += fmt.Sprintf(` choices:"%s"`, strings.Join(choices(f.Choices), ","))
		case "reference":
			tag += fmt.Sprintf(` ref:"%s"`, f.Ref)
			if f.Inverse != "" {
				tag += fmt.Sprintf(` inverse:"%s"`, f.Inverse)
			}
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
		if len(f.Read) > 0 { // the builder always reads and sets what it builds
			tag += fmt.Sprintf(` read:"%s"`, strings.Join(append([]string{Builder}, f.Read...), ","))
		}
		if len(f.Write) > 0 {
			tag += fmt.Sprintf(` write:"%s"`, strings.Join(append([]string{Builder}, f.Write...), ","))
		}
		fieldType := goType(f.Type)
		// Optional scalar values need a nil state. Otherwise an omitted number
		// or boolean is indistinguishable from an explicitly supplied zero.
		if !f.Required && (f.Type == "integer" || f.Type == "decimal" || f.Type == "boolean" || f.Type == "datetime" || f.Type == "money") {
			fieldType = reflect.PointerTo(fieldType)
		}
		fields = append(fields, reflect.StructField{Name: goName(f.Name), Type: fieldType, Tag: reflect.StructTag(tag)})
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
	std, scope, roles := access(o)
	return platform.Entity{Type: TypeOf(o.Name), Title: o.Title, Plural: o.Plural, Description: o.Description, Model: model, Display: display,
		Standard: std, Scope: scope, Lifecycle: lifecycle(o, roles, creates, lookup), PropertyBindings: propertyBindings(o.Fields), Implements: slices.Clone(o.Implements)}
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
