package build

import "platformserver/internal/construction"

import (
	"platformserver/platform"
	"reflect"
)

func (b *Build) checkConstraints(o construction.Object) error {
	if len(o.Constraints) == 0 {
		return nil
	}
	info, err := platform.Describe(construction.ID, Entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := ruleLookup(o, b.conditionLookup)
	if err := platform.CheckConstraints(info, lookup, true); err != nil {
		return err
	}
	for _, constraint := range o.Constraints {
		if err := b.checkRuleCollections(platform.ConstraintSpec(info, constraint), "constraint "+constraint.Name, lookup); err != nil {
			return err
		}
	}
	return nil
}
