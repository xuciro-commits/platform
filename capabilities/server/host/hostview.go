package host

import (
	"fmt"
	pageowner "platformserver/services/apps/pages"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/kernel/records"
	"platformserver/platform"
)

type hostView struct {
	t        *Tenant
	app      platform.App
	decision *stagedDecision
}

func (h hostView) ProtocolContract(id string) (platform.Protocol, bool) {
	if provider, ok := h.t.router().Resolve(id); ok {
		return provider.Provision.Protocol, true
	}
	return platform.Protocol{}, false
}

func (h hostView) ReadProtocolRecords(c platform.Caller, typ string) ([]any, *kernel.Error) {
	store := h.t.Projection
	if h.decision != nil {
		store = h.decision.Records
	}
	page, refusal := h.t.recordsFrom(store, c.Member, typ, platform.Query{Limit: 200}, time.Now())
	if refusal != nil {
		return nil, refusal
	}
	if page.Total > 200 {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Protocol record reads support at most 200 visible records")
	}
	return page.Records, nil
}

func (h hostView) Install(c platform.Caller, e platform.Entity, actions []platform.Action, pages ...platform.Page) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.t.Install(h.app, e, actions, pages...)
}

func (h hostView) installationDraft() *Tenant {
	return h.installationView(h.t.Projection.ForkRecords())
}

func (h hostView) installationView(recordView *records.Store) *Tenant {
	draft := &Tenant{services: h.t.services, disabledOwners: h.t.disabledOwners, ID: h.t.ID, ownerSet: h.t.with(h.t.apps),
		identity: h.t.identity, directory: h.Directory(), Projection: recordView, definitions: slices.Clone(h.t.definitions), includeArchivedDefinitions: h.t.includeArchivedDefinitions, Files: h.t.files(), procs: h.t.procs, releases: h.t.releases}
	return draft
}

func (h hostView) ValidateInstallDependents(c platform.Caller, e platform.Entity, actions []platform.Action, generatedPage string, pages ...platform.Page) error {
	for protocol, provider := range h.t.bindings.All() {
		if provider != h.app.Manifest().ID {
			continue
		}
		belongs := false
		for _, provision := range h.t.App(provider).Manifest().Provides {
			for _, schema := range provision.Actions {
				belongs = belongs || provision.Protocol.ID() == protocol && strings.HasPrefix(schema, e.Type+".")
			}
		}
		if belongs && !slices.ContainsFunc(e.Protocols, func(p platform.Provision) bool { return p.Protocol.ID() == protocol }) {
			return fmt.Errorf("protocol %s is bound to this object; retain its provider declaration", protocol)
		}
	}
	draft := h.installationDraft()
	if current := draft.Projection.Type(e.Type); current != nil && current.Info().Go != reflect.TypeOf(e.Model) {
		current.SetRows(records.Index[*records.Row]{})
	}
	if err := draft.Install(h.app, e, actions, pages...); err != nil {
		return err
	}
	var engine platform.RuleEngine = h.t.services.Rules
	if c.Replaying {
		engine = platform.NativeRules{}
	}
	for _, et := range draft.Projection.SortedTypes() {
		if err := draft.checkRecordSetDeclarations(et.Info(), engine); err != nil {
			return err
		}
		if err := platform.CheckConstraints(et.Info(), draft.Entity, true, engine); err != nil {
			return err
		}
	}
	for _, def := range h.t.definitions {
		if def.LinkType != nil {
			view := hostView{t: draft, app: h.t.App(def.Ref.App)}
			if err := view.ValidateInstallLinkType(*def.LinkType); err != nil {
				return err
			}
			for _, l := range def.LinkVersions {
				if err := view.ValidateInstallLinkType(l); err != nil {
					return err
				}
			}
		}
		if def.Application != nil {
			for _, q := range def.Application.Queries {
				if q.Object.Name == e.Type {
					if err := draft.checkPageQueries(def.Application.QueryPage()); err != nil {
						return err
					}
					break
				}
			}
		}
		if def.Query != nil && def.Query.Object == e.Type {
			view := hostView{t: draft, app: h.t.App(def.Ref.App)}
			if err := view.ValidateInstallQuery(*def.Query); err != nil {
				return err
			}
			for _, q := range def.QueryVersions {
				if err := view.ValidateInstallQuery(q); err != nil {
					return err
				}
			}
		}
		if def.Function != nil && def.Function.Object == e.Type {
			if err := (hostView{t: draft, app: h.t.App(def.Ref.App)}).ValidateInstallFunction(*def.Function); err != nil {
				return err
			}
		}
	}
	removedAction := func(ref platform.AssetRef) bool {
		owned := ref.App == h.app.Manifest().ID && slices.ContainsFunc(h.t.definitions, func(d platform.Definition) bool {
			return d.Ref == ref && d.Action != nil && d.Action.Target == e.Type
		})
		return owned && !slices.ContainsFunc(actions, func(a platform.Action) bool { return a.Schema == ref.Name })
	}
	for _, def := range h.t.definitions {
		if def.Ref.Kind != platform.AssetPage || def.Page == nil ||
			def.Ref.App == h.app.Manifest().ID && def.Ref.Name == generatedPage {
			continue
		}
		p, err := def.Page.Safety()
		if err != nil {
			return err
		}
		if p.Object.Name != e.Type && !slices.ContainsFunc(p.QueryReferences(), func(ref platform.AssetRef) bool { return ref.Kind == platform.AssetObject && ref.Name == e.Type }) && !slices.ContainsFunc(p.DataBindings, func(s platform.PageDataBinding) bool {
			return s.Object.Name == e.Type
		}) {
			continue
		}
		for _, trigger := range p.ActionTriggers {
			for _, ref := range trigger.Actions {
				if removedAction(ref) {
					return fmt.Errorf("page %s: removed action %s", p.Name, ref.Name)
				}
			}
		}
		owner := draft.App(def.Ref.App)
		if owner == nil {
			return fmt.Errorf("page %s has no owner %s", def.Ref, def.Ref.App)
		}
		if err := draft.InstallPage(owner, p); err != nil {
			return fmt.Errorf("release dependent %s: %w", def.Ref, err)
		}
	}
	return nil
}

