package host

import (
	"encoding/json"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
	"platformserver/services/security"
	"time"
)

func (t *Tenant) modelTransition(ref platform.AssetRef) (platform.ActionType, string, bool) {
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

func (r pageModelRuntime) ReadRecordPath(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *platform.Error) {
	t := r.t
	return pageowner.RecordPath{Record: t.objectOwner(t.Projection).HeldRecordJSON, Entity: func(typ string) (platform.EntityInfo, bool) {
		entity := t.Projection.Type(typ)
		if entity == nil {
			return platform.EntityInfo{}, false
		}
		return entity.Info(), true
	}}.Read(c, typ, id, path, at)
}

func (t *Tenant) pageOperation(binding *platform.AssetBinding) (platform.Operation, int, error) {
	return pageowner.PinnedOperation{App: t.App, Definition: t.operationDefinition, Registry: t.assetRegistryLocked}.Resolve(binding)
}

func (t *Tenant) PageContentDefinition(m platform.Member, ref platform.AssetRef, version string) (platform.Definition, *platform.Error) {
	return pageowner.ContentHost{Admits: t.Admits, Current: func() []platform.Definition { return t.definitions }, Visible: t.Definitions,
		Pinned: func(ref platform.AssetRef, version, content string) (platform.ReleaseAsset, error) {
			registry, err := t.assetRegistryLocked()
			if err != nil {
				return platform.ReleaseAsset{}, err
			}
			return registry.ResolvePinned(ref, version, content)
		},
		Registered:  func() ([]platform.Definition, error) { return t.ownedAssetDefinitions(t.definitions) },
		Definitions: t.definitionsFrom,
	}.Definition(m, ref, version)
}
