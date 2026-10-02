package build

import (
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func linkTypeReleaseAsset(f LinkType, sourceVersion string) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(f.definition())
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: f.Name}, ContractVersion: f.definition().Contract(),
		SourceVersion: sourceVersion + ".link-" + strconv.Itoa(f.Version), Requires: linkTypeRequires(f.definition()), Body: body}, err
}

// LinkTypeReleaseAsset resolves only a retained version from this owner.
func (b *Build) LinkTypeReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	ordinalText, found := strings.CutPrefix(sourceVersion, b.Manifest().Version+".link-")
	version, err := strconv.Atoi(ordinalText)
	if !found || err != nil || strconv.Itoa(version) != ordinalText {
		return platform.ReleaseAsset{}, fmt.Errorf("invalid retained link type version %s", sourceVersion)
	}
	f, ordinal, ok := b.LinkTypeDefinition(name, version)
	if !ok || ordinal != version || version < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("linkType %s version %d is not retained", name, version)
	}
	body, err := json.Marshal(f)
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: name}, ContractVersion: f.Contract(),
		SourceVersion: b.Manifest().Version + ".link-" + strconv.Itoa(version), Requires: linkTypeRequires(f), Body: body}, err
}

func (b *Build) linkTypeAssets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	// This cache is reconstructed only from validated publications or an
	// immutable candidate. Draft rows must never replace installed assets.
	for _, declaration := range b.linkTypeDeclarations() {
		f := b.linkTypes[declaration.Name]
		asset, err := linkTypeReleaseAsset(f, b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// InstallLinkTypeAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallLinkTypeAsset(asset platform.ReleaseAsset) error {
	prefix := b.Manifest().Version + ".link-"
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, prefix)
	version, err := strconv.Atoi(ordinal)
	var declaration platform.LinkType
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetLinkType || json.Unmarshal(asset.Body, &declaration) != nil || asset.ContractVersion != declaration.Contract() || declaration.Check() != nil || declaration.Name != asset.Ref.Name || declaration.Parent.App != ID || declaration.Child.App != ID || !reflect.DeepEqual(asset.Requires, linkTypeRequires(declaration)) {
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
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	list, problem := b.linkTypeInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(f LinkType) bool { return f.ID == id && !f.Archived })
	if i < 0 {
		err = fmt.Errorf("draft linkType %q not found", id)
		return
	}
	f := list[i]
	if old, ok := wasPublished[LinkType](f.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetLinkType, Name: f.Name}
	if err = b.checkLinkType(&f); err != nil {
		return
	}
	if f.Version >= 64 {
		err = fmt.Errorf("link type version family is full")
		return
	}
	f.Version++
	asset, problem := linkTypeReleaseAsset(f, b.Manifest().Version)
	if problem != nil {
		err = problem
		return
	}
	after = slices.Clone(before)
	if hadPrior {
		after = slices.DeleteFunc(after, func(a platform.ReleaseAsset) bool { return a.Ref == prior })
	}
	after = append(after, asset)
	return
}

// Freeze the immutable candidate beside the mutable editor row. Existing
// ordinals must match retained bytes; only the next ordinal can publish.
func (b *Build) prepareLinkTypeReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	list, err := b.linkTypeInventory()
	if err != nil {
		return nil, err
	}
	var out []ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetLinkType {
			continue
		}
		ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".link-")
		version, err := strconv.Atoi(ordinal)
		if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal {
			return nil, fmt.Errorf("invalid link type version")
		}
		i := slices.IndexFunc(list, func(q LinkType) bool { return !q.Archived && q.Name == asset.Ref.Name })
		if i < 0 {
			return nil, fmt.Errorf("link type has no source owner")
		}
		record := list[i]
		if version <= record.Version {
			prior, err := b.LinkTypeReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			var retained, candidate any
			if err != nil || json.Unmarshal(prior.Body, &retained) != nil || json.Unmarshal(asset.Body, &candidate) != nil || !reflect.DeepEqual(retained, candidate) {
				return nil, fmt.Errorf("link type retained bytes differ")
			}
			continue
		}
		prior, _ := wasPublished[LinkType](record.Published)
		var q platform.LinkType
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Parent.Name != record.Parent || q.Child.Name != record.Child || q.Via != record.Via || !linkKeepsGuarantees(prior, linkTypeFromDeclaration(q)) {
			return nil, fmt.Errorf("invalid next link type version")
		}
		if err := b.host.ValidateInstallLinkType(q); err != nil {
			return nil, err
		}
		frozen := linkTypeFromDeclaration(q)
		frozen.Record = record.Record
		frozen.State = "published"
		frozen.Version = version
		record.Required = frozen.Required
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		raw, _ := json.Marshal(record)
		out = append(out, ReleasePublication{Schema: SchemaLinkType, Image: raw})
	}
	return out, nil
}

func linkTypeRequires(l platform.LinkType) []platform.AssetRef {
	if l.Parent == l.Child {
		return []platform.AssetRef{l.Parent}
	}
	return []platform.AssetRef{l.Parent, l.Child}
}
func linkTypeFromDeclaration(l platform.LinkType) LinkType {
	return LinkType{Name: l.Name, Title: l.Title, Description: l.Description, Parent: l.Parent.Name, Child: l.Child.Name, Via: l.Via, Forward: l.Forward, Reverse: l.Reverse, Required: l.Required, Cardinality: l.Cardinality, DeletePolicy: l.DeletePolicy}
}
