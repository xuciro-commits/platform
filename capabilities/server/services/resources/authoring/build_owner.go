package authoring

import (
	"cmp"
	"encoding/json"
	"fmt"
	"maps"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"reflect"

	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
	dataowner "platformserver/services/data"
	devopsmodel "platformserver/services/devops/descriptor"
	ontology "platformserver/services/ontology"
	branchowner "platformserver/services/resources/merge/owner"
	"platformserver/services/resources/publication"
	"slices"
	"strings"
	"time"
)

func (b *Build) SavedObjectActions(c platform.Caller) ([]platform.Action, error) {
	objects, err := readDefinitionInventory[ontology.Object](c)
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
	resolver                  publication.Resolver
	publications              []publication.Hooks
	publicationBase           []publication.Hooks
	objectPublication         func(ontology.ObjectPublicationEnvironment) publication.Hooks
	queryFactory              func(publication.QueryAuthoringEnvironment) publication.QueryAuthoringBinding
	queries                   publication.QueryAuthoringBinding
	functionFactory           func(publication.FunctionEnvironment) publication.FunctionAuthoring
	agentFactory              func(platform.AgentAuthoringEnvironment) platform.AgentAuthoringBinding
	agent                     platform.AgentAuthoringBinding
	testPlanFactory           func(testPlanEnvironment, evaluationPublication) testPlanPublication
	evaluationSetFactory      func(testPlanEnvironment) evaluationPublication
	functionEvaluationFactory func(publication.FunctionEvaluationEnvironment) publication.FunctionEvaluationBinding
	evaluations               publication.FunctionEvaluationBinding
	testPlans                 testPlanPublication
	sampleSetFactory          func(devopsmodel.SampleSetEnvironment) devopsmodel.SampleSetPublication
	sampleSets                devopsmodel.SampleSetPublication
	installationCatalog       *platform.Catalog
	host                      runtimeprotocol.BuildHost
	transaction               platform.TransactionHost
	data                      *dataowner.Owner
	automation                publication.AutomationAuthoring
	automationFactory         func(publication.AutomationEnvironment, publication.Resolver) publication.AutomationAuthoring
	branches                  *branchowner.Owner
	authority                 platform.Authority
	authorities               platform.Authorities
	installed                 map[string]platform.RecordType
	jointEntities             map[string]platform.RecordTypeInfo
	jointAssets               map[platform.AssetRef]bool
	jointDeclarations         map[platform.AssetRef]platform.ReleaseAsset
	jointMode                 bool
	linkTypes                 map[string]ontology.LinkType
	propertyTypes             map[string]ontology.PropertyType
	valueTypes                map[string]ontology.ValueType
	objectTypeGroups          map[string]ontology.ObjectTypeGroup
	interfaces                map[string]ontology.InterfaceRecord
	contracts                 map[string]ontology.ContractRecord
	functions                 publication.FunctionAuthoring
	apps                      publication.ApplicationAuthoring
	application               ApplicationOwners
	manifestCache             *buildManifestCache
	manifestPages             []platform.Page
}

type testPlanEnvironment = devopsmodel.TestPlanEnvironment[runtimeprotocol.Function]
type evaluationPublication = devopsmodel.EvaluationPublication[ontology.Object, runtimeprotocol.Process, runtimeprotocol.Function]
type testPlanPublication = devopsmodel.TestPlanPublication[ontology.Object, runtimeprotocol.Process, runtimeprotocol.Function]

func versionedDraft(load func(string, func() ([]platform.ReleaseAsset, error)) ([]platform.ReleaseAsset, []platform.ReleaseAsset, platform.AssetRef, platform.AssetRef, bool, error)) func(string, func() ([]platform.ReleaseAsset, error)) (publication.DraftClosure, error) {
	return func(id string, assets func() ([]platform.ReleaseAsset, error)) (publication.DraftClosure, error) {
		var draft publication.DraftClosure
		var err error
		draft.Before, draft.After, draft.Prior, draft.Next, draft.HasPrior, err = load(id, assets)
		return draft, err
	}
}

