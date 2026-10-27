package platform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const (
	CapabilityPure          = "pure"
	CapabilityEffect        = "effect"
	CapabilityAuthoritative = "authoritative"
)

// Dependencies is the one owner-produced description used by release closure,
// lineage, impact and callable discovery. It carries no permission grant.
type Dependencies struct {
	Ref      AssetRef     `json:"ref"`
	Requires []AssetRef   `json:"requires"`
	Provides []Capability `json:"provides"`
	Version  string       `json:"version"`
}

type Capability struct {
	Kind    string      `json:"kind"`
	Name    string      `json:"name"`
	Inputs  ValueSchema `json:"inputs"`
	Outputs ValueSchema `json:"outputs"`
	Roles   []string    `json:"roles"`
	Side    string      `json:"side"`
	// Parameters preserves an owner's opaque JSON payload rather than
	// inventing a structural schema for it. Empty schemas mean owner-defined.
	Parameters []Field `json:"parameters,omitempty"`
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

// ProtocolReleaseDescriptor freezes the original protocol and its provider
// bindings. Reads remain Probe/read calls; actions remain independent requests.
type ProtocolReleaseDescriptor struct {
	Name           string              `json:"name"`
	Version        int                 `json:"version"`
	Actions        []Action            `json:"actions"`
	Reads          []string            `json:"reads"`
	ActionBindings map[string]AssetRef `json:"actionBindings,omitempty"`
	ActionRoles    map[string][]string `json:"actionRoles,omitempty"`
	ReadBindings   map[string]AssetRef `json:"readBindings,omitempty"`
}

// MarshalJSON retains historical omission of Provides, while a newly produced
// descriptor with no callable capabilities still seals an explicit empty list.
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

// DescribeAsset derives edges and capabilities from complete owner bytes. A
// caller-supplied Requires/Provides list is never used as semantic input.
func DescribeAsset(asset ReleaseAsset) (Dependencies, error) {
	return describeAsset(asset, false)
}

func describeAsset(asset ReleaseAsset, historical bool) (Dependencies, error) {
	out := Dependencies{Ref: asset.Ref, Requires: []AssetRef{}, Provides: []Capability{}, Version: asset.SourceVersion}
	if err := asset.Ref.Check(); err != nil {
		return out, err
	}
	if _, err := canonicalBody(asset.Body); err != nil {
		return out, fmt.Errorf("%s: %w", asset.Ref, err)
	}
	var identity struct{ Name, Type, Schema string }
	if err := json.Unmarshal(asset.Body, &identity); err != nil {
		return out, err
	}
	name := identity.Name
	if asset.Ref.Kind == AssetObject {
		name = identity.Type
	}
	if asset.Ref.Kind == AssetAction {
		name = identity.Schema
	}
	if name != asset.Ref.Name {
		return out, fmt.Errorf("release asset %s descriptor identity differs", asset.Ref)
	}
	var bindings struct {
		Objects   map[string]AssetRef `json:"objectBindings"`
		Protocols map[string]AssetRef `json:"protocolBindings"`
	}
	if err := json.Unmarshal(asset.Body, &bindings); err != nil {
		return out, err
	}
	scan := &assetScan{asset: asset, historical: historical, out: out, bindings: bindings}
	if scanner, ok := assetScanners[asset.Ref.Kind]; ok {
		if err := scanner(scan); err != nil {
			return scan.out, err
		}
	}
	out = scan.out
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

// CapabilityFieldsSchema projects original payload fields. An empty Type
// leaves opaque JSON with its owner instead of falsely advertising text.
func CapabilityFieldsSchema(fields []Field) ValueSchema {
	schema := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	for _, field := range fields {
		value := CapabilityFieldSchema(field.Type, field.Description, field.Choices)
		if value.Type == "" {
			return ValueSchema{}
		}
		schema.Properties[field.Name] = value
		if field.Required {
			schema.Required = append(schema.Required, field.Name)
		}
	}
	slices.Sort(schema.Required)
	return schema
}

func CapabilityFieldSchema(kind, description string, choices []string) ValueSchema {
	value := ValueSchema{Description: description}
	switch kind {
	case "integer":
		value.Type = "integer"
	case "decimal", "number":
		value.Type = "number"
	case "boolean":
		value.Type = "boolean"
	case "string[]", "tags", "references":
		item := ValueSchema{Type: "string"}
		value.Type = "array"
		value.Items = &item
		value.MaxItems = 1000
	case "money":
		value.Type = "object"
		value.Properties = map[string]ValueSchema{"amount": {Type: "integer"}, "currency": {Type: "string"}}
		value.Required = []string{"amount", "currency"}
	case "json":
		return ValueSchema{}
	default:
		value.Type = "string"
		value.Enum = slices.Clone(choices)
	}
	return value
}

// CapabilityEntitySchema describes an object's original fields. The host
// applies its existing field permissions before using this projection.
func CapabilityEntitySchema(info EntityInfo) ValueSchema {
	schema := ValueSchema{Type: "object", Properties: map[string]ValueSchema{"id": {Type: "string"}, "revision": {Type: "integer"}, "archived": {Type: "boolean"}}, Required: []string{"id", "revision"}}
	for _, field := range info.Fields {
		value := capabilityRecordFieldSchema(field)
		if value.Type == "" {
			return ValueSchema{}
		}
		schema.Properties[field.Name] = value
	}
	if schema.Check() != nil {
		return ValueSchema{}
	}
	return schema
}

func capabilityRecordFieldSchema(field FieldInfo) ValueSchema {
	if field.Type != "lines" {
		return CapabilityFieldSchema(field.Type, field.Help, field.Choices)
	}
	row := ValueSchema{Type: "object", Properties: map[string]ValueSchema{}}
	for _, column := range field.Fields {
		value := capabilityRecordFieldSchema(column)
		if value.Type == "" {
			return ValueSchema{}
		}
		row.Properties[column.Name] = value
	}
	return ValueSchema{Type: "array", Items: &row, MaxItems: 1000, Description: field.Help}
}

func pageDependencies(page Page) []AssetRef {
	var refs []AssetRef
	if page.Object.Name != "" {
		refs = append(refs, page.Object)
	}
	refs = append(refs, page.NavigationTargets()...)
	refs = append(refs, page.ModelDependencies()...)
	for _, resource := range page.ComputeResources() {
		refs = append(refs, resource.Operation.Ref)
	}
	for _, embedding := range page.EmbeddingBindings() {
		refs = append(refs, embedding.Page.Ref)
	}
	refs = append(refs, page.Actions...)
	refs = append(refs, page.QueryReferences()...)
	for _, selection := range page.Selections {
		refs = append(refs, selection.Object)
	}
	for _, section := range page.Sections {
		if section.Function != nil {
			refs = append(refs, section.Function.Ref)
		}
		if section.Operation != nil {
			refs = append(refs, section.Operation.Ref)
		}
		if section.Object.Name != "" {
			refs = append(refs, section.Object)
		}
		if section.Query.Name != "" {
			refs = append(refs, section.Query)
		}
		for _, group := range section.RecordLinks {
			refs = append(refs, group.Object)
		}
		refs = append(refs, section.ServiceDependencies()...)
		refs = append(refs, section.ExplorationReferences()...)
		refs = append(refs, section.Actions...)
		if section.InlineEdit != nil {
			refs = append(refs, section.InlineEdit.Action)
		}
	}
	return refs
}

func checkReleaseBindings(ref AssetRef, body []byte, declared []AssetRef) error {
	description, err := describeAsset(ReleaseAsset{Ref: ref, Body: body}, true)
	if err != nil {
		return err
	}
	for _, dep := range description.Requires {
		if !slices.Contains(declared, dep) {
			return fmt.Errorf("release asset %s omits bound dependency %s", ref, dep)
		}
	}
	return nil
}

// CheckAssetDependencies validates the frozen owner description. Historical
// assets keep the original subset rules; newly produced assets must be exact.
func CheckAssetDependencies(asset ReleaseAsset) error { return checkAssetDescription(asset) }

func checkAssetDescription(asset ReleaseAsset) error {
	if asset.Provides == nil {
		return checkReleaseBindings(asset.Ref, asset.Body, asset.Requires)
	}
	description, err := DescribeAsset(asset)
	if err != nil {
		return err
	}
	actual := slices.Clone(asset.Requires)
	slices.SortFunc(actual, compareRef)
	for _, dependency := range description.Requires {
		if !slices.Contains(actual, dependency) {
			return fmt.Errorf("release asset %s omits bound dependency %s", asset.Ref, dependency)
		}
	}
	if !slices.Equal(actual, description.Requires) {
		return fmt.Errorf("%s requires list differs from its body references", asset.Ref)
	}
	for _, capability := range asset.Provides {
		if err := capability.Check(); err != nil {
			return err
		}
	}
	actualCapabilities, _ := json.Marshal(asset.Provides)
	expectedCapabilities, _ := json.Marshal(description.Provides)
	if !bytes.Equal(actualCapabilities, expectedCapabilities) {
		return fmt.Errorf("%s provides list differs from its owner declaration", asset.Ref)
	}
	return nil
}
