package build

import (
	"encoding/json"
	"fmt"
	"platformserver/internal/construction"
	"slices"
	"strconv"

	"platformserver/platform"
)

func functionReleaseAsset(f construction.Function, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: construction.ID, Kind: platform.AssetFunction, Name: f.Name}, sourceVersion+".function-"+strconv.Itoa(f.Version), f.Definition())
}

// FunctionReleaseAsset resolves only a retained version from this owner.
func (b *Build) FunctionReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "function", "function", platform.AssetFunction, name, sourceVersion, b.FunctionDefinition)
}

func (b *Build) functionAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.functions, func(record construction.Function) (platform.ReleaseAsset, error) {
		return functionReleaseAsset(record, b.Manifest().Version)
	})
}

// InstallFunctionAsset compiles one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor repeats this compilation before snapshot restore.
func (b *Build) InstallFunctionAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetFunction, "function")
	var f construction.Function
	if version == 0 || asset.ContractVersion != 1 ||
		json.Unmarshal(asset.Body, &f) != nil || f.Name != asset.Ref.Name ||
		f.Definition().Check() != nil {
		return fmt.Errorf("invalid builder function asset %s", asset.Ref)
	}
	if _, exists := b.functions[f.Name]; exists {
		return fmt.Errorf("function asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installFunction(platform.Caller{Replaying: true}, f)
}

func (b *Build) functionDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetFunction, "function", b.functionInventory, func(record construction.Function) (construction.Function, error) {
		return record, b.checkFunction(record)
	}, func(record construction.Function) (platform.ReleaseAsset, error) {
		record.Version++
		return functionReleaseAsset(record, b.Manifest().Version)
	})
}

func (b *Build) prepareFunctionReleasePublications(assets []platform.ReleaseAsset) ([]construction.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.functionInventory, platform.AssetFunction, "function", SchemaFunction, "function", b.FunctionReleaseAsset, func(record construction.Function, asset platform.ReleaseAsset, version int) (construction.Function, error) {
		var frozen construction.Function
		if version != record.Version+1 || json.Unmarshal(asset.Body, &frozen) != nil || frozen.Definition().Check() != nil || frozen.Name != record.Name || frozen.Object != record.Object {
			return record, fmt.Errorf("function version requires its retained predecessors and matching target definition")
		}
		if asset.ContractVersion != 1 || platform.CheckAssetDependencies(asset) != nil {
			return record, fmt.Errorf("function candidate dependencies differ")
		}
		frozen.Record, frozen.Version, frozen.State = record.Record, version, "published"
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}
