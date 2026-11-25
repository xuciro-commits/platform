package publication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/kernel/protocol"
	"platformserver/platform"
)

func (resolver Resolver) checkReleaseBindings(ref platform.AssetRef, body []byte, declared []platform.AssetRef) error {
	description, err := resolver.describeAsset(platform.ReleaseAsset{Ref: ref, Body: body, Requires: declared}, true)
	if err != nil {
		return err
	}
	for _, dep := range description.Requires {
		if !slices.Contains(declared, dep) {
			return fmt.Errorf("release asset %s omits bound dependency %s", ref, dep)
		}
	}
	return nil
}

func (resolver Resolver) CheckAssetDependencies(asset platform.ReleaseAsset) error {
	if asset.Provides == nil {
		return resolver.checkReleaseBindings(asset.Ref, asset.Body, asset.Requires)
	}
	legacyConnector := asset.Ref.Kind == protocol.AssetImplementation && len(asset.Requires) == 0
	description, err := resolver.describeAsset(asset, legacyConnector)
	if err != nil {
		return err
	}
	actual := slices.Clone(asset.Requires)
	slices.SortFunc(actual, CompareAssetRef)
	for _, dependency := range description.Requires {
		if !slices.Contains(actual, dependency) {
			return fmt.Errorf("release asset %s omits bound dependency %s", asset.Ref, dependency)
		}
	}
	if !slices.Equal(actual, description.Requires) {
		return fmt.Errorf("%s requires list differs from its body references", asset.Ref)
	}
	for _, capability := range asset.Provides {
		if err := capability.Check(); err != nil {
			return err
		}
	}
	actualCapabilities, _ := json.Marshal(asset.Provides)
	expectedCapabilities, _ := json.Marshal(description.Provides)
	if !bytes.Equal(actualCapabilities, expectedCapabilities) {
		return fmt.Errorf("%s provides list differs from its owner declaration", asset.Ref)
	}
	return nil
}
