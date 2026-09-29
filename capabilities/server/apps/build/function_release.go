package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"

	"platformserver/platform"
)

func functionReleaseAsset(f Function, sourceVersion string) (platform.ReleaseAsset, error) {
	body, err := json.Marshal(f.definition())
	return platform.ReleaseAsset{Ref: platform.AssetRef{App: ID, Kind: platform.AssetFunction, Name: f.Name}, ContractVersion: 1,
		SourceVersion: sourceVersion + ".function-" + strconv.Itoa(f.Version), Requires: []platform.AssetRef{{App: ID, Kind: platform.AssetObject, Name: f.Object}}, Body: body}, err
}

func (b *Build) functionAssets() ([]platform.ReleaseAsset, error) {
	list, err := b.functionInventory()
	if err != nil {
		return nil, err
	}
	var out []platform.ReleaseAsset
	for _, record := range list {
		if record.Published == "" {
			continue
		}
		raw, _ := json.Marshal(record)
		f, err := functionImage(raw)
		if err != nil {
			return nil, err
		}
		asset, err := functionReleaseAsset(f, b.Manifest().Version)
		if err != nil {
			return nil, err
		}
		out = append(out, asset)
	}
	return out, nil
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
