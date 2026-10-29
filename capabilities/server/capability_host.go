package platformserver

import (
	"context"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	agentowner "platformserver/engine/agents"
	"platformserver/platform"
	"platformserver/studio/authoring"
	"slices"
	"strconv"
)

func (t *Tenant) Capabilities(m platform.Member) []platform.CapabilityDescriptor {
	answer, _ := recordRead(t, context.Background(), func() ([]platform.CapabilityDescriptor, *kernel.Error) { return t.capabilities(m), nil })
	return answer
}
func (t *Tenant) capabilities(m platform.Member) []platform.CapabilityDescriptor {
	if t.admits(m) != nil {
		return []platform.CapabilityDescriptor{}
	}
	m = t.authorizationMember(m)
	definitions := t.Definitions(m)
	visible := make([]platform.Definition, 0, len(definitions))
	for _, definition := range definitions {
		if len(definition.Provides) == 0 {
			continue
		}
		var permission string
		var roles []string
		switch {
		case definition.Action != nil:
			permission, roles = definition.Ref.Name, definition.Action.Roles
		case definition.Function != nil:
			permission, roles = definition.Function.Name, definition.Function.Roles
		case definition.Operation != nil:
			permission, roles = definition.Operation.Name, definition.Operation.Roles
		}
		if permission != "" {
			caller := platform.NewCaller(runtime{t}, m, definition.Ref.App, false, false)
			if !t.authorizeCapability(caller, definition.Ref.App, permission, roles, true, authorizationDetails{}).Allow {
				continue
			}
		}
		visible = append(visible, definition)
	}
	return authoring.CapabilityPalette(visible, m.Holds("build", "builder"))
}

func (t *Tenant) DescribeCapability(m platform.Member, ref platform.AssetRef, version int) (platform.CapabilityDescriptor, *kernel.Error) {
	return recordRead(t, context.Background(), func() (platform.CapabilityDescriptor, *kernel.Error) {
		return t.describeCapability(m, ref, version)
	})
}

func (t *Tenant) describeCapability(m platform.Member, ref platform.AssetRef, version int) (platform.CapabilityDescriptor, *kernel.Error) {
	if refusal := t.admits(m); refusal != nil {
		return platform.CapabilityDescriptor{}, refusal
	}
	if version > 0 {
		if descriptor, ok := t.retainedCapability(m, ref, version); ok {
			return descriptor, nil
		}
		return platform.CapabilityDescriptor{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Retained capability is unavailable to this member")
	}
	for _, descriptor := range t.capabilities(m) {
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
	selected := authoring.CapabilityVersion{Ref: ref, AppVersion: app.Manifest().Version, Revision: version}
	switch ref.Kind {
	case platform.AssetQuery:
		query, source, ok := t.memberQueryVersion(m, ref, version)
		if !ok {
			return platform.CapabilityDescriptor{}, false
		}
		selected.Query, selected.QueryVersion = &query, source
		for _, entity := range t.Entities(m) {
			if entity.Type == query.Object {
				selected.Entity = entity
				break
			}
		}
	case platform.AssetCompute:
		operation, ordinal, ok := operationDefinition(app, ref.Name, version)
		caller := platform.NewCaller(runtime{t}, m, ref.App, false, false)
		if !ok || ordinal != version || !t.authorizeCapability(caller, ref.App, operation.Name, operation.Roles, true, authorizationDetails{}).Allow {
			return platform.CapabilityDescriptor{}, false
		}
		selected.Operation = &operation
	case platform.AssetFunction:
		function, ordinal, ok := agentowner.FunctionDefinition(app, ref.Name, version)
		caller := platform.NewCaller(runtime{t}, m, ref.App, false, false)
		if !ok || ordinal != version || !t.authorizeCapability(caller, ref.App, function.Name, function.Roles, true, authorizationDetails{}).Allow {
			return platform.CapabilityDescriptor{}, false
		}
		readable := false
		for _, entity := range t.Entities(m) {
			if entity.Type == function.Object && !slices.ContainsFunc(function.Fields, func(name string) bool { _, exists := entity.Field(name); return !exists }) {
				readable = true
			}
		}
		if !readable {
			return platform.CapabilityDescriptor{}, false
		}
		selected.Function = &function
	default:
		return platform.CapabilityDescriptor{}, false
	}
	return authoring.DescribeRetainedCapability(selected)
}

func (t *Tenant) memberQueryVersion(m platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, string, bool) {
	app := t.app(ref.App)
	if app == nil || ref.Kind != platform.AssetQuery || version < 0 || version > 64 {
		return platform.NamedQuery{}, "", false
	}
	for _, d := range t.Definitions(m) {
		if d.Ref != ref {
			continue
		}
		if version == 0 {
			if d.Source == "code" && d.Query != nil {
				return *d.Query, d.Version, true
			}
			break
		}
		if d.Source != "tenant" {
			break
		}
		source := app.Manifest().Version + ".query-" + strconv.Itoa(version)
		if selected := d.QueryVersion(source); selected != nil {
			return *selected.Query, source, true
		}
		break
	}
	return platform.NamedQuery{}, "", false
}
