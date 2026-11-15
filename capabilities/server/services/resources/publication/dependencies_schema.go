package publication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/platform"
)

func pageDependencies(original platform.Page) []platform.AssetRef {
	page, err := original.Safety()
	if err != nil {
		return nil
	}
	refs := []platform.AssetRef{}
	if page.Object.Name != "" {
		refs = append(refs, page.Object)
	}
	refs = append(refs, page.ModelDependencies()...)
	refs = append(refs, page.QueryReferences()...)
	refs = append(refs, page.NavigationTargets()...)
	for _, compute := range page.ComputeResources() {
		refs = append(refs, compute.Operation.Ref)
	}
	for _, selection := range page.InputSchema.Selections {
		refs = append(refs, selection.Object)
	}
	for _, binding := range page.DataBindings {
		if binding.Object.Name != "" {
			refs = append(refs, binding.Object)
		}
		if binding.Query != nil {
			refs = append(refs, binding.Query.Ref)
		}
		for _, resource := range binding.Resources {
			if resource.Asset != nil {
				refs = append(refs, resource.Asset.Ref)
			}
			if resource.Object != nil {
				refs = append(refs, *resource.Object)
			}
		}
	}
	for _, trigger := range page.ActionTriggers {
		refs = append(refs, trigger.Actions...)
		if trigger.InlineEdit != nil {
			refs = append(refs, trigger.InlineEdit.Action)
		}
		if trigger.Function != nil {
			refs = append(refs, trigger.Function.Ref)
		}
		if trigger.Operation != nil {
			refs = append(refs, trigger.Operation.Ref)
		}
		for _, event := range trigger.Events {
			for _, effect := range event.Effects {
				if effect.Action != nil {
					refs = append(refs, effect.Action.Ref)
				}
			}
		}
	}
	return refs
}

func checkReleaseBindings(ref platform.AssetRef, body []byte, declared []platform.AssetRef) error {
	description, err := describeAsset(platform.ReleaseAsset{Ref: ref, Body: body}, true)
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

func CheckAssetDependencies(asset platform.ReleaseAsset) error {
	if asset.Provides == nil {
		return checkReleaseBindings(asset.Ref, asset.Body, asset.Requires)
	}
	description, err := DescribeAsset(asset)
	if err != nil {
		return err
	}
	actual := slices.Clone(asset.Requires)
	slices.SortFunc(actual, platform.CompareAssetRef)
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
