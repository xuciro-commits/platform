package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

func describeProtocolDependencies(asset ReleaseAsset) (out Dependencies, err error) {
	add := func(ref AssetRef) {
		if ref.Name != "" {
			out.Requires = append(out.Requires, ref)
		}
	}
	capability := func(kind, name, side string, roles []string, input, output ValueSchema, fields []Field) {
		out.Provides = append(out.Provides, Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})
	}
	var protocol ProtocolReleaseDescriptor
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
		capability("protocol", protocol.Name+"#"+action.Schema, CapabilityAuthoritative, roles, CapabilityFieldsSchema(action.Payload), ValueSchema{}, action.Payload)
	}
	for _, read := range protocol.Reads {
		capability("protocol", protocol.Name+"#"+read, CapabilityPure, nil, ValueSchema{}, ValueSchema{}, nil)
	}
	for _, ref := range protocol.ActionBindings {
		add(ref)
	}
	for _, ref := range protocol.ReadBindings {
		add(ref)
	}

	return out, nil
}

func describeIntegrationDependencies(asset ReleaseAsset, historical bool, object func(string) AssetRef) (out Dependencies, err error) {
	add := func(ref AssetRef) {
		if ref.Name != "" {
			out.Requires = append(out.Requires, ref)
		}
	}
	addObject := func(name string) {
		if name != "" && name != "enterprise.element" {
			add(object(name))
		}
	}
	capability := func(kind, name, side string, roles []string, input, output ValueSchema, fields []Field) {
		out.Provides = append(out.Provides, Capability{Kind: kind, Name: name, Side: side, Roles: slices.Clone(roles), Inputs: input, Outputs: output, Parameters: fields})
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
			Producer string    `json:"producer"`
			Binding  *AssetRef `json:"binding"`
		} `json:"prefer"`
		ProducerBindings map[string]AssetRef `json:"producers"`
	}
	if err := json.Unmarshal(asset.Body, &integration); err != nil {
		return out, err
	}
	for kind, names := range map[AssetKind][]string{AssetPipeline: {integration.Pipeline}, AssetConnection: {integration.Connection}, AssetDataset: {integration.Dataset, integration.Input, integration.OutputDataset}} {
		for _, name := range names {
			if name != "" {
				add(AssetRef{App: asset.Ref.App, Kind: kind, Name: name})
			}
		}
	}
	addObject(integration.Object)
	addObject(integration.OutputObject)
	for _, step := range integration.Steps {
		if step.Dataset != "" {
			add(AssetRef{App: asset.Ref.App, Kind: AssetDataset, Name: step.Dataset})
		}
	}
	if asset.Ref.Kind == AssetWriteback && !slices.Contains([]string{"", "confirmation", "dispatch", "transaction"}, integration.On) {
		ref := object(integration.Object)
		ref.Kind = AssetAction
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
	if slices.Contains([]AssetKind{AssetSource, AssetPipeline, AssetWriteback, AssetMatching}, asset.Ref.Kind) {
		capability("operation", asset.Ref.Name, CapabilityEffect, nil, ValueSchema{}, ValueSchema{}, nil)
	}
	return out, nil
}

func completeAssetDependencies(asset ReleaseAsset, out Dependencies) (Dependencies, error) {
	slices.SortFunc(out.Requires, compareRef)
	out.Requires = slices.Compact(out.Requires)
	for _, ref := range out.Requires {
		if err := ref.Check(); err != nil {
			return out, fmt.Errorf("%s has invalid binding: %w", asset.Ref, err)
		}
	}
	slices.SortFunc(out.Provides, func(a, b Capability) int {
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

func DescribeAsset(asset ReleaseAsset) (Dependencies, error) {
	return describeAsset(asset, false)
}
