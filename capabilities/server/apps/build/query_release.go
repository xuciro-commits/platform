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

func queryReleaseAsset(f Query, sourceVersion string) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(f.definition())
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: f.Name}, ContractVersion: 1,
		SourceVersion: sourceVersion + ".query-" + strconv.Itoa(f.Version), Requires: f.definition().Dependencies(), Body: body}, err
}

// QueryReleaseAsset resolves only a retained version from this owner.
func (b *Build) QueryReleaseAsset(name string, sourceVersion string) (platform.ReleaseAsset, error) {
	ordinalText, found := strings.CutPrefix(sourceVersion, b.Manifest().Version+".query-")
	version, err := strconv.Atoi(ordinalText)
	if !found || err != nil || strconv.Itoa(version) != ordinalText {
		return platform.ReleaseAsset{}, fmt.Errorf("invalid retained query version %s", sourceVersion)
	}
	f, ordinal, ok := b.QueryDefinition(name, version)
	if !ok || ordinal != version || version < 1 {
		return platform.ReleaseAsset{}, fmt.Errorf("query %s version %d is not retained", name, version)
	}
	body, err := json.Marshal(f)
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: name}, ContractVersion: 1,
		SourceVersion: b.Manifest().Version + ".query-" + strconv.Itoa(version), Requires: f.Dependencies(), Body: body}, err
}

func (b *Build) queryAssets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	// This cache is reconstructed only from validated publications or an
	// immutable candidate. Draft rows must never replace installed assets.
	for _, declaration := range b.queryDeclarations() {
		f := b.queries[declaration.Name]
		asset, err := queryReleaseAsset(f, b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// InstallQueryAsset installs one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor checks this declaration before snapshot restore.
func (b *Build) InstallQueryAsset(asset platform.ReleaseAsset) error {
	prefix := b.Manifest().Version + ".query-"
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, prefix)
	version, err := strconv.Atoi(ordinal)
	var f Query
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal ||
		asset.ContractVersion != 1 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetQuery ||
		json.Unmarshal(asset.Body, &f) != nil || f.Name != asset.Ref.Name ||
		f.definition().Check() != nil || !slices.Equal(asset.Requires, f.definition().Dependencies()) {
		return fmt.Errorf("invalid builder query asset %s", asset.Ref)
	}
	if _, exists := b.queries[f.Name]; exists {
		return fmt.Errorf("query asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installQuery(platform.Caller{Replaying: true}, f)
}

func (b *Build) queryDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	list, problem := b.queryInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(f Query) bool { return f.ID == id && !f.Archived })
	if i < 0 {
		err = fmt.Errorf("draft query %q not found", id)
		return
	}
	f := list[i]
	if old, ok := wasPublished[Query](f.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetQuery, Name: f.Name}
	f, err = b.bindQuery(f)
	if err != nil {
		return
	}
	if f.Version >= 64 {
		err = fmt.Errorf("query version family is full")
		return
	}
	f.Version++
	asset, problem := queryReleaseAsset(f, b.Manifest().Version)
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
func (b *Build) prepareQueryReleasePublications(assets []platform.ReleaseAsset) ([]ReleasePublication, error) {
	list, err := b.queryInventory()
	if err != nil {
		return nil, err
	}
	var out []ReleasePublication
	for _, asset := range assets {
		if asset.Ref.App != ID || asset.Ref.Kind != platform.AssetQuery {
			continue
		}
		ordinal, ok := strings.CutPrefix(asset.SourceVersion, b.Manifest().Version+".query-")
		version, err := strconv.Atoi(ordinal)
		if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal {
			return nil, fmt.Errorf("invalid query version")
		}
		i := slices.IndexFunc(list, func(q Query) bool { return !q.Archived && q.Name == asset.Ref.Name })
		if i < 0 {
			return nil, fmt.Errorf("query has no source owner")
		}
		record := list[i]
		if version <= record.Version {
			prior, err := b.QueryReleaseAsset(asset.Ref.Name, asset.SourceVersion)
			var retained, candidate any
			if err != nil || json.Unmarshal(prior.Body, &retained) != nil || json.Unmarshal(asset.Body, &candidate) != nil || !reflect.DeepEqual(retained, candidate) {
				return nil, fmt.Errorf("query retained bytes differ")
			}
			continue
		}
		var q platform.NamedQuery
		if version != record.Version+1 || json.Unmarshal(asset.Body, &q) != nil || q.Check() != nil || q.Name != record.Name || q.Object != record.Object || q.Interface != record.Interface {
			return nil, fmt.Errorf("invalid next query version")
		}
		if q.Interface != "" {
			var overlays []platform.EntityInfo
			for _, selected := range assets {
				if selected.Ref.App != ID || selected.Ref.Kind != platform.AssetObject {
					continue
				}
				var object Object
				if json.Unmarshal(selected.Body, &object) != nil || TypeOf(object.Name) != selected.Ref.Name {
					continue
				}
				info, err := platform.Describe(ID, Entity(object), func(reflect.Type) string { return "" })
				if err != nil {
					return nil, err
				}
				overlays = append(overlays, info)
			}
			if _, err := b.host.BindInterfaceQuery(q, overlays...); err != nil {
				return nil, err
			}
		} else if err := b.host.ValidateInstallQuery(q); err != nil {
			return nil, err
		}
		frozen := Query{Record: record.Record, Name: q.Name, Title: q.Title, Description: q.Description, Object: q.Object, Interface: q.Interface, InterfaceShape: q.InterfaceShape, Implementations: q.Implementations, By: q.By, Domain: q.Domain, Sort: q.Sort, Limit: q.Limit, State: "published", Version: version}
		record.State, record.Version, record.Published = "published", version, published(frozen)
		record.Versions = append(slices.Clone(record.Versions), record.Published)
		raw, _ := json.Marshal(record)
		out = append(out, ReleasePublication{Schema: SchemaQuery, Image: raw})
	}
	return out, nil
}
