// Package build is the platform's builder app, on the app API and
// internal/host: objects a tenant defines, published into the
// running host so they behave like an app's own — the same records, generated
// actions and forms, search, aggregates, journal and replay. The definition is
// data; nothing about it is a second runtime.
package build

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/internal/construction"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/internal/systemscope"
	"platformserver/platform"
)

// Build is the builder app of one tenant.
type Build struct {
	installationCatalog *platform.Catalog // private installation declarations; never authoritative ledger state
	releaseDraftIDs     map[platform.AssetRef]string
	host                host.BuildHost
	ledger              *platform.Ledger
	// installed are the objects published in this tenant, by entity type: their
	// declarations, so generated actions of a defined object reach the record store.
	installed map[string]platform.Entity
	// jointEntities are the object drafts of the candidate being built, so the
	// same validation answers entity lookups as the candidate would install
	// them. Empty outside a joint build.
	jointEntities     map[string]platform.EntityInfo
	jointAssets       map[platform.AssetRef]bool
	jointDeclarations map[platform.AssetRef]platform.ReleaseAsset
	// jointMode is set while a candidate holds more than one draft: the host's
	// installed-state install checks then move to the candidate's own install
	// dry run, because no single draft's image is installed yet.
	jointMode     bool
	linkTypes     map[string]LinkType
	propertyTypes map[string]PropertyType
	queries       map[string]Query
	functions     map[string]construction.Function
	agents        map[string]Agent
	codes         map[string]Code
	tables        map[string]construction.Table // published decision tables, by name
}

// Attach is called by the host when a tenant is composed.
func (b *Build) AttachBuild(h host.BuildHost) { b.host = h }

// New is a tenant's builder app.
func New(tenant string) *Build {
	b := &Build{installed: map[string]platform.Entity{}, linkTypes: map[string]LinkType{}, propertyTypes: map[string]PropertyType{}, queries: map[string]Query{}, functions: map[string]construction.Function{}, agents: map[string]Agent{}, codes: map[string]Code{}, tables: map[string]construction.Table{}}
	actions := append(platform.EntityActions(b.objectEntity()), platform.EntityActions(b.pageEntity())...)
	actions = append(actions, platform.EntityActions(b.applicationEntity())...)
	actions = append(actions, platform.EntityActions(b.testPlanEntity())...)
	actions = append(actions, platform.EntityActions(b.processEntity())...)
	actions = append(actions, platform.EntityActions(b.functionEntity())...)
	actions = append(actions, platform.EntityActions(b.linkTypeEntity())...)
	actions = append(actions, platform.Action{Schema: construction.LinkTypeType + ".migrate", Target: construction.LinkTypeType, Title: "Migrate relationships", Description: "Move historical links into the endpoint owner's accepted records", Capability: construction.LinkTypeType, Roles: []string{construction.Builder}, Payload: []platform.Field{}})
	actions = append(actions, platform.EntityActions(b.propertyTypeEntity())...)
	actions = append(actions, platform.EntityActions(b.queryEntity())...)
	actions = append(actions, functionCallActions([]string{construction.Builder, construction.User})...)
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
	actions = append(actions, platform.Action{Schema: SchemaEvaluationSetPublish, Target: construction.TestPlanType, Title: "Publish test plan", Description: "Freeze a portable business scenario or the evaluation cases for one function version.", Capability: "objects", Roles: []string{construction.Builder}, Payload: []platform.Field{}})
	b.ledger = platform.NewLedger(tenant, construction.ID, platform.NewCatalog(actions...), construction.ObjectType, construction.PageType, construction.AppType, construction.TestPlanType, construction.ProcessType, construction.FunctionType, construction.PropertyTypeType, construction.LinkTypeType, construction.QueryType, construction.FunctionCallType, construction.EvaluationType, construction.CodeType, construction.SourceType, construction.ConnectionType, construction.DatasetType, construction.DatasetVersionType, construction.PipelineType, construction.WritebackType, MatchType, construction.TableType, AgentType, construction.AlertType, SampleSetType, construction.ObservationType, platform.OwnedRelationType(construction.ID), confirmationType, platform.DispatchType)
	return b
}

// releaseProfile is declared, not hidden: an operator sets it through the
// platform's settings surface.
func (b *Build) settings() []platform.Setting {
	return []platform.Setting{{Name: SettingReleaseProfile, Title: "Release profile", Type: "choice",
		Choices: []string{ProfileProduction, ProfileDevelopment}, Default: ProfileDevelopment,
		Description: "production: definitions are delivered only through a saved release candidate; development: direct install stays available for authoring, import and probes."}}
}

