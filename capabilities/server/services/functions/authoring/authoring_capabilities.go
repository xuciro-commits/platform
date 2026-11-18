package authoring

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"slices"
	"strconv"
	"strings"
)

func CapabilityPalette(defs []platform.Definition, controls bool) []protocol.CapabilityDescriptor {
	out := []protocol.CapabilityDescriptor{}
	if controls {
		out = append(out, protocol.ControlBlocks()...)
	}
	for _, d := range defs {
		if len(d.Provides) == 0 {
			continue
		}
		provided := d.Provides[0]
		c := protocol.CapabilityDescriptor{Ref: d.Ref, Source: d.Source, Version: d.Version, Effects: []string{},
			Ports: []protocol.BlockPort{{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"}, {ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"}, {ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"}}}
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
	slices.SortFunc(out, func(a, b protocol.CapabilityDescriptor) int {
		if n := strings.Compare(a.Group, b.Group); n != 0 {
			return n
		}
		return strings.Compare(a.Ref.String(), b.Ref.String())
	})
	return out
}

func ActionBlock(ref platform.AssetRef, source, version string, action platform.Action) protocol.CapabilityDescriptor {
	c := protocol.CapabilityDescriptor{Ref: ref, Source: source, Version: version, Kind: "action",
		Title: action.Title, Description: action.Description, Group: "Actions", Icon: "zap", Tone: "success", Target: action.Target,
		Effects: []string{"decision"}, Ports: []protocol.BlockPort{
			{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"},
			{ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"},
			{ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"},
		}}
	if action.NeedsApproval || action.Approval != nil {
		c.Effects = append(c.Effects, "approval")
	}
	return c
}

func DraftActionBlocks(app string, actions []platform.Action) []protocol.CapabilityDescriptor {
	result := []protocol.CapabilityDescriptor{}
	for _, action := range actions {
		if action.Automation {
			continue
		}
		block := ActionBlock(platform.AssetRef{App: app, Kind: platform.AssetAction, Name: action.Schema}, "draft", "", action)
		block.Side = platform.CapabilityAuthoritative
		block.Parameters = slices.Clone(action.Payload)
		input := protocol.CapabilityFieldsSchema(action.Payload)
		if input.Type != "" {
			block.Input = &input
		}
		result = append(result, block)
	}
	return result
}

type CapabilityVersion struct {
	Ref          platform.AssetRef
	AppVersion   string
	Revision     int
	Query        *platform.NamedQuery
	QueryVersion string
	Entity       platform.RecordTypeInfo
	Operation    *platform.Operation
	Function     *platform.AIFunction
}

func DescribeRetainedCapability(version CapabilityVersion) (protocol.CapabilityDescriptor, bool) {
	d := protocol.CapabilityDescriptor{Ref: version.Ref, Version: version.AppVersion, Revision: version.Revision, Source: "tenant", Side: platform.CapabilityPure, Effects: []string{}, Ports: []protocol.BlockPort{
		{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"},
		{ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"},
		{ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"},
	}}
	switch {
	case version.Query != nil:
		q := version.Query
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Version = "query", q.Title, q.Description, "Data", "database", "info", q.Object, version.QueryVersion
		d.Input, d.Output = protocol.QueryCapabilitySchemas(*q, version.Entity)
	case version.Operation != nil:
		op := version.Operation
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Input, d.Output = "compute", op.Title, op.Description, "Code", "braces", "info", &op.Input, &op.Output
		d.Version += ".compute-" + strconv.Itoa(version.Revision)
	case version.Function != nil:
		f := version.Function
		input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}
		output := protocol.CapabilityFieldsSchema(f.Output)
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Input, d.Output = "ai", f.Title, f.Description, "AI", "sparkles", "info", f.Object, &input, &output
		d.Version += ".function-" + strconv.Itoa(version.Revision)
		d.Side, d.Effects = platform.CapabilityEffect, []string{"model"}
	default:
		return d, false
	}
	return d, true
}
