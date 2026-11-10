package build

import (
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/kernel/systemscope"
	"platformserver/platform"
	appsauthoring "platformserver/services/apps/authoring"
	automationauthoring "platformserver/services/automate/authoring"
	dataowner "platformserver/services/data"
	functionauthoring "platformserver/services/functions/authoring"
	ontology "platformserver/services/ontology"
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
			if err := b.bindObjectActions(c, &object); err != nil {
				return nil, err
			}
			actions = append(actions, objectActions(object, Entity(object))...)
		}
	}
	return actions, nil
}

type Build struct {
	installationCatalog *platform.Catalog
	host                platform.BuildHost
	data                *dataowner.Owner
	automation          *automationauthoring.Owner
	ledger              *platform.Ledger
	installed           map[string]platform.Entity
	jointEntities       map[string]platform.EntityInfo
	jointAssets         map[platform.AssetRef]bool
	jointDeclarations   map[platform.AssetRef]platform.ReleaseAsset
	jointMode           bool
	linkTypes           map[string]LinkType
	propertyTypes       map[string]PropertyType
	valueTypes          map[string]ValueType
	objectTypeGroups    map[string]ObjectTypeGroup
	queries             map[string]Query
	interfaces          map[string]InterfaceRecord
	functions           *functionauthoring.Owner
	apps                *appsauthoring.Owner
	agents              map[string]Agent
	codes               map[string]Code
	tables              map[string]Table
}

func (b *Build) AttachBuild(h platform.BuildHost) { b.host = h }
func (b *Build) AttachFiles(h platform.FileHost)  { b.data.AttachFiles(h) }

func New(tenant string) *Build {
	b := &Build{installed: map[string]platform.Entity{}, linkTypes: map[string]LinkType{}, propertyTypes: map[string]PropertyType{}, valueTypes: map[string]ValueType{}, objectTypeGroups: map[string]ObjectTypeGroup{}, queries: map[string]Query{}, interfaces: map[string]InterfaceRecord{}, agents: map[string]Agent{}, codes: map[string]Code{}, tables: map[string]Table{}}
	b.data = dataowner.New(buildDataHost{b})
	b.automation = automationauthoring.New(automationEnvironment{b})
	b.functions = functionauthoring.NewOwner(functionEnvironment{b})
	b.apps = appsauthoring.NewOwner(appEnvironment{b})
	actions := append(platform.EntityActions(b.objectEntity()), platform.EntityActions(b.apps.PageEntity())...)
	actions = append(actions, platform.EntityActions(b.actionTypeEntity())...)
	actions = append(actions, platform.Action{Schema: SchemaExtractActions, Target: ObjectType, Title: "Extract action types", Description: "Move current inline action drafts into independent retained resources.", Automation: true, Payload: []platform.Field{}})
	actions = append(actions, platform.EntityActions(b.apps.ApplicationEntity())...)
	actions = append(actions, platform.EntityActions(b.testPlanEntity())...)
	actions = append(actions, b.automation.ProcessActions()...)
	actions = append(actions, b.functions.Actions()...)
	actions = append(actions, platform.EntityActions(linkKind.entity(b))...)
	actions = append(actions, platform.Action{Schema: LinkTypeType + ".migrate", Target: LinkTypeType, Title: "Migrate relationships", Description: "Move historical links into the endpoint owner's accepted records", Capability: LinkTypeType, Roles: []string{Builder}, Payload: []platform.Field{}})
	actions = append(actions, platform.EntityActions(propertyKind.entity(b))...)
	actions = append(actions, platform.EntityActions(valueKind.entity(b))...)
	actions = append(actions, platform.EntityActions(b.objectTypeGroupEntity())...)
	actions = append(actions, platform.EntityActions(queryKind.entity(b))...)
	actions = append(actions, platform.EntityActions(interfaceKind.entity(b))...)
	actions = append(actions, evaluationActions()...)
	actions = append(actions, platform.EntityActions(b.codeEntity())...)
	actions = append(actions, b.data.Actions()...)
	actions = append(actions, platform.EntityActions(b.branchEntity())...)
	for _, entity := range b.resourceEntities() {
		actions = append(actions, platform.EntityActions(entity)...)
	}
	actions = append(actions, workingStateActions()...)
	actions = append(actions, confirmationActions()...)
	actions = append(actions, dispatchActions()...)
	actions = append(actions, platform.EntityActions(b.tableEntity())...)
	actions = append(actions, platform.EntityActions(b.agentEntity())...)
	actions = append(actions, platform.EntityActions(b.sampleSetEntity())...)
	actions = append(actions, codeActions()...)
	actions = append(actions, platform.EntityActions(b.productEntity())...)
	actions = append(actions, productVersionActions()...)
	actions = append(actions, platform.Action{Schema: SchemaEvaluationSetPublish, Target: TestPlanType, Title: "Publish test plan", Description: "Freeze a portable business scenario or the evaluation cases for one function version.", Capability: "objects", Roles: []string{Builder}, Payload: []platform.Field{}})
	actions = resourceActions(actions)
	classes := []string{ObjectType, ActionTypeType, PageType, AppType, TestPlanType, automationauthoring.ProcessType, FunctionType, PropertyTypeType, ValueTypeType, ObjectTypeGroupType, LinkTypeType, QueryType, InterfaceType, FunctionCallType, EvaluationType, CodeType, TableType, AgentType, SampleSetType, BranchType, ResourceType, FolderType, WorkingStateType, ProductType, ProductVersionType, platform.OwnedRelationType(ID), confirmationType, platform.DispatchType}
	classes = append(classes, b.data.Types()...)
	b.ledger = platform.NewLedger(tenant, ID, platform.NewCatalog(actions...), classes...)
	return b
}

