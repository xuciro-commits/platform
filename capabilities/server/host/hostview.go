package host

import (
	"encoding/json"
	"fmt"
	pageowner "platformserver/services/apps/pages"
	"platformserver/services/devops/packages"
	"platformserver/services/objects"
	"platformserver/services/ontology"
	"platformserver/services/security"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"

	runtimekernel "platformserver/kernel"
	runtimeprotocol "platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type hostView struct {
	t        *Tenant
	app      platform.Service
	decision *stagedDecision
}

func (h hostView) ProtocolContract(id string) (platform.Protocol, bool) {
	if provider, ok := h.t.router().Resolve(id); ok {
		return provider.Provision.Protocol, true
	}
	return platform.Protocol{}, false
}
func (h hostView) ReadProtocolRecords(c platform.Caller, typ string) ([]any, *platform.Error) {
	store := h.t.Projection
	if h.decision != nil {
		store = h.decision.Records
	}
	return objects.ProtocolRecords(h.t.objectOwner(store).RecordsFrom(store, c.Member, typ, platform.Query{Limit: 200}, time.Now()))
}
func (h hostView) Install(c platform.Caller, e platform.RecordType, actions []platform.Action, pages ...platform.Page) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.t.Install(h.app, e, actions, pages...)
}
func (h hostView) installationDraft() *Tenant {
	return h.installationView(h.t.Projection.ForkRecords())
}
func (h hostView) installationView(recordView *records.Store) *Tenant {
	draft := &Tenant{services: h.t.services, disabledOwners: h.t.disabledOwners, ID: h.t.ID, ownerSet: h.t.with(h.t.apps), ownerRoles: h.t.ownerRoles,
		Projection: recordView, definitions: slices.Clone(h.t.definitions), includeArchivedDefinitions: h.t.includeArchivedDefinitions, Files: h.t.files(), releases: h.t.releases}
	return draft
}
func (h hostView) ValidateInstallDependents(c platform.Caller, e platform.RecordType, actions []platform.Action, generatedPage string, pages ...platform.Page) error {
	var engine platform.RuleEngine = h.t.services.Rules
	if c.Replaying {
		engine = platform.NativeRules{}
	}
	return ontology.InstallDependents{
		App: h.app.Manifest().ID, Entity: e, Actions: actions, GeneratedPage: generatedPage, Engine: engine,
		Definitions: h.t.definitions, Bindings: h.t.bindings.All(), ProjectPage: pageowner.Safety, ApplicationPage: pageowner.ApplicationPage, QueryReferences: pageowner.QueryReferences,
		Provisions: func(provider string) []platform.Provision { return h.t.App(provider).Manifest().Provides },
		Begin: func() ontology.InstallDraft {
			draft := h.installationDraft()
			owner := func(id string) hostView { return hostView{t: draft, app: h.t.App(id)} }
			return ontology.InstallDraft{
				Records: draft.Projection,
				Install: func() error { return draft.Install(h.app, e, actions, pages...) },
				CheckSets: func(info platform.RecordTypeInfo, engine platform.RuleEngine) error {
					return draft.objectOwner(draft.Projection).CheckRecordSetDeclarations(info, engine)
				},
				Entity: draft.Entity, CheckQueries: draft.checkPageQueries,
				App:         func(id string) platform.Service { return draft.App(id) },
				LinkType:    func(app string, l platform.LinkType) error { return owner(app).ValidateInstallLinkType(l) },
				Query:       func(app string, q platform.NamedQuery) error { return owner(app).ValidateInstallQuery(q) },
				Function:    func(app string, f platform.AIFunction) error { return owner(app).ValidateInstallFunction(f) },
				InstallPage: func(app platform.Service, p platform.Page) error { return draft.InstallPage(app, p) },
			}
		},
	}.Validate()
}
func (h hostView) OwnerOf(dataClass string) (string, bool) {
	return runtimekernel.OwnerOfClass(h.t.apps, dataClass)
}
func (h hostView) ProtocolEvent(name string) (platform.ProtocolEvent, bool) {
	return h.t.router().Event(name)
}
func (h hostView) As(c platform.Caller, app string) platform.Caller {
	return platform.RouteCaller(c, app)
}
func (h hostView) Caller(parent platform.Caller, m platform.Member, app string) platform.Caller {
	if !parent.Staging() {
		return platform.NewCaller(runtime{t: h.t, scoped: true}, m, app, parent.Replaying, false)
	}
	return platform.ActingCaller(parent, m, app, false)
}
func (h hostView) Automation(parent platform.Caller, app string) platform.Caller {
	return h.t.automated(parent, app)
}
func (h hostView) identity() platform.IdentityDirectory {
	owner := h.t.Identity
	if owner != nil && h.decision != nil {
		return h.decision.stateApp(owner).(platform.IdentityDirectory)
	}
	return owner
}
func (h hostView) Member(id string) (platform.Member, bool) {
	if owner := h.identity(); owner != nil {
		return owner.MemberByID(id)
	}
	return platform.Member{}, false
}
func (h hostView) Holding(app, role string) []string {
	if owner := h.identity(); owner != nil {
		return owner.Holding(app, role)
	}
	return nil
}
func (h hostView) Action(schema string) (string, platform.Action, bool) {
	return ontology.DeclaredAction(h.actionOwner(schema), h.t.definitions, schema)
}
func (h hostView) actionOwner(schema string) platform.Service {
	return h.t.App(h.t.owner["action:"+schema])
}