func (b *Build) bindPublications() {
	object := b.objectPublication(ontology.ObjectPublicationEnvironment{
		Host:      func() runtimeprotocol.BuildHost { return b.host },
		Authority: func() platform.Authority { return b.authority },
		Entity:    b.entity,
		Portable:  b.data.PortableObject,
		Actions:   objectActions,
		Page:      func(object ontology.Object) platform.Page { return b.application.ObjectPage(object, ID) },
		Pages:     b.objectInstallationPages,
		Validate:  b.validateObjectInstallation,
		Installed: func(entity platform.RecordType) {
			b.installed[entity.Type] = entity
			b.invalidateManifest()
		},
		Contracts: b.checkContracts,
	})
	b.publications = append([]publication.Hooks{object}, b.publicationBase...)
	queries := b.queries
	b.publications = append(b.publications, publication.Hooks{
		Kinds: []platform.AssetKind{platform.AssetQuery}, Schemas: []publication.SchemaHooks{{Name: SchemaQueryPublish, Validate: queries.ValidateImage, Install: queries.InstallImage}},
		FreezeBatch: queries.FreezeBatch, LoadExecution: queries.LoadExecution,
		VersionedDraft: versionedDraft(queries.VersionedDraft),
	})
	agent := b.agent
	b.publications = append(b.publications, publication.Hooks{
		Kinds: []platform.AssetKind{platform.AssetAgent}, Schemas: []publication.SchemaHooks{{Name: SchemaAgentPublish, Validate: agent.ValidateImage, Install: agent.InstallImage}},
		ReadFrozen:  agent.ReadFrozen,
		FreezeBatch: agent.FreezeBatch, LoadExecution: agent.LoadExecution,
		VersionedDraft: versionedDraft(agent.VersionedDraft),
	})
	plan := b.testPlans
	b.publications = append(b.publications, publication.Hooks{
		Kinds: []platform.AssetKind{platform.AssetEvaluation, platform.AssetTestPlan}, Schemas: []publication.SchemaHooks{{Name: SchemaEvaluationSetPublish, Decode: plan.Decode, Validate: plan.Validate, Install: plan.Validate}},
		ReadFrozen:     plan.ReadFrozen,
		FreezeBatch:    plan.FreezeBatch,
		VersionedDraft: versionedDraft(plan.VersionedDraft),
	})
	b.publications = append(b.publications, b.functions.ComputePublication())
	samples := b.sampleSets
	b.publications = append(b.publications, publication.Hooks{
		Kinds:              []platform.AssetKind{platform.AssetSampleSet},
		DirectInstallation: samples.DecodeInstallation, FreezeInstallations: samples.PrepareInstallations, ReadFrozen: samples.ReadFrozen,
		VersionedDraft: versionedDraft(samples.VersionedDraft),
	})
}

func (b *Build) AttachBuild(h runtimeprotocol.BuildHost)      { b.host = h }
func (b *Build) AttachTransaction(h platform.TransactionHost) { b.transaction = h }
func (b *Build) AttachFiles(h runtimeprotocol.MediaFileHost)  { b.data.AttachFiles(h) }

func (b *Build) AttachOwner(bindings runtimeprotocol.OwnerBindings) {
	b.AttachBuild(bindings.Build)
	b.AttachTransaction(bindings.Transaction)
	b.AttachFiles(bindings.Files)
}

