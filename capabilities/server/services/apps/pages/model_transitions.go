package pages

import "platformserver/platform"

func ModelTransition(owner platform.App, ref platform.AssetRef) (platform.Transition, string, bool) {
	if ref.Kind != platform.AssetAction || owner == nil || owner.Manifest().ID != ref.App {
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

func ModelCriteria(owner platform.App, ref platform.AssetRef) (*platform.RuleCriteria, bool) {
	transition, _, ok := ModelTransition(owner, ref)
	if !ok || transition.Criteria == nil {
		return nil, false
	}
	return transition.Criteria, true
}

func VisibleLookup(visible []platform.EntityInfo) func(string) (platform.EntityInfo, bool) {
	entities := map[string]platform.EntityInfo{}
	for _, entity := range visible {
		entities[entity.Type] = entity
	}
	return func(typ string) (platform.EntityInfo, bool) {
		value, ok := entities[typ]
		return platform.ConstraintEntity(value), ok
	}
}
