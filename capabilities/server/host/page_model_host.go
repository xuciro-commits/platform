package host

import (
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	"platformserver/services/objects"
)

type pageModelHost struct{ t *Tenant }

func (t *Tenant) pageModels() pageowner.ModelReader {
	return pageowner.NewModelReader(pageModelHost{t})
}

func (h pageModelHost) ModelMember(m platform.Member) (platform.Member, *kernel.Error) {
	m = h.t.authorizationMember(m)
	return m, h.t.Admits(m)
}

func (h pageModelHost) AuthorizePreview(m platform.Member) *kernel.Error {
	caller := platform.NewCaller(runtime{t: h.t, scoped: true}, m, "build", false, false)
	return authorizationRefusal(h.t.authorizeNative(caller, "POST /v1/page-model/preview", "read", []string{"builder"}, true))
}

func (h pageModelHost) ModelCaller(m platform.Member, app string) platform.Caller {
	return platform.NewCaller(runtime{t: h.t, scoped: true}, m, app, false, false)
}

func (h pageModelHost) FilterCaller(c platform.Caller) platform.Caller {
	return platform.NewCaller(pageModelRuntime{runtime{t: h.t, scoped: true}}, c.Member, c.App, c.Replaying, c.Automation)
}

func (h pageModelHost) Entity(typ string) (platform.EntityInfo, bool) { return h.t.Entity(typ) }
func (h pageModelHost) Definitions(m platform.Member) []platform.Definition {
	return h.t.Definitions(m)
}
func (h pageModelHost) AllDefinitions() []platform.Definition { return h.t.definitions }
func (h pageModelHost) ModelQueryBinding(ref platform.AssetRef) (platform.AssetBinding, bool) {
	return (hostView{t: h.t}).ModelQueryBinding(ref)
}
func (h pageModelHost) ModelCriteria(ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	return h.t.modelCriteria(ref)
}
func (h pageModelHost) Rules() platform.RuleEngine { return h.t.services.Rules }
func (h pageModelHost) VisibleLookup(m platform.Member) func(string) (platform.EntityInfo, bool) {
	return h.t.modelVisibleLookup(m)
}
func (h pageModelHost) RuleEntities() map[string]platform.EntityInfo {
	return h.t.Projection.EntityInfos()
}
func (h pageModelHost) RuleRecord(typ, id string, fallback any) any {
	if row, ok := h.t.Projection.LockedRow(typ, id); ok {
		return row
	}
	return fallback
}
func (h pageModelHost) LoadObjectSet(c platform.Caller, set platform.ObjectSet, w objects.Window, at time.Time, bound ...platform.NamedQuery) (objects.RecordPage, string, *kernel.Error) {
	return h.t.loadObjectSet(h.t.Projection, c, set, w, at, bound...)
}
