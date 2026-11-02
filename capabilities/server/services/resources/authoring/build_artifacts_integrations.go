package build

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
)

func (b *Build) AssetEnvironmentRequirements(asset platform.ReleaseAsset) ([]platform.AssetEnvironmentRequirement, error) {
	if !ValueIntegrationKind(asset.Ref.Kind) {
		return nil, nil
	}
	var req platform.AssetEnvironmentRequirement
	switch asset.Ref.Kind {
	case platform.AssetConnection:
		var d ConnectionDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Name != asset.Ref.Name || !slices.Contains(ConnectionKinds, d.Kind) {
			return nil, fmt.Errorf("invalid connection binding descriptor")
		}
		req = platform.AssetEnvironmentRequirement{Ref: asset.Ref, Profile: d.Kind, Credential: d.Credential}
	case platform.AssetSource:
		var d SourceDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		kind := "http"
		if d.Profile == "odata" {
			kind = "odata"
		}
		if d.Profile == "table" {
			kind = "postgres"
		}
		if d.Connection == "" {
			return nil, fmt.Errorf("source has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: d.Connection}, Profile: kind}
	case platform.AssetObservation:
		var d platform.ObservationDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection.Kind != platform.AssetConnection || d.Connection.Name == "" {
			return nil, fmt.Errorf("observation source has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: d.Connection, Profile: "http"}
	case platform.AssetWriteback:
		var d WritebackDefinition
		if err := ValueDecodeIntegration(asset.Body, &d); err != nil {
			return nil, err
		}
		if d.Connection == "" {
			return nil, fmt.Errorf("writeback has no environment connection")
		}
		req = platform.AssetEnvironmentRequirement{Ref: platform.AssetRef{App: ID, Kind: platform.AssetConnection, Name: d.Connection}}
	default:
		return nil, nil
	}
	return []platform.AssetEnvironmentRequirement{req}, nil
}

func (b *Build) integrationTargets(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) (func(), error) {
	saved := b.releaseDraftIDs
	b.releaseDraftIDs = maps.Clone(saved)
	if b.releaseDraftIDs == nil {
		b.releaseDraftIDs = map[platform.AssetRef]string{}
	}
	restore := func() { b.releaseDraftIDs = saved }
	for _, asset := range assets {
		if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		rows, err := b.integrationInventory(asset.Ref.Kind)
		if err != nil {
			restore()
			return nil, err
		}
		id, found := asset.Ref.Name, false
		for _, row := range rows {
			name, _, _, _ := integrationMetadata(row)
			if name != asset.Ref.Name {
				continue
			}
			if found {
				restore()
				return nil, fmt.Errorf("integration name %s is ambiguous", name)
			}
			found, id = true, integrationRecord(row).ID
			if connection, ok := row.(Connection); ok {
				var definition ConnectionDefinition
				if err := ValueDecodeIntegration(asset.Body, &definition); err != nil {
					restore()
					return nil, err
				}
				if definition.Kind != connection.Kind {
					restore()
					return nil, fmt.Errorf("connection %s has an incompatible local kind", asset.Ref)
				}
			}
		}
		b.releaseDraftIDs[asset.Ref] = id
	}
	for ref, id := range ctx.DraftIDs {
		b.releaseDraftIDs[ref] = id
	}
	return restore, nil
}

func (b *Build) ReleaseDrafts(assets []platform.ReleaseAsset, ctx platform.AssetReleaseContext) ([]platform.AssetDraft, error) {
	ordered, err := orderOwnerAssets(assets)
	if err != nil {
		return nil, err
	}
	assets = ordered
	restore, err := b.integrationTargets(assets, ctx)
	if err != nil {
		return nil, err
	}
	defer restore()
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
	for _, asset := range assets {
		if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}
		if asset.Ref.Kind == platform.AssetConnection {
			connection, err := portableConnectionDraft(asset)
			if err != nil {
				return nil, err
			}
			out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: ConnectionType, Name: connection.Name, Schema: ConnectionType + ".create", Image: platform.Raw(connection)})
			continue
		}
		record, err := b.integrationConfiguration(asset)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		typ := integrationType(asset.Ref.Kind)
		out = append(out, platform.AssetDraft{Ref: asset.Ref, Type: typ, Name: asset.Ref.Name, Schema: typ + ".create", Image: raw})
	}
	for _, asset := range assets {
		if asset.Ref.App != ID {
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
	restore, err := b.integrationTargets(assets, ctx)
	if err != nil {
		return nil, err
	}
	defer restore()
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
	for _, p := range publications {
		plan, err := b.DecodeAssetInstallation(p.Schema, p.Image)
		if err != nil {
			return nil, err
		}
		out = append(out, plan)
	}
	for _, asset := range assets {
		if asset.Ref.App != ID || !ValueIntegrationKind(asset.Ref.Kind) {
			continue
		}

		rows, err := b.integrationInventory(asset.Ref.Kind)
		if err != nil {
			return nil, err
		}
		var current any
		for _, r := range rows {
			name, _, _, _ := integrationMetadata(r)
			if name == asset.Ref.Name {
				current = r
				break
			}
		}
		if current == nil {
			return nil, fmt.Errorf("asset %s has no target draft", asset.Ref)
		}
		record, err := retainIntegration(current, asset)
		if err != nil {
			return nil, err
		}
		switch r := record.(type) {
		case Listener:
			r.State, r.Enabled = "published", true
			record = r
		case Source:
			r.State, r.Publisher, r.PublisherScopes = "published", ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			if r.Stream && r.Dataset != "" && r.Puller == "" {
				r.Puller, r.PullerScopes = ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			}
			record = r
		case platform.MediaSet:
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return nil, err
			}
			fixed := configuration.(platform.MediaSet)
			fixed.Record, fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.Record, r.DefinitionVersion, r.Published, r.Versions
			record = fixed
		case platform.Schedule:
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return nil, err
			}
			fixed := configuration.(platform.Schedule)
			fixed.Record, fixed.LastRun, fixed.Runner, fixed.RunnerScopes = r.Record, r.LastRun, ctx.Member.ID, string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			record = fixed
		case Pipeline:
			if !ctx.DraftsOnly && !ctx.Member.CanDeclassify(r.PublishedDeclassifications()) {
				return nil, fmt.Errorf("declassification publication requires clearance and permission for every selected marking")
			}
			r.State = "published"
			record = r
		case Writeback:
			r.State, r.Publisher = "published", ctx.Member.ID
			record = r
		case Observation:
			configuration, err := b.integrationConfiguration(asset)
			if err != nil {
				return nil, err
			}
			fixed := configuration.(Observation)
			fixed.Record, fixed.State, fixed.Publisher = r.Record, "published", ctx.Member.ID
			fixed.PublisherScopes = string(platform.Raw(ctx.Member.Scopes))
			fixed.DefinitionVersion, fixed.Published, fixed.Versions = r.DefinitionVersion, r.Published, r.Versions
			record = fixed
		case Match:
			r.State, r.Publisher = "published", ctx.Member.ID
			record = r
		}
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		if before, _ := json.Marshal(current); bytes.Equal(before, raw) {
			continue
		}
		out = append(out, platform.AssetInstallation{Ref: asset.Ref, Schema: integrationType(asset.Ref.Kind) + ".publish", Record: integrationType(asset.Ref.Kind) + "/" + integrationRecord(record).ID, Image: raw})
	}
	if !ctx.DraftsOnly {
		for _, plan := range out {
			if slices.Contains([]platform.AssetKind{platform.AssetSource, platform.AssetListener, platform.AssetPipeline, platform.AssetWriteback, platform.AssetObservation, platform.AssetMatching}, plan.Ref.Kind) && (!ctx.Member.Active() || !ctx.Member.InScope(plan.Schema)) {
				return nil, fmt.Errorf("release publication token does not permit %s", plan.Schema)
			}
		}
	}
	return orderOwnerInstallations(assets, out)
}
