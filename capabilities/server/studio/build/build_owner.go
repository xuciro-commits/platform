package build

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/internal/systemscope"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

func (b *Build) SavedObjectActions(c platform.Caller) ([]platform.Action, error) {
	objects, err := readDefinitionInventory[Object](c)
	if err != nil {
		return nil, err
	}
	actions := []platform.Action{}
	for _, object := range objects {
		if !object.Archived {
			actions = append(actions, objectActions(object, Entity(object))...)
		}
	}
	return actions, nil
}

type Build struct {
	installationCatalog *platform.Catalog
	releaseDraftIDs     map[platform.AssetRef]string
	host                platform.BuildHost
	ledger              *platform.Ledger
	installed           map[string]platform.Entity
	jointEntities       map[string]platform.EntityInfo
	jointAssets         map[platform.AssetRef]bool
	jointDeclarations   map[platform.AssetRef]platform.ReleaseAsset
	jointMode           bool
	linkTypes           map[string]LinkType
	propertyTypes       map[string]PropertyType
	valueTypes          map[string]ValueType
	queries             map[string]Query
	functions           map[string]Function
	agents              map[string]Agent
	codes               map[string]Code
	tables              map[string]Table
}

func (b *Build) AttachBuild(h platform.BuildHost) { b.host = h }

func New(tenant string) *Build {
	b := &Build{installed: map[string]platform.Entity{}, linkTypes: map[string]LinkType{}, propertyTypes: map[string]PropertyType{}, valueTypes: map[string]ValueType{}, queries: map[string]Query{}, functions: map[string]Function{}, agents: map[string]Agent{}, codes: map[string]Code{}, tables: map[string]Table{}}
	actions := append(platform.EntityActions(b.objectEntity()), platform.EntityActions(b.pageEntity())...)
	actions = append(actions, platform.EntityActions(b.applicationEntity())...)
	actions = append(actions, platform.EntityActions(b.testPlanEntity())...)
	actions = append(actions, platform.EntityActions(b.processEntity())...)
	actions = append(actions, platform.EntityActions(functionKind.entity(b))...)
	actions = append(actions, platform.EntityActions(linkKind.entity(b))...)
	actions = append(actions, platform.Action{Schema: LinkTypeType + ".migrate", Target: LinkTypeType, Title: "Migrate relationships", Description: "Move historical links into the endpoint owner's accepted records", Capability: LinkTypeType, Roles: []string{Builder}, Payload: []platform.Field{}})
	actions = append(actions, platform.EntityActions(propertyKind.entity(b))...)
	actions = append(actions, platform.EntityActions(valueKind.entity(b))...)
	actions = append(actions, platform.EntityActions(queryKind.entity(b))...)
	actions = append(actions, functionCallActions([]string{Builder, User})...)
	actions = append(actions, evaluationActions()...)
	actions = append(actions, platform.EntityActions(b.codeEntity())...)
	actions = append(actions, platform.EntityActions(b.sourceEntity())...)
	actions = append(actions, platform.EntityActions(b.connectionEntity())...)
	actions = append(actions, platform.EntityActions(b.datasetEntity())...)
	actions = append(actions, platform.EntityActions(b.datasetVersionEntity())...)
	actions = append(actions, platform.EntityActions(b.pipelineEntity())...)
	actions = append(actions, platform.EntityActions(b.writebackEntity())...)
	actions = append(actions, platform.EntityActions(b.matchEntity())...)
	actions = append(actions, writebackActions()...)
	actions = append(actions, confirmationActions()...)
	actions = append(actions, dispatchActions()...)
	actions = append(actions, datasetActions()...)
	actions = append(actions, pipelineActions()...)
	actions = append(actions, platform.EntityActions(b.tableEntity())...)
	actions = append(actions, platform.EntityActions(b.agentEntity())...)
	actions = append(actions, platform.EntityActions(b.alertEntity())...)
	actions = append(actions, sourceActions()...)
	actions = append(actions, platform.EntityActions(b.sampleSetEntity())...)
	actions = append(actions, platform.EntityActions(b.observationEntity())...)
	actions = append(actions, connectionActions()...)
	actions = append(actions, codeActions()...)
	actions = append(actions, platform.Action{Schema: SchemaEvaluationSetPublish, Target: TestPlanType, Title: "Publish test plan", Description: "Freeze a portable business scenario or the evaluation cases for one function version.", Capability: "objects", Roles: []string{Builder}, Payload: []platform.Field{}})
	b.ledger = platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), ObjectType, PageType, AppType, TestPlanType, ProcessType, FunctionType, PropertyTypeType, ValueTypeType, LinkTypeType, QueryType, FunctionCallType, EvaluationType, CodeType, SourceType, ConnectionType, DatasetType, DatasetVersionType, PipelineType, WritebackType, MatchType, TableType, AgentType, AlertType, SampleSetType, ObservationType, platform.OwnedRelationType(ID), confirmationType, platform.DispatchType)
	return b
}

