package build

import (
	"encoding/json"
	"reflect"

	"platformserver/platform"
)

func sameRuleAsset(left, right platform.ReleaseAsset) bool {
	var a, b any
	return json.Unmarshal(platform.Raw(left), &a) == nil && json.Unmarshal(platform.Raw(right), &b) == nil && reflect.DeepEqual(a, b)
}

func (b *Build) checkConstraints(o Object, engines ...platform.RuleEngine) error {
	if len(o.Constraints) == 0 {
		return nil
	}
	info, err := platform.Describe(ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := ruleLookup(o, b.conditionLookup)
	if err := platform.CheckConstraints(info, lookup, true, engines...); err != nil {
		return err
	}
	for _, constraint := range o.Constraints {
		if err := b.checkRuleCollections(platform.ConstraintSpec(info, constraint), "constraint "+constraint.Name, lookup); err != nil {
			return err
		}
	}
	return nil
}

const (
	Integrator = "integrator"
)
