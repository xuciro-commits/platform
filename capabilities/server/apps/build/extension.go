package build

import (
	"encoding/json"
	"fmt"
	"reflect"

	"platformserver/platform"
)

func (b *Build) captureExtensionSource(object *Object) error {
	if object.Extends == "" {
		object.ExtensionSource = nil
		return b.checkExtensionDependents(*object)
	}
	base, exists := b.lookupEntity(object.Extends)
	if !exists || b.host == nil {
		return fmt.Errorf("extension base %s is unavailable", object.Extends)
	}
	binding, exists := b.host.EntitySource(object.Extends)
	if !exists && base.App == ID {
		if _, selected := b.jointEntities[base.Type]; selected {
			binding, exists = platform.AssetBinding{Ref: platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: base.Type}, SourceVersion: b.Manifest().Version}, true
		}
	}
	if !exists {
		return fmt.Errorf("extension base %s has no declared package version", object.Extends)
	}
	// Copy the declaration, including nested field descriptors, so authoring
	// and installed registry slices cannot change publication provenance.
	raw, err := json.Marshal(platform.ExtensionSource{Base: binding, Fields: base.Fields})
	if err != nil {
		return err
	}
	var source platform.ExtensionSource
	if err := json.Unmarshal(raw, &source); err != nil {
		return err
	}
	object.ExtensionSource = &source
	info, err := platform.Describe(ID, Entity(*object), func(reflect.Type) string { return "" })
	if err != nil {
		return err
	}
	return platform.CheckExtension(info, base)
}

func (b *Build) checkExtensionDependents(object Object) error {
	objects, _, _, err := b.releaseInventory()
	if err != nil {
		return err
	}
	var base platform.EntityInfo
	for _, record := range objects {
		published, exists := wasPublished[Object](record.Published)
		if !exists || published.Extends != ID+"."+object.Name {
			continue
		}
		if base.Type == "" {
			base, err = platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
			if err != nil {
				return err
			}
		}
		extension, err := platform.Describe(ID, Entity(published), func(reflect.Type) string { return "" })
		if err != nil {
			return err
		}
		if next, selected := b.jointEntities[extension.Type]; selected {
			extension = next
			extension.ExtensionSource = nil // explicit joint publication captures its new base below
		}
		if err := platform.CheckExtension(extension, base); err != nil {
			return err
		}
	}
	return nil
}
