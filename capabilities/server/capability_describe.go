package platformserver

import (
	"slices"
	"strconv"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) DescribeCapability(m platform.Member, ref platform.AssetRef, version int) (platform.CapabilityDescriptor, *kernel.Error) {
	if refusal := t.admits(m); refusal != nil {
		return platform.CapabilityDescriptor{}, refusal
	}
	if version > 0 {
		if descriptor, ok := t.retainedCapability(m, ref, version); ok {
			return descriptor, nil
		}
		return platform.CapabilityDescriptor{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Retained capability is unavailable to this member")
	}
	for _, descriptor := range t.Capabilities(m) {
		if descriptor.Ref != ref {
			continue
		}
		if ref.Kind == platform.AssetCompute {
			op, ordinal, ok := operationDefinition(t.app(ref.App), ref.Name, version)
			if !ok || !slices.Contains(op.Roles, m.Roles[ref.App]) {
				break
			}
			descriptor.Input, descriptor.Output, descriptor.Revision = &op.Input, &op.Output, ordinal
			descriptor.Title, descriptor.Description = op.Title, op.Description
			return descriptor, nil
		}
		if version == 0 {
			return descriptor, nil
		}
	}
	return platform.CapabilityDescriptor{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Capability or retained version is unavailable to this member")
}

func (t *Tenant) retainedCapability(m platform.Member, ref platform.AssetRef, version int) (platform.CapabilityDescriptor, bool) {
	app := t.app(ref.App)
	if app == nil || version < 1 {
		return platform.CapabilityDescriptor{}, false
	}
	d := platform.CapabilityDescriptor{Ref: ref, Version: app.Manifest().Version, Revision: version, Source: "tenant", Effects: []string{}, Ports: []platform.BlockPort{
		{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"},
		{ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"},
		{ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"},
	}}
	if ref.Kind == platform.AssetCompute {
		op, ordinal, ok := operationDefinition(app, ref.Name, version)
		if !ok || ordinal != version || !slices.Contains(op.Roles, m.Roles[ref.App]) {
			return d, false
		}
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Input, d.Output = "compute", op.Title, op.Description, "Code", "braces", "info", &op.Input, &op.Output
		d.Version += ".compute-" + strconv.Itoa(version)
		return d, true
	}
	if ref.Kind == platform.AssetFunction {
		f, ordinal, ok := functionDefinition(app, ref.Name, version)
		if !ok || ordinal != version || !slices.Contains(f.Roles, m.Roles[ref.App]) {
			return d, false
		}
		readable := false
		for _, entity := range t.Entities(m) {
			if entity.Type == f.Object && !slices.ContainsFunc(f.Fields, func(name string) bool { _, exists := entity.Field(name); return !exists }) {
				readable = true
			}
		}
		if !readable {
			return d, false
		}
		input := platform.ValueSchema{Type: "object", Properties: map[string]platform.ValueSchema{"source": {Type: "string"}}, Required: []string{"source"}}
		output := fieldsSchema(f.Output)
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Input, d.Output = "ai", f.Title, f.Description, "AI", "sparkles", "info", f.Object, &input, &output
		d.Version += ".function-" + strconv.Itoa(version)
		d.Effects = []string{"model"}
		return d, true
	}
	return d, false
}
