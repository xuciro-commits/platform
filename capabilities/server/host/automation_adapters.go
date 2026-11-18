package host

import releaseowner "platformserver/services/devops/releases"

import (
	"platformserver/kernel/protocol"
	"platformserver/platform"
	automation "platformserver/services/automate/runtime"
)

func (h hostView) flowBindings() automation.FlowBindingContext {
	checks := releaseowner.NewRetentionChecks(PublicationResolver())
	return automation.FlowBindingContext{Resolver: PublicationResolver(), App: h.t.App, Asset: h.t.flowReleaseAsset, Assets: func() ([]platform.ReleaseAsset, error) { return h.t.releaseAssetsLocked(nil, false) }, Active: h.t.releases.Active, Candidate: h.t.releases.Candidate, Applications: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
		return h.t.bindApplications(ref, release, dependencies)
	}, CloseCandidate: h.t.candidateWithBindings, SameRetained: checks.SameRetained, CodeMatches: checks.CodeMatches}
}
func (h hostView) BindFlow(app, name string, version int, prior protocol.FlowBinding) (protocol.FlowBinding, error) {
	return h.flowBindings().BindFlow(app, name, version, prior)
}
func (t *Tenant) nativeFlowAsset(owner platform.App, name string, version int) (platform.ReleaseAsset, error) {
	return releaseowner.NativeFlowAsset(t.Processes, t.definitions, owner, name, version)
}
func (t *Tenant) flowReleaseAsset(app, name string, version int) (platform.ReleaseAsset, error) {
	return releaseowner.FlowReleaseAsset(t.builders[app], name, version, func() (platform.ReleaseAsset, error) { return t.nativeFlowAsset(t.App(app), name, version) })
}
