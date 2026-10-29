package platformserver

import (
	"encoding/json"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
)

func (t *Tenant) PageContentDefinition(m platform.Member, ref platform.AssetRef, version string) (platform.Definition, *kernel.Error) {
	if refusal := t.admits(m); refusal != nil {
		return platform.Definition{}, refusal
	}
	if ref.Check() != nil || ref.Kind != platform.AssetPage || platform.CheckPageContentVersion(version) != nil {
		return platform.Definition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid page content identity")
	}
	denied := func() (platform.Definition, *kernel.Error) {
		return platform.Definition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Page content is unavailable to this member")
	}
	current := slices.IndexFunc(t.definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
	if current < 0 {
		return denied()
	}
	visible := t.Definitions(m)
	if !slices.ContainsFunc(visible, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil }) {
		return denied()
	}
	original := t.definitions[current]
	digest, err := platform.PageContentVersion(*original.Page)
	if err != nil {
		return denied()
	}
	if digest != version {
		registry, err := t.assetRegistryLocked()
		if err != nil {
			return denied()
		}
		asset, err := registry.ResolvePinned(ref, original.Version, version)
		if err != nil {
			return denied()
		}
		var page platform.Page
		if json.Unmarshal(asset.Body, &page) != nil {
			return denied()
		}
		original.Page = &page
		original.Requires = asset.Requires
	}
	registered, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return denied()
	}
	current = slices.IndexFunc(registered, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
	if current < 0 {
		return denied()
	}
	registered[current] = original
	for _, d := range t.definitionsFrom(m, registered) {
		if d.Ref == ref && d.Page != nil && d.ContentVersion == version {
			return d, nil
		}
	}
	return denied()
}
