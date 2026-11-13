package host

import (
	"encoding/json"
	"platformserver/services/security"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	pageowner "platformserver/services/apps/pages"
)

func (t *Tenant) modelTransition(ref platform.AssetRef) (platform.Transition, string, bool) {
	if ref.Kind != platform.AssetAction {
		return platform.Transition{}, "", false
	}
	owner := t.App(t.owner["action:"+ref.Name])
	if owner == nil || owner.Manifest().ID != ref.App {
		return platform.Transition{}, "", false
	}
	for _, entity := range owner.Manifest().Entities {
		if entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Transitions {
			if entity.Type+"."+transition.Name == ref.Name {
				return transition, entity.Lifecycle.Field, true
			}
		}
	}
	return platform.Transition{}, "", false
}

func (t *Tenant) modelCriteria(ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	transition, _, ok := t.modelTransition(ref)
	if !ok || transition.Criteria == nil {
		return nil, false
	}
	return transition.Criteria, true
}

func (t *Tenant) modelVisibleLookup(member platform.Member) func(string) (platform.EntityInfo, bool) {
	entities := map[string]platform.EntityInfo{}
	for _, entity := range t.Entities(member) {
		entities[entity.Type] = entity
	}
	return func(typ string) (platform.EntityInfo, bool) {
		value, ok := entities[typ]
		return platform.ConstraintEntity(value), ok
	}
}

type pageModelRuntime struct{ runtime }

func (r pageModelRuntime) Authorize(c platform.Caller, q platform.AuthzRequest, at time.Time) platform.AuthzDecision {
	return r.t.authorizeDepth(r.t.Projection, c, q, at, true, 0, security.NewTraversal())
}

func (r pageModelRuntime) ReadRecordPath(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	return r.t.readRecordPathHeld(c, typ, id, path, at)
}

func (t *Tenant) ruleEntitySnapshot() map[string]platform.EntityInfo {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	out := map[string]platform.EntityInfo{}
	for _, entity := range t.Projection.Types() {
		out[entity.Info().Type] = entity.Info()
	}
	return out
}

func (t *Tenant) readRecordPathHeld(c platform.Caller, typ, id string, path []string, at time.Time) (json.RawMessage, []string, *kernel.Error) {
	return pageowner.RecordPath{Record: t.heldRecordJSON, Entity: func(typ string) (platform.EntityInfo, bool) {
		entity := t.Projection.Type(typ)
		if entity == nil {
			return platform.EntityInfo{}, false
		}
		return entity.Info(), true
	}}.Read(c, typ, id, path, at)
}

func (t *Tenant) heldRecordJSON(c platform.Caller, typ, id string, at time.Time) (json.RawMessage, *kernel.Error) {
	entity := t.Projection.Type(typ)
	if entity == nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path object is unavailable")
	}
	visible, refusal := t.visibleForCallerIn(t.Projection, c, entity, at)
	if refusal != nil {
		return nil, refusal
	}
	row := entity.Rows().Get(id)
	if row == nil || visible != nil && !visible(row.Value) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path record is unavailable")
	}
	_, hidden := viewOf(c.Member, entity)
	record := records.Mask(entity.Info(), row.Value, hidden)
	if entity.Info().HasReadSources() || len(entity.Info().VersionPins) > 0 {
		narrowed, _ := t.narrowerIn(t.Projection, c.Member, at, true).Derive(entity, reflect.ValueOf(record))
		record = narrowed.Interface()
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
	}
	return raw, nil
}
