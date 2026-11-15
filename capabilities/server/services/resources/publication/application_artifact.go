package publication

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"platformserver/platform"
)

func (resolver Resolver) ArtifactOfApplication(root platform.AssetRef, available []platform.ReleaseAsset) (platform.ApplicationArtifact, error) {
	artifact := platform.ApplicationArtifact{Ref: root, Model: []platform.AssetBinding{}, Rules: []platform.AssetBinding{}, Pages: []platform.AssetBinding{}, Flows: []platform.AssetBinding{}, AI: []platform.AssetBinding{}, Integrations: []platform.AssetBinding{}, Samples: []platform.AssetBinding{}, Roles: []platform.ApplicationRole{}, Depends: []platform.AssetBinding{}, Environment: []platform.ApplicationBinding{}}
	if root.Kind != platform.AssetApp {
		return artifact, fmt.Errorf("an application artifact needs its application root")
	}
	candidate, err := resolver.Candidate([]platform.AssetRef{root}, available)
	if err != nil {
		return artifact, err
	}
	artifact.Version = candidate.ID
	for _, asset := range candidate.Assets {
		if asset.Ref == root {
			var declaration platform.Application
			if err := json.Unmarshal(asset.Body, &declaration); err != nil {
				return artifact, err
			}
			if err := declaration.CheckResources(); err != nil {
				return artifact, err
			}
			artifact.Title, artifact.Profile = declaration.Title, declaration.Profile
			continue
		}
		binding := platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}
		roles, err := applicationAssetRoles(asset)
		if err != nil {
			return artifact, err
		}
		artifact.Roles = append(artifact.Roles, roles...)
		switch asset.Ref.Kind {
		case platform.AssetObject, platform.AssetObjectTypeGroup, platform.AssetLinkType, platform.AssetPropertyType, platform.AssetValueType, platform.AssetQuery:
			artifact.Model = append(artifact.Model, binding)
		case platform.AssetAction:
			artifact.Rules = append(artifact.Rules, binding)
		case platform.AssetPage:
			artifact.Pages = append(artifact.Pages, binding)
		case platform.AssetFlow:
			artifact.Flows = append(artifact.Flows, binding)
		case platform.AssetFunction, platform.AssetAgent, platform.AssetEvaluation:
			artifact.AI = append(artifact.AI, binding)
		case platform.AssetConnection, platform.AssetSource, platform.AssetListener, platform.AssetDataset, platform.AssetMediaSet, platform.AssetSchedule, platform.AssetPipeline, platform.AssetWriteback, platform.AssetMatching, platform.AssetObservation:
			artifact.Integrations = append(artifact.Integrations, binding)
		case platform.AssetSampleSet, platform.AssetTestPlan:
			artifact.Samples = append(artifact.Samples, binding)
		case platform.AssetCompute:
			artifact.Rules = append(artifact.Rules, binding)
		}
		if asset.Ref.Kind == platform.AssetConnection {
			var contract struct {
				Kind       string `json:"kind"`
				Credential bool   `json:"credential"`
			}
			if err := json.Unmarshal(asset.Body, &contract); err != nil {
				return artifact, err
			}
			artifact.Environment = append(artifact.Environment, platform.ApplicationBinding{Ref: asset.Ref, Kind: contract.Kind, Credential: contract.Credential})
		}
		if asset.Ref.Kind == platform.AssetEvaluation {
			set, err := platform.ReadEvaluationSet(asset.Body)
			if err != nil {
				return artifact, err
			}
			artifact.Environment = append(artifact.Environment, platform.ApplicationBinding{Ref: asset.Ref, Kind: "ai-model", Name: set.Model})
		}
		if asset.Ref.App != root.App {
			artifact.Depends = append(artifact.Depends, binding)
		}
	}
	if artifact.Profile != nil {
		if err := checkApplicationProfileClosure(root, candidate.Assets); err != nil {
			return artifact, err
		}
	}
	return artifact, nil
}

