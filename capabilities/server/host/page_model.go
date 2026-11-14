package host

import (
	"encoding/json"
	"platformserver/services/security"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
)

func (t *Tenant) modelTransition(ref platform.AssetRef) (platform.Transition, string, bool) {
	return pageowner.ModelTransition(t.App(t.owner["action:"+ref.Name]), ref)
}

func (t *Tenant) modelCriteria(ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	return pageowner.ModelCriteria(t.App(t.owner["action:"+ref.Name]), ref)
}

func (t *Tenant) modelVisibleLookup(member platform.Member) func(string) (platform.EntityInfo, bool) {
	return pageowner.VisibleLookup(t.Entities(member))
}

type pageModelRuntime struct{ runtime }

func (r pageModelRuntime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	return r.t.authorizeDepth(r.t.Projection, c, q, at, true, 0, security.NewTraversal())
}

func (r pageModelRuntime) ReadRecordPath(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	t := r.t
	return pageowner.RecordPath{Record: t.objectOwner(t.Projection).HeldRecordJSON, Entity: func(typ string) (platform.EntityInfo, bool) {
		entity := t.Projection.Type(typ)
		if entity == nil {
			return platform.EntityInfo{}, false
		}
		return entity.Info(), true
	}}.Read(c, typ, id, path, at)
}
