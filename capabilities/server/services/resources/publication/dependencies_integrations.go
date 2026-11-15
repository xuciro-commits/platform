package publication

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func WithDependencies(asset platform.ReleaseAsset) (platform.ReleaseAsset, error) {
	description, err := DescribeAsset(asset)
	if err != nil {
		return platform.ReleaseAsset{}, err
	}
	asset.Requires, asset.Provides = description.Requires, description.Provides
	return asset, nil
}

func describeProtocolDependencies(asset platform.ReleaseAsset) (out platform.Dependencies, err error) {
	add := func(ref platform.AssetRef) {
		if ref.Name != "" {
			out.Requires = append(out.Requires, ref)
		}
	}
	capability := func(kind, name, side string, roles []string, input, output platform.ValueSchema, fields []platform.Field) {
		out.Provides = append(out.Provides, platform.Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})
	}
	var protocol platform.ProtocolReleaseDescriptor
	if err := json.Unmarshal(asset.Body, &protocol); err != nil {
		return out, err
	}
	if protocol.Version < 1 {
		return out, fmt.Errorf("protocol %s needs a pinned version", asset.Ref)
	}
	for _, action := range protocol.Actions {
		roles := action.Roles
		if frozen, ok := protocol.ActionRoles[action.Schema]; ok {
			roles = frozen
		}
		capability("protocol", protocol.Name+"#"+action.Schema, platform.CapabilityAuthoritative, roles, platform.CapabilityFieldsSchema(action.Payload), platform.ValueSchema{}, action.Payload)
	}
	for _, read := range protocol.Reads {
		capability("protocol", protocol.Name+"#"+read, platform.CapabilityPure, nil, platform.ValueSchema{}, platform.ValueSchema{}, nil)
	}
	for _, ref := range protocol.ActionBindings {
		add(ref)
	}
	for _, ref := range protocol.ReadBindings {
		add(ref)
	}

	return out, nil
}

func describeIntegrationDependencies(asset platform.ReleaseAsset, historical bool, object func(string) platform.AssetRef) (out platform.Dependencies, err error) {
	add := func(ref platform.AssetRef) {
		if ref.Name != "" {
			out.Requires = append(out.Requires, ref)
		}
	}
	addObject := func(name string) {
		if name != "" && name != "enterprise.element" {
			add(object(name))
		}
	}
	capability := func(kind, name, side string, roles []string, input, output platform.ValueSchema, fields []platform.Field) {
		out.Provides = append(out.Provides, platform.Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})
	}
	if historical {
		return out, nil
	}
	var integration struct {
		Pipeline      string `json:"pipeline"`
		Connection    string `json:"connection"`
		Dataset       string `json:"dataset"`
		Object        string `json:"object"`
		Input         string `json:"input"`
		OutputDataset string `json:"outputDataset"`
		OutputObject  string `json:"outputObject"`
		On            string `json:"on"`
		Steps         []struct {
			Dataset string `json:"dataset"`
		} `json:"steps"`
		Prefer []struct {
			Producer string             `json:"producer"`
			Binding  *platform.AssetRef `json:"binding"`
		} `json:"prefer"`
		ProducerBindings map[string]platform.AssetRef `json:"producers"`
	}
	if err := json.Unmarshal(asset.Body, &integration); err != nil {
		return out, err
	}
	for kind, names := range map[platform.AssetKind][]string{platform.AssetPipeline: {integration.Pipeline}, platform.AssetConnection: {integration.Connection}, platform.AssetDataset: {integration.Dataset, integration.Input, integration.OutputDataset}} {
		for _, name := range names {
			if name != "" {
				add(platform.AssetRef{App: asset.Ref.App, Kind: kind, Name: name})
			}
		}
	}
	addObject(integration.Object)
	addObject(integration.OutputObject)
	for _, step := range integration.Steps {
		if step.Dataset != "" {
			add(platform.AssetRef{App: asset.Ref.App, Kind: platform.AssetDataset, Name: step.Dataset})
		}
	}
	if asset.Ref.Kind == platform.AssetWriteback && !slices.Contains([]string{"", "confirmation", "dispatch", "transaction"}, integration.On) {
		ref := object(integration.Object)
		ref.Kind = platform.AssetAction
		ref.Name = integration.Object + "." + integration.On
		add(ref)
	}
	for _, preference := range integration.Prefer {
		if preference.Binding != nil {
			add(*preference.Binding)
		} else if ref, ok := integration.ProducerBindings[preference.Producer]; ok {
			add(ref)
		}
	}
	if slices.Contains([]platform.AssetKind{platform.AssetSource, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching}, asset.Ref.Kind) {
		capability("operation", asset.Ref.Name, platform.CapabilityEffect, nil, platform.ValueSchema{}, platform.ValueSchema{}, nil)
	}
	return out, nil
}

func completeAssetDependencies(asset platform.ReleaseAsset, out platform.Dependencies) (platform.Dependencies, error) {
	slices.SortFunc(out.Requires, platform.CompareAssetRef)
	out.Requires = slices.Compact(out.Requires)
	for _, ref := range out.Requires {
		if err := ref.Check(); err != nil {
			return out, fmt.Errorf("%s has invalid binding: %w", asset.Ref, err)
		}
	}
	slices.SortFunc(out.Provides, func(a, b platform.Capability) int {
		if a.Kind != b.Kind {
			return strings.Compare(a.Kind, b.Kind)
		}
		return strings.Compare(a.Name, b.Name)
	})
	for i, c := range out.Provides {
		if err := c.Check(); err != nil {
			return out, err
		}
		if i > 0 && c.Kind == out.Provides[i-1].Kind && c.Name == out.Provides[i-1].Name {
			return out, fmt.Errorf("%s repeats capability %s", asset.Ref, c.Name)
		}
	}
	return out, nil
}

func DescribeAsset(asset platform.ReleaseAsset) (platform.Dependencies, error) {
	return describeAsset(asset, false)
}
