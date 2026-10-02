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

func propertyTypeReleaseAsset(f PropertyType, sourceVersion string) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(f.definition())
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetPropertyType, Name: f.Name}, ContractVersion: 1,
		SourceVersion: sourceVersion + ".property-" + strconv.Itoa(f.Version), Requires: propertyTypeRequires(f.definition()), Body: body}, err
}

// PropertyTypeReleaseAsset resolves only a retained version from this owner.
func (b *Build) PropertyTypeReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	ordinalText, found := strings.CutPrefix(sourceVersion, b.Manifest().Version+".property-")
	version, err := strconv.Atoi(ordinalText)
	if !found || err != nil || strconv.Itoa(version) != ordinalText {
		return platform.ReleaseAsset{}, fmt.Errorf("invalid retained property type version %s", sourceVersion)
	}
	f, ordinal, ok := b.PropertyTypeDefinition(name, version)
	if !ok || ordinal != version || version < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("propertyType %s version %d is not retained", name, version)
	}
	body, err := json.Marshal(f)
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetPropertyType, Name: name}, ContractVersion: 1,
		SourceVersion: b.Manifest().Version + ".property-" + strconv.Itoa(version), Requires: propertyTypeRequires(f), Body: body}, err
}

func (b *Build) propertyTypeAssets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	// This cache is reconstructed only from validated publications or an
	// immutable candidate. Draft rows must never replace installed assets.
	for _, declaration := range b.propertyTypeDeclarations() {
		f := b.propertyTypes[declaration.Name]
		asset, err := propertyTypeReleaseAsset(f, b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// InstallPropertyTypeAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallPropertyTypeAsset(asset platform.ReleaseAsset) error {
	prefix := b.Manifest().Version + ".property-"
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, prefix)
	version, err := strconv.Atoi(ordinal)
	var declaration platform.PropertyType
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal || asset.ContractVersion != 1 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetPropertyType || json.Unmarshal(asset.Body, &declaration) != nil || declaration.Check() != nil || declaration.Name != asset.Ref.Name || !reflect.DeepEqual(asset.Requires, propertyTypeRequires(declaration)) {
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
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	list, problem := b.propertyTypeInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(f PropertyType) bool { return f.ID == id && !f.Archived })
	if i < 0 {
		err = fmt.Errorf("draft propertyType %q not found", id)
		return
	}
	f := list[i]
	if old, ok := wasPublished[PropertyType](f.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetPropertyType, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetPropertyType, Name: f.Name}
	if err = b.checkPropertyType(&f); err != nil {
		return
	}
	if f.Version >= 64 {
		err = fmt.Errorf("property type version family is full")
		return
	}
	f.Version++
	asset, problem := propertyTypeReleaseAsset(f, b.Manifest().Version)
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
func (b *Build) preparePropertyTypeReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	list, err := b.propertyTypeInventory()
	if err != nil {
		return nil, err
	}
	var out []ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetPropertyType {
			continue
		}
		ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".property-")
		version, err := strconv.Atoi(ordinal)
		if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal {
			return nil, fmt.Errorf("invalid property type version")
		}
		i := slices.IndexFunc(list, func(q PropertyType) bool { return !q.Archived && q.Name == asset.Ref.Name })
		if i < 0 {
			return nil, fmt.Errorf("property type has no source owner")
		}
		record := list[i]
		if version <= record.Version {
			prior, err := b.PropertyTypeReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			var retained, candidate any
			if err != nil || json.Unmarshal(prior.Body, &retained) != nil || json.Unmarshal(asset.Body, &candidate) != nil || !reflect.DeepEqual(retained, candidate) {
				return nil, fmt.Errorf("property type retained bytes differ")
			}
			continue
		}
		var q platform.PropertyType
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Type != record.Type {
			return nil, fmt.Errorf("invalid next property type version")
		}
		if err := b.host.ValidateInstallPropertyType(q); err != nil {
			return nil, err
		}
		frozen := propertyTypeFromDeclaration(q)
		frozen.Record = record.Record
		frozen.State = "published"
		frozen.Version = version
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		raw, _ := json.Marshal(record)
		out = append(out, ReleasePublication{Schema: SchemaPropertyType, Image: raw})
	}
	return out, nil
}

func propertyTypeRequires(p platform.PropertyType) []platform.AssetRef { return nil }
func propertyTypeFromDeclaration(p platform.PropertyType) PropertyType {
	return PropertyType{Name: p.Name, Title: p.Title, Description: p.Description, Type: p.Type}
}
