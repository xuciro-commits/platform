package platformserver

import (
	"fmt"
	"platformserver/platform"
	"platformserver/studio/build"
	releaseowner "platformserver/studio/releases"
	"time"
)

func (t *Tenant) ReleaseCandidate(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releaseCandidateLocked(roots)
}

func (t *Tenant) releaseCandidateLocked(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	available, err := t.releaseAssetsLocked(nil, false)
	if err != nil {
		return platform.ReleaseCandidate{}, err
	}
	return t.candidateWithBindings(roots, available, roots)
}

func (t *Tenant) ReferencedDrafts(m platform.Member, kind platform.AssetKind, id string) (releaseowner.ReleaseDraftClosure, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/drafts/referenced", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleaseDraftClosure{}, fmt.Errorf("tenant has no builder")
	}
	drafts, err := owner.ReferencedDrafts(kind, id)
	if err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	if drafts == nil {
		drafts = []build.JointDraftRef{}
	}
	return releaseowner.ReleaseDraftClosure{Drafts: drafts}, nil
}

func (t *Tenant) PreviewRelease(m platform.Member, kind platform.AssetKind, id string) (releaseowner.ReleasePreview, error) {
	return t.PreviewReleaseDrafts(m, []build.JointDraftRef{{Kind: kind, ID: id}})
}

func (t *Tenant) PreviewReleaseDrafts(m platform.Member, drafts []build.JointDraftRef) (releaseowner.ReleasePreview, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/preview", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.releasePlannerLocked().PreviewWithActions(drafts, m)
}

func (t *Tenant) PreviewApplicationDrafts(m platform.Member, id string) (releaseowner.ReleasePreview, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	if !m.Holds(build.ID, build.Builder) {
		return releaseowner.ReleasePreview{}, fmt.Errorf("Only a builder can inspect application drafts")
	}
	t.mu.Lock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		t.mu.Unlock()
		return releaseowner.ReleasePreview{}, fmt.Errorf("Construction owner is unavailable")
	}
	drafts, err := owner.ApplicationDrafts(id)
	t.mu.Unlock()
	if err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	return t.PreviewReleaseDrafts(m, drafts)
}

func (t *Tenant) releasePlannerLocked() releaseowner.Planner {
	owner, _ := t.app(build.ID).(build.ConstructionDrafts)
	return releaseowner.Planner{Definitions: t.definitions, Registry: t.assetRegistryLocked, Drafts: owner,
		Validate: func(candidate platform.ReleaseCandidate, member platform.Member, at time.Time) error {
			_, _, err := t.prepareReleaseInstallationLocked(candidate, member, at, true, false)
			return err
		}}
}
func (t *Tenant) releaseAssetsLocked(assets []platform.ReleaseAsset, replace bool) ([]platform.ReleaseAsset, error) {
	return t.releasePlannerLocked().Assets(assets, replace)
}
func (t *Tenant) codeReleaseAssetsLocked(app platform.App) ([]platform.ReleaseAsset, error) {
	return releaseowner.CodeAssets(app, t.definitions, t.definitionAsset, t.nativeFlowAsset, t.describeOwnedAsset, t.sharedSystemAssets)
}
func (t *Tenant) previewReleaseLocked(drafts []build.JointDraftRef, member platform.Member) (releaseowner.ReleasePreview, platform.ReleaseCandidate, error) {
	return t.releasePlannerLocked().Preview(drafts, member)
}

func (t *Tenant) candidateWithBindings(roots []platform.AssetRef, available []platform.ReleaseAsset, fixed []platform.AssetRef) (platform.ReleaseCandidate, error) {
	return t.releasePlannerLocked().WithBindings(roots, available, fixed)
}
