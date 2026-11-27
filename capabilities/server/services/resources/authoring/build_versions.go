package authoring

import (
	"fmt"
	"platformserver/kernel/protocol"
	"platformserver/services/ontology"
	"platformserver/services/resources/publication"

	"platformserver/kernel/records"
	"platformserver/platform"
	actioncatalog "platformserver/services/actions/catalog"
	"reflect"
	"slices"
)

func (b *Build) checkVersioning(o ontology.Object) error {
	if o.Versioning == nil && len(o.VersionPins) == 0 {
		return nil
	}
	info, err := records.Describe(ID, b.entity(o), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	lookup := func(typ string) (platform.RecordTypeInfo, bool) {
		if typ == info.Type {
			return info, true
		}
		return b.conditionLookup(typ)
	}
	if err := protocol.CheckVersioning(info, lookup); err != nil {
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
	var closures map[string][]string
	if previous, exists := publication.Read[ontology.Object](object.Published); exists {
		closures = protocol.MergeFlowClosures(object.RetainedFlowClosures, previous.RetainedFlowClosures)
		if b.host != nil {
			active, activeErr := b.host.UnboundFlowClosures(previous.Type())
			if activeErr != nil {
				return activeErr
			}
			closures = protocol.MergeFlowClosures(closures, active)
		}
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
	object.RetainedFlowClosures = closures
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

func objectActions(object ontology.Object, entity platform.RecordType) []platform.Action {
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
	for _, declared := range object.Actions {
		if len(declared.Uses) == 0 {
			continue
		}
		for index := range actions {
			if actions[index].Schema == entity.Type+"."+declared.Name {
				actions[index].Uses = slices.Clone(declared.Uses)
			}
		}
	}
	return actions
}

func HasRetainedObjectFlowClosure(store *records.Store, ref platform.AssetRef, version int, dependencies string) bool {
	if store == nil || dependencies == "" {
		return false
	}
	et := store.Type(ObjectType)
	if et == nil {
		return false
	}
	for _, row := range et.Rows().All() {
		obj, _ := row.Value.Interface().(ontology.Object)
		if protocol.HasFlowClosure(obj.RetainedFlowClosures, ref, version, dependencies) {
			return true
		}
		if pub, ok := publication.Read[ontology.Object](obj.Published); ok && protocol.HasFlowClosure(pub.RetainedFlowClosures, ref, version, dependencies) {
			return true
		}
	}
	return false
}
