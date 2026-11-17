package build

import (
	"platformserver/services/ontology"
	"reflect"

	"platformserver/platform"
)

func (b *Build) checkConstraints(o ontology.Object, engines ...platform.RuleEngine) error {
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
