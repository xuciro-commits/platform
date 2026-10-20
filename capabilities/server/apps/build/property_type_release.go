package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

func propertyTypeReleaseAsset(f PropertyType, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: construction.ID, Kind: platform.AssetPropertyType, Name: f.Name}, sourceVersion+".property-"+strconv.Itoa(f.Version), f.Definition())
}

// PropertyTypeReleaseAsset resolves only a retained version from this owner.
func (b *Build) PropertyTypeReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "property", "property type", platform.AssetPropertyType, name, sourceVersion, b.PropertyTypeDefinition)
}

func (b *Build) propertyTypeAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.propertyTypes, func(record PropertyType) (platform.ReleaseAsset, error) {
		return propertyTypeReleaseAsset(record, b.Manifest().Version)
	})
}

// InstallPropertyTypeAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallPropertyTypeAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetPropertyType, "property")
	var declaration platform.PropertyType
	if version == 0 || asset.ContractVersion != 1 || json.Unmarshal(asset.Body, &declaration) != nil || declaration.Check() != nil || declaration.Name != asset.Ref.Name {
		return fmt.Errorf("invalid builder property type asset %s", asset.Ref)
	}
	f := propertyTypeFromDeclaration(declaration)
	if _, exists := b.propertyTypes[f.Name]; exists {
		return fmt.Errorf("propertyType asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installPropertyType(platform.Caller{Replaying: true}, f)
}

func (b *Build) propertyTypeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetPropertyType, "property type", b.propertyTypeInventory, func(record PropertyType) (PropertyType, error) {
		err := b.checkPropertyType(&record)
		return record, err
	}, func(record PropertyType) (platform.ReleaseAsset, error) {
		record.Version++
		return propertyTypeReleaseAsset(record, b.Manifest().Version)
	})
}

// Freeze the immutable candidate beside the mutable editor row. Existing
// ordinals must match retained bytes; only the next ordinal can publish.
func (b *Build) preparePropertyTypeReleasePublications(assets []platform.ReleaseAsset) ([]construction.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.propertyTypeInventory, platform.AssetPropertyType, "property", SchemaPropertyType, "property type", b.PropertyTypeReleaseAsset, func(record PropertyType, asset platform.ReleaseAsset, version int) (PropertyType, error) {
		var q platform.PropertyType
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Type != record.Type {
			return record, fmt.Errorf("invalid next property type version")
		}
		frozen := propertyTypeFromDeclaration(q)
		frozen.Record = record.Record
		frozen.State = "published"
		frozen.Version = version
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}

func propertyTypeFromDeclaration(p platform.PropertyType) PropertyType {
	return PropertyType{Name: p.Name, Title: p.Title, Description: p.Description, Type: p.Type}
}