func applicationAssetRoles(asset platform.ReleaseAsset) ([]platform.ApplicationRole, error) {
	var descriptor struct {
		Entity platform.EntityInfo `json:"entity"`
		Access []struct {
			Role string `json:"role"`
			Read string `json:"read"`
		} `json:"access"`
		Fields      json.RawMessage `json:"fields"`
		Owners      []string        `json:"owners"`
		HandoffRole string          `json:"handoffRole"`
		Definition  struct {
			Owners []string `json:"owners"`
		} `json:"definition"`
		Scope struct {
			Levels map[string]string `json:"levels"`
		} `json:"scope"`
		Standard  struct{ Roles []string }                         `json:"standard"`
		Roles     []string                                         `json:"roles"`
		Action    platform.Action                                  `json:"action"`
		Lifecycle struct{ Transitions []struct{ Roles []string } } `json:"lifecycle"`
	}
	if !slices.Contains([]platform.AssetKind{platform.AssetObject, platform.AssetAction, platform.AssetFunction, platform.AssetCompute, platform.AssetFlow, platform.AssetAgent, platform.AssetObservation}, asset.Ref.Kind) {
		return nil, nil
	}
	if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
		return nil, err
	}
	roles := map[string]*platform.ApplicationRole{}
	add := func(role string) *platform.ApplicationRole {
		if value := roles[role]; value != nil {
			return value
		}
		value := &platform.ApplicationRole{Resource: platform.AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Role: role}
		roles[role] = value
		return value
	}
	for _, access := range descriptor.Access {
		add(access.Role).Scope = access.Read
	}
	for _, role := range append(slices.Clone(descriptor.Owners), descriptor.Definition.Owners...) {
		add(role)
	}
	if descriptor.HandoffRole != "" {
		add(descriptor.HandoffRole)
	}
	for role, scope := range descriptor.Scope.Levels {
		add(role).Scope = scope
	}
	for _, role := range append(append(slices.Clone(descriptor.Roles), descriptor.Standard.Roles...), descriptor.Action.Roles...) {
		add(role)
	}
	for _, transition := range descriptor.Lifecycle.Transitions {
		for _, role := range transition.Roles {
			add(role)
		}
	}
	var fields []platform.FieldInfo
	if asset.Ref.Kind == platform.AssetObject {
		info, err := queryObjectDescriptor(asset.Body)
		if err != nil {
			return nil, err
		}
		fields = info.Fields
	}
	for _, field := range fields {
		for _, role := range field.Read {
			value := add(role)
			value.ReadFields = append(value.ReadFields, field.Name)
		}
		for _, role := range field.Write {
			value := add(role)
			value.WriteFields = append(value.WriteFields, field.Name)
		}
	}
	out := make([]platform.ApplicationRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, *role)
	}
	slices.SortFunc(out, func(a, b platform.ApplicationRole) int { return strings.Compare(a.Role, b.Role) })
	return out, nil
}

func checkApplicationProfileClosure(root platform.AssetRef, assets []platform.ReleaseAsset) error {
	lookup := map[platform.AssetRef]platform.ReleaseAsset{}
	for _, asset := range assets {
		lookup[asset.Ref] = asset
	}
	var application platform.Application
	if err := json.Unmarshal(lookup[root].Body, &application); err != nil {
		return err
	}
	if application.Profile == nil {
		return nil
	}
	objects := map[string]bool{}
	seen := map[platform.AssetRef]bool{}
	var visit func(platform.AssetRef)
	visit = func(ref platform.AssetRef) {
		if seen[ref] {
			return
		}
		seen[ref] = true
		asset, exists := lookup[ref]
		if !exists {
			return
		}
		if ref.Kind == platform.AssetObject {
			objects[ref.Name] = true
		}
		for _, dependency := range asset.Requires {
			visit(dependency)
		}
	}
	visit(root)
	for typ := range application.Profile.ExpectedRecords {
		if !objects[typ] {
			return fmt.Errorf("expected record type %s is outside the application artifact", typ)
		}
	}
	return nil
}