func (b *Build) Manifest() platform.Manifest {
	catalog := b.ledger.Catalog
	if b.installationCatalog != nil {
		catalog = b.installationCatalog
	}
	entities := []platform.Entity{platform.RelationEntity(construction.ID), b.objectEntity(), b.pageEntity(), b.applicationEntity(), b.testPlanEntity(), b.processEntity(), b.propertyTypeEntity(), b.linkTypeEntity(), b.queryEntity(), b.functionEntity(), b.functionCallEntity(), b.evaluationEntity(), b.agentEntity(), b.alertEntity(), b.codeEntity(), b.sourceEntity(), b.connectionEntity(), b.datasetEntity(), b.datasetVersionEntity(), b.pipelineEntity(), b.writebackEntity(), b.matchEntity(), b.tableEntity(), b.sampleSetEntity(), b.observationEntity(), b.confirmationEntity(), b.dispatchEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	// Every role an object names is a role of this app, which the Console
	// grants — one that only reads grants no action. Builder and
	// Publisher are the two roles releases need.
	roles := []string{construction.User, construction.Builder, construction.Publisher, construction.Integrator}
	for _, typ := range sortedTypes(b.installed) {
		for role := range b.installed[typ].Scope.Levels {
			if !slices.Contains(roles, role) {
				roles = append(roles, role)
			}
		}
	}
	slices.Sort(roles)
	provisions, reads := b.objectProtocols()
	return platform.Manifest{ID: construction.ID, Title: "Builder", Version: construction.ValueDefinitionVersion, Sequences: b.transactionSequences(), Actions: catalog, Entities: entities, Roles: roles, Settings: b.settings(), Jobs: []platform.Job{{Name: "confirmations", Title: "Advance business confirmations", Every: time.Second}, {Name: "dispatches", Title: "Advance device dispatches", Every: time.Second}}, Provides: provisions, Consumes: b.confirmationProtocols(), Reads: append([]string{ReadReleaseProfile}, reads...), ReadSources: protocolReadSources(reads), Queries: b.queryDeclarations(), Functions: b.functionDeclarations(), Agents: b.agentDeclarations(), Operations: b.operationDeclarations(),
		Pages: []platform.Page{{Name: "objects", Title: "Objects", Description: "The objects this organisation defines. Publish one to install it.",
			Layout: "list-detail", Object: platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: construction.ObjectType},
			ListFields:   []string{"title", "name", "state", "installed"},
			DetailFields: []string{"title", "name", "plural", "description", "fields", "state", "installed"},
			Actions: []platform.AssetRef{{App: construction.ID, Kind: platform.AssetAction, Name: construction.ObjectType + ".create"},
				{App: construction.ID, Kind: platform.AssetAction, Name: construction.ObjectType + ".edit"}, {App: construction.ID, Kind: platform.AssetAction, Name: construction.ObjectType + ".archive"}, {App: construction.ID, Kind: platform.AssetAction, Name: construction.SchemaPublish}}},
			{Name: "applications", Title: "Applications", Description: "The applications this organisation hands to its people. Each holds pages and appears in their launcher.",
				Layout: "list-detail", Object: platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: construction.AppType},
				ListFields:   []string{"title", "name", "icon", "state"},
				DetailFields: []string{"title", "name", "description", "icon", "pages", "groups", "state"},
				Actions: []platform.AssetRef{{App: construction.ID, Kind: platform.AssetAction, Name: construction.AppType + ".create"},
					{App: construction.ID, Kind: platform.AssetAction, Name: construction.AppType + ".edit"}, {App: construction.ID, Kind: platform.AssetAction, Name: construction.AppType + ".archive"}, {App: construction.ID, Kind: platform.AssetAction, Name: construction.SchemaHandOver}}},
			{Name: "pages", Title: "Pages", Description: "The pages this organisation composes over the objects it may read. Publish one to put it in the workspace.",
				Layout: "list-detail", Object: platform.AssetRef{App: construction.ID, Kind: platform.AssetObject, Name: construction.PageType},
				ListFields:   []string{"title", "name", "object", "state"},
				DetailFields: []string{"title", "name", "description", "object", "list", "detail", "actions", "state"},
				Actions: []platform.AssetRef{{App: construction.ID, Kind: platform.AssetAction, Name: construction.PageType + ".create"},
					{App: construction.ID, Kind: platform.AssetAction, Name: construction.PageType + ".edit"}, {App: construction.ID, Kind: platform.AssetAction, Name: construction.PageType + ".archive"}, {App: construction.ID, Kind: platform.AssetAction, Name: construction.SchemaRelease}}}}}
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

// SettingReleaseProfile declares how definitions reach operators (
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
var directInstallSchemas = []string{construction.SchemaPublish, construction.SchemaRelease, construction.SchemaHandOver, construction.SchemaProcess,
	SchemaPropertyType, construction.SchemaLinkType, SchemaQuery, SchemaFunction, SchemaCodePublish,
	SchemaTablePublish, SchemaAlertPublish, SchemaAgentPublish,
	construction.SourceType + ".publish", construction.PipelineType + ".publish", construction.WritebackType + ".publish", MatchType + ".publish"}

