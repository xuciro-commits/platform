package host

import (
	"context"
	"fmt"
	"platformkernel/kernel"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"slices"
	"time"
)

func (t *Tenant) ReleaseCandidate(roots []platform.AssetRef) (platform.ReleaseCandidate, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (platform.ReleaseCandidate, error) { return t.releaseCandidateLocked(roots) })
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
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleaseDraftClosure, error) {
		return t.scopedReferencedDrafts(m, kind, id, branch, now)
	})
}

func (t *Tenant) scopedReferencedDrafts(m platform.Member, kind platform.AssetKind, id, branch string, now time.Time) (releaseowner.ReleaseDraftClosure, error) {
	if err := t.builderAllowed(m, "POST /v1/releases/drafts/referenced"); err != nil {
		return releaseowner.ReleaseDraftClosure{}, err
	}
	world, refusal := t.draftWorldLocked(m, branch, now)
	if refusal != nil {
		return releaseowner.ReleaseDraftClosure{}, refusal
	}
	owner, ok := world.App(build.ID).(build.ConstructionDrafts)
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

func (t *Tenant) builderAllowed(m platform.Member, permission string) error {
	if err := t.Admits(m); err != nil {
		return err
	}
	c := platform.NewCaller(runtime{t: t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, permission, "read", []string{build.Builder}, true)); err != nil {
		return err
	}
	return nil
}

func (t *Tenant) builderDrafts(m platform.Member, permission string) (build.ConstructionDrafts, error) {
	if err := t.builderAllowed(m, permission); err != nil {
		return nil, err
	}
	owner, ok := t.App(build.ID).(build.ConstructionDrafts)
	if !ok {
		return nil, fmt.Errorf("tenant has no builder")
	}
	return owner, nil
}

func (t *Tenant) DraftChange(m platform.Member, kind platform.AssetKind, id string) (build.DraftChange, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (build.DraftChange, error) { return t.scopedDraftChange(m, kind, id) })
}

func (t *Tenant) scopedDraftChange(m platform.Member, kind platform.AssetKind, id string) (build.DraftChange, error) {
	owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/change")
	if err != nil {
		return build.DraftChange{}, err
	}
	change, found, err := owner.DraftChangeOf(m, kind, id)
	if err == nil && !found {
		err = fmt.Errorf("The draft has no working changes")
	}
	return change, err
}

func (t *Tenant) DraftRestore(m platform.Member, kind platform.AssetKind, id string, now time.Time) (releaseowner.ReleaseDraftRestore, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleaseDraftRestore, error) {
		return t.scopedDraftRestore(m, kind, id, now)
	})
}

func (t *Tenant) scopedDraftRestore(m platform.Member, kind platform.AssetKind, id string, now time.Time) (releaseowner.ReleaseDraftRestore, error) {
	owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/restore")
	if err != nil {
		return releaseowner.ReleaseDraftRestore{}, err
	}
	return releaseowner.DraftRestore(owner, t.Entities(m), m, kind, id, now)
}

func (t *Tenant) WorkingDrafts(m platform.Member) (releaseowner.ReleaseWorkingState, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleaseWorkingState, error) { return t.scopedWorkingDrafts(m) })
}

func (t *Tenant) scopedWorkingDrafts(m platform.Member) (releaseowner.ReleaseWorkingState, error) {
	owner, err := t.builderDrafts(m, "GET /v1/releases/drafts/working")
	if err != nil {
		return releaseowner.ReleaseWorkingState{}, err
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
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleasePreview, error) {
		return t.scopedPreviewReleaseDrafts(m, drafts, branch, now)
	})
}

func (t *Tenant) scopedPreviewReleaseDrafts(m platform.Member, drafts []build.JointDraftRef, branch string, now time.Time) (releaseowner.ReleasePreview, error) {
	if err := t.builderAllowed(m, "POST /v1/releases/preview"); err != nil {
		return releaseowner.ReleasePreview{}, err
	}
	world, refusal := t.draftWorldLocked(m, branch, now)
	if refusal != nil {
		return releaseowner.ReleasePreview{}, refusal
	}
	reply, err := world.releasePlannerLocked().PreviewWithActions(drafts, m)
	if err != nil {
		return reply, err
	}
	definitions, err := world.DefinitionsFor(m)
	if err != nil {
		return reply, err
	}
	visible := map[platform.AssetRef]bool{}
	for _, definition := range definitions {
		visible[definition.Ref] = true
	}
	for i := range reply.DraftReviews {
		reply.DraftReviews[i].Downstream = slices.DeleteFunc(reply.DraftReviews[i].Downstream, func(resource releaseowner.ReleaseDraftResource) bool {
			return !visible[resource.Ref]
		})
	}
	return reply, nil
}

func (t *Tenant) releasePlannerLocked() releaseowner.Planner {
	owner, _ := t.App(build.ID).(build.ConstructionDrafts)
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
	return releaseowner.CodeAssets(app, t.definitions, t.describer().DefinitionAsset, t.nativeFlowAsset, t.describer().DescribeOwnedAsset)
}
func (t *Tenant) previewReleaseLocked(drafts []build.JointDraftRef, member platform.Member) (releaseowner.ReleasePreview, platform.ReleaseCandidate, error) {
	return t.releasePlannerLocked().Preview(drafts, member)
}

func (t *Tenant) candidateWithBindings(roots []platform.AssetRef, available []platform.ReleaseAsset, fixed []platform.AssetRef) (platform.ReleaseCandidate, error) {
	return t.releasePlannerLocked().WithBindings(roots, available, fixed)
}
