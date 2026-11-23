package authoring

import (
	"encoding/json"
	"fmt"
	"platformserver/kernel/protocol"

	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/publication"
)

func (b *Build) ReleaseDrafts(assets []platform.ReleaseAsset, ctx protocol.AssetReleaseContext) ([]protocol.AssetDraft, error) {
	ordered, err := b.resolver.OrderReleaseAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	old, err := b.releaseDraftsOf(protocol.ReleaseCandidate{Assets: assets})
	if err != nil {
		return nil, err
	}
	var out []protocol.AssetDraft
	for _, draft := range old {
		ref, err := draftAssetRef(draft.Type, qualified(namespaceOf(draft.Image), draft.Name))
		if err != nil {
			return nil, err
		}
		name := draft.Name
		if draft.Type == ObjectType {
			name = b.objectDraftID(ctx.Member.Tenant, ref, qualified(namespaceOf(draft.Image), draft.Name))
		}
		out = append(out, protocol.AssetDraft{Ref: ref, Type: draft.Type, Name: name, Schema: draft.Type + ".create", Image: draft.Image})
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
		out = append(out, protocol.AssetDraft{Ref: asset.Ref, Type: typ, Name: asset.Ref.Name, Schema: typ + ".create", Image: raw})
	}
	byRef := map[platform.AssetRef]protocol.AssetDraft{}
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

func (b *Build) PrepareReleaseAssets(assets []platform.ReleaseAsset, ctx protocol.AssetReleaseContext) ([]protocol.AssetInstallation, error) {
	ordered, err := b.resolver.OrderReleaseAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	publications, installations, err := b.prepareReleasePublications(assets)
	if err != nil {
		return nil, err
	}
	out := []protocol.AssetInstallation{}
	sampleHooks, _ := publication.Kind(b.publications, platform.AssetSampleSet)
	samples, err := sampleHooks.FreezeInstallations(assets)
	if err != nil {
		return nil, err
	}
	out = append(out, samples...)
	for _, installation := range installations {
		plan, err := b.DecodeAssetInstallation(installation.Schema, installation.Image)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
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
	return b.resolver.OrderInstallations(assets, out)
}

func (b *Build) AssetEnvironmentRequirements(asset platform.ReleaseAsset) ([]protocol.AssetEnvironmentRequirement, error) {
	if dataowner.IsIntegration(asset.Ref.Kind) {
		return b.data.EnvironmentRequirements(asset)
	}
	return nil, nil
}

func (b *Build) objectDraftID(tenant string, ref platform.AssetRef, name string) string {
	if b.host != nil {
		if record, ok := b.host.Record(ResourceType + "/" + protocol.ResourceID(tenant, ref)); ok {
			var resource struct{ Draft string }
			if json.Unmarshal(platform.Raw(record), &resource) == nil && resource.Draft != "" {
				return resource.Draft
			}
		}
	}
	return name
}
