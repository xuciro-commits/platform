package host

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/records"
	"platformserver/platform"
	agentowner "platformserver/services/ai/runtime"
	pageowner "platformserver/services/apps/pages"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/publication"
)

func PublicationResolver() publication.Resolver {
	return agentowner.WithPublicationResolver(pageowner.PublicationResolver())
}

func (h hostView) ValidateCodeAsset(asset platform.ReleaseAsset) error {
	return releaseowner.ValidateCodeAsset(asset, h.t.App, h.t.codeReleaseAssetsLocked)
}
func (t *Tenant) assetRegistryLocked() (*publication.AssetRegistry, error) {
	return releaseowner.NewRegistry(PublicationResolver(), t.LiveApps(), t.codeReleaseAssetsLocked, func(app platform.App, ref platform.AssetRef, source string) (platform.ReleaseAsset, error) {
		for _, flow := range app.Manifest().Flows {
			asset, err := t.nativeFlowAsset(app, flow.Name, flow.Version)
			if err != nil {
				return asset, err
			}
			if asset.Ref == ref && asset.SourceVersion == source {
				return asset, nil
			}
		}
		return platform.ReleaseAsset{}, fmt.Errorf("native flow is not retained by its code owner")
	})
}

func (t *Tenant) definitionOwners(includeArchived bool) records.DefinitionOwners {
	owners := t.assetOwners(t.OwnerActive)
	return records.DefinitionOwners{Owners: owners, Active: t.OwnerActive, IncludeArchived: includeArchived,
		DescribeAsset: PublicationResolver().DescribeAsset,
		InstallationDecoder: func() (func(string, string, json.RawMessage) (platform.AssetInstallation, error), error) {
			registry, err := PublicationResolver().NewAssetRegistry(owners...)
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
