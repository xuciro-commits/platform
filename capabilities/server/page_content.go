package platformserver

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"slices"
)

// PageContentDefinition reuses the one discovery projection over an original
// owner snapshot. A digest never grants access or exposes an unpublished draft.
// Like Definitions, it is called under the tenant lock at the HTTP boundary.
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
		owner, ok := t.app(ref.App).(interface {
			PageContent(string, string) (platform.Page, bool)
		})
		if !ok {
			return denied()
		}
		page, ok := owner.PageContent(ref.Name, version)
		if !ok {
			return denied()
		}
		actual, err := platform.PageContentVersion(page)
		if err != nil || actual != version || page.Name != ref.Name {
			return denied()
		}
		original.Page = &page
		asset, err := platform.PageReleaseAsset(ref.App, original.Version, page)
		if err != nil {
			return denied()
		}
		original.Requires = asset.Requires
	}
	registered := slices.Clone(t.definitions)
	registered[current] = original
	for _, d := range t.definitionsFrom(m, registered) {
		if d.Ref == ref && d.Page != nil && d.ContentVersion == version {
			return d, nil
		}
	}
	return denied()
}
