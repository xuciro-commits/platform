package host

import (
	"context"
	"fmt"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"time"
)

func (h *Host) promotionsOf(source, candidate string) []releaseowner.PromotionTarget {
	var out []releaseowner.PromotionTarget
	for _, other := range h.currentTenants() {
		if other.ID == source {
			continue
		}
		state := other
		image := other.readImage.Load()
		if image != nil {
			state = image.state
		} else {
			other.Lane.Lock()
		}
		origin, ok := state.releases.Origin(candidate)
		active := state.releases.Active() == candidate
		if image == nil {
			other.Lane.Unlock()
		}
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
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleasePage, error) {
		return t.scopedSavedReleases(m, offset, limit, application...)
	})
}

func (t *Tenant) scopedSavedReleases(m platform.Member, offset, limit int, application ...string) (releaseowner.ReleasePage, error) {
	if err := t.buildAllowed(m, "GET /v1/releases/candidates", build.Builder, build.Publisher); err != nil {
		return releaseowner.ReleasePage{}, err
	}
	var ref *platform.AssetRef
	if len(application) > 0 && application[0] != "" {
		ref = &platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: application[0]}
	}
	return t.releases.Page(offset, limit, ref)
}

func (t *Tenant) ReviewSavedRelease(m platform.Member, id string) (releaseowner.SavedReleaseReview, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.SavedReleaseReview, error) { return t.scopedReviewSavedRelease(m, id) })
}

func (t *Tenant) scopedReviewSavedRelease(m platform.Member, id string) (releaseowner.SavedReleaseReview, error) {
	if err := t.buildAllowed(m, "GET /v1/releases/candidates/{id}", build.Builder, build.Publisher); err != nil {
		return releaseowner.SavedReleaseReview{}, err
	}

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
	return t.releases.Review(id, context)
}
