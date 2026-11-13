package host

import (
	"platformserver/kernel/records"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/objects"
	"platformserver/services/resources/authoring"
	"slices"
	"strings"
	"time"
)

func viewOf(m platform.Member, et *records.Type) (*records.Type, []platform.FieldInfo) {
	return objects.ViewOf(m, et)
}

func (t *Tenant) Entities(m platform.Member) []platform.EntityInfo {
	gen, key := t.definitionsFingerprint(), transport.Rights(m)
	if out, ok := t.viewCache().Entities(gen, key); ok {
		return out
	}
	out := t.entityView(m)
	t.viewCache().StoreEntities(gen, key, out)
	return out
}

func (t *Tenant) entityView(m platform.Member) []platform.EntityInfo {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	out := []platform.EntityInfo{}
	for _, et := range t.Projection.Types() {
		if build.ProjectDelegated(m) && et.Info().App == build.ID && !build.BranchDraftType(et.Info().Type) && et.Info().Type != build.ResourceType && et.Info().Type != build.FolderType && et.Info().Type != build.WorkingStateType && et.Info().Type != build.BranchType {
			if resources := t.Projection.Type(build.ResourceType); resources != nil {
				if row := resources.Rows().Get(platform.ResourceID(t.ID, platform.AssetRef{App: build.ID, Kind: platform.AssetObject, Name: et.Info().Type})); row != nil {
					if role := t.ResourceRole(m, row.Value.Interface().(build.Resource).Project); role == "" || role == platform.ProjectDiscovererRole {
						continue
					}
				}
			}
		}
		roles := m.RolesIn(et.Info().App)
		if len(roles) > 0 && et.Info().Scope.LevelFor(roles, t.services.Authorization) == platform.ScopeNone && et.Info().Scope.Participants == nil {
			continue
		}
		if len(roles) > 0 || et.Info().Scope.Participants != nil || et.Info().Scope.Through != nil {
			view, _ := viewOf(m, et)
			out = append(out, view.Info())
		}
	}
	slices.SortFunc(out, func(a, b platform.EntityInfo) int { return strings.Compare(a.Type, b.Type) })
	return out
}

func (t *Tenant) readableIn(store *records.Store, m platform.Member, ref string, now time.Time) bool {
	return t.readableAs(store, platform.NewCaller(runtime{t: t}, m, "", false, false), ref, now)
}

func (t *Tenant) readableAs(store *records.Store, c platform.Caller, ref string, now time.Time) bool {
	typ, id, _ := strings.Cut(ref, "/")
	if typ == "enterprise.element" && t.directory != nil {
		_, ok := t.directory.Element(id, now.UTC().Format(time.DateOnly))
		return ok
	}
	et := store.Type(typ)
	if et == nil {
		return false
	}
	r := et.Rows().Get(id)
	if r == nil {
		return false
	}
	visible, err := t.visibleForCallerIn(store, platform.NewCaller(runtime{t: t}, c.Member, et.Info().App, c.Replaying, c.Automation), et, now)
	return err == nil && (visible == nil || visible(r.Value))
}

func (t *Tenant) readable(m platform.Member, ref string, now time.Time) bool {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return t.readableIn(t.Projection, m, ref, now)
}
