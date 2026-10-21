package platformserver

import (
	"context"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"platformkernel/kernel"
	"platformserver/platform"
)

// Capabilities derives the Block palette from the same permission-filtered
// owner definitions used by pages. It owns no storage or business execution.
func (t *Tenant) Capabilities(m platform.Member) []platform.CapabilityDescriptor {
	answer, _ := recordRead(t, context.Background(), func() ([]platform.CapabilityDescriptor, *kernel.Error) {
		return t.capabilities(m), nil
	})
	return answer
}

func (t *Tenant) capabilities(m platform.Member) []platform.CapabilityDescriptor {
	if t.admits(m) != nil {
		return []platform.CapabilityDescriptor{}
	}
	m = t.authorizationMember(m)
	defs := t.Definitions(m)
	out := []platform.CapabilityDescriptor{}
	if m.Holds("build", "builder") {
		out = append(out, platform.ControlBlocks()...)
	}
	for _, d := range defs {
		if len(d.Provides) == 0 {
			continue
		}
		provided := d.Provides[0]
		c := platform.CapabilityDescriptor{Ref: d.Ref, Source: d.Source, Version: d.Version, Effects: []string{},
			Ports: []platform.BlockPort{{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"}, {ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"}, {ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"}}}
		switch {
		case d.Action != nil:
			c = actionBlock(d.Ref, d.Source, d.Version, *d.Action)
		case d.Query != nil:
			q := d.Query
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone, c.Target = "query", q.Title, q.Description, "Data", "database", "info", q.Object
			if d.Source == "tenant" {
				_, text, found := strings.Cut(d.Version, ".query-")
				if found {
					c.Revision, _ = strconv.Atoi(text)
				}
			}
		case d.Function != nil:
			f := d.Function
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone, c.Target = "ai", f.Title, f.Description, "AI", "sparkles", "info", f.Object
			c.Effects = []string{"model"}
			_, text, found := strings.Cut(d.Version, ".function-")
			if found {
				c.Revision, _ = strconv.Atoi(text)
			}
		case d.Operation != nil:
			o := d.Operation
			c.Kind, c.Title, c.Description, c.Group, c.Icon, c.Tone = "compute", o.Title, o.Description, "Code", "braces", "info"
			_, text, found := strings.Cut(d.Version, ".compute-")
			if found {
				c.Revision, _ = strconv.Atoi(text)
			}
		default:
			continue
		}
		c.Side = provided.Side
		c.Parameters = slices.Clone(provided.Parameters)
		if provided.Inputs.Type != "" {
			c.Input = &provided.Inputs
		}
		if provided.Outputs.Type != "" {
			c.Output = &provided.Outputs
		}
		out = append(out, c)
	}
	slices.SortFunc(out, func(a, b platform.CapabilityDescriptor) int {
		if n := strings.Compare(a.Group, b.Group); n != 0 {
			return n
		}
		return strings.Compare(a.Ref.String(), b.Ref.String())
	})
	return out
}

func actionBlock(ref platform.AssetRef, source, version string, action platform.Action) platform.CapabilityDescriptor {
	c := platform.CapabilityDescriptor{Ref: ref, Source: source, Version: version, Kind: "action",
		Title: action.Title, Description: action.Description, Group: "Actions", Icon: "zap", Tone: "success", Target: action.Target,
		Effects: []string{"decision"}, Ports: []platform.BlockPort{
			{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"},
			{ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"},
			{ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"},
		}}
	if action.NeedsApproval || action.Approval != nil {
		c.Effects = append(c.Effects, "approval")
	}
	return c
}

func fieldValueSchema(kind, description string, choices []string) platform.ValueSchema {
	s := platform.CapabilityFieldSchema(kind, description, choices)

	return s
}
func entityValueSchema(info platform.EntityInfo) (platform.ValueSchema, bool) {
	stamp := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"by": {Type: "string"}, "at": {Type: "string"}, "change": {Type: "string"}}}
	s := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{
		"id": {Type: "string"}, "revision": {Type: "integer"}, "created": stamp, "changed": stamp, "archived": {Type: "boolean"},
	}, Required: []string{"id", "revision", "created", "changed"}}
	for _, f := range info.Fields {
		field, known := recordFieldSchema(f)
		if !known {
			return s, false
		}
		if info.Go != nil && info.Go.Kind() == reflect.Struct && len(f.Index) > 0 {
			kind := info.Go.FieldByIndex(f.Index).Type.Kind()
			field.Nullable = kind == reflect.Pointer || kind == reflect.Slice
		}
		s.Properties[f.Name] = field
	}
	return s, true
}

func recordFieldSchema(f platform.FieldInfo) (platform.ValueSchema, bool) {
	if f.Type == "json" {
		return platform.ValueSchema{}, false
	}
	if f.Type != "lines" {
		choices := slices.Clone(f.Choices)
		if len(choices) > 0 && !f.Required && !slices.Contains(choices, "") {
			choices = append(choices, "")
		}
		return fieldValueSchema(f.Type, f.Help, choices), true
	}
	row := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	for _, column := range f.Fields {
		child, known := recordFieldSchema(column)
		if !known {
			return row, false
		}
		row.Properties[column.Name] = child
	}
	return platform.ValueSchema{Type: "array", Nullable: true, Items: &row, MaxItems: 1000, Description: f.Help}, true
}
