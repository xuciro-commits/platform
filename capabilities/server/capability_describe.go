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
		if descriptor.Ref == ref {
			return descriptor, nil
		}
	}
	return platform.CapabilityDescriptor{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Capability or retained version is unavailable to this member")
}

func (t *Tenant) retainedCapability(m platform.Member, ref platform.AssetRef, version int) (platform.CapabilityDescriptor, bool) {
	m = t.authorizationMember(m)
	app := t.app(ref.App)
	if app == nil || version < 1 {
		return platform.CapabilityDescriptor{}, false
	}
	d := platform.CapabilityDescriptor{Ref: ref, Version: app.Manifest().Version, Revision: version, Source: "tenant", Side: platform.CapabilityPure, Effects: []string{}, Ports: []platform.BlockPort{
		{ID: "in", Title: "In", Direction: "input", Channel: "control", Type: "flow"},
		{ID: "next", Title: "Next", Direction: "output", Channel: "control", Type: "flow"},
		{ID: "error", Title: "Error", Direction: "output", Channel: "control", Type: "flow"},
	}}
	if ref.Kind == platform.AssetQuery {
		q, source, ok := t.memberQueryVersion(m, ref, version)
		if !ok {
			return d, false
		}
		var info platform.EntityInfo
		for _, entity := range t.Entities(m) {
			if entity.Type == q.Object {
				info = entity
				break
			}
		}
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Version = "query", q.Title, q.Description, "Data", "database", "info", q.Object, source
		d.Input, d.Output = queryCapabilitySchemas(q, info)
		return d, true
	}
	if ref.Kind == platform.AssetCompute {
		op, ordinal, ok := operationDefinition(app, ref.Name, version)
		caller := platform.NewCaller(runtime{t}, m, ref.App, false, false)
		if !ok || ordinal != version || !t.authorizeCapability(caller, ref.App, op.Name, op.Roles, true, authorizationDetails{}).Allow {
			return d, false
		}
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Input, d.Output = "compute", op.Title, op.Description, "Code", "braces", "info", &op.Input, &op.Output
		d.Version += ".compute-" + strconv.Itoa(version)
		return d, true
	}
	if ref.Kind == platform.AssetFunction {
		d.Side = platform.CapabilityEffect
		f, ordinal, ok := functionDefinition(app, ref.Name, version)
		caller := platform.NewCaller(runtime{t}, m, ref.App, false, false)
		if !ok || ordinal != version || !t.authorizeCapability(caller, ref.App, f.Name, f.Roles, true, authorizationDetails{}).Allow {
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
		output := platform.CapabilityFieldsSchema(f.Output)
		d.Kind, d.Title, d.Description, d.Group, d.Icon, d.Tone, d.Target, d.Input, d.Output = "ai", f.Title, f.Description, "AI", "sparkles", "info", f.Object, &input, &output
		d.Version += ".function-" + strconv.Itoa(version)
		d.Effects = []string{"model"}
		return d, true
	}
	return d, false
}