func New(tenant string, authorities platform.Authorities, publications []publication.Hooks, resolver publication.Resolver, objectPublication func(ontology.ObjectPublicationEnvironment) publication.Hooks, queryFactory func(publication.QueryAuthoringEnvironment) publication.QueryAuthoringBinding, functionFactory func(publication.FunctionEnvironment) publication.FunctionAuthoring, agentFactory func(platform.AgentAuthoringEnvironment) platform.AgentAuthoringBinding, functionEvaluationFactory func(publication.FunctionEvaluationEnvironment) publication.FunctionEvaluationBinding, testPlanFactory func(testPlanEnvironment, evaluationPublication) testPlanPublication, evaluationSetFactory func(testPlanEnvironment) evaluationPublication, sampleSetFactory func(devopsmodel.SampleSetEnvironment) devopsmodel.SampleSetPublication, application ApplicationOwners, automationFactory func(publication.AutomationEnvironment, publication.Resolver) publication.AutomationAuthoring) *Build {
	b := &Build{resolver: resolver, publicationBase: slices.Clone(publications), objectPublication: objectPublication, queryFactory: queryFactory, functionFactory: functionFactory, agentFactory: agentFactory, functionEvaluationFactory: functionEvaluationFactory, testPlanFactory: testPlanFactory, evaluationSetFactory: evaluationSetFactory, sampleSetFactory: sampleSetFactory, installed: map[string]platform.RecordType{}, linkTypes: map[string]ontology.LinkType{}, propertyTypes: map[string]ontology.PropertyType{}, valueTypes: map[string]ontology.ValueType{}, objectTypeGroups: map[string]ontology.ObjectTypeGroup{}, interfaces: map[string]ontology.InterfaceRecord{}, contracts: map[string]ontology.ContractRecord{}, manifestCache: new(buildManifestCache)}
	b.queries = b.queryFactory(b.queryEnvironment())
	b.data = dataowner.New(buildDataHost{b}, resolver)
	b.data.AttachIntegrationRuntime(b.integrationRuntime())
	b.automationFactory = automationFactory
	b.automation = automationFactory(automationEnvironment{b}, resolver)
	b.functions = b.functionFactory(functionEnvironment{b})
	b.evaluations = b.functionEvaluationFactory(b.functionEvaluationEnvironment())
	b.application = application
	b.manifestPages = b.buildManifestPages()
	b.apps = b.application.New(appEnvironment{b})
	b.branches = branchowner.New(branchEnvironment{b})
	b.agent = b.agentFactory(b.agentEnvironment())
	b.bindTestPlans()
	b.bindSampleSets()
	b.bindPublications()
	actions := append(actioncatalog.EntityActions(b.objectEntity()), actioncatalog.EntityActions(b.apps.PageEntity())...)
	actions = append(actions, actioncatalog.EntityActions(b.actionTypeEntity())...)
	actions = append(actions, actioncatalog.EntityActions(b.apps.ApplicationEntity())...)
	actions = append(actions, actioncatalog.EntityActions(b.testPlanEntity())...)
	actions = append(actions, b.automation.ProcessActions()...)
	actions = append(actions, b.functions.Actions()...)
	actions = append(actions, actioncatalog.EntityActions(linkKind.entity(b))...)
	actions = append(actions, platform.Action{Schema: LinkTypeType + ".migrate", Target: LinkTypeType, Title: "Migrate relationships", Description: "Move historical links into the endpoint owner's accepted records", Capability: LinkTypeType, Roles: []string{Builder}, Payload: []platform.Field{}})
	actions = append(actions, actioncatalog.EntityActions(propertyKind.entity(b))...)
	actions = append(actions, actioncatalog.EntityActions(valueKind.entity(b))...)
	actions = append(actions, actioncatalog.EntityActions(b.objectTypeGroupEntity())...)
	actions = append(actions, actioncatalog.EntityActions(b.queries.Entity())...)
	actions = append(actions, actioncatalog.EntityActions(interfaceKind.entity(b))...)
	actions = append(actions, actioncatalog.EntityActions(contractKind.entity(b))...)
	actions = append(actions, b.evaluations.Actions()...)
	actions = append(actions, actioncatalog.EntityActions(b.functions.CodeEntity())...)
	actions = append(actions, b.data.Actions()...)
	actions = append(actions, b.branches.Actions()...)
	for _, entity := range b.resourceEntities() {
		actions = append(actions, actioncatalog.EntityActions(entity)...)
	}
	actions = append(actions, workingStateActions()...)
	actions = append(actions, dataowner.ConfirmationActions()...)
	actions = append(actions, dataowner.DispatchActions()...)
	actions = append(actions, actioncatalog.EntityActions(b.functions.TableEntity())...)
	actions = append(actions, actioncatalog.EntityActions(b.agent.Entity())...)
	actions = append(actions, actioncatalog.EntityActions(b.sampleSets.Entity())...)
	actions = append(actions, b.functions.CodeActions()...)
	actions = append(actions, actioncatalog.EntityActions(b.productEntity())...)
	actions = append(actions, devopsmodel.ProductVersionActions()...)
	actions = append(actions, platform.Action{Schema: SchemaEvaluationSetPublish, Target: TestPlanType, Title: "Publish test plan", Description: "Freeze a portable business scenario or the evaluation cases for one function version.", Capability: "objects", Roles: []string{Builder}, Payload: []platform.Field{}})
	actions = resourceActions(actions)
	classes := []string{ObjectType, ActionTypeType, runtimeprotocol.PageType, runtimeprotocol.AppType, TestPlanType, runtimeprotocol.ProcessType, runtimeprotocol.FunctionType, PropertyTypeType, ValueTypeType, ObjectTypeGroupType, LinkTypeType, b.queries.Entity().Type, InterfaceType, ContractType, runtimeprotocol.FunctionCallType, b.evaluations.Entity().Type, runtimeprotocol.CodeType, runtimeprotocol.TableType, AgentType, SampleSetType, BranchType, ResourceType, FolderType, WorkingStateType, ProductType, ProductVersionType, runtimeprotocol.OwnedRelationType(ID), runtimeprotocol.ConfirmationType, runtimeprotocol.DispatchType}
	classes = append(classes, b.data.Types()...)
	b.authorities = authorities
	b.authority = authorities(tenant, ID, platform.NewCatalog(actions...), classes...)
	return b
}

