package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

func queryReleaseAsset(f Query, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: f.Name}, sourceVersion+".query-"+strconv.Itoa(f.Version), f.Definition())
}

// QueryReleaseAsset resolves only a retained version from this owner.
func (b *Build) QueryReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "query", "query", platform.AssetQuery, name, sourceVersion, b.QueryDefinition)
}

func (b *Build) queryAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.queries, func(record Query) (platform.ReleaseAsset, error) {
		return queryReleaseAsset(record, b.Manifest().Version)
	})
}

// InstallQueryAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallQueryAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetQuery, "query")
	var f Query
	if version == 0 || asset.ContractVersion != 1 ||
		json.Unmarshal(asset.Body, &f) != nil || f.Name != asset.Ref.Name ||
		f.Definition().Check() != nil {
		return fmt.Errorf("invalid builder query asset %s", asset.Ref)
	}
	if _, exists := b.queries[f.Name]; exists {
		return fmt.Errorf("query asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installQuery(platform.Caller{Replaying: true}, f)
}

func (b *Build) queryDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetQuery, "query", b.queryInventory, func(record Query) (Query, error) {
		return b.bindQuery(record)
	}, func(record Query) (platform.ReleaseAsset, error) {
		record.Version++
		return queryReleaseAsset(record, b.Manifest().Version)
	})
}

// Freeze the immutable candidate beside the mutable editor row. Existing
// ordinals must match retained bytes; only the next ordinal can publish.
func (b *Build) prepareQueryReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.queryInventory, platform.AssetQuery, "query", SchemaQuery, "query", b.QueryReleaseAsset, func(record Query, asset platform.ReleaseAsset, version int) (Query, error) {
		var q platform.NamedQuery
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Object != record.Object || q.Interface != record.Interface {
			return record, fmt.Errorf("invalid next query version")
		}
		frozen := Query{Record: record.Record, Name: q.Name, Title: q.Title, Description: q.Description, Object: q.Object, Interface: q.Interface, InterfaceShape: q.InterfaceShape, Implementations: q.Implementations, By: q.By, Domain: q.Domain, Sort: q.Sort, Limit: q.Limit, State: "published", Version: version}
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}
