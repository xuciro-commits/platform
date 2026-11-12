package host

import (
	"encoding/json"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
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
	return r.t.authorizeDepth(r.t.Projection, c, q, at, true, 0, newAuthorizationWalk())
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
	if len(path) == 0 || len(path) > 16 {
		return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record paths need 1–16 fields")
	}
	var sources []string
	for i, name := range path {
		raw, refusal := t.heldRecordJSON(c, typ, id, at)
		if refusal != nil {
			return nil, nil, refusal
		}
		entity := t.Projection.Type(typ)
		if entity == nil {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Record path object is unavailable")
		}
		info := entity.Info()
		field, declared := info.Field(name)
		var fields map[string]json.RawMessage
		_ = json.Unmarshal(raw, &fields)
		value, visible := fields[name]
		if !declared || !field.ReadsAny(c.RolesIn(info.App), c.Rules()) {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		permission := c.Authorize(platform.AuthzRequest{Principal: c.ID, Tenant: c.Tenant, Kind: "read", Type: typ, Record: id, Fields: []string{name}}, at)
		if !permission.Allow || len(permission.Withheld) > 0 {
			return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Record input is unavailable to this member")
		}
		ref := typ + "/" + id
		sources = append(sources, ref, ref+"#"+name)
		if !visible {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input has no value")
		}
		if i == len(path)-1 {
			return value, sources, nil
		}
		if field.Type != "reference" || field.Ref == "" {
			result, err := (platform.Binding{Source: "subject", Path: path[i+1:]}).Resolve(&platform.Run{}, value)
			if err != nil {
				return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, err.Error())
			}
			return result, sources, nil
		}
		if json.Unmarshal(value, &id) != nil || id == "" {
			return nil, sources, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input reference is empty")
		}
		typ = field.Ref
	}
	return nil, nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Record input is unavailable")
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
