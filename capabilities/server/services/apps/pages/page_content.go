package pages

import (
	"encoding/json"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ContentHost struct {
	Admits      func(platform.Member) *kernel.Error
	Current     func() []platform.Definition
	Visible     func(platform.Member) []platform.Definition
	Pinned      func(ref platform.AssetRef, version, content string) (platform.ReleaseAsset, error)
	Registered  func() ([]platform.Definition, error)
	Definitions func(m platform.Member, registered []platform.Definition) []platform.Definition
}

func (h ContentHost) Definition(m platform.Member, ref platform.AssetRef, version string) (platform.Definition, *kernel.Error) {
	if refusal := h.Admits(m); refusal != nil {
		return platform.Definition{}, refusal
	}
	if ref.Check() != nil || ref.Kind != platform.AssetPage || platform.CheckPageContentVersion(version) != nil {
		return platform.Definition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Invalid page content identity")
	}
	denied := func() (platform.Definition, *kernel.Error) {
		return platform.Definition{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Page content is unavailable to this member")
	}
	definitions := h.Current()
	current := slices.IndexFunc(definitions, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
	if current < 0 {
		return denied()
	}
	if !slices.ContainsFunc(h.Visible(m), func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil }) {
		return denied()
	}
	original := definitions[current]
	digest, err := platform.PageContentVersion(*original.Page)
	if err != nil {
		return denied()
	}
	if digest != version {
		asset, err := h.Pinned(ref, original.Version, version)
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
	registered, err := h.Registered()
	if err != nil {
		return denied()
	}
	current = slices.IndexFunc(registered, func(d platform.Definition) bool { return d.Ref == ref && d.Page != nil })
	if current < 0 {
		return denied()
	}
	registered[current] = original
	for _, d := range h.Definitions(m, registered) {
		if d.Ref == ref && d.Page != nil && d.ContentVersion == version {
			return d, nil
		}
	}
	return denied()
}