func (b *Build) settings() []platform.Setting {
	return []platform.Setting{{Name: SettingReleaseProfile, Title: "Release profile", Type: "choice",
		Choices: []string{ProfileProduction, ProfileDevelopment}, Default: ProfileDevelopment,
		Description: "production: definitions are delivered only through a saved release candidate; development: direct install stays available for authoring, import and probes."}}
}

func (b *Build) manifestRoles() []string {
	roles := []string{User, Builder, Publisher, Integrator, platform.ProjectViewerRole, platform.ProjectDiscovererRole}
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
	entities := []platform.Entity{platform.RelationEntity(ID), b.objectEntity(), b.actionTypeEntity(), b.apps.PageEntity(), b.apps.ApplicationEntity(), b.testPlanEntity(), b.automation.ProcessEntity(), propertyKind.entity(b), valueKind.entity(b), b.objectTypeGroupEntity(), linkKind.entity(b), queryKind.entity(b), interfaceKind.entity(b), b.functions.Entity(), b.functions.CallEntity(), b.evaluationEntity(), b.agentEntity(), b.codeEntity(), b.tableEntity(), b.sampleSetEntity(), b.branchEntity(), b.confirmationEntity(), b.dispatchEntity(), b.productEntity(), b.productVersionEntity()}
	entities = append(entities, b.data.Entities()...)
	entities = append(entities, b.resourceEntities()...)
	entities = append(entities, workingStateEntity())
	for i := range entities {
		if BranchDraftType(entities[i].Type) {
			if entities[i].Scope.Levels == nil {
				entities[i].Scope.Levels = map[string]string{}
			}
			entities[i].Scope.Levels[platform.ProjectViewerRole] = platform.ScopeTenant
		}
	}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	provisions, reads := b.objectProtocols()
	return platform.Manifest{ID: ID, Title: platform.BuiltinManifestTitle(ID), Version: ValueDefinitionVersion, Sequences: b.transactionSequences(), Actions: catalog, Entities: entities, Roles: b.manifestRoles(), Settings: b.settings(), Jobs: []platform.Job{{Name: "confirmations", Title: "Advance business confirmations", Every: time.Second}, {Name: "dispatches", Title: "Advance device dispatches", Every: time.Second}}, Provides: provisions, Consumes: b.consumedProtocols(), Reads: append([]string{ReadReleaseProfile}, reads...), ReadSources: protocolReadSources(reads), Queries: definitionDeclarations(b.queries), Functions: b.functions.Declarations(), Agents: b.agentDeclarations(), Operations: b.operationDeclarations(),
		Pages: []platform.Page{
			platform.ListDetailPage("objects", "Objects", "The objects this organisation defines. Publish one to install it.",
				platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: ObjectType},
				[]string{"title", "name", "state", "installed"},
				[]string{"title", "name", "plural", "description", "fields", "state", "installed"},
				[]platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: ObjectType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaPublish}}),
			platform.ListDetailPage("applications", "Applications", "The applications this organisation hands to its people. Each holds pages and appears in their launcher.",
				platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: AppType},
				[]string{"title", "name", "icon", "state"},
				[]string{"title", "name", "description", "icon", "pages", "groups", "state"},
				[]platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: AppType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: AppType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: AppType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaHandOver}}),
			platform.ListDetailPage("pages", "Pages", "The pages this organisation composes over the objects it may read. Publish one to put it in the workspace.",
				platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: PageType},
				[]string{"title", "name", "object", "state"},
				[]string{"title", "name", "description", "object", "state"},
				[]platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: PageType + ".create"},
					{App: ID, Kind: platform.AssetAction, Name: PageType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: PageType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaRelease}})}}
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