func (b *Build) settings() []platform.Setting {
	return []platform.Setting{{Name: SettingReleaseProfile, Title: "Release profile", Type: "choice",
		Choices: []string{ProfileProduction, ProfileDevelopment}, Default: ProfileDevelopment,
		Description: "production: definitions are delivered only through a saved release candidate; development: direct install stays available for authoring, import and probes."}}
}

func (b *Build) manifestRoles() []string {
	roles := []string{User, Builder, Publisher, Integrator, runtimeprotocol.ProjectViewerRole, runtimeprotocol.ProjectDiscovererRole}
	for _, typ := range sortedTypes(b.installed) {
		if platform.OwnerOfType(typ) == ID && !strings.HasPrefix(typ, ID+".") {
			continue
		}
		for role := range b.installed[typ].Scope.Levels {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	slices.Sort(roles)
	return roles
}

var buildManifestJobs = []platform.Job{
	{Name: "confirmations", Title: "Advance business confirmations", Every: time.Second},
	{Name: "dispatches", Title: "Advance device dispatches", Every: time.Second},
}

func (b *Build) buildManifestPages() []platform.Page {
	return []platform.Page{
		b.application.ListDetailPage("objects", "Objects", "The objects this organisation defines. Publish one to install it.",
			platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: ObjectType},
			[]string{"title", "name", "state", "installed"},
			[]string{"title", "name", "plural", "description", "fields", "state", "installed"},
			[]platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".create"},
				{App: ID, Kind: platform.AssetAction, Name: ObjectType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: ObjectType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: SchemaPublish}}),
		b.application.ListDetailPage("applications", "Applications", "The applications this organisation hands to its people. Each holds pages and appears in their launcher.",
			platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: runtimeprotocol.AppType},
			[]string{"title", "name", "icon", "state"},
			[]string{"title", "name", "description", "icon", "pages", "groups", "state"},
			[]platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.AppType + ".create"},
				{App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.AppType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.AppType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.SchemaHandOver}}),
		b.application.ListDetailPage("pages", "Pages", "The pages this organisation composes over the objects it may read. Publish one to put it in the workspace.",
			platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: runtimeprotocol.PageType},
			[]string{"title", "name", "object", "state"},
			[]string{"title", "name", "description", "object", "state"},
			[]platform.AssetRef{{App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.PageType + ".create"},
				{App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.PageType + ".edit"}, {App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.PageType + ".archive"}, {App: ID, Kind: platform.AssetAction, Name: runtimeprotocol.SchemaRelease}}),
	}
}

func (b *Build) Manifest() platform.Manifest {
	catalog := b.authority.Catalog()
	if b.installationCatalog != nil {
		catalog = b.installationCatalog
	}
	entities, roles, sequences, provisions, reads, readSources := b.cachedManifestState()
	pages := b.manifestPages
	if pages == nil {
		pages = b.buildManifestPages()
	}
	return platform.Manifest{ID: ID, Title: platform.BuiltinManifestTitle(ID), Version: ValueDefinitionVersion, Sequences: sequences, Actions: catalog, Entities: entities, Roles: roles, Settings: b.settings(), Jobs: buildManifestJobs, Provides: provisions, Consumes: b.consumedProtocols(), Reads: reads, ReadSources: readSources, Queries: b.queries.Declarations(), Functions: b.functions.Declarations(), Agents: b.agent.Declarations(), Operations: b.functions.OperationDeclarations(), Pages: pages, Subscribes: []string{ontology.SchemaPublish}}
}

