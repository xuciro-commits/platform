package publication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

func CandidateDiff(before, after platform.ReleaseCandidate) (added, removed, changed []platform.AssetRef, err error) {
	before, err = ReadCandidate(before.ID, before.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("previous candidate: %w", err)
	}
	after, err = ReadCandidate(after.ID, after.Bytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("next candidate: %w", err)
	}
	prior := map[platform.AssetRef][]byte{}
	for _, asset := range before.Assets {
		raw, _ := json.Marshal(asset)
		prior[asset.Ref] = raw
	}
	for _, asset := range after.Assets {
		raw, _ := json.Marshal(asset)
		old, found := prior[asset.Ref]
		if !found {
			added = append(added, asset.Ref)
		} else if !bytes.Equal(old, raw) {
			changed = append(changed, asset.Ref)
		}
		delete(prior, asset.Ref)
	}
	for ref := range prior {
		removed = append(removed, ref)
	}
	slices.SortFunc(added, platform.CompareAssetRef)
	slices.SortFunc(removed, platform.CompareAssetRef)
	slices.SortFunc(changed, platform.CompareAssetRef)
	return added, removed, changed, nil
}
