package ontology

import (
	"reflect"

	"platformserver/platform"
)

func CheckObjectConstraints(o Object, entity platform.Entity, lookup func(string) (platform.EntityInfo, bool), checkCollections func(platform.RuleReadSpec, string, func(string) (platform.EntityInfo, bool)) error, engines ...platform.RuleEngine) error {
	if len(o.Constraints) == 0 {
		return nil
	}
	info, err := platform.Describe(AppID, entity, func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup = RuleLookup(o, AppID, BuilderRole, lookup)
	if err := platform.CheckConstraints(info, lookup, true, engines...); err != nil {
		return err
	}
	for _, constraint := range o.Constraints {
		if err := checkCollections(platform.ConstraintSpec(info, constraint), "constraint "+constraint.Name, lookup); err != nil {
			return err
		}
	}
	return nil
}
