package platformserver

import (
	"fmt"
	"platformserver/internal/systemscope"
	"platformserver/platform"
	"platformserver/studio/build"
	releaseowner "platformserver/studio/releases"
	"time"
)

func (h *Host) promotionsOf(source, candidate string) []releaseowner.PromotionTarget {
	var out []releaseowner.PromotionTarget
	for _, other := range h.currentTenants() {
		if other.ID == source {
			continue
		}
		other.mu.Lock()
		origin, ok := other.releases.Origin(candidate)
		active := other.releases.Active() == candidate
		other.mu.Unlock()
		if ok && origin.From == source {
			out = append(out, releaseowner.PromotionTarget{Tenant: other.ID, Member: origin.Member, At: origin.At, Active: active})
		}
	}
	return out
}

func (h *Host) withPromotions(source string, page releaseowner.ReleasePage) releaseowner.ReleasePage {
	for i := range page.Candidates {
		page.Candidates[i].PromotedTo = h.promotionsOf(source, page.Candidates[i].ID)
	}
	return page
}

func (t *Tenant) SavedReleases(m platform.Member, offset, limit int, application ...string) (releaseowner.ReleasePage, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleasePage{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/releases/candidates", "read", []string{build.Builder, build.Publisher}, true)); err != nil {
		return releaseowner.ReleasePage{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var ref *platform.AssetRef
	if len(application) > 0 && application[0] != "" {
		ref = &platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: application[0]}
	}
	return t.releases.Page(offset, limit, ref)
}

func (t *Tenant) ReviewSavedRelease(m platform.Member, id string) (releaseowner.SavedReleaseReview, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.SavedReleaseReview{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/releases/candidates/{id}", "read", []string{build.Builder, build.Publisher}, true)); err != nil {
		return releaseowner.SavedReleaseReview{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	context := releaseowner.ReviewContext{
		AI: func(assets []platform.ReleaseAsset, visible map[platform.AssetRef]bool) ([]platform.ApplicationAI, error) {
			ai, refusal := t.applicationAILocked(m, assets, visible)
			if refusal != nil {
				return nil, fmt.Errorf("candidate AI metadata is unavailable: %s", refusal.Message)
			}
			return ai, nil
		}, Extensions: t.releaseExtensionReview, Running: t.runningReleaseLocked,
		CheckExisting: func(id string, raw []byte) error {
			if err := t.checkExtensionSet(); err != nil {
				return err
			}
			return t.pendingWorkFitsLocked(id, raw)
		},
		Prepare: func(saved platform.ReleaseCandidate, upgrade bool) error {
			_, _, err := t.prepareReleaseLifecycleLocked(saved, m, time.Now().UTC(), upgrade)
			return err
		},
		Upgrade: func(saved platform.ReleaseCandidate) (*releaseowner.ReleaseUpgradePlan, error) {
			return t.releaseUpgradePlanLocked(saved, m)
		},
	}
	if t.ID == systemscope.Tenant {
		context.ActivationDiagnostic = systemReleaseActivationDiagnostic
	}
	return t.releases.Review(id, context)
}

func (t *Tenant) SavedDraftChanges(m platform.Member, application, typ, id string) (releaseowner.ReleaseDraftChanges, error) {
	if err := t.admits(m); err != nil {
		return releaseowner.ReleaseDraftChanges{}, err
	}
	c := platform.NewCaller(runtime{t}, m, build.ID, false, false)
	if err := authorizationRefusal(t.authorizeNative(c, "GET /v1/releases/drafts/changes", "read", []string{build.Builder}, true)); err != nil {
		return releaseowner.ReleaseDraftChanges{}, err
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	owner, ok := t.app(build.ID).(build.ConstructionDrafts)
	if !ok {
		return releaseowner.ReleaseDraftChanges{}, fmt.Errorf("tenant has no builder")
	}
	changes, err := owner.SavedDraftChanges(c, application, typ, id)
	return releaseowner.ReleaseDraftChanges{Changes: changes}, err
}