func (h hostView) OwnerOf(dataClass string) (string, bool) {
	for _, a := range h.t.apps {
		if slices.ContainsFunc(a.Declarations(), func(d *pb.AuthorityDeclaration) bool { return d.GetDataClass() == dataClass }) {
			return a.Manifest().ID, true
		}
	}
	return "", false
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
	owner := h.t.identity
	if owner != nil && h.decision != nil {
		if private, ok := h.decision.stateApp(owner).(platform.IdentityDirectory); ok {
			return private
		}
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
	a := h.t.App(h.t.owner["action:"+schema])
	if a == nil {
		return "", platform.Action{}, false
	}
	declared, ok := a.Manifest().Actions.Action(schema)
	if !ok {
		for _, definition := range h.t.definitions {
			if definition.Ref == (platform.AssetRef{App: a.Manifest().ID, Kind: platform.AssetAction, Name: schema}) && definition.Action != nil && definition.Action.Schema == schema {
				return a.Manifest().ID, *definition.Action, true
			}
		}
	}
	return a.Manifest().ID, declared, ok
}

func (h hostView) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a := h.t.App(h.t.owner["action:"+s.GetSchema().GetName()])
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if declared, ok := a.Manifest().Actions.Action(s.GetSchema().GetName()); ok && declared.Approval != nil && !c.Automation {
		return c.RequestApproval(a.Manifest().ID, s, now)
	}
	return platform.Decide(c, a, s, now)
}

func (h hostView) Attempt(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	decide := func() (*pb.ChangeRecord, *kernel.Error) {
		a := h.t.App(h.t.owner["action:"+s.GetSchema().GetName()])
		if a == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return platform.Decide(c, a, s, now)
	}
	return platform.Attempt(c, decide)
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
	return h.t.directory
}

func (h hostView) Seen(c platform.Caller, keys ...string) {
	if c.Staging() {
		platform.MarkSeen(c, keys...)
		return
	}
	h.t.notices.MarkKeysRead(c.App, keys)
}

func (h hostView) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *kernel.Error) {
	ref, _, err := h.t.router().Invoke(c, protocol, action, id, payload, key, correlation, now)
	return ref, err
}

func (h hostView) Tasks() platform.Tasks {
	if h.decision != nil && h.t.tasks != nil {
		return h.decision.stateApp(h.t.tasks.(platform.App)).(platform.Tasks)
	}
	return h.t.tasks
}

func (h hostView) Processes() platform.Processes {
	if h.decision != nil && h.t.procs != nil {
		return h.decision.stateApp(h.t.procs.(platform.App)).(platform.Processes)
	}
	return h.t.procs
}

func (h hostView) ActiveRelease() string { return h.t.releases.Active() }

