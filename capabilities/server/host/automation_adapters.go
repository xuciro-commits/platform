package host

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
	releaseowner "platformserver/services/devops/releases"
	build "platformserver/services/resources/authoring"
	controlowner "platformserver/services/security/control"
)

func (h hostView) flowBindings() automation.FlowBindingContext {
	checks, store, private := releaseowner.NewRetentionChecks(PublicationResolver()), h.t.Projection, map[string]platform.Service(nil)
	if h.decision != nil && h.decision.Records != nil {
		store, private = h.decision.Records, h.decision.PrivateApps
	}
	return automation.FlowBindingContext{Resolver: PublicationResolver(), App: h.t.App, Asset: h.t.flowReleaseAsset, Assets: func() ([]platform.ReleaseAsset, error) { return h.t.releaseAssetsLocked(nil, false) }, Active: h.t.releases.Active, Candidate: h.t.releases.Candidate, Applications: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
		return h.t.bindApplications(ref, release, dependencies)
	}, CloseCandidate: h.t.candidateWithBindings, SameRetained: checks.SameRetained, RetainedClosure: func(ref platform.AssetRef, version int, dep string) bool {
		return controlowner.RetainedFlowClosure(private, h.t.App(controlowner.PlatformApp), func() bool { return build.HasRetainedObjectFlowClosure(store, ref, version, dep) }, ref, version, dep)
	}, CodeMatches: checks.CodeMatches}
}
func (h hostView) BindFlow(app, name string, version int, prior protocol.FlowBinding) (protocol.FlowBinding, error) {
	return h.flowBindings().BindFlow(app, name, version, prior)
}
func (t *Tenant) nativeFlowAsset(owner platform.Service, name string, version int) (platform.ReleaseAsset, error) {
	return releaseowner.NativeFlowAsset(t.Processes, t.definitions, owner, name, version)
}
func (t *Tenant) flowReleaseAsset(app, name string, version int) (platform.ReleaseAsset, error) {
	return releaseowner.FlowReleaseAsset(t.builders[app], name, version, func() (platform.ReleaseAsset, error) { return t.nativeFlowAsset(t.App(app), name, version) })
}