func (b *Build) Interested(names []string, _ platform.Event) bool {
	return ontology.FillsReference(names) || ontology.HandlesPackageDemo(names)
}

func (b *Build) Handle(c platform.Caller, e platform.Event) *platform.Error {
	return b.Listen(c, e, []string{e.Record.GetSubmission().GetSchema().GetName()}, e.Record.GetRecordedTime().AsTime())
}

func (b *Build) Listen(c platform.Caller, e platform.Event, names []string, now time.Time) *platform.Error {
	if !ontology.FillsReference(names) && !ontology.HandlesPackageDemo(names) {
		return nil
	}
	model := func(typ string) (reflect.Type, bool) {
		installed, ok := b.installed[typ]
		return reflect.TypeOf(installed.Model), ok
	}
	submit := func(s *pb.Submission) *platform.Error {
		_, refusal := b.Submit(c, s, now)
		return refusal
	}
	member, _ := b.host.Member(e.Record.GetSubmission().GetPrincipalId())
	if ontology.FillsReference(names) {
		lang := cmp.Or(member.Language, b.host.Caller(c, member, "platform").Setting("language"))
		if refusal := ontology.FillReference(c, lang, model, submit); refusal != nil {
			return refusal
		}
	}
	pkgID := e.Record.GetSubmission().GetTarget().GetId()
	seedCaller := c
	if member.ID != "" {
		seedCaller.Member = member
	}
	roles := maps.Clone(seedCaller.Member.Roles)
	if roles == nil {
		roles = map[string]string{}
	}
	roles[ID] = Builder
	seedCaller.Member.Roles = roles
	seedSubmit := func(s *pb.Submission) *platform.Error {
		_, refusal := b.Submit(seedCaller, s, now)
		return refusal
	}
	if slices.Contains(names, "platform.package.install") {
		if seeds, ok := b.host.PackageSeed(pkgID); ok {
			if refusal := ontology.FillSeed(seedCaller, pkgID, seeds, model, seedSubmit); refusal != nil {
				return refusal
			}
		}
	}
	if slices.Contains(names, "platform.package.clear-demo") {
		if seeds, ok := b.host.PackageSeed(pkgID); ok {
			if refusal := ontology.ClearSeed(seedCaller, pkgID, seeds, model, seedSubmit); refusal != nil {
				return refusal
			}
		}
	}
	return nil
}

func sortedTypes(installed map[string]platform.RecordType) []string {
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

func (b *Build) checkReleaseProfile(c platform.Caller, schema string) *platform.Error {
	directInstall := directInstallSchemas()
	if c.Setting(SettingReleaseProfile) != ProfileProduction || !slices.Contains(directInstall, schema) {
		return nil
	}
	return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED, Message: fmt.Sprintf(
		"%s installs a definition for operators at once, and this tenant delivers through a saved release candidate: review the draft and activate the candidate (POST /v1/releases/candidates). Direct install is limited to the development and import profiles.", schema)}
}

func (b *Build) Declarations() []*pb.AuthorityDeclaration { return b.authority.Declarations() }
func (b *Build) Authority() platform.Authority            { return b.authority }

func (b *Build) SnapshotFormat() string { return "platform.ledger.v1" }

func (b *Build) Snapshot() (json.RawMessage, error) { return b.authority.Snapshot() }
func (b *Build) Restore(raw json.RawMessage) error  { return b.authority.Restore(raw) }

