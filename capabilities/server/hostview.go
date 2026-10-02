package platformserver

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/host"
	"platformserver/platform"
)

// hostView is the tenant as its own apps see it (internal/host.Host, ADR-0025 D4).
// hostView is the tenant as one of its own apps sees it; app is that app, for
// what it declares at runtime (ADR-0034).
type hostView struct {
	t   *Tenant
	app platform.App
}

// Install declares an entity this app composed at runtime (ADR-0034).
func (h hostView) Install(c platform.Caller, e platform.Entity, actions []platform.Action, pages ...platform.Page) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.t.Install(h.app, e, actions, pages...)
}

// Validation uses the same installation code against a private registry and
// record store; a failed or refused publication cannot change live metadata.
func (h hostView) installationDraft() *Tenant {
	draft := &Tenant{ID: h.t.ID, apps: h.t.apps, owner: maps.Clone(h.t.owner),
		records: h.t.records.forkRecords(), definitions: slices.Clone(h.t.definitions), Files: h.t.files(), procs: h.t.procs}
	return draft
}

func (h hostView) ValidateInstall(e platform.Entity, actions []platform.Action, pages ...platform.Page) error {
	return h.installationDraft().Install(h.app, e, actions, pages...)
}

func (h hostView) ValidateInstallDependents(e platform.Entity, actions []platform.Action, generatedPage string, pages ...platform.Page) error {
	draft := h.installationDraft()
	if err := draft.Install(h.app, e, actions, pages...); err != nil {
		return err
	}
	for _, def := range h.t.definitions {
		if def.LinkType != nil {
			view := hostView{t: draft, app: h.t.app(def.Ref.App)}
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
			view := hostView{t: draft, app: h.t.app(def.Ref.App)}
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
			if err := (hostView{t: draft, app: h.t.app(def.Ref.App)}).ValidateInstallFunction(*def.Function); err != nil {
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
		if def.Ref.App == h.app.Manifest().ID && def.Ref.Kind == platform.AssetAction &&
			def.Action != nil && def.Action.Target == e.Type &&
			!slices.ContainsFunc(actions, func(a platform.Action) bool { return a.Schema == def.Ref.Name }) {
			return fmt.Errorf("published action %s would remain routed after removal", def.Ref.Name)
		}
		if def.Ref.Kind != platform.AssetPage || def.Page == nil ||
			def.Ref.App == h.app.Manifest().ID && def.Ref.Name == generatedPage {
			continue
		}
		p := *def.Page
		if p.Object.Name != e.Type && !slices.ContainsFunc(p.QueryReferences(), func(ref platform.AssetRef) bool { return ref.Kind == platform.AssetObject && ref.Name == e.Type }) && !slices.ContainsFunc(p.Sections, func(s platform.Section) bool {
			return s.Object.Name == e.Type
		}) {
			continue
		}
		for _, ref := range p.Actions {
			if removedAction(ref) {
				return fmt.Errorf("page %s: removed action %s", p.Name, ref.Name)
			}
		}
		for _, section := range p.Sections {
			for _, ref := range section.Actions {
				if removedAction(ref) {
					return fmt.Errorf("page %s: removed action %s", p.Name, ref.Name)
				}
			}
		}
		owner := draft.app(def.Ref.App)
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
	return h.t.protocolEvent(name)
}

func (h hostView) As(c platform.Caller, app string) platform.Caller {
	return platform.RouteCaller(c, app)
}

func (h hostView) Caller(parent platform.Caller, m platform.Member, app string) platform.Caller {
	if !parent.Staging() {
		return platform.NewCaller(runtime{h.t}, m, app, parent.Replaying, false)
	}
	return platform.ActingCaller(parent, m, app, false)
}

func (h hostView) Automation(parent platform.Caller, app string) platform.Caller {
	return h.t.automated(parent, app)
}

func (h hostView) Member(id string) (platform.Member, bool) { return h.t.member(id) }

func (h hostView) Holding(app, role string) []string {
	if d, ok := h.t.app(PlatformApp).(*Console); ok {
		return d.holding(app, role)
	}
	return nil
}

func (h hostView) Action(schema string) (string, platform.Action, bool) {
	a := h.t.owner["action:"+schema]
	if a == nil {
		return "", platform.Action{}, false
	}
	declared, ok := a.Manifest().Actions.Action(schema)
	return a.Manifest().ID, declared, ok
}

func (h hostView) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	a := h.t.owner["action:"+s.GetSchema().GetName()]
	if a == nil {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	}
	if declared, ok := a.Manifest().Actions.Action(s.GetSchema().GetName()); ok && declared.Approval != nil && !c.Automation {
		return c.RequestApproval(a.Manifest().ID, s, now)
	}
	return platform.Decide(c, a, s, now)
}

func (h hostView) Attempt(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	// Work uses Attempt after its approval has completed. Re-entering public
	// Submit here would hold the very same approved submission again.
	decide := func() (*pb.ChangeRecord, *kernel.Error) {
		a := h.t.owner["action:"+s.GetSchema().GetName()]
		if a == nil {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		return platform.Decide(c, a, s, now)
	}
	if c.Staging() {
		return platform.Attempt(c, decide)
	}
	return decide()
}

func (h hostView) Recipients(c platform.Caller, now time.Time, to []platform.Recipient) []string {
	return h.t.recipients(c, now, to)
}

func (h hostView) Directory() host.Directory { return h.t.directory }

func (h hostView) Seen(c platform.Caller, keys ...string) {
	if c.Staging() {
		platform.MarkSeen(c, keys...)
		return
	}
	h.t.opsMu.Lock()
	defer h.t.opsMu.Unlock()
	for i, n := range h.t.notices {
		if n.App == c.App && slices.Contains(keys, n.Key) {
			h.t.notices[i].Read = true
		}
	}
}

func (h hostView) Invoke(c platform.Caller, protocol, action, id string, payload []byte, key, correlation string, now time.Time) (*pb.EntityRef, *kernel.Error) {
	ref, _, err := h.t.invoke(c, protocol, action, id, payload, key, correlation, now)
	return ref, err
}

func (h hostView) Tasks() host.Tasks { return h.t.tasks }

func (h hostView) Processes() host.Processes { return h.t.procs }

// ActiveRelease is read inside a decision, which already holds the tenant lock.
func (h hostView) ActiveRelease() string { return h.t.activeRelease }

func (h hostView) Runs() host.Runs {
	if h.t.agents == nil {
		return nil
	}
	return h.t.agents
}

// InstallPage offers a page composed in this tenant (ADR-0034).
func (h hostView) InstallPage(c platform.Caller, p platform.Page) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.t.InstallPage(h.app, p)
}
func (h hostView) ValidateInstallPage(p platform.Page) error {
	return h.installationDraft().InstallPage(h.app, p)
}

// InstallApplication offers an application handed over in this tenant (ADR-0036).
func (h hostView) InstallApplication(c platform.Caller, a platform.Application) error {
	if c.Staging() {
		unsupportedStagedEffect()
	}
	return h.t.InstallApplication(h.app, a)
}
func (h hostView) ValidateInstallApplication(a platform.Application) error {
	return h.installationDraft().InstallApplication(h.app, a)
}

// Entity is an entity type's declaration.
func (h hostView) Entity(typ string) (platform.EntityInfo, bool) { return h.t.entity(typ) }

func (h hostView) Declares(name string) bool { return h.t.declares(name) }

func (h hostView) Readable(m platform.Member, ref string, now time.Time) bool {
	return h.t.Readable(m, ref, now)
}

func (h hostView) Stored(tenant, hash string) bool {
	return h.t.files().Exists(context.Background(), tenant+"/"+hash)
}

func (h hostView) Record(ref string) (any, bool) { return h.t.Held(ref) }
