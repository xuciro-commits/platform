package platformserver

import (
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
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

func (t *Tenant) draftWorldLocked(m platform.Member, branch string, now time.Time) (*Tenant, *kernel.Error) {
	if branch == "" {
		return t, nil
	}
	world, refusal := t.branchWorldLocked(m, branch, now)
	if refusal != nil {
		return nil, refusal
	}
	return world.view, nil
}

func (t *Tenant) ReferencedDrafts(m platform.Member, kind platform.AssetKind, id, branch string, now time.Time) (releaseowner.ReleaseDraftClosure, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/drafts/referenced", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	world, refusal := t.draftWorldLocked(m, branch, now)
	if refusal != nil {
		return releaseowner.ReleaseDraftClosure{}, refusal
	}
	owner, ok := world.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleaseDraftClosure{}, fmt.Errorf("tenant has no builder")
	}
	mine := ""
	if branch == "" {
		mine = m.ID
	}
	drafts, err := owner.ReferencedDrafts(kind, id, mine)
	if err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	if drafts == nil {
		drafts = []build.JointDraftRef{}
	}
	return releaseowner.ReleaseDraftClosure{Drafts: drafts}, nil
}

func (t *Tenant) builderDrafts(m platform.Member, permission string) (build.ConstructionDrafts, error) {
	if err := t.admits(m); err != nil {
		return nil, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, permission, "read", []string{build.Builder}, true)); err != nil {
		return nil, err
	}
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return nil, fmt.Errorf("tenant has no builder")
	}
	return owner, nil
}

func (t *Tenant) DraftChange(m platform.Member, kind platform.AssetKind, id string) (build.DraftChange, error) {
	owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/change")
	if err != nil {
		return build.DraftChange{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	change, found, err := owner.DraftChangeOf(kind, id)
	if err == nil && !found {
		err = fmt.Errorf("The draft has no working changes")
	}
	return change, err
}

func (t *Tenant) DraftRestore(m platform.Member, kind platform.AssetKind, id string, now time.Time) (releaseowner.ReleaseDraftRestore, error) {
	owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/restore")
	if err != nil {
		return releaseowner.ReleaseDraftRestore{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	typ, published, found, err := owner.DraftPublished(kind, id)
	if err != nil || !found {
		return releaseowner.ReleaseDraftRestore{}, fmt.Errorf("The draft is unavailable")
	}
	if published == "" {
		return releaseowner.ReleaseDraftRestore{Type: typ, Action: "archive", Payload: json.RawMessage(`{}`)}, nil
	}
	var asset platform.ReleaseAsset
	if err := json.Unmarshal([]byte(published), &asset); err != nil || asset.Ref.Kind == "" {
		return releaseowner.ReleaseDraftRestore{}, fmt.Errorf("The installed version of this draft cannot be restored")
	}
	lifecycle, ok := owner.(platform.AssetLifecycleOwner)
	if !ok {
		return releaseowner.ReleaseDraftRestore{}, fmt.Errorf("tenant has no builder")
	}
	drafts, err := lifecycle.ReleaseDrafts([]platform.ReleaseAsset{asset}, platform.AssetReleaseContext{DraftsOnly: true, Member: m, At: now})
	if err != nil {
		return releaseowner.ReleaseDraftRestore{}, err
	}
	var info *platform.EntityInfo
	for _, entity := range t.Entities(m) {
		if entity.Type == typ {
			info = &entity
		}
	}
	for _, draft := range drafts {
		if draft.Type != typ || info == nil {
			continue
		}
		payload, err := releaseowner.AssetDraftPayload(*info, draft.Image)
		if err != nil {
			return releaseowner.ReleaseDraftRestore{}, err
		}
		fields := map[string]json.RawMessage{}
		_ = json.Unmarshal(payload, &fields)
		for _, field := range info.Fields {
			if _, ok := fields[field.Name]; !ok && !field.ReadOnly && !field.Aside {
				fields[field.Name] = json.RawMessage("null")
			}
		}
		payload, _ = json.Marshal(fields)
		return releaseowner.ReleaseDraftRestore{Type: typ, Action: "edit", Payload: payload}, nil
	}
	return releaseowner.ReleaseDraftRestore{}, fmt.Errorf("The installed version of this draft cannot be restored")
}

func (t *Tenant) WorkingDrafts(m platform.Member) (releaseowner.ReleaseWorkingState, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleaseWorkingState{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/releases/drafts/working", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleaseWorkingState{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleaseWorkingState{}, fmt.Errorf("tenant has no builder")
	}
	changes, err := owner.WorkingDrafts(m.ID)
	if err != nil {
		return releaseowner.ReleaseWorkingState{}, err
	}
	state := releaseowner.ReleaseWorkingState{Drafts: []releaseowner.ReleaseWorkingDraft{}}
	for _, change := range changes {
		state.Drafts = append(state.Drafts, releaseowner.ReleaseWorkingDraft{Kind: change.Ref.Kind, ID: change.ID, Type: change.Type, Title: change.Title})
	}
	return state, nil
}

func (t *Tenant) PreviewReleaseDrafts(m platform.Member, drafts []build.JointDraftRef, branch string, now time.Time) (releaseowner.ReleasePreview, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "POST /v1/releases/preview", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	world, refusal := t.draftWorldLocked(m, branch, now)
	if refusal != nil {
		return releaseowner.ReleasePreview{}, refusal
	}
	return world.releasePlannerLocked().PreviewWithActions(drafts, m)
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