func (h hostView) Runs() platform.Runs {
	if h.t.agents == nil {
		return nil
	}
	if h.decision != nil {
		return h.decision.stateApp(h.t.agents).(platform.Runs)
	}
	return h.t.agents
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

func (h hostView) Entity(typ string) (platform.EntityInfo, bool) { return h.t.Entity(typ) }

func (h hostView) EntitySource(typ string) (platform.AssetBinding, bool) {
	for _, definition := range h.t.definitions {
		if definition.Ref.Kind == platform.AssetObject && definition.Ref.Name == typ && definition.Version != "" {
			return platform.AssetBinding{Ref: definition.Ref, SourceVersion: definition.Version}, true
		}
	}
	return platform.AssetBinding{}, false
}

func (h hostView) Declares(name string) bool { return h.t.declares(name) }

func (h hostView) Interfaces() []platform.Interface {
	return h.t.interfaces()
}

func (t *Tenant) interfaces() []platform.Interface {
	seen := map[platform.AssetRef]bool{}
	out := []platform.Interface{}
	for _, definition := range t.definitions {
		if definition.Interface == nil || seen[definition.Ref] {
			continue
		}
		seen[definition.Ref] = true
		out = append(out, *definition.Interface)
	}
	for _, app := range t.apps {
		manifest := app.Manifest()
		for _, shape := range manifest.Interfaces {
			ref := platform.AssetRef{App: manifest.ID, Kind: platform.AssetInterface, Name: shape.Name}
			if seen[ref] {
				continue
			}
			seen[ref] = true
			out = append(out, shape)
		}
	}
	slices.SortFunc(out, func(a, b platform.Interface) int { return strings.Compare(a.Name, b.Name) })
	return out
}

func (h hostView) BindInterfaceQuery(q platform.NamedQuery, drafts ...platform.EntityInfo) (platform.NamedQuery, error) {
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
func (h hostView) Calendar(party string, day platform.Date) platform.Calendar {
	if directory := h.Directory(); directory != nil {
		return directory.Calendar(party, day)
	}
	return platform.Calendar{}
}
func (h hostView) Units(party, structure string, day platform.Date) []string {
	if directory := h.Directory(); directory != nil {
		return directory.Units(party, structure, day)
	}
	return nil
}
func (h hostView) Holders(structure, unit, role string, day platform.Date) []string {
	if directory := h.Directory(); directory != nil {
		return directory.Holders(structure, unit, role, day)
	}
	return nil
}

func (h hostView) RuleQuery(app, name string, version int) (platform.NamedQuery, int, bool) {
	owner := h.t.App(app)
	if owner == nil {
		return platform.NamedQuery{}, 0, false
	}
	if source, ok := owner.(interface {
		QueryDefinition(string, int) (platform.NamedQuery, int, bool)
	}); ok {
		return source.QueryDefinition(name, version)
	}
	if version == 0 {
		q, ok := h.t.namedQuery(app, name)
		return q, 0, ok
	}
	return platform.NamedQuery{}, 0, false
}

func (h hostView) ModelQueryBinding(ref platform.AssetRef) (platform.AssetBinding, bool) {
	for _, d := range h.t.definitions {
		if d.Ref == ref && d.Query != nil {
			return platform.AssetBinding{Ref: ref, SourceVersion: d.Version}, true
		}
	}
	return platform.AssetBinding{}, false
}

func (h hostView) CheckPageModel(c platform.Caller, p platform.Page) error {
	if err := p.CheckModelBindings(h.t.modelVisibleLookup(c.Member), h.t.modelCriteria, c.Rules()); err != nil {
		return err
	}
	return pageowner.CheckModelQueries(p, h.t.definitions, h.t.Entity)
}

func (h hostView) ownerBindings() platform.OwnerBindings {
	return platform.OwnerBindings{Training: h, Agent: h, Console: h, Build: h, Files: h, Knowledge: h, Relations: h, Flow: h, Work: h}
}
func (h hostView) bindOwner(app platform.App) { h.t.services.BindOwner(app, h.ownerBindings()) }

func (h hostView) BuildProcessConsumptions() []platform.Consumption {
	processes := h.t.procs
	if h.decision != nil {
		if app := h.decision.PrivateApps[platform.FlowApp]; app != nil {
			processes = app.(platform.Processes)
		}
	}
	if processes == nil {
		return nil
	}
	return processes.Consumptions("build")
}

func (h hostView) ProjectApprovals(project string) int {
	if access, ok := h.t.App("platform").(interface {
		SnapshotState() platform.AuthorizationState
	}); ok {
		if config := access.SnapshotState().Projects[project]; config != nil {
			return max(1, config.Approvals)
		}
	}
	return 1
}

func (h hostView) CheckResourceSchema(schema string, version *platform.SchemaVersion) *kernel.Error {
	if version == nil {
		return nil
	}
	owner := h.t.App(h.t.owner["action:"+schema])
	if owner == nil {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA, "The action definition is unavailable; refresh definitions")
	}
	_, refusal := h.t.submissionSchemaVersion(owner, &pb.Submission{Schema: &pb.SchemaRef{Name: schema, Version: version.Family}}, version.Version)
	return refusal
}

func (h hostView) CompartmentExists(id string) bool {
	for _, marking := range h.t.App("platform").(interface{ MarkingViews() []platform.Marking }).MarkingViews() {
		if marking.ID == id && !marking.Classification && !marking.Archived {
			return true
		}
	}
	return false
}
