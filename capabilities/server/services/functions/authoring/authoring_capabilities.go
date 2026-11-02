package authoring

import (
	"platformserver/platform"
	"reflect"
	"slices"
	"strconv"
	"strings"
)

func CapabilityPalette(defs []platform.Definition, controls bool) []platform.CapabilityDescriptor {
	out := []platform.CapabilityDescriptor{}
	if controls {
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
			c = ActionBlock(d.Ref, d.Source, d.Version, *d.Action)
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

func ActionBlock(ref platform.AssetRef, source, version string, action platform.Action) platform.CapabilityDescriptor {
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
		return platform.CapabilityFieldSchema(f.Type, f.Help, choices), true
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

func QueryInputSchema(q platform.NamedQuery) platform.ValueSchema {
	input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{}}
	if q.By != "" {
		input.Properties["for"] = platform.ValueSchema{Type: "string", Description: "The record this query is run for"}
		input.Required = []string{"for"}
	}
	return input
}
func QueryCapabilitySchemas(q platform.NamedQuery, info platform.EntityInfo) (*platform.ValueSchema, *platform.ValueSchema) {
	input := QueryInputSchema(q)
	if q.InterfaceShape != nil {
		common := platform.EntityInfo{}
		for _, f := range q.InterfaceShape.Fields {
			common.Fields = append(common.Fields, platform.FieldInfo{Name: f.Name, Type: f.Type})
		}
		record, known := entityValueSchema(common)
		for _, f := range q.InterfaceShape.Fields {
			field := record.Properties[f.Name]
			field.Nullable = true
			record.Properties[f.Name] = field
		}
		row := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"type": {Type: "string", Enum: q.Implementations}, "id": {Type: "string"}, "record": record}, Required: []string{"type", "id", "record"}}
		text := platform.ValueSchema{Type: "string"}
		result := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"records": {Type: "array", Items: &row, MaxItems: 200}, "total": {Type: "integer"}, "sources": {Type: "array", Items: &text}}, Required: []string{"records", "total", "sources"}}
		if !known || result.Check() != nil {
			return &input, nil
		}
		return &input, &result
	}
	if info.Type == "" {
		return &input, nil
	}
	record, known := entityValueSchema(info)
	rows := platform.ValueSchema{Type: "array", Items: &record, MaxItems: 200}
	text := platform.ValueSchema{Type: "string"}
	result := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"records": rows, "total": {Type: "integer"}, "sources": {Type: "array", Items: &text}}, Required: []string{"records", "total", "sources"}}
	if !known || result.Check() != nil {
		return &input, nil
	}
	return &input, &result
}

type CapabilityVersion struct {
	Ref          platform.AssetRef
	AppVersion   string
	Revision     int
	Query        *platform.NamedQuery
	QueryVersion string
	Entity       platform.EntityInfo
	Operation    *platform.Operation
	Function     *platform.AIFunction
}

func DescribeRetainedCapability(version CapabilityVersion) (platform.CapabilityDescriptor, bool) {
	d := platform.CapabilityDescriptor{Ref: version.Ref, Version: version.AppVersion, Revision: version.Revision, Source: "tenant", Side: platform.CapabilityPure, Effects: []string{}, Ports: []platform.BlockPort{
		{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"},
		{ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"},
		{ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"},
	}}
	switch {
	case version.Query != nil:
		q := version.Query
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Version = "query", q.Title, q.Description, "Data", "database", "info", q.Object, version.QueryVersion
		d.Input, d.Output = QueryCapabilitySchemas(*q, version.Entity)
	case version.Operation != nil:
		op := version.Operation
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Input, d.Output = "compute", op.Title, op.Description, "Code", "braces", "info", &op.Input, &op.Output
		d.Version += ".compute-" + strconv.Itoa(version.Revision)
	case version.Function != nil:
		f := version.Function
		input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}
		output := platform.CapabilityFieldsSchema(f.Output)
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Input, d.Output = "ai", f.Title, f.Description, "AI", "sparkles", "info", f.Object, &input, &output
		d.Version += ".function-" + strconv.Itoa(version.Revision)
		d.Side, d.Effects = platform.CapabilityEffect, []string{"model"}
	default:
		return d, false
	}
	return d, true
}
