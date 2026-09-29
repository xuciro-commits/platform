package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"platformserver/platform"
)

func functionReleaseAsset(f Function, sourceVersion string) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(f.definition())
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: f.Name}, ContractVersion: 1,
		SourceVersion: sourceVersion + ".function-" + strconv.Itoa(f.Version), Requires: []platform.AssetRef{{App: ID, Kind: platform.AssetObject, Name: f.Object}}, Body: body}, err
}

func (b *Build) functionAssets() ([]platform.ReleaseAsset, error) {
	var out []platform.ReleaseAsset
	// This cache is reconstructed only from validated publications or an
	// immutable candidate. Draft rows must never replace installed assets.
	for _, declaration := range b.functionDeclarations() {
		f := b.functions[declaration.Name]
		asset, err := functionReleaseAsset(f, b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
}

// InstallFunctionAsset compiles one immutable candidate version into a fresh
// test app. It does not invent a publication history or run a publish action.
// The candidate constructor repeats this compilation before snapshot restore.
func (b *Build) InstallFunctionAsset(asset platform.ReleaseAsset) error {
	prefix := b.Manifest().Version + ".function-"
	ordinal, ok := strings.CutPrefix(asset.SourceVersion, prefix)
	version, err := strconv.Atoi(ordinal)
	var f Function
	if !ok || err != nil || version < 1 || version > 64 || strconv.Itoa(version) != ordinal ||
		asset.ContractVersion != 1 || asset.Ref.App != ID || asset.Ref.Kind != platform.AssetFunction ||
		json.Unmarshal(asset.Body, &f) != nil || f.Name != asset.Ref.Name ||
		f.definition().Check() != nil || len(asset.Requires) != 1 || asset.Requires[0] != (platform.AssetRef{App: ID, Kind: platform.AssetObject, Name: f.Object}) {
		return fmt.Errorf("invalid builder function asset %s", asset.Ref)
	}
	if _, exists := b.functions[f.Name]; exists {
		return fmt.Errorf("function asset %s is already installed", asset.Ref)
	}
	f.Version, f.State = version, "published"
	return b.installFunction(platform.Caller{Replaying: true}, f)
}

func (b *Build) functionDraftAssets(id string) (before, after []platform.ReleaseAsset, prior, next platform.AssetRef, hadPrior bool, err error) {
	before, err = b.ReleaseAssets()
	if err != nil {
		return
	}
	list, problem := b.functionInventory()
	if problem != nil {
		err = problem
		return
	}
	i := slices.IndexFunc(list, func(f Function) bool { return f.ID == id && !f.Archived })
	if i < 0 {
		err = fmt.Errorf("draft function %q not found", id)
		return
	}
	f := list[i]
	if old, ok := wasPublished[Function](f.Published); ok {
		prior = platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: old.Name}
		hadPrior = true
	}
	next = platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: f.Name}
	if err = b.checkFunction(f); err != nil {
		return
	}
	if f.Version >= 64 {
		err = fmt.Errorf("function version family is full")
		return
	}
	f.Version++
	asset, problem := functionReleaseAsset(f, b.Manifest().Version)
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
