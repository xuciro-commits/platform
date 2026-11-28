package host

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	agentowner "platformserver/services/ai/functions"
	"platformserver/services/functions/authoring"
)

func (t *Tenant) capabilities(m platform.Member) []protocol.CapabilityDescriptor {
	if t.Admits(m) != nil {
		return []protocol.CapabilityDescriptor{}
	}
	m = t.authorizationMember(m)
	return authoring.VisibleCapabilities(t.Definitions(m), m.Holds("build", "builder"), func(d platform.Definition, permission string, roles []string) bool {
		return t.authorizeCapability(platform.NewCaller(runtime{t: t}, m, d.Ref.App, false, false), d.Ref.App, permission, roles, true, authorizationDetails{}).Allow
	})
}

func (t *Tenant) describeCapability(m platform.Member, ref platform.AssetRef, version int) (protocol.CapabilityDescriptor, *platform.Error) {
	if refusal := t.Admits(m); refusal != nil {
		return protocol.CapabilityDescriptor{}, refusal
	}
	return authoring.DescribeCapability(ref, version, func() (protocol.CapabilityDescriptor, bool) { return t.retainedCapability(m, ref, version) }, func() []protocol.CapabilityDescriptor { return t.capabilities(m) })
}

func (t *Tenant) retainedCapability(m platform.Member, ref platform.AssetRef, version int) (protocol.CapabilityDescriptor, bool) {
	m = t.authorizationMember(m)
	return authoring.RetainedCapability(t.App(ref.App), ref, version, authoring.RetainedHooks{
		Query:    func() (platform.NamedQuery, string, bool) { return t.memberQueryVersion(m, ref, version) },
		Entities: func() []platform.RecordTypeInfo { return t.Entities(m) },
		Function: agentowner.FunctionDefinition, Operation: t.operationDefinition,
		Allowed: func(name string, roles []string) bool {
			return t.authorizeCapability(platform.NewCaller(runtime{t: t}, m, ref.App, false, false), ref.App, name, roles, true, authorizationDetails{}).Allow
		}})
}

func (t *Tenant) memberQueryVersion(m platform.Member, ref platform.AssetRef, version int) (platform.NamedQuery, string, bool) {
	app := t.App(ref.App)
	if app == nil {
		return platform.NamedQuery{}, "", false
	}
	return authoring.MemberQueryVersion(t.Definitions(m), app.Manifest().Version, ref, version, PublicationResolver())
}