// checkReleaseProfile refuses a direct install that a production tenant has
// retired, naming the schema and the route that replaces it. Replay and
// recovery never take this path, so a historical Published image, its versions
// and a restore keep working in every profile.
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

// Read release-profile answers how this tenant delivers definitions, for the
// authoring surfaces that must not offer an entry the owner would refuse.
// Every role of this app may read it: it is a declaration of
// the tenant's profile, not a record.
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

// ReleaseProfile is how a tenant delivers definitions to operators.
type ReleaseProfile struct {
	Profile       string `json:"profile"`
	DirectInstall bool   `json:"directInstall"`
}

// Draft archival never retires installed definitions or retained runtime versions.
func (b *Build) checkDefinitionArchive(c platform.Caller, typ, id string) *kernel.Error {
	if !c.Holds(c.App, construction.Builder) && !(typ == construction.ObservationType && c.Holds(c.App, construction.Integrator)) {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
	}
	published := false
	switch typ {
	case construction.ObjectType:
		record, _ := platform.Get[construction.Object](c, id)
		published = record.Published != ""
	case construction.PageType:
		record, _ := platform.Get[Page](c, id)
		published = record.Published != ""
	case construction.AppType:
		record, _ := platform.Get[Application](c, id)
		published = record.Published != ""
	case construction.ProcessType:
		record, _ := platform.Get[construction.Process](c, id)
		published = record.Published != ""
	case construction.PropertyTypeType:
		record, _ := platform.Get[PropertyType](c, id)
		published = record.Published != ""
	case construction.LinkTypeType:
		record, _ := platform.Get[LinkType](c, id)
		published = record.Published != ""
	case construction.QueryType:
		record, _ := platform.Get[Query](c, id)
		published = record.Published != ""
	case construction.FunctionType:
		record, _ := platform.Get[construction.Function](c, id)
		published = record.Published != ""
	case construction.TableType:
		record, _ := platform.Get[construction.Table](c, id)
		published = record.Published != ""
	case construction.AlertType:
		record, _ := platform.Get[construction.Alert](c, id)
		published = record.Published != "" || legacyAlert(record)
	case AgentType:
		record, _ := platform.Get[Agent](c, id)
		published = record.Published != ""
	case construction.ObservationType:
		record, _ := platform.Get[construction.Observation](c, id)
		published = record.Published != ""
	case SampleSetType:
		record, _ := platform.Get[SampleSetRecord](c, id)
		published = record.Published != ""
	case construction.TestPlanType:
		record, _ := platform.Get[construction.TestPlan](c, id)
		published = record.Published != ""
	case construction.CodeType:
		record, _ := platform.Get[Code](c, id)
		published = record.Published != ""
		if record.State == "compiling" {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A compiling code function cannot be archived; wait for its build result")
		}
	}
	if !published {
		return nil
	}
	if typ == construction.ObjectType {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published object cannot be archived; its installed definition must be retained")
	}
	return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "A published definition cannot be archived; its installed version must be retained")
}

