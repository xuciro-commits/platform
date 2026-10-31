package platform

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type AgentReleaseDescriptor struct {
	Name         string              `json:"name"`
	Title        string              `json:"title"`
	Description  string              `json:"description"`
	Model        string              `json:"model,omitempty"`
	ModelOptions ModelOptions        `json:"modelOptions,omitzero"`
	Instructions string              `json:"instructions"`
	Tools        []string            `json:"tools"`
	ToolBindings map[string]AssetRef `json:"toolBindings,omitempty"`
	Budget       Budget              `json:"budget"`
	Checkpoints  []string            `json:"checkpoints,omitempty"`
	HandoffRole  string              `json:"handoffRole,omitempty"`
	Cases        json.RawMessage     `json:"cases,omitempty"`
	Native       bool                `json:"native,omitempty"`
	Guard        bool                `json:"guard,omitempty"`
	Handoff      bool                `json:"handoff,omitempty"`
}

func (a AgentReleaseDescriptor) Check() error {
	if !functionName.MatchString(a.Name) || a.Title == "" || strings.TrimSpace(a.Instructions) == "" || len(a.Instructions) > 65536 || len(a.Tools) > 1000 || a.Budget.Steps < 0 || a.Budget.Tokens < 0 || a.Budget.Actions < 0 || a.Budget.Cost < 0 || len(a.Cases) > 262144 {
		return fmt.Errorf("agent needs a valid identity, instructions and bounded tools/budget")
	}
	if len(a.Cases) > 0 && (!json.Valid(a.Cases) || a.Cases[0] != '[') {
		return fmt.Errorf("agent evaluation cases need an array")
	}
	for tool, ref := range a.ToolBindings {
		if !slices.Contains(a.Tools, tool) || ref.Check() != nil {
			return fmt.Errorf("agent tool binding must retain an original declared tool and asset")
		}
	}
	for _, tool := range a.Checkpoints {
		if !slices.Contains(a.Tools, tool) {
			return fmt.Errorf("agent checkpoint is not a declared tool")
		}
	}
	return nil
}

func (a AgentReleaseDescriptor) Dependencies(owner string) []AssetRef {
	refs := []AssetRef{}
	for _, tool := range a.Tools {
		if ref, ok := a.ToolBindings[tool]; ok {
			refs = append(refs, ref)
			continue
		}
		if a.Native && !strings.Contains(tool, ".") && !strings.HasPrefix(tool, "query:") {
			continue
		}
		if name, ok := strings.CutPrefix(tool, "query:"); ok {
			refs = append(refs, AssetRef{App: owner, Kind: AssetQuery, Name: name})
			continue
		}
		if strings.ContainsAny(tool, "#:") {
			continue
		}
		refs = append(refs, AssetRef{App: owner, Kind: AssetAction, Name: tool})
	}
	slices.SortFunc(refs, compareRef)
	return slices.Compact(refs)
}

const (
	CapabilityPure          = "pure"
	CapabilityEffect        = "effect"
	CapabilityAuthoritative = "authoritative"
)

type Dependencies struct {
	Ref      AssetRef     `json:"ref"`
	Requires []AssetRef   `json:"requires"`
	Provides []Capability `json:"provides"`
	Version  string       `json:"version"`
}

type Capability struct {
	Kind       string      `json:"kind"`
	Name       string      `json:"name"`
	Inputs     ValueSchema `json:"inputs"`
	Outputs    ValueSchema `json:"outputs"`
	Roles      []string    `json:"roles"`
	Side       string      `json:"side"`
	Parameters []Field     `json:"parameters,omitempty"`
}

func (c Capability) Check() error {
	if !slices.Contains([]string{"action", "query", "function", "operation", "protocol", "interface"}, c.Kind) || c.Name == "" || !slices.Contains([]string{CapabilityPure, CapabilityEffect, CapabilityAuthoritative}, c.Side) {
		return fmt.Errorf("capability %q needs a supported kind, stable name and side", c.Name)
	}
	for _, schema := range []ValueSchema{c.Inputs, c.Outputs} {
		if schema.Type != "" {
			if err := schema.Check(); err != nil {
				return fmt.Errorf("capability %s: %w", c.Name, err)
			}
		}
	}
	return nil
}

type ProtocolReleaseDescriptor struct {
	Name           string              `json:"name"`
	Version        int                 `json:"version"`
	Actions        []Action            `json:"actions"`
	Reads          []string            `json:"reads"`
	ActionBindings map[string]AssetRef `json:"actionBindings,omitempty"`
	ActionRoles    map[string][]string `json:"actionRoles,omitempty"`
	ReadBindings   map[string]AssetRef `json:"readBindings,omitempty"`
}

func (a ReleaseAsset) MarshalJSON() ([]byte, error) {
	type wire ReleaseAsset
	var provides *[]Capability
	if a.Provides != nil {
		provides = &a.Provides
	}
	return json.Marshal(struct {
		wire
		Provides *[]Capability `json:"provides,omitempty"`
	}{wire: wire(a), Provides: provides})
}

func WithDependencies(asset ReleaseAsset) (ReleaseAsset, error) {
	description, err := DescribeAsset(asset)
	if err != nil {
		return ReleaseAsset{}, err
	}
	asset.Requires, asset.Provides = description.Requires, description.Provides
	return asset, nil
}
