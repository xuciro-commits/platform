package pages

import (
	ruleprotocol "platformserver/kernel/protocol"
	"platformserver/platform"
)

func ModelTransition(owner platform.Service, ref platform.AssetRef) (platform.ActionType, string, bool) {
	if ref.Kind != platform.AssetAction || owner == nil || owner.Manifest().ID != ref.App {
		return platform.ActionType{}, "", false
	}
	for _, entity := range owner.Manifest().Entities {
		if entity.Lifecycle == nil {
			continue
		}
		for _, transition := range entity.Lifecycle.Actions {
			if entity.Type+"."+transition.Name == ref.Name {
				return transition, entity.Lifecycle.Field, true
			}
		}
	}
	return platform.ActionType{}, "", false
}

func ModelCriteria(owner platform.Service, ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	transition, _, ok := ModelTransition(owner, ref)
	if !ok || transition.Criteria.Business == nil {
		return nil, false
	}
	return transition.Criteria.Business, true
}

func VisibleLookup(visible []platform.RecordTypeInfo) func(string) (platform.RecordTypeInfo, bool) {
	entities := map[string]platform.RecordTypeInfo{}
	for _, entity := range visible {
		entities[entity.Type] = entity
	}
	return func(typ string) (platform.RecordTypeInfo, bool) {
		value, ok := entities[typ]
		return ruleprotocol.ConstraintEntity(value), ok
	}
}
