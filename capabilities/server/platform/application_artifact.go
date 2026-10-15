package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// ApplicationProfile is a declared operating envelope, not a performance or
// recovery guarantee. Original owners retain and recover their own data.
type ApplicationProfile struct {
	ExpectedRecords map[string]int64  `json:"expectedRecords,omitempty"`
	ExpectedRuns    int64             `json:"expectedRuns,omitempty"`
	Retention       map[string]string `json:"retention,omitempty"`
	RPO             string            `json:"rpo,omitempty"`
	RTO             string            `json:"rto,omitempty"`
}

func (p *ApplicationProfile) UnmarshalJSON(raw []byte) error {
	type declaration ApplicationProfile
	var decoded declaration
	reader := json.NewDecoder(bytes.NewReader(raw))
	reader.DisallowUnknownFields()
	if err := reader.Decode(&decoded); err != nil {
		return err
	}
	if reader.Decode(new(any)) != io.EOF {
		return fmt.Errorf("application profile has trailing data")
	}
	*p = ApplicationProfile(decoded)
	return p.Check()
}

func (p ApplicationProfile) Check() error {
	if len(p.ExpectedRecords) > 1000 || p.ExpectedRuns < 0 || len(p.Retention) > 16 {
		return fmt.Errorf("application profile exceeds its declaration budget")
	}
	for typ, count := range p.ExpectedRecords {
		if count < 1 || !strings.Contains(typ, ".") || len(typ) > 256 {
			return fmt.Errorf("expected records need a type and a positive count")
		}
	}
	for name, duration := range p.Retention {
		if !slices.Contains([]string{"runs", "logs", "effects", "ai", "compute"}, name) {
			return fmt.Errorf("unknown application retention category")
		}
		if value, err := time.ParseDuration(duration); err != nil || value <= 0 {
			return fmt.Errorf("application retention needs a positive duration")
		}
	}
	for _, duration := range []string{p.RPO, p.RTO} {
		if duration == "" {
			continue
		}
		if value, err := time.ParseDuration(duration); err != nil || value <= 0 {
			return fmt.Errorf("application recovery declarations need positive durations")
		}
	}
	return nil
}

// ApplicationArtifact is the original release owner's closed candidate rooted
// at one application. Bindings carry immutable owner versions; no second
// publication, asset registry or runtime state is kept here.
type ApplicationArtifact struct {
	Samples      []AssetBinding       `json:"samples"`
	Ref          AssetRef             `json:"ref"`
	Version      string               `json:"version"`
	Title        string               `json:"title"`
	Profile      *ApplicationProfile  `json:"profile,omitempty"`
	Model        []AssetBinding       `json:"model"`
	Rules        []AssetBinding       `json:"rules"`
	Pages        []AssetBinding       `json:"pages"`
	Flows        []AssetBinding       `json:"flows"`
	AI           []AssetBinding       `json:"ai"`
	Integrations []AssetBinding       `json:"integrations"`
	Roles        []ApplicationRole    `json:"roles"`
	Depends      []AssetBinding       `json:"depends"`
	Environment  []ApplicationBinding `json:"environment"`
}

// Roles project the frozen owners' declarations; they never create grants.
type ApplicationRole struct {
	Resource    AssetBinding `json:"resource"`
	Role        string       `json:"role"`
	Scope       string       `json:"scope,omitempty"`
	ReadFields  []string     `json:"readFields,omitempty"`
	WriteFields []string     `json:"writeFields,omitempty"`
}

// A connection's local address, credentials and readiness stay outside the
// artifact; it declares only the owner identity and required connection kind.
type ApplicationBinding struct {
	Ref        AssetRef `json:"ref"`
	Kind       string   `json:"kind"`
	Name       string   `json:"name,omitempty"`
	Credential bool     `json:"credential,omitempty"`
}