func (h hostView) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return runtimekernel.SubmitTo(c, h.actionOwner(s.GetSchema().GetName()), &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, s, now)
}

func (h hostView) Attempt(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *platform.Error) {
	return runtimekernel.AttemptTo(c, func() platform.Service { return h.actionOwner(s.GetSchema().GetName()) }, &platform.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}, s, now)
}

func (h hostView) Recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	if h.decision != nil {
		return h.decision.recipients(c, now, to)
	}
	return h.t.recipients(c, now, to)
}

func (h hostView) Directory() platform.Directory {
	if h.decision != nil {
		return h.decision.directoryView()
	}
	return h.t.Directory
}

func (h hostView) Seen(c platform.Caller, keys ...string) {
	if c.Staging() {
		platform.MarkSeen(c, keys...)
		return
	}
	h.t.Notices.MarkKeysRead(c.App, keys)
}

func (h hostView) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *platform.Error) {
	ref, _, err := h.t.router().Invoke(c, protocol, action, id, payload, key, correlation, now)
	return ref, err
}

func (h hostView) Tasks() platform.Tasks {
	if h.decision != nil && h.t.Tasks != nil {
		return h.decision.stateApp(h.t.Tasks.(platform.Service)).(platform.Tasks)
	}
	return h.t.Tasks
}

func (h hostView) Processes() runtimeprotocol.Processes {
	if h.decision != nil && h.t.Processes != nil {
		return h.decision.stateApp(h.t.Processes.(platform.Service)).(runtimeprotocol.Processes)
	}
	return h.t.Processes
}

func (h hostView) ActiveRelease() string { return h.t.releases.Active() }
func (h hostView) Runs() platform.Runs {
	if h.t.Agents == nil {
		return nil
	}
	if h.decision != nil {
		return h.decision.stateApp(h.t.Agents).(platform.Runs)
	}
	return h.t.Agents
}

func (h hostView) InstallPage(c platform.Caller, p platform.Page) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if c.Replaying {
		return h.t.installPage(h.app, p, false)
	}
	return h.t.installPage(h.app, p, true, c.Rules())
}
func (h hostView) ValidateInstallPage(p platform.Page) error {
	return h.installationDraft().InstallPage(h.app, p)
}

func (h hostView) InstallApplication(c platform.Caller, a platform.Application) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	if c.Replaying {
		return h.t.storeApplication(h.app, a)
	}
	return h.t.InstallApplication(h.app, a)
}
func (h hostView) ValidateInstallApplication(a platform.Application) error {
	return h.installationDraft().InstallApplication(h.app, a)
}

func (h hostView) Entity(typ string) (platform.RecordTypeInfo, bool) { return h.t.Entity(typ) }
func (h hostView) EntitySource(typ string) (platform.AssetBinding, bool) {
	return ontology.EntitySource(h.t.definitions, typ)
}

func (h hostView) Declares(name string) bool { return h.t.declares(name) }
func (h hostView) Interfaces() []platform.Interface {
	manifests := make([]platform.Manifest, len(h.t.apps))
	for i, app := range h.t.apps {
		manifests[i] = app.Manifest()
	}
	return ontology.Interfaces(h.t.definitions, manifests)
}

func (h hostView) DynamicAgents() []runtimeprotocol.DynamicAgentDefinitions {
	return h.t.dynamicAgentList()
}

func (h hostView) DynamicAgent(id string) (runtimeprotocol.DynamicAgentDefinitions, bool) {
	owner, ok := h.t.dynamicAgents[id]
	return owner, ok
}

