package platformserver

import releaseowner "platformserver/studio/releases"

import (
	"encoding/json"
	"fmt"
	"time"

	"platformserver/internal/systemscope"
	"platformserver/platform"
	"platformserver/studio/build"
)

// promotionsOf finds, across this host's tenants, where a candidate saved in
// the source went by promotion and whether it is active there. Each target
// records the origin where the result is applied (accepted_release.go);
// the source keeps nothing, so the answer is read from the targets.
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

// withPromotions annotates an inventory with where each candidate went.
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
	if offset < 0 || limit < 1 || limit > 100 {
		return releaseowner.ReleasePage{}, fmt.Errorf("release page needs a nonnegative offset and a limit from 1 to 100")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ids := t.releases.Ids()
	if len(application) > 0 && application[0] != "" {
		ref := platform.AssetRef{App: build.ID, Kind: platform.AssetApp, Name: application[0]}
		filtered := make([]string, 0, len(ids))
		for _, id := range ids {
			candidate, err := t.releases.Candidate(id)
			if err != nil {
				return releaseowner.ReleasePage{}, err
			}
			for _, asset := range candidate.Assets {
				if asset.Ref == ref {
					filtered = append(filtered, id)
					break
				}
			}
		}
		ids = filtered
	}
	reply := releaseowner.ReleasePage{Candidates: []releaseowner.ReleaseSummary{}, Total: len(ids), ActiveID: t.releases.Active()}
	start := min(offset, len(ids))
	for _, id := range ids[start : start+min(limit, len(ids)-start)] {
		candidate, err := t.releases.Candidate(id)
		if err != nil {
			return releaseowner.ReleasePage{}, err
		}
		// Use an application or business object label rather than a generated page name.
		title := id
		for _, kind := range []platform.AssetKind{platform.AssetApp, platform.AssetObject, platform.AssetFlow, platform.AssetFunction, platform.AssetPage} {
			found := false
			for _, asset := range candidate.Assets {
				if asset.Ref.Kind != kind {
					continue
				}
				var label struct {
					Title string `json:"title"`
				}
				_ = json.Unmarshal(asset.Body, &label)
				title = label.Title
				if title == "" {
					title = asset.Ref.Name
				}
				found = true
				break
			}
			if found {
				break
			}
		}
		summary := releaseowner.ReleaseSummary{ID: id, Title: title, Assets: len(candidate.Assets)}
		if origin, ok := t.releases.Origin(id); ok {
			summary.From, summary.PromotedAt = origin.From, origin.At
		}
		reply.Candidates = append(reply.Candidates, summary)
	}
	return reply, nil
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
	raw, exists := t.releases.Candidates()[id]
	if !exists {
		return releaseowner.SavedReleaseReview{}, fmt.Errorf("saved release candidate not found")
	}
	saved, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return releaseowner.SavedReleaseReview{}, err
	}
	visible := map[platform.AssetRef]bool{}
	for _, asset := range saved.Assets {
		visible[asset.Ref] = true
	}
	ai, refusal := t.applicationAILocked(m, saved.Assets, visible)
	if refusal != nil {
		return releaseowner.SavedReleaseReview{}, fmt.Errorf("candidate AI metadata is unavailable: %s", refusal.Message)
	}
	reply := releaseowner.SavedReleaseReview{AI: ai, Assets: saved.Assets, Active: t.releases.Active() == id, Preview: releaseowner.ReleasePreview{
		CandidateID: id, Included: []platform.AssetRef{}, Added: []platform.AssetRef{}, Removed: []platform.AssetRef{}, Changed: []platform.AssetRef{},
	}}
	for _, asset := range saved.Assets {
		reply.Preview.Included = append(reply.Preview.Included, asset.Ref)
	}
	if origin, ok := t.releases.Origin(id); ok {
		reply.From, reply.PromotedAt = origin.From, origin.At
	}
	reply.Extensions, err = t.releaseExtensionReview(saved)
	if err != nil {
		return reply, err
	}
	running, err := t.runningReleaseLocked(saved)
	if err != nil {
		reply.RunningDiagnostic = err.Error()
	} else if running.ID == "" {
		reply.Preview.Added = append(reply.Preview.Added, reply.Preview.Included...)
	} else {
		reply.Preview.CurrentID = running.ID
		reply.RunningMatches = running.ID == id
		reply.Preview.Added, reply.Preview.Removed, reply.Preview.Changed, err = platform.CandidateDiff(running, saved)
		if err != nil {
			return reply, err
		}
	}
	if t.ID == systemscope.Tenant {
		reply.ActivationDiagnostic = systemReleaseActivationDiagnostic
		return reply, nil
	}
	if reply.RunningMatches {
		err = t.checkExtensionSet()
		if err == nil {
			err = t.pendingWorkFitsLocked(id, raw)
		}
	} else {
		_, _, err = t.prepareReleaseLifecycleLocked(saved, m, time.Now().UTC(), false)
	}
	reply.CanActivate = err == nil
	if err != nil {
		reply.ActivationDiagnostic = err.Error()
		if plan, planErr := t.releaseUpgradePlanLocked(saved, m); planErr == nil && plan != nil {
			if _, _, checkErr := t.prepareReleaseLifecycleLocked(saved, m, time.Now().UTC(), true); checkErr == nil {
				reply.UpgradePlan = plan
			}
		}
	}
	return reply, nil
}