func ArtifactOfApplication(root AssetRef, available []ReleaseAsset) (ApplicationArtifact, error) {
	artifact := ApplicationArtifact{Ref: root, Model: []AssetBinding{}, Rules: []AssetBinding{}, Pages: []AssetBinding{}, Flows: []AssetBinding{}, AI: []AssetBinding{}, Integrations: []AssetBinding{}, Samples: []AssetBinding{}, Roles: []ApplicationRole{}, Depends: []AssetBinding{}, Environment: []ApplicationBinding{}}
	if root.Kind != AssetApp {
		return artifact, fmt.Errorf("an application artifact needs its application root")
	}
	candidate, err := Candidate([]AssetRef{root}, available)
	if err != nil {
		return artifact, err
	}
	artifact.Version = candidate.ID
	for _, asset := range candidate.Assets {
		if asset.Ref == root {
			var declaration Application
			if err := json.Unmarshal(asset.Body, &declaration); err != nil {
				return artifact, err
			}
			if err := declaration.CheckResources(); err != nil {
				return artifact, err
			}
			artifact.Title, artifact.Profile = declaration.Title, declaration.Profile
			continue
		}
		binding := AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}
		roles, err := applicationAssetRoles(asset)
		if err != nil {
			return artifact, err
		}
		artifact.Roles = append(artifact.Roles, roles...)
		switch asset.Ref.Kind {
		case AssetObject, AssetLinkType, AssetPropertyType, AssetQuery:
			artifact.Model = append(artifact.Model, binding)
		case AssetAction:
			artifact.Rules = append(artifact.Rules, binding)
		case AssetPage:
			artifact.Pages = append(artifact.Pages, binding)
		case AssetFlow:
			artifact.Flows = append(artifact.Flows, binding)
		case AssetFunction, AssetAgent, AssetEvaluation:
			artifact.AI = append(artifact.AI, binding)
		case AssetConnection, AssetSource, AssetDataset, AssetPipeline, AssetWriteback, AssetMatching, AssetObservation:
			artifact.Integrations = append(artifact.Integrations, binding)
		case AssetSampleSet, AssetTestPlan:
			artifact.Samples = append(artifact.Samples, binding)
		case AssetCompute, AssetMigration:
			artifact.Rules = append(artifact.Rules, binding)
		}
		if asset.Ref.Kind == AssetConnection {
			var contract struct {
				Kind       string `json:"kind"`
				Credential bool   `json:"credential"`
			}
			if err := json.Unmarshal(asset.Body, &contract); err != nil {
				return artifact, err
			}
			artifact.Environment = append(artifact.Environment, ApplicationBinding{Ref: asset.Ref, Kind: contract.Kind, Credential: contract.Credential})
		}
		if asset.Ref.Kind == AssetEvaluation {
			set, err := ReadEvaluationSet(asset.Body)
			if err != nil {
				return artifact, err
			}
			artifact.Environment = append(artifact.Environment, ApplicationBinding{Ref: asset.Ref, Kind: "ai-model", Name: set.Model})
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

func applicationAssetRoles(asset ReleaseAsset) ([]ApplicationRole, error) {
	var descriptor struct {
		Entity EntityInfo `json:"entity"`
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
		Action    Action                                           `json:"action"`
		Lifecycle struct{ Transitions []struct{ Roles []string } } `json:"lifecycle"`
	}
	if !slices.Contains([]AssetKind{AssetObject, AssetAction, AssetFunction, AssetCompute, AssetFlow, AssetAgent, AssetObservation}, asset.Ref.Kind) {
		return nil, nil
	}
	if err := json.Unmarshal(asset.Body, &descriptor); err != nil {
		return nil, err
	}
	roles := map[string]*ApplicationRole{}
	add := func(role string) *ApplicationRole {
		if value := roles[role]; value != nil {
			return value
		}
		value := &ApplicationRole{Resource: AssetBinding{Ref: asset.Ref, SourceVersion: asset.SourceVersion}, Role: role}
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
	var fields []FieldInfo
	if asset.Ref.Kind == AssetObject {
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
	out := make([]ApplicationRole, 0, len(roles))
	for _, role := range roles {
		out = append(out, *role)
	}
	slices.SortFunc(out, func(a, b ApplicationRole) int { return strings.Compare(a.Role, b.Role) })
	return out, nil
}

func checkApplicationProfileClosure(root AssetRef, assets []ReleaseAsset) error {
	lookup := map[AssetRef]ReleaseAsset{}
	for _, asset := range assets {
		lookup[asset.Ref] = asset
	}
	var application Application
	if err := json.Unmarshal(lookup[root].Body, &application); err != nil {
		return err
	}
	if application.Profile == nil {
		return nil
	}
	objects := map[string]bool{}
	seen := map[AssetRef]bool{}
	var visit func(AssetRef)
	visit = func(ref AssetRef) {
		if seen[ref] {
			return
		}
		seen[ref] = true
		asset, exists := lookup[ref]
		if !exists {
			return
		}
		if ref.Kind == AssetObject {
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
