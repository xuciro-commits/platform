package build

import (
	"fmt"
	"platformserver/platform"
	"reflect"
)

// Reinstall retained sources before their pin consumers. Ordinary recursive
// data references retain their existing installation semantics.
func businessVersionInstallOrder(objects []Object) ([]Object, error) {
	byType := map[string]Object{}
	for _, record := range objects {
		if object, ok := wasPublished[Object](record.Published); ok && !record.Archived {
			byType["build."+object.Name] = object
		}
	}
	status := map[string]int{}
	ordered := []Object{}
	var visit func(string) error
	visit = func(typ string) error {
		if status[typ] == 2 {
			return nil
		}
		if status[typ] == 1 {
			return fmt.Errorf("business version pin definitions form a cycle")
		}
		object, known := byType[typ]
		if !known {
			return nil
		}
		status[typ] = 1
		for _, pin := range object.VersionPins {
			for _, field := range object.Fields {
				if field.Name == pin.Reference {
					if err := visit(field.Ref); err != nil {
						return err
					}
				}
			}
		}
		status[typ] = 2
		ordered = append(ordered, object)
		return nil
	}
	for _, record := range objects {
		if err := visit("build." + record.Name); err != nil {
			return nil, err
		}
	}
	return ordered, nil
}

func (b *Build) checkVersioning(o Object) error {
	if o.Versioning == nil && len(o.VersionPins) == 0 {
		return nil
	}
	info, err := platform.Describe(ID, b.entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := func(typ string) (platform.EntityInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return b.conditionLookup(typ)
	}
	if err := platform.CheckVersioning(info, lookup); err != nil {
		return err
	}
	if old, ok := wasPublished[Object](o.Published); ok && (old.Versioning != nil && !reflect.DeepEqual(old.Versioning, o.Versioning) || len(old.VersionPins) > 0 && !reflect.DeepEqual(old.VersionPins, o.VersionPins)) {
		return fmt.Errorf("published business versioning and pin bindings are retained")
	}
	return nil
}
