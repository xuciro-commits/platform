package build

import "platformserver/internal/construction"

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

func linkTypeReleaseAsset(f LinkType, sourceVersion string) (platform.ReleaseAsset, error) {
	return definitionReleaseAsset(platform.AssetRef{App: construction.ID, Kind: platform.AssetLinkType, Name: f.Name}, sourceVersion+".link-"+strconv.Itoa(f.Version), f.Definition())
}

// LinkTypeReleaseAsset resolves only a retained version from this owner.
func (b *Build) LinkTypeReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	return retainedDefinitionAsset(b.Manifest().Version, "link", "link type", platform.AssetLinkType, name, sourceVersion, b.LinkTypeDefinition)
}

func (b *Build) linkTypeAssets() ([]platform.ReleaseAsset, error) {
	return installedDefinitionAssets(b.linkTypes, func(record LinkType) (platform.ReleaseAsset, error) {
		return linkTypeReleaseAsset(record, b.Manifest().Version)
	})
}

// InstallLinkTypeAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallLinkTypeAsset(asset platform.ReleaseAsset) error {
	version := b.definitionAssetVersion(asset, platform.AssetLinkType, "link")
	var declaration platform.LinkType
	if version == 0 || json.Unmarshal(asset.Body, &declaration) != nil || asset.ContractVersion != declaration.Contract() || declaration.Check() != nil || declaration.Name != asset.Ref.Name || declaration.Parent.App != construction.ID || declaration.Child.App != construction.ID {
		return fmt.Errorf("invalid builder link type asset %s", asset.Ref)
	}
	f := linkTypeFromDeclaration(declaration)
	if _, exists := b.linkTypes[f.Name]; exists {
		return fmt.Errorf("linkType asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installLinkType(platform.Caller{Replaying: true}, f)
}

func (b *Build) linkTypeDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	return draftDefinitionAssets(b, id, platform.AssetLinkType, "link type", b.linkTypeInventory, func(record LinkType) (LinkType, error) {
		err := b.checkLinkType(&record)
		return record, err
	}, func(record LinkType) (platform.ReleaseAsset, error) {
		record.Version++
		return linkTypeReleaseAsset(record, b.Manifest().Version)
	})
}

// Freeze the immutable candidate beside the mutable editor row. Existing
// ordinals must match retained bytes; only the next ordinal can publish.
func (b *Build) prepareLinkTypeReleasePublications(assets []platform.ReleaseAsset) ([]construction.ReleasePublication, error) {
	return prepareDefinitionPublications(b, assets, b.linkTypeInventory, platform.AssetLinkType, "link", construction.SchemaLinkType, "link type", b.LinkTypeReleaseAsset, func(record LinkType, asset platform.ReleaseAsset, version int) (LinkType, error) {
		prior, retained := construction.ReadPublication[LinkType](record.Published)
		var q platform.LinkType
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Parent.Name != record.Parent || q.Child.Name != record.Child || q.Via != record.Via || retained && !linkKeepsGuarantees(prior, linkTypeFromDeclaration(q)) {
			return record, fmt.Errorf("invalid next link type version")
		}
		frozen := linkTypeFromDeclaration(q)
		frozen.Record = record.Record
		frozen.State = "published"
		frozen.Version = version
		record.Required = frozen.Required
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		return record, nil
	})
}

func linkTypeFromDeclaration(l platform.LinkType) LinkType {
	return LinkType{Name: l.Name, Title: l.Title, Description: l.Description, Parent: l.Parent.Name, Child: l.Child.Name, Via: l.Via, Forward: l.Forward, Reverse: l.Reverse, Required: l.Required, Storage: l.Storage, Roles: slices.Clone(l.Roles), Cardinality: l.Cardinality, DeletePolicy: l.DeletePolicy}
}