func (b *Build) settings() []platform.Setting {
	return []platform.Setting{{Name: SettingReleaseProfile, Title: "Release profile", Type: "choice",
		Choices: []string{ProfileProduction, ProfileDevelopment}, Default: ProfileDevelopment,
		Description: "production: definitions are delivered only through a saved release candidate; development: direct install stays available for authoring, import and probes."}}
}

func (b *Build) manifestRoles() []string {
	roles := []string{User, Builder, Publisher, Integrator}
	for _, typ := range sortedTypes(b.installed) {
		for role := range b.installed[typ].Scope.Levels {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	slices.Sort(roles)
	return roles
}

func (b *Build) Manifest() platform.Manifest {
	catalog := b.ledger.Catalog
	if b.installationCatalog != nil {
		catalog = b.installationCatalog
	}
	entities := []platform.Entity{platform.RelationEntity(ID), b.objectEntity(), b.pageEntity(), b.applicationEntity(), b.testPlanEntity(), b.processEntity(), propertyKind.entity(b), valueKind.entity(b), linkKind.entity(b), queryKind.entity(b), functionKind.entity(b), b.functionCallEntity(), b.evaluationEntity(), b.agentEntity(), b.alertEntity(), b.codeEntity(), b.sourceEntity(), b.connectionEntity(), b.datasetEntity(), b.datasetVersionEntity(), b.pipelineEntity(), b.writebackEntity(), b.matchEntity(), b.tableEntity(), b.sampleSetEntity(), b.observationEntity(), b.confirmationEntity(), b.dispatchEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	provisions, reads := b.objectProtocols()
	return platform.Manifest{ID: ID, Title: "Builder", Version: ValueDefinitionVersion, Sequences: b.transactionSequences(), Actions: catalog, Entities: entities, Roles: b.manifestRoles(), Settings: b.settings(), Jobs: []platform.Job{{Name: "confirmations", Title: "Advance business confirmations", Every: time.Second}, {Name: "dispatches", Title: "Advance device dispatches", Every: time.Second}}, Provides: provisions, Consumes: b.consumedProtocols(), Reads: append([]string{ReadReleaseProfile}, reads...), ReadSources: protocolReadSources(reads), Queries: definitionDeclarations(b.queries), Functions: definitionDeclarations(b.functions), Agents: b.agentDeclarations(), Operations: b.operationDeclarations(),
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

func sortedTypes(installed map[string]platform.Entity) []string {
	out := make([]string, 0, len(installed))
	for typ := range installed {
		out = append(out, typ)
	}
	slices.Sort(out)
	return out
}

const SettingReleaseProfile = "releaseProfile"

const ReadReleaseProfile = "release-profile"

const (
	ProfileProduction  = "production"
	ProfileDevelopment = "development"
)

var directInstallSchemas = []string{SchemaPublish, SchemaRelease, SchemaHandOver, SchemaProcess,
	SchemaPropertyType, SchemaValueType, SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish,
	SchemaTablePublish, SchemaAlertPublish, SchemaAgentPublish,
	SourceType + ".publish", PipelineType + ".publish", WritebackType + ".publish", MatchType + ".publish"}

func (b *Build) checkReleaseProfile(c platform.Caller, schema string) *kernel.Error {
	if c.Tenant == systemscope.Tenant && slices.Contains(directInstallSchemas, schema) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System definitions are published through an approved Platform change")
	}
	if c.Setting(SettingReleaseProfile) != ProfileProduction || !slices.Contains(directInstallSchemas, schema) {
		return nil
	}
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: fmt.Sprintf(
		"%s installs a definition for operators at once, and this tenant delivers through a saved release candidate: review the draft and activate the candidate (POST /v1/releases/candidates). Direct install is limited to the development and import profiles.", schema)}
}

func (b *Build) Declarations() []*pb.AuthorityDeclaration { return b.ledger.Declarations() }
func (b *Build) AcceptedLedger() *platform.Ledger         { return b.ledger }
func (b *Build) SnapshotFormat() string                   { return "platform.ledger.v1" }

func (b *Build) Snapshot() (json.RawMessage, error) { return b.ledger.Snapshot() }
func (b *Build) Restore(raw json.RawMessage) error  { return b.ledger.Restore(raw) }

func (b *Build) Read(c platform.Caller, name string) (any, *kernel.Error) {
	if name != ReadReleaseProfile {
		return b.readProtocol(c, name)
	}
	profile := c.Setting(SettingReleaseProfile)
	if profile != ProfileProduction {
		profile = ProfileDevelopment
	}
	return ReleaseProfile{Profile: profile, DirectInstall: profile != ProfileProduction}, nil
}

type ReleaseProfile struct {
	Profile       string `json:"profile"`
	DirectInstall bool   `json:"directInstall"`
}

func (b *Build) checkDefinitionArchive(c platform.Caller, typ, id string) *kernel.Error {
	if !c.Holds(c.App, Builder) && !(typ == ObservationType && c.Holds(c.App, Integrator)) {
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
	case ValueTypeType:
		record, _ := platform.Get[ValueType](c, id)
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
	case TableType:
		record, _ := platform.Get[Table](c, id)
		published = record.Published != ""
	case AlertType:
		record, _ := platform.Get[Alert](c, id)
		published = record.Published != "" || legacyAlert(record)
	case AgentType:
		record, _ := platform.Get[Agent](c, id)
		published = record.Published != ""
	case ObservationType:
		record, _ := platform.Get[Observation](c, id)
		published = record.Published != ""
	case SampleSetType:
		record, _ := platform.Get[SampleSetRecord](c, id)
		published = record.Published != ""
	case TestPlanType:
		record, _ := platform.Get[TestPlan](c, id)
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

func (b *Build) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if !c.Replaying {
		if err := b.checkSystemDefinitionWrite(s); err != nil {
			return nil, err
		}
	}
	if s.GetSchema().GetName() == LinkTypeType+".migrate" {
		return b.migrateLinkType(c, s, now)
	}
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
	if s.GetSchema().GetName() == SchemaConnectionChecked {
		return b.submitConnectionChecked(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaDatasetLoad {
		return b.submitDatasetLoad(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaPipelineRan {
		return b.submitPipelineRan(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaWritebackAnswered {
		return b.submitWritebackAnswered(c, s, now)
	}
	if slices.Contains([]string{confirmationAnswer, confirmationReview, confirmationTick}, s.GetSchema().GetName()) {
		return b.updateConfirmation(c, s, now)
	}
	if slices.Contains([]string{platform.DispatchAnswerSchema, platform.DispatchReviewSchema, platform.DispatchTickSchema}, s.GetSchema().GetName()) {
		return b.updateDispatch(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaProposalReview {
		return b.submitProposalReview(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == SchemaFunctionCall || name == SchemaFunctionAnswer {
		return b.submitFunctionCall(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaEvaluationSetPublish {
		return b.submitTestPlan(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == SchemaEvaluationStart || name == SchemaEvaluationAnswer {
		return b.submitEvaluation(c, s, now)
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive && !c.Replaying && slices.Contains([]string{ObjectType, PageType, AppType, ProcessType, PropertyTypeType, ValueTypeType, LinkTypeType, QueryType, FunctionType, CodeType, TableType, AlertType, AgentType, TestPlanType, SampleSetType, ObservationType}, typ) {
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
					err = b.checkValueTypeFields(*p.Fields)
				}
				if err == nil {
					err = CheckFields(*p.Fields, b.fieldKnown)
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
	pageEntity := b.pageEntity()
	if name := s.GetSchema().GetName(); name == PageType+".create" || name == PageType+".edit" {
		pageEntity = b.pageDraftEntity()
	}
	agentEntity := b.agentEntity()
	if name := s.GetSchema().GetName(); name == AgentType+".create" || name == AgentType+".edit" {
		agentEntity = b.agentDraftEntity()
	}
	entities := []platform.Entity{b.objectEntity(), pageEntity, b.applicationEntity(), b.testPlanEntity(), b.processEntity(), propertyKind.entity(b), valueKind.entity(b), linkKind.entity(b), queryKind.entity(b), functionKind.entity(b), b.functionCallEntity(), b.evaluationEntity(), agentEntity, b.alertEntity(), b.codeEntity(), b.sourceEntity(), b.connectionEntity(), b.datasetEntity(), b.datasetVersionEntity(), b.pipelineEntity(), b.writebackEntity(), b.matchEntity(), b.tableEntity(), b.sampleSetEntity(), b.observationEntity(), b.confirmationEntity(), b.dispatchEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	if s.GetSchema().GetName() == AppType+".withdraw" || s.GetSchema().GetName() == AppType+".restore" {
		for i := range entities {
			if entities[i].Type == AppType {
				entities[i].Validate = nil
			}
		}
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive {
		if _, err := draftAssetRef(typ, ""); err == nil {
			for i := range entities {
				if entities[i].Type == typ {
					entities[i].Validate = nil
				}
			}
		}
	}
	if record, err, ok := b.ledger.Generated(c, s, now, nil, entities...); ok {
		return record, err
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (b *Build) ForkDecision() (platform.App, error) {
	catalog := b.installationCatalog
	if catalog != nil {
		catalog = platform.NewCatalog(catalog.All()...)
	}
	return &Build{host: b.host, ledger: b.ledger,
		installationCatalog: catalog,
		releaseDraftIDs:     maps.Clone(b.releaseDraftIDs), installed: maps.Clone(b.installed), jointEntities: maps.Clone(b.jointEntities), jointAssets: maps.Clone(b.jointAssets), jointDeclarations: maps.Clone(b.jointDeclarations), jointMode: b.jointMode,
		linkTypes: maps.Clone(b.linkTypes), propertyTypes: maps.Clone(b.propertyTypes), valueTypes: maps.Clone(b.valueTypes),
		queries: maps.Clone(b.queries), functions: maps.Clone(b.functions), agents: maps.Clone(b.agents),
		codes: maps.Clone(b.codes), tables: maps.Clone(b.tables)}, nil
}

var _ platform.DecisionForkApp = (*Build)(nil)

func (b *Build) NewSimulationApp(tenant string) platform.App { return New(tenant) }
func (b *Build) CandidateActions(asset platform.ReleaseAsset) ([]platform.Action, error) {
	code, err := b.retainedCodeObject(asset)
	if err != nil {
		return nil, err
	}
	if code {
		var actions []platform.Action
		for _, action := range b.Manifest().Actions.All() {
			if action.Target == asset.Ref.Name {
				actions = append(actions, action)
			}
		}
		return actions, nil
	}
	var object Object
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject || json.Unmarshal(asset.Body, &object) != nil || TypeOf(object.Name) != asset.Ref.Name {
		return nil, fmt.Errorf("invalid construction object descriptor")
	}
	return objectActions(object, Entity(object)), nil
}

var _ ConstructionDrafts = (*Build)(nil)
var _ IntegrationDefinitions = (*Build)(nil)
var _ ConstructionSandbox = (*Build)(nil)

const (
	ID                     = "build"
	ValueDefinitionVersion = "1"
	ObjectType             = "build.object"
	Builder                = "builder"
	Publisher              = "publisher"
	User                   = "user"
	SchemaPublish          = ObjectType + ".publish"
)

type Object struct {
	platform.Record
	Name              string                       `json:"name" field:"required,search" help:"Its name in the platform, lower-case letters and digits" example:"visit"`
	Title             string                       `json:"title" field:"required,search" title:"What people call it" example:"Visit"`
	Plural            string                       `json:"plural,omitempty" title:"What people call several" example:"Visits"`
	Icon              string                       `json:"icon,omitempty" title:"Icon" help:"An icon name, lower-case with hyphens" example:"calendar"`
	Color             string                       `json:"color,omitempty" title:"Color" choices:"neutral,info,success,warning,danger,purple,pink,teal,orange"`
	Display           string                       `json:"display,omitempty" title:"Title field" help:"The text field that names one record; empty: the first searchable field" example:"name"`
	Description       string                       `json:"description,omitempty" type:"longtext" help:"What one record of it is, for people and agents"`
	Fields            []Field                      `json:"fields" title:"Fields"`
	DataSources       []platform.DataSourceBinding `json:"datasources,omitempty" type:"json" field:"aside" title:"Data sources"`
	Protocols         []platform.ProtocolBinding   `json:"protocols,omitempty" type:"json" field:"aside" title:"Protocol bindings"`
	Reads             *platform.RuleReadSpec       `json:"reads,omitempty" type:"json" field:"aside"`
	Constraints       []platform.Constraint        `json:"constraints,omitempty" type:"json" field:"aside" title:"Business constraints"`
	Sets              []platform.RecordSet         `json:"sets,omitempty" type:"json" field:"aside" title:"Declared record sets"`
	DispatchCommands  []platform.DispatchCommand   `json:"dispatchCommands,omitempty" type:"json" field:"aside" title:"Device commands"`
	CreateTransaction *platform.BusinessTxn        `json:"createTransaction,omitempty" type:"json" field:"aside" title:"Creation transaction"`
	Versioning        *platform.Versioning         `json:"versioning,omitempty" type:"json" field:"aside" title:"Business versioning"`
	VersionPins       []platform.VersionPinBinding `json:"versionPins,omitempty" type:"json" field:"aside" title:"Pinned business versions"`
	EnterpriseUse     *platform.Expr               `json:"enterpriseUse,omitempty" type:"json" field:"aside" title:"Current enterprise use"`
	States            []State                      `json:"states,omitempty" field:"aside" title:"States"`
	Actions           []Action                     `json:"actions,omitempty" field:"aside" title:"Actions"`
	Access            []Access                     `json:"access,omitempty" field:"aside" title:"Access"`
	Scope             *ObjectScope                 `json:"scope,omitempty" type:"json" field:"aside" title:"Scope fields"`
	Implements        []string                     `json:"implements,omitempty" field:"aside" title:"Implements" help:"Interfaces whose fields this object has, e.g. core.coded"`
	Extends           string                       `json:"extends,omitempty" field:"aside" title:"Extends" help:"The installed object type this object adds fields to, e.g. core.person; records pair one to one through the base field"`
	ExtensionSource   *platform.ExtensionSource    `json:"extensionSource,omitempty" field:"readonly,aside" type:"json" title:"Extension source" help:"The original base package version and field shape captured at publication"`
	SchemaVersions    []platform.SchemaVersion     `json:"schemaVersions,omitempty" field:"readonly,aside" type:"json" title:"Schema versions" help:"Retained content versions and compatible schema families captured at publication"`
	Numbering         *Numbering                   `json:"numbering,omitempty" field:"aside" type:"json" title:"Document numbering"`
	State             string                       `json:"state" field:"readonly" choices:"draft,published"`
	Installed         string                       `json:"installed,omitempty" field:"readonly" title:"Installed as" help:"The type records of it are stored under"`
	Published         string                       `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

type Field struct {
	AIInput     *bool                  `json:"aiInput,omitempty" title:"Allow AI context"`
	SourceType  string                 `json:"sourceType,omitempty" choices:"record,user-input,external-input,generated" title:"Text source type"`
	Property    *platform.AssetBinding `json:"property,omitempty" type:"json" field:"aside"`
	ValueType   *platform.AssetBinding `json:"valueType,omitempty" type:"json" field:"aside"`
	Format      []platform.FormatRule  `json:"format,omitempty" type:"json" field:"aside"`
	Name        string                 `json:"name" field:"required" help:"Lower-case letters and digits" example:"visited"`
	Title       string                 `json:"title" field:"required" title:"Label" example:"Visited on"`
	Type        string                 `json:"type" field:"required" choices:"text,longtext,integer,decimal,money,date,datetime,boolean,choice,reference" example:"date"`
	Choices     string                 `json:"choices,omitempty" help:"For a choice: the values, comma-separated" example:"open,done"`
	Ref         string                 `json:"ref,omitempty" title:"Refers to" help:"For a reference: the object it points at" example:"core.partner"`
	Stereotype  string                 `json:"stereotype,omitempty" title:"Enterprise element type"`
	Stereotypes []string               `json:"stereotypes,omitempty" title:"Enterprise element types"`
	Inverse     string                 `json:"inverse,omitempty" title:"Seen from there as" help:"For a reference: what the referenced record calls these records" example:"visits"`
	Required    bool                   `json:"required,omitempty"`
	Search      bool                   `json:"search,omitempty" title:"Searchable"`
	ReadOnly    bool                   `json:"readOnly,omitempty" title:"Read only"`
	AliasOf     string                 `json:"aliasOf,omitempty" title:"Read alias of" help:"The retained old name reads this field; it cannot be written directly"`
	When        string                 `json:"when,omitempty" title:"Only when" help:"field=value,value — another choice or boolean field of this object and the values that make this field apply" example:"kind=refund,return"`
	FormulaExpr *platform.Expr         `json:"formulaExpr,omitempty" type:"json" field:"aside"`
	Formula     string                 `json:"formula,omitempty" help:"For a number: price * qty, (ordered - received), …" example:"price * qty"`
	Read        []string               `json:"read,omitempty" title:"Read by" help:"Roles that read it; empty: every role that reads the object"`
	Write       []string               `json:"write,omitempty" title:"Set by" help:"Roles that set it; empty: every role that edits the object"`
}

func TypeOf(name string) string { return ID + "." + name }

func ValueChoices(s string) []string {
	var out []string
	for _, c := range strings.Split(s, ",") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func CheckManifest(m platform.Manifest, runsJobs bool) error {
	if m.ID == "" || m.Actions == nil {
		return fmt.Errorf("manifest without ID or action catalog")
	}
	for _, action := range m.Actions.All() {
		if action.Title == "" || action.Description == "" || action.Target == "" || action.Payload == nil || len(action.Roles) == 0 && !action.Automation {
			return fmt.Errorf("action %s lacks a title, description, target, payload fields or roles", action.Schema)
		}
	}
	if len(m.Jobs) > 0 && !runsJobs {
		return fmt.Errorf("declares jobs but has no Run")
	}
	for _, j := range m.Jobs {
		if j.Name == "" || j.Every <= 0 {
			return fmt.Errorf("job %q needs a name and a positive interval", j.Name)
		}
	}
	for i, s := range m.Settings {
		if s.Name == "" || !s.Accepts(s.Default) || slices.ContainsFunc(m.Settings[:i], func(x platform.Setting) bool { return x.Name == s.Name }) {
			return fmt.Errorf("setting %q: unnamed, repeated, or its default is not a %s", s.Name, s.Type)
		}
	}
	for i, s := range m.Sequences {
		if err := s.Check(); err != nil || slices.ContainsFunc(m.Sequences[:i], func(x platform.Sequence) bool { return x.Name == s.Name }) {
			return fmt.Errorf("sequence %q: badly declared or repeated", s.Name)
		}
	}
	for i, e := range m.Emits {
		if e.Name == "" || strings.Contains(e.Name, "/") || slices.ContainsFunc(m.Emits[:i], func(x platform.EffectKind) bool { return x.Name == e.Name }) {
			return fmt.Errorf("effect kind %q: unnamed, repeated or containing '/'", e.Name)
		}
	}
	for _, action := range m.Actions.All() {
		for _, used := range action.Uses {
			protocol, _, ok := strings.Cut(used, "#")
			if !ok || !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
				return fmt.Errorf("action %s uses %s, not an action of a protocol it consumes", action.Schema, used)
			}
		}
	}
	for _, r := range m.Everyone {
		if !slices.Contains(m.Reads, r) {
			return fmt.Errorf("read %q is opened to everyone but not declared", r)
		}
	}
	for name, fields := range m.ReadInputs {
		if !slices.Contains(m.Reads, name) {
			return fmt.Errorf("read %q declares inputs but is not declared", name)
		}
		if err := platform.CheckInputs(fields); err != nil {
			return fmt.Errorf("read %q: %w", name, err)
		}
	}
	for name, source := range m.ReadSources {
		if !slices.Contains(m.Reads, name) || !slices.Contains([]string{"record", "user-input", "external-input"}, source) {
			return fmt.Errorf("read %q has an undeclared name or invalid original source type %q", name, source)
		}
	}
	return nil
}

func CheckSubscriptions(m platform.Manifest, subscriber bool) error {
	for _, action := range m.Subscribes {
		if protocol, _, ok := strings.Cut(action, "#"); ok {
			if !slices.ContainsFunc(m.Consumes, func(c platform.Consumption) bool { return c.Protocol == protocol }) {
				return fmt.Errorf("%s subscribes to %s of a protocol it does not consume", m.ID, action)
			}
			continue
		}
		if _, own := m.Actions.Action(action); !own {
			return fmt.Errorf("%s subscribes to %s, neither its own action nor a protocol event", m.ID, action)
		}
		if !subscriber {
			return fmt.Errorf("%s subscribes but has no Handle", m.ID)
		}
	}
	return nil
}

func CallbackFreeTransition(m platform.Manifest, schema string) bool {
	for _, entity := range m.Entities {
		verb, match := strings.CutPrefix(schema, entity.Type+".")
		if !match || entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if transition.Name == verb && transition.Do == nil && transition.After == nil && transition.Approval == nil {
				return true
			}
		}
	}
	return false
}

type ConstructionDrafts interface {
	platform.App
	CompleteBlueprintAssets([]platform.ReleaseAsset) ([]platform.ReleaseAsset, error)
	RootDrafts([]platform.AssetRef) ([]JointDraftRef, error)
	ApplicationDrafts(string) ([]JointDraftRef, error)
	ReferencedDrafts(platform.AssetKind, string) ([]JointDraftRef, error)
	SavedDraftChanges(platform.Caller, string, string, string) ([]DraftChange, error)
	SavedObjectActions(platform.Caller) ([]platform.Action, error)
	DraftReleaseAssetsMulti([]JointDraftRef, platform.Member) (JointDraftAssets, error)
	CandidateActions(platform.ReleaseAsset) ([]platform.Action, error)
	CheckProcess(platform.Caller, Process) *kernel.Error
	CheckPipeline(platform.Caller, Pipeline) *kernel.Error
}

type ConstructionDraftReader interface {
	CanonicalObject(Object) (Object, error)
}

type IntegrationDefinitions interface {
	ConnectionConfiguration(Connection) (Connection, error)
	ExecutionSource(Source) (Source, error)
	ExecutionConnection(Connection) (Connection, error)
	ExecutionDataset(Dataset) (Dataset, error)
	ExecutionPipeline(Pipeline) (Pipeline, error)
	ExecutionWriteback(Writeback) (Writeback, error)
	ExecutionMatching(Match) (Match, error)
}

type ConstructionSandbox interface {
	platform.App
	platform.AssetDefinitionReader
	Reinstall() error
	BeginAssetBootstrap([]platform.ReleaseAsset) (func(), error)
	ProcessFromAsset(platform.ReleaseAsset) (Process, error)
	InstallDefinitionAsset(platform.ReleaseAsset) error
	InstallFunctionAsset(platform.ReleaseAsset) error
	InstallAgentAsset(platform.ReleaseAsset) error
	InstallOperationAsset(platform.ReleaseAsset) error
}

type AlertDefinitions interface {
	AlertRules(string) ([]Alert, error)
}
type ObservationDefinitions interface {
	ExecutionObservation(Observation) (platform.ObservationDefinition, error)
}
