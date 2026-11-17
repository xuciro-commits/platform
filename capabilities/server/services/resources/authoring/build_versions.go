package build

import (
	"fmt"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"

	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
	"reflect"
	"slices"
)

func (b *Build) checkVersioning(o ontology.Object) error {
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
	if old, ok := publication.Read[ontology.Object](o.Published); ok && (old.Versioning != nil && !reflect.DeepEqual(old.Versioning, o.Versioning) || len(old.VersionPins) > 0 && !reflect.DeepEqual(old.VersionPins, o.VersionPins)) {
		return fmt.Errorf("published business versioning and pin bindings are retained")
	}
	return nil
}

func (b *Build) captureObjectSchema(object *ontology.Object) error {
	portable, err := b.data.PortableObject(*object)
	if err != nil {
		return err
	}
	body, err := ontology.ObjectSchemaBody(portable)
	if err != nil {
		return err
	}
	version, err := publication.SchemaContentVersion(body)
	if err != nil {
		return err
	}
	var versions []platform.SchemaVersion
	family := uint32(1)
	if previous, exists := publication.Read[ontology.Object](object.Published); exists {
		versions = slices.Clone(previous.SchemaVersions)
		if len(versions) == 0 {
			portable, err := b.data.PortableObject(previous)
			if err != nil {
				return err
			}
			old, err := ontology.ObjectSchemaBody(portable)
			if err != nil {
				return err
			}
			identity, err := publication.SchemaContentVersion(old)
			if err != nil {
				return err
			}
			versions = append(versions, platform.SchemaVersion{Version: identity, Family: 1})
		}
		family = versions[len(versions)-1].Family
		if !ontology.CompatibleObjectSchema(previous, *object) {
			if family >= 64 {
				return fmt.Errorf("schema family limit reached")
			}
			family++
		}
	}
	for _, retained := range versions {
		if err := retained.Check(); err != nil {
			return err
		}
	}
	if len(versions) > 0 && versions[len(versions)-1] == (platform.SchemaVersion{Version: version, Family: family}) {
		object.SchemaVersions = versions
		return nil
	}
	if len(versions) >= 64 {
		return fmt.Errorf("an object may retain at most 64 schema versions")
	}
	object.SchemaVersions = append(versions, platform.SchemaVersion{Version: version, Family: family})
	return nil
}

func objectActions(object ontology.Object, entity platform.Entity) []platform.Action {
	actions := actioncatalog.EntityActions(entity)
	var binding platform.SchemaVersion
	if len(object.SchemaVersions) > 0 {
		binding = object.SchemaVersions[len(object.SchemaVersions)-1]
	} else if body, err := ontology.ObjectSchemaBody(object); err == nil {
		binding.Version, _ = publication.SchemaContentVersion(body)
		binding.Family = 1
	}
	if binding.Check() == nil {
		for index := range actions {
			version := binding
			actions[index].SchemaVersion = &version
		}
	}
	return actions
}
