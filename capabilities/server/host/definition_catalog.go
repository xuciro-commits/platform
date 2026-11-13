package host

import (
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (t *Tenant) definitionOwners(includeArchived bool) records.DefinitionOwners {
	return records.DefinitionOwners{Owners: t.assetOwners(t.OwnerActive), Active: t.OwnerActive, IncludeArchived: includeArchived,
		Publishes: func(app string) bool { return t.assets[app] != nil },
		Reader: func(app string) (platform.AssetDefinitionReader, bool) {
			owner, ok := t.App(app).(platform.AssetDefinitionReader)
			return owner, ok
		}}
}

func (t *Tenant) ownedMetadataKind(ref platform.AssetRef) bool {
	return t.definitionOwners(false).Metadata(ref)
}

func (t *Tenant) ownedAssetDefinitions(base []platform.Definition) ([]platform.Definition, error) {
	return t.ownedAssetDefinitionsWithArchived(base, t.includeArchivedDefinitions)
}

func (t *Tenant) ownedAssetDefinitionsWithArchived(base []platform.Definition, includeArchived bool) ([]platform.Definition, error) {
	return t.Projection.OwnedDefinitions(base, t.definitionOwners(includeArchived))
}