func (h hostView) BindInterfaceQuery(q platform.NamedQuery, drafts ...platform.RecordTypeInfo) (platform.NamedQuery, error) {
	for _, info := range drafts {
		if info.App != h.app.Manifest().ID {
			return q, fmt.Errorf("interface query draft belongs to another owner")
		}
	}
	return h.t.bindInterfaceQuery(q, drafts...)
}

func (h hostView) Readable(m platform.Member, ref string, now time.Time) bool {
	return h.t.readable(m, ref, now)
}

func (h hostView) Stored(tenant, hash string) bool {
	t := h.t
	if t.readOrigin != nil {
		t = t.readOrigin
	}
	if tenant != t.ID || t.uploads.Unavailable(hash) {
		return false
	}
	if h.t.preparedResult != nil {
		h.t.preparedResult.uploads = append(h.t.preparedResult.uploads, hash)
	}
	return h.t.files().Exists(h.t.readContext(), tenant+"/"+hash)
}

func (h hostView) Record(ref string) (any, bool) {
	if h.decision != nil {
		return (&Tenant{Projection: h.decision.Records}).held(ref)
	}
	return h.t.held(ref)
}

func (h hostView) BuildProcesses() platform.ProcessDefinitions { return h.Processes() }
func (h hostView) UnboundFlowClosures(typ string) (map[string][]string, error) {
	if h.t.Processes == nil {
		return nil, nil
	}
	return h.t.Processes.UnboundRunningClosures(typ)
}

func (h hostView) Calendar(party string, day platform.Date) platform.Calendar {
	return runtimekernel.OptionalRoles{Directory: h.Directory()}.Calendar(party, day)
}
func (h hostView) Units(party, structure string, day platform.Date) []string {
	return runtimekernel.OptionalRoles{Directory: h.Directory()}.PartyUnits(party, structure, day)
}
func (h hostView) Holders(structure, unit, role string, day platform.Date) []string {
	return runtimekernel.OptionalRoles{Directory: h.Directory()}.Holders(structure, unit, role, day)
}

func (h hostView) RuleQuery(app, name string, version int) (platform.NamedQuery, int, bool) {
	return ontology.RuleQuery(h.t.App(app), name, version, func(name string) (platform.NamedQuery, bool) { return h.t.namedQuery(app, name) })
}

func (h hostView) ModelQueryBinding(ref platform.AssetRef) (platform.AssetBinding, bool) {
	return ontology.ModelQueryBinding(h.t.definitions, ref)
}

func (h hostView) CheckPageModel(c platform.Caller, p platform.Page) error {
	if err := pageowner.CheckModelBindings(p, h.t.modelVisibleLookup(c.Member), h.t.modelCriteria, c.Rules()); err != nil {
		return err
	}
	return pageowner.CheckModelQueries(p, h.t.definitions, h.t.Entity)
}

func (h hostView) ownerBindings() runtimeprotocol.OwnerBindings {
	return runtimeprotocol.OwnerBindings{Training: h, Agent: h, Console: h, Build: h, Transaction: h, Files: h, Knowledge: h, Relations: h, Flow: h, Work: h}
}
func (h hostView) bindOwner(app platform.Service) { h.t.services.BindOwner(app, h.ownerBindings()) }
func (h hostView) BuildProcessConsumptions() []platform.Consumption {
	var processes runtimeprotocol.Processes = h.t.Processes
	if h.decision != nil {
		if app := h.decision.PrivateApps[runtimeprotocol.FlowApp]; app != nil {
			processes = app.(runtimeprotocol.Processes)
		}
	}
	if processes == nil {
		return nil
	}
	return processes.Consumptions("build")
}

func (h hostView) ProjectApprovals(project string) int {
	return security.ProjectApprovals(h.t.App("platform"), project)
}

func (h hostView) CheckResourceSchema(schema string, version *platform.SchemaVersion) *platform.Error {
	if version == nil {
		return nil
	}
	owner := h.actionOwner(schema)
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	_, refusal := h.t.SchemaBinding(owner, &pb.Submission{Schema: &pb.SchemaRef{Name: schema, Version: version.Family}}, version.Version, h.t.actionSchemaVersion)
	return refusal
}

func (h hostView) CompartmentExists(id string) bool {
	return security.CompartmentExists(h.t.App("platform"), id)
}

func (h hostView) PackageSeed(id string) ([]json.RawMessage, bool) {
	return packages.SeedOf(h.t.PackageOwner, id)
}