func (b *Build) Read(c platform.Caller, name string) (any, *platform.Error) {
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

func (b *Build) checkDefinitionArchive(c platform.Caller, typ, id string) *platform.Error {
	descriptor, _ := resourceDescriptorOfType(typ)
	if descriptor.published == nil && typ != ProductType {
		return nil
	}
	if !c.Holds(c.App, Builder) && !(typ == dataowner.ObservationType && c.Holds(c.App, Integrator)) {
		return &platform.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	if typ == ProductType {
		versions, err := readDefinitionInventory[ProductVersion](c)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Product versions could not be read")
		}
		record, _ := platform.Get[Product](c, id)
		if slices.ContainsFunc(versions, func(version ProductVersion) bool { return version.Product == record.Name }) {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A product with published versions cannot be archived")
		}
	}
	if descriptor.published == nil || !descriptor.published(b, c, id) {
		return nil
	}
	if typ == ObjectType {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published object cannot be archived; its installed definition must be retained")
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be archived; its installed version must be retained")
}

func (b *Build) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
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
	if s.GetTarget().GetType() == WorkingStateType {
		return b.submitWorkingState(c, s, now)
	}
	if s.GetTarget().GetType() == ResourceType || IsResourceSave(s) || IsResourceCreate(s) {
		return b.submitResource(c, s, now)
	}
	if !c.Replaying && !c.Automation {
		if err := b.checkManagedDefinitionWrite(s); err != nil {
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
	if record, err, handled := b.automation.Decide(b.authority, c, s, now); handled {
		return record, err
	}
	if record, err, handled := b.functions.DecideCodeBuild(c, s, now); handled {
		return record, err
	}
	if record, err, handled := b.data.Decide(b.authority, c, s, now); handled {
		return record, err
	}
	if slices.Contains([]string{runtimeprotocol.ConfirmationAnswerSchema, runtimeprotocol.ConfirmationType + ".review", runtimeprotocol.ConfirmationType + ".tick"}, s.GetSchema().GetName()) {
		return b.data.UpdateConfirmation(c, s, now)
	}
	if slices.Contains([]string{runtimeprotocol.DispatchAnswerSchema, runtimeprotocol.DispatchReviewSchema, runtimeprotocol.DispatchTickSchema}, s.GetSchema().GetName()) {
		return b.data.UpdateDispatch(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == runtimeprotocol.SchemaProposalReview || name == runtimeprotocol.SchemaFunctionCall || name == runtimeprotocol.SchemaFunctionAnswer {
		return b.functions.Submit(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaEvaluationSetPublish {
		return b.testPlans.Submit(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaProductVersion {
		return b.submitProductVersion(c, s, now)
	}
	if name := s.GetSchema().GetName(); b.evaluations.Handles(name) {
		return b.evaluations.Submit(c, s, now)
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive && !c.Replaying {
		if err := b.checkDefinitionArchive(c, typ, s.GetTarget().GetId()); err != nil {
			return nil, err
		}
	}
	if record, err, handled := b.functions.DecideCompute(c, s, now); handled {
		return record, err
	}
	if name := s.GetSchema().GetName(); (name == TestPlanType+".create" || name == TestPlanType+".edit") && !c.Replaying {
		if err := b.checkTestPlan(c, s); err != nil {
			return nil, err
		}
	}
	if name := s.GetSchema().GetName(); (name == ObjectType+".create" || name == ObjectType+".edit") && !c.Replaying {
		var p struct {
			Name   *string           `json:"name"`
			Fields *[]ontology.Field `json:"fields"`
		}
		if json.Unmarshal(s.GetPayload(), &p) == nil {
			var err error
			if p.Name != nil {
				err = b.checkName("", *p.Name, s.GetTarget().GetId())
			}
			if err == nil && p.Fields != nil {
				err = ontology.CheckPropertyFields(*p.Fields, ID, b.host)
				if err == nil {
					err = ontology.CheckValueTypeFields(*p.Fields, ID, b.host)
				}
				if err == nil {
					err = ontology.CheckFields(*p.Fields, b.fieldKnown)
				}
			}
			if err != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
			}
		}
	}
	if name := s.GetSchema().GetName(); (name == runtimeprotocol.PageType+".create" || name == runtimeprotocol.PageType+".edit") && !c.Replaying {
		var p runtimeprotocol.Page
		if json.Unmarshal(s.GetPayload(), &p) == nil && p.Object != "" {
			known, err := b.apps.CheckPageObject(c, p.Object)
			if err != nil {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
			}
			if !known {
				return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: fmt.Sprintf("this tenant has no object %q", p.Object)}
			}
		}
	}
	if !c.Replaying && (s.GetTarget().GetType() == ObjectType || s.GetTarget().GetType() == ActionTypeType) && (s.GetSchema().GetName() == s.GetTarget().GetType()+".create" || s.GetSchema().GetName() == s.GetTarget().GetType()+".edit") {
		return b.submitOntologyWrite(c, s, now)
	}
	entities := b.recordTypes(s.GetSchema().GetName(), false)
	if record, err, ok := b.authority.Generated(c, s, now, nil, entities...); ok {
		return record, err
	}
	return nil, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (b *Build) Fork(authority platform.Authority) (platform.Service, error) {
	catalog := b.installationCatalog
	if catalog != nil {
		catalog = catalog.CloneActions()
	}
	fork := *b
	fork.authority = authority
	fork.installationCatalog = catalog
	fork.manifestCache = new(buildManifestCache)
	fork.installed = maps.Clone(b.installed)
	fork.jointEntities = maps.Clone(b.jointEntities)
	fork.jointAssets = maps.Clone(b.jointAssets)
	fork.jointDeclarations = maps.Clone(b.jointDeclarations)
	fork.linkTypes = maps.Clone(b.linkTypes)
	fork.propertyTypes = maps.Clone(b.propertyTypes)
	fork.valueTypes = maps.Clone(b.valueTypes)
	fork.objectTypeGroups = maps.Clone(b.objectTypeGroups)
	fork.queries = b.queries.Fork(fork.queryEnvironment())
	fork.interfaces = maps.Clone(b.interfaces)
	fork.contracts = maps.Clone(b.contracts)
	fork.functions = b.functions.Fork(functionEnvironment{&fork})
	fork.evaluations = b.functionEvaluationFactory(fork.functionEvaluationEnvironment())
	fork.apps = b.application.New(appEnvironment{&fork})
	fork.branches = b.branches.Fork(branchEnvironment{&fork})
	fork.data = b.data.Fork(buildDataHost{&fork})
	fork.data.AttachIntegrationRuntime(fork.integrationRuntime())
	fork.automation = b.automationFactory(automationEnvironment{&fork}, b.resolver)
	fork.agent = b.agent.Fork(fork.agentEnvironment())
	fork.bindTestPlans()
	fork.bindSampleSets()
	fork.bindPublications()
	return &fork, nil
}

func (b *Build) newSimulation(tenant string) *Build {
	return New(tenant, b.authorities, b.publicationBase, b.resolver, b.objectPublication, b.queryFactory, b.functionFactory, b.agentFactory, b.functionEvaluationFactory, b.testPlanFactory, b.evaluationSetFactory, b.sampleSetFactory, b.application, b.automationFactory)
}
func (b *Build) NewSimulationApp(tenant string) platform.Service { return b.newSimulation(tenant) }
func (b *Build) NewSimulationSandbox(tenant string) ConstructionSandbox {
	return b.newSimulation(tenant)
}
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
	var object ontology.Object
	if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetObject || json.Unmarshal(asset.Body, &object) != nil || object.Type() != asset.Ref.Name {
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
	Integrator             = "integrator"
	Publisher              = "publisher"
	User                   = ontology.UserRole
)

func TypeOf(name string) string { return ID + "." + name }

func qualified(namespace, name string) string {
	if namespace == "" || name == "" {
		return name
	}
	return namespace + "." + name
}

func qualifiedType(namespace, name string) string {
	return ontology.Object{Namespace: namespace, Name: name}.Type()
}

func namespaceOf(image []byte) string {
	var meta struct{ Namespace string }
	_ = json.Unmarshal(image, &meta)
	return meta.Namespace
}

type ConstructionDrafts interface {
	platform.Service
	runtimeprotocol.FlowReleaseOwner
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
	CheckProcess(platform.Caller, runtimeprotocol.Process) *platform.Error
	NewSimulationSandbox(string) ConstructionSandbox
}

type ConstructionDraftReader interface {
	CanonicalObject(ontology.Object) (ontology.Object, error)
}

type ConstructionSandbox interface {
	platform.Service
	runtimeprotocol.AssetDefinitionReader
	Reinstall() error
	BeginAssetBootstrap([]platform.ReleaseAsset) (func(), error)
	ProcessFromAsset(platform.ReleaseAsset) (runtimeprotocol.Process, error)
	InstallDefinitionAsset(platform.ReleaseAsset) error
	InstallAgentAsset(platform.ReleaseAsset) error
	InstallOperationAsset(platform.ReleaseAsset) error
}
