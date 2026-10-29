package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"fmt"
	automation "platformserver/engine/automation"
	"platformserver/platform"
	"platformserver/studio/build"
	"slices"
	"strings"
	"time"
)

func (h hostView) flowBindings() automation.FlowBindingContext {
	return automation.FlowBindingContext{App: h.t.app, Asset: h.t.flowReleaseAsset, Assets: func() ([]platform.ReleaseAsset, error) { return h.t.releaseAssetsLocked(nil, false) }, Active: h.t.releases.Active, Candidate: h.t.releases.Candidate, Applications: func(ref platform.AssetRef, release, dependencies string) ([]platform.AssetBinding, error) {
		return h.t.bindApplications(ref, release, dependencies)
	}, CloseCandidate: h.t.candidateWithBindings, SameRetained: sameRetainedCapability, CodeMatches: releaseowner.ImmutableCodeAssetMatches}
}
func (h hostView) BindFlow(app, name string, version int, prior platform.FlowBinding) (platform.FlowBinding, error) {
	return h.flowBindings().BindFlow(app, name, version, prior)
}
func (t *Tenant) nativeFlowAsset(owner platform.App, name string, version int) (platform.ReleaseAsset, error) {
	provider, ok := t.procs.(platform.NativeFlowAssets)
	if !ok {
		return platform.ReleaseAsset{}, fmt.Errorf("native flow has no original descriptor owner")
	}
	manifest := owner.Manifest()
	refs := []platform.AssetRef{}
	for _, definition := range t.definitions {
		if definition.Source == "code" && definition.Ref.App == manifest.ID && !slices.Contains([]platform.AssetKind{platform.AssetApp, platform.AssetFlow, platform.AssetProtocol, platform.AssetInterface}, definition.Ref.Kind) {
			refs = append(refs, definition.Ref)
		}
	}
	return provider.NativeFlowAsset(manifest, name, version, releaseowner.UniqueRefs(refs))
}
func (t *Tenant) flowReleaseAsset(app, name string, version int) (platform.ReleaseAsset, error) {
	owner := t.app(app)
	if owner == nil {
		return platform.ReleaseAsset{}, fmt.Errorf("flow owner is unavailable")
	}
	if provider, ok := owner.(platform.FlowReleaseOwner); ok {
		return provider.FlowReleaseAsset(name, version)
	}
	return t.nativeFlowAsset(owner, name, version)
}

func (t *Tenant) raiseAlerts(e platform.Event, now time.Time) {
	s := e.Record.GetSubmission()
	typ := s.GetTarget().GetType()
	if e.App != build.ID || !strings.HasPrefix(typ, build.ID+".") || typ == build.AlertType {
		return
	}
	c := t.automation(build.ID, false)
	owner, ok := t.app(build.ID).(build.AlertDefinitions)
	if !ok {
		return
	}
	rules, err := owner.AlertRules(typ)
	if err != nil {
		return
	}
	if len(rules) == 0 {
		return
	}
	info, ok := t.entity(typ)
	if !ok {
		return
	}
	held, ok := t.held(typ + "/" + s.GetTarget().GetId())
	if !ok {
		return
	}
	record := map[string]any{}
	raw, _ := json.Marshal(held)
	json.Unmarshal(raw, &record)
	compiled := make([]automation.AlertDefinition, 0, len(rules))
	for _, rule := range rules {
		compiled = append(compiled, automation.AlertDefinition{ID: rule.ID, Rule: rule.Definition()})
	}
	automation.RaiseAlerts(c, compiled, info, record, e, now)
}