// Submit takes the builder's own actions and those generated for every object
// it has installed: a defined object's records are decided like any other's.
func (b *Build) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if !c.Replaying {
		if err := b.checkSystemDefinitionWrite(s); err != nil {
			return nil, err
		}
	}
	if s.GetSchema().GetName() == construction.LinkTypeType+".migrate" {
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
	if s.GetSchema().GetName() == construction.SchemaSourcePulled {
		return b.submitSourcePulled(c, s, now)
	}
	if s.GetSchema().GetName() == construction.SchemaConnectionChecked {
		return b.submitConnectionChecked(c, s, now)
	}
	if s.GetSchema().GetName() == construction.SchemaDatasetLoad {
		return b.submitDatasetLoad(c, s, now)
	}
	if s.GetSchema().GetName() == construction.SchemaPipelineRan {
		return b.submitPipelineRan(c, s, now)
	}
	if s.GetSchema().GetName() == construction.SchemaWritebackAnswered {
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
	if name := s.GetSchema().GetName(); name == construction.SchemaFunctionCall || name == construction.SchemaFunctionAnswer {
		return b.submitFunctionCall(c, s, now)
	}
	if s.GetSchema().GetName() == SchemaEvaluationSetPublish {
		return b.submitTestPlan(c, s, now)
	}
	if name := s.GetSchema().GetName(); name == construction.SchemaEvaluationStart || name == construction.SchemaEvaluationAnswer {
		return b.submitEvaluation(c, s, now)
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive && !c.Replaying && slices.Contains([]string{construction.ObjectType, construction.PageType, construction.AppType, construction.ProcessType, construction.PropertyTypeType, construction.LinkTypeType, construction.QueryType, construction.FunctionType, construction.CodeType, construction.TableType, construction.AlertType, AgentType, construction.TestPlanType, SampleSetType, construction.ObservationType}, typ) {
		if err := b.checkDefinitionArchive(c, typ, s.GetTarget().GetId()); err != nil {
			return nil, err
		}
	}
	if name := s.GetSchema().GetName(); (name == construction.TestPlanType+".create" || name == construction.TestPlanType+".edit") && !c.Replaying {
		if err := b.checkTestPlan(c, s); err != nil {
			return nil, err
		}
	}
	if name := s.GetSchema().GetName(); (name == construction.ObjectType+".create" || name == construction.ObjectType+".edit") && !c.Replaying {
		// What the payload carries is checked while it is still a draft, so the
		// person is told at once; publishing checks the whole definition again.
		var p struct {
			Name   *string               `json:"name"`
			Fields *[]construction.Field `json:"fields"`
		}
		if json.Unmarshal(s.GetPayload(), &p) == nil {
			var err error
			if p.Name != nil {
				err = b.checkName(*p.Name, s.GetTarget().GetId())
			}
			if err == nil && p.Fields != nil {
				err = b.checkPropertyFields(*p.Fields)
				if err == nil {
					err = construction.CheckFields(*p.Fields, b.fieldKnown)
				}
			}
			if err != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
			}
		}
	}
	if name := s.GetSchema().GetName(); (name == construction.PageType+".create" || name == construction.PageType+".edit") && !c.Replaying {
		var p Page
		if json.Unmarshal(s.GetPayload(), &p) == nil && p.Object != "" {
			if !b.fieldKnown(p.Object) {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: fmt.Sprintf("this tenant has no object %q", p.Object)}
			}
		}
	}
	pageEntity := b.pageEntity()
	if name := s.GetSchema().GetName(); name == construction.PageType+".create" || name == construction.PageType+".edit" {
		pageEntity = b.pageDraftEntity()
	}
	agentEntity := b.agentEntity()
	if name := s.GetSchema().GetName(); name == AgentType+".create" || name == AgentType+".edit" {
		agentEntity = b.agentDraftEntity()
	}
	entities := []platform.Entity{b.objectEntity(), pageEntity, b.applicationEntity(), b.testPlanEntity(), b.processEntity(), b.propertyTypeEntity(), b.linkTypeEntity(), b.queryEntity(), b.functionEntity(), b.functionCallEntity(), b.evaluationEntity(), agentEntity, b.alertEntity(), b.codeEntity(), b.sourceEntity(), b.connectionEntity(), b.datasetEntity(), b.datasetVersionEntity(), b.pipelineEntity(), b.writebackEntity(), b.matchEntity(), b.tableEntity(), b.sampleSetEntity(), b.observationEntity(), b.confirmationEntity(), b.dispatchEntity()}
	for _, typ := range sortedTypes(b.installed) {
		entities = append(entities, b.installed[typ])
	}
	if s.GetSchema().GetName() == construction.AppType+".withdraw" || s.GetSchema().GetName() == construction.AppType+".restore" {
		for i := range entities {
			if entities[i].Type == construction.AppType {
				entities[i].Validate = nil
			}
		}
	}
	if typ, archive := strings.CutSuffix(s.GetSchema().GetName(), ".archive"); archive {
		// Discarding a Build draft does not compile or publish it. The original
		// archive authorization and retained-publication guard ran above.
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

// check refuses a definition the host could not install, with the reason, while
// it is still a draft: a name that is not a name, a field the platform has no
// type for, a choice without values, a reference to an object that is not there.
// Published definitions are immutable values; installs replace registry
// entries after acceptance. A private builder owns registry maps and its joint
// validation context, while retaining declarations and the ledger identity.
func (b *Build) ForkDecision() (platform.App, error) {
	catalog := b.installationCatalog
	if catalog != nil {
		catalog = platform.NewCatalog(catalog.All()...)
	}
	return &Build{host: b.host, ledger: b.ledger,
		installationCatalog: catalog,
		releaseDraftIDs:     maps.Clone(b.releaseDraftIDs), installed: maps.Clone(b.installed), jointEntities: maps.Clone(b.jointEntities), jointAssets: maps.Clone(b.jointAssets), jointDeclarations: maps.Clone(b.jointDeclarations), jointMode: b.jointMode,
		linkTypes: maps.Clone(b.linkTypes), propertyTypes: maps.Clone(b.propertyTypes),
		queries: maps.Clone(b.queries), functions: maps.Clone(b.functions), agents: maps.Clone(b.agents),
		codes: maps.Clone(b.codes), tables: maps.Clone(b.tables)}, nil
}

var _ platform.DecisionForkApp = (*Build)(nil)
