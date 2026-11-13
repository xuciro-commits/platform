package host

import (
	"context"
	"fmt"
	"platformserver/platform"
	releaseowner "platformserver/services/devops/releases"
	"platformserver/services/resources/authoring"
	"time"
)

func (h *Host) promotionSnapshots(source string) []releaseowner.PromotionSnapshot {
	snapshots := []releaseowner.PromotionSnapshot{}
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
		snapshots = append(snapshots, state.releases.PromotionSnapshot(other.ID))
		if image == nil {
			other.Lane.Unlock()
		}
	}
	return snapshots
}

func (h *Host) promotionsOf(source, candidate string) []releaseowner.PromotionTarget {
	return releaseowner.PromotionTargets(source, candidate, h.promotionSnapshots(source))
}

func (h *Host) withPromotions(source string, page releaseowner.ReleasePage) releaseowner.ReleasePage {
	return releaseowner.WithPromotions(page, source, h.promotionSnapshots(source))
}

func (t *Tenant) SavedReleases(m platform.Member, offset, limit int, application ...string) (releaseowner.ReleasePage, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.ReleasePage, error) {
		if err := t.buildAllowed(m, "GET /v1/releases/candidates", build.Builder, build.Publisher); err != nil {
			return releaseowner.ReleasePage{}, err
		}
		var ref *platform.AssetRef
		if len(application) > 0 && application[0] != "" {
			ref = &platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: application[0]}
		}
		return t.releases.Page(offset, limit, ref)
	})
}

func (t *Tenant) ReviewSavedRelease(m platform.Member, id string) (releaseowner.SavedReleaseReview, error) {
	return readWithError(t, context.Background(), func(t *Tenant) (releaseowner.SavedReleaseReview, error) {
		if err := t.buildAllowed(m, "GET /v1/releases/candidates/{id}", build.Builder, build.Publisher); err != nil {
			return releaseowner.SavedReleaseReview{}, err
		}
		return t.releases.Review(id, releaseowner.ReviewContext{
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
		})
	})
}
