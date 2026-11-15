package host

import (
	"encoding/json"
	"platformserver/kernel/records"
	"platformserver/platform"
)

func (t *Tenant) definitionOwners(includeArchived bool) records.DefinitionOwners {
	owners := t.assetOwners(t.OwnerActive)
	return records.DefinitionOwners{Owners: owners, Active: t.OwnerActive, IncludeArchived: includeArchived,
		DescribeAsset: platform.DescribeAsset,
		InstallationDecoder: func() (func(string, string, json.RawMessage) (platform.AssetInstallation, error), error) {
			registry, err := platform.NewAssetRegistry(owners...)
			if err != nil {
				return nil, err
			}
			return registry.DecodeAssetInstallation, nil
		},
		Publishes: func(app string) bool { return t.assets[app] != nil },
		Reader: func(app string) (platform.AssetDefinitionReader, bool) {
			owner, ok := t.App(app).(platform.AssetDefinitionReader)
			return owner, ok
		}}
}

func (t *Tenant) ownedAssetDefinitions(base []platform.Definition) ([]platform.Definition, error) {
	return t.Projection.OwnedDefinitions(base, t.definitionOwners(t.includeArchivedDefinitions))
}