func (b *Build) checkReleaseProfile(c platform.Caller, schema string) *kernel.Error {
	directInstall := directInstallSchemas()
	if c.Tenant == systemscope.Tenant && slices.Contains(directInstall, schema) {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "System definitions are published through an approved Platform change")
	}
	if c.Setting(SettingReleaseProfile) != ProfileProduction || !slices.Contains(directInstall, schema) {
		return nil
	}
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: fmt.Sprintf(
		"%s installs a definition for operators at once, and this tenant delivers through a saved release candidate: review the draft and activate the candidate (POST /v1/releases/candidates). Direct install is limited to the development and import profiles.", schema)}
}

func (b *Build) Declarations() []*pb.AuthorityDeclaration { return b.ledger.Declarations() }
func (b *Build) AcceptedLedger() *platform.Ledger         { return b.ledger }

func (b *Build) ForkRead(ledger *platform.Ledger) (platform.App, error) {
	fork, err := b.ForkDecision()
	if err != nil {
		return nil, err
	}
	fork.(*Build).ledger = ledger
	return fork, nil
}
func (b *Build) SnapshotFormat() string { return "platform.ledger.v1" }

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
	if !c.Holds(c.App, Builder) && !(typ == dataowner.ObservationType && c.Holds(c.App, Integrator)) {
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
	case PropertyTypeType:
		record, _ := platform.Get[PropertyType](c, id)
		published = record.Published != ""
	case ValueTypeType:
		record, _ := platform.Get[ValueType](c, id)
		published = record.Published != ""
	case ObjectTypeGroupType:
		record, _ := platform.Get[ObjectTypeGroup](c, id)
		published = record.Published != ""
	case LinkTypeType:
		record, _ := platform.Get[LinkType](c, id)
		published = record.Published != ""
	case QueryType:
		record, _ := platform.Get[Query](c, id)
		published = record.Published != ""
	case InterfaceType:
		record, _ := platform.Get[InterfaceRecord](c, id)
		published = record.Published != ""
	case FunctionType:
		record, _ := platform.Get[Function](c, id)
		published = record.Published != ""
	case TableType:
		record, _ := platform.Get[Table](c, id)
		published = record.Published != ""
	case AgentType:
		record, _ := platform.Get[Agent](c, id)
		published = record.Published != ""
	case dataowner.ObservationType:
		record, _ := platform.Get[dataowner.Observation](c, id)
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
	case ProductType:
		versions, err := readDefinitionInventory[ProductVersion](c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Product versions could not be read")
		}
		record, _ := platform.Get[Product](c, id)
		if slices.ContainsFunc(versions, func(version ProductVersion) bool { return version.Product == record.Name }) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A product with published versions cannot be archived")
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
	if !c.Replaying && strings.HasSuffix(s.GetSchema().GetName(), ".edit") && BranchDraftType(s.GetTarget().GetType()) {
		var input struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(s.GetPayload(), &input) == nil && input.Name != "" {
			raw, refusal := c.ReadRecord(s.GetTarget().GetType(), s.GetTarget().GetId(), now)
			if refusal == nil {
				var prior struct {
					Name string `json:"name"`
				}
				if json.Unmarshal(raw, &prior) == nil && prior.Name != input.Name {
					return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "A resource API name is immutable")
				}
			}
		}
	}
	if !c.Replaying && s.GetSchema().GetName() == SchemaExtractActions {
		return b.submitOntologyWrite(c, s, now)
	}
	if s.GetTarget().GetType() == WorkingStateType {
		return b.submitWorkingState(c, s, now)
	}
	if s.GetTarget().GetType() == ResourceType || IsResourceSave(s) || IsResourceCreate(s) {
		return b.submitResource(c, s, now)
	}
	if !c.Replaying && !c.Automation {
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
	if record, err, handled := b.automation.Decide(b.ledger, c, s, now); handled {
		return record, err
	}
	if s.GetSchema().GetName() == SchemaCodeCompile {
		return b.submitCodeCompile(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaCodeCompiled {
		return b.submitCodeCompiled(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaCodeAttach {
		return b.submitCodeAttach(c, s, now)
	}
	if record, err, handled := b.data.Decide(b.ledger, c, s, now); handled {
		return record, err
	}
	if slices.Contains([]string{confirmationAnswer, confirmationReview, confirmationTick}, s.GetSchema().GetName()) {
		return b.updateConfirmation(c, s, now)
	}
	if slices.Contains([]string{platform.DispatchAnswerSchema, platform.DispatchReviewSchema, platform.DispatchTickSchema}, s.GetSchema().GetName()) {
		return b.updateDispatch(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == SchemaProposalReview || name == SchemaFunctionCall || name == SchemaFunctionAnswer {
		return b.functions.Submit(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaEvaluationSetPublish {
		return b.submitTestPlan(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaProductVersion {
		return b.submitProductVersion(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == SchemaEvaluationStart || name == SchemaEvaluationAnswer {
		return b.submitEvaluation(c, s, now)
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive && !c.Replaying && slices.Contains([]string{ObjectType, PageType, AppType, PropertyTypeType, ValueTypeType, ObjectTypeGroupType, LinkTypeType, QueryType, InterfaceType, FunctionType, CodeType, TableType, AgentType, TestPlanType, SampleSetType, dataowner.ObservationType, ProductType}, typ) {
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
					err = ontology.CheckFields(*p.Fields, b.fieldKnown)
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
			known, err := b.apps.CheckPageObject(c, p.Object)
			if err != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
			}
			if !known {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: fmt.Sprintf("this tenant has no object %q", p.Object)}
			}
		}
	}
	if !c.Replaying && (s.GetTarget().GetType() == ObjectType || s.GetTarget().GetType() == ActionTypeType) && (s.GetSchema().GetName() == s.GetTarget().GetType()+".create" || s.GetSchema().GetName() == s.GetTarget().GetType()+".edit") {
		return b.submitOntologyWrite(c, s, now)
	}
	pageEntity := b.apps.PageEntity()
	if name := s.GetSchema().GetName(); name == PageType+".create" || name == PageType+".edit" {
		pageEntity = b.apps.PageDraftEntity()
	}
	agentEntity := b.agentEntity()
	if name := s.GetSchema().GetName(); name == AgentType+".create" || name == AgentType+".edit" {
		agentEntity = b.agentDraftEntity()
	}
	entities := []platform.Entity{b.objectEntity(), b.actionTypeEntity(), pageEntity, b.apps.ApplicationEntity(), b.testPlanEntity(), propertyKind.entity(b), valueKind.entity(b), b.objectTypeGroupEntity(), linkKind.entity(b), queryKind.entity(b), interfaceKind.entity(b), b.functions.Entity(), b.functions.CallEntity(), b.evaluationEntity(), agentEntity, b.codeEntity(), b.tableEntity(), b.sampleSetEntity(), b.branchEntity(), b.confirmationEntity(), b.dispatchEntity(), b.productEntity(), b.productVersionEntity()}
	entities = append(entities, b.data.Entities()...)
	entities = append(entities, b.resourceEntities()...)
	entities = append(entities, workingStateEntity())
	for i := range entities {
		if BranchDraftType(entities[i].Type) {
			if entities[i].Scope.Levels == nil {
				entities[i].Scope.Levels = map[string]string{}
			}
			entities[i].Scope.Levels[platform.ProjectViewerRole] = platform.ScopeTenant
		}
	}
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
	fork := *b
	fork.installationCatalog = catalog
	fork.installed = maps.Clone(b.installed)
	fork.jointEntities = maps.Clone(b.jointEntities)
	fork.jointAssets = maps.Clone(b.jointAssets)
	fork.jointDeclarations = maps.Clone(b.jointDeclarations)
	fork.linkTypes = maps.Clone(b.linkTypes)
	fork.propertyTypes = maps.Clone(b.propertyTypes)
	fork.valueTypes = maps.Clone(b.valueTypes)
	fork.objectTypeGroups = maps.Clone(b.objectTypeGroups)
	fork.queries = maps.Clone(b.queries)
	fork.interfaces = maps.Clone(b.interfaces)
	fork.agents = maps.Clone(b.agents)
	fork.codes = maps.Clone(b.codes)
	fork.tables = maps.Clone(b.tables)
	fork.functions = b.functions.Fork(functionEnvironment{&fork})
	fork.apps = b.apps.Fork(appEnvironment{&fork})
	fork.data = b.data.Fork(buildDataHost{&fork})
	fork.automation = b.automation.Fork(automationEnvironment{&fork})
	return &fork, nil
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
var _ ConstructionSandbox = (*Build)(nil)

const (
	ID                     = ontology.AppID
	ValueDefinitionVersion = "1"
	Builder                = ontology.BuilderRole
	Publisher              = "publisher"
	User                   = ontology.UserRole
)

func TypeOf(name string) string { return ID + "." + name }

type ConstructionDrafts interface {
	platform.App
	CompleteBlueprintAssets([]platform.ReleaseAsset) ([]platform.ReleaseAsset, error)
	RootDrafts([]platform.AssetRef) ([]JointDraftRef, error)
	ApplicationDrafts(string) ([]JointDraftRef, error)
	ReferencedDrafts(platform.AssetKind, string, string) ([]JointDraftRef, error)
	WorkingDrafts(string) ([]DraftChange, error)
	DraftChangeOf(platform.Member, platform.AssetKind, string) (DraftChange, bool, error)
	DraftPublished(platform.AssetKind, string) (string, string, bool, error)
	SavedObjectActions(platform.Caller) ([]platform.Action, error)
	DraftReleaseAssetsMulti([]JointDraftRef, platform.Member, bool) (JointDraftAssets, error)
	CandidateActions(platform.ReleaseAsset) ([]platform.Action, error)
	CheckProcess(platform.Caller, automationauthoring.Process) *kernel.Error
}

type ConstructionDraftReader interface {
	CanonicalObject(Object) (Object, error)
}

type ConstructionSandbox interface {
	platform.App
	platform.AssetDefinitionReader
	Reinstall() error
	BeginAssetBootstrap([]platform.ReleaseAsset) (func(), error)
	ProcessFromAsset(platform.ReleaseAsset) (automationauthoring.Process, error)
	InstallDefinitionAsset(platform.ReleaseAsset) error
	InstallFunctionAsset(platform.ReleaseAsset) error
	InstallAgentAsset(platform.ReleaseAsset) error
	InstallOperationAsset(platform.ReleaseAsset) error
}
