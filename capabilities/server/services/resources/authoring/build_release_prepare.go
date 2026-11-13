package build

import (
	"encoding/json"
	"fmt"

	"platformserver/platform"
	dataowner "platformserver/services/data"
)

func (b *Build) ReleaseDrafts(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	old, err := b.releaseDraftsOf(platform.ReleaseCandidate{Assets: assets})
	if err != nil {
		return nil, err
	}
	var out []platform.AssetDraft
	for _, draft := range old {
		ref, err := draftAssetRef(draft.Type, draft.Name)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.AssetDraft{Ref: ref, Type: draft.Type, Name: draft.Name, Schema: draft.Type + ".create", Image: draft.Image})
	}
	integrations, err := b.data.IntegrationDrafts(assets, ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, integrations...)
	for _, asset := range assets {
		if asset.Ref.App != ID || dataowner.IsIntegration(asset.Ref.Kind) {
			continue
		}
		typ, record, err := b.declarationReleaseDraft(asset)
		if err != nil {
			return nil, err
		}
		if typ == "" {
			continue
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: typ, Name: asset.Ref.Name, Schema: typ + ".create", Image: raw})
	}
	byRef := map[platform.AssetRef]platform.AssetDraft{}
	for _, draft := range out {
		byRef[draft.Ref] = draft
	}
	out = nil
	for _, asset := range assets {
		if draft, ok := byRef[asset.Ref]; ok {
			out = append(out, draft)
			delete(byRef, asset.Ref)
		}
	}
	if len(byRef) != 0 {
		return nil, fmt.Errorf("draft has no candidate owner identity")
	}
	for i := range out {
		if id := ctx.DraftIDs[out[i].Ref]; id != "" {
			out[i].Name = id
		}
	}
	return out, nil
}

func (b *Build) PrepareReleaseAssets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetInstallation, error) {
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	publications, err := b.prepareReleasePublications(assets)
	if err != nil {
		return nil, err
	}
	out := []platform.AssetInstallation{}
	samples, err := b.prepareSampleSetInstallations(assets)
	if err != nil {
		return nil, err
	}
	out = append(out, samples...)
	for _, publication := range publications {
		plan, err := b.DecodeAssetInstallation(publication.Schema, publication.Image)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	integrations, err := b.data.Prepare(assets, ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, integrations...)
	return orderOwnerInstallations(assets, out)
}

func (b *Build) AssetEnvironmentRequirements(asset platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	if dataowner.IsIntegration(asset.Ref.Kind) {
		return b.data.EnvironmentRequirements(asset)
	}
	return nil, nil
}
