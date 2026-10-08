package platformserver

import (
	"encoding/json"
	"fmt"
	"time"

	"platformserver/apps/build"
	"platformserver/platform"
)

// Saved candidates are read from committed bytes, independently of today's drafts.
type ReleaseSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Assets int    `json:"assets"`
	// From and PromotedAt are set when the candidate arrived from another
	// environment: the release holder here sees what was promoted in and
	// still waits for review, not an anonymous saved candidate.
	From       string    `json:"from,omitempty"`
	PromotedAt time.Time `json:"promotedAt,omitzero"`
	// PromotedTo lists the environments of this host that received the
	// candidate from here, so the source sees where its release went.
	PromotedTo []PromotionTarget `json:"promotedTo,omitempty"`
}

// PromotionTarget is one environment a candidate was promoted into.
type PromotionTarget struct {
	Tenant string    `json:"tenant"`
	Member string    `json:"member"`
	At     time.Time `json:"at"`
	Active bool      `json:"active"`
}

// promotionsOf finds, across this host's tenants, where a candidate saved in
// the source went by promotion and whether it is active there. Each target
// records the origin where the result is applied (accepted_release.go);
// the source keeps nothing, so the answer is read from the targets.
func (h *Host) promotionsOf(source, candidate string) []PromotionTarget {
	var out []PromotionTarget
	for _, other := range h.currentTenants() {
		if other.ID == source {
			continue
		}
		other.mu.Lock()
		origin, ok := other.releases.origin(candidate)
		active := other.releases.active == candidate
		other.mu.Unlock()
		if ok && origin.From == source {
			out = append(out, PromotionTarget{Tenant: other.ID, Member: origin.Member, At: origin.At, Active: active})
		}
	}
	return out
}

// withPromotions annotates an inventory with where each candidate went.
func (h *Host) withPromotions(source string, page ReleasePage) ReleasePage {
	for i := range page.Candidates {
		page.Candidates[i].PromotedTo = h.promotionsOf(source, page.Candidates[i].ID)
	}
	return page
}

type ReleasePage struct {
	Candidates []ReleaseSummary `json:"candidates"`
	Total      int              `json:"total"`
	ActiveID   string           `json:"activeId"`
}

type SavedReleaseReview struct {
	Preview              ReleasePreview          `json:"preview"`
	Assets               []platform.ReleaseAsset `json:"assets"`
	Active               bool                    `json:"active"`
	RunningMatches       bool                    `json:"runningMatches"`
	RunningDiagnostic    string                  `json:"runningDiagnostic,omitempty"`
	CanActivate          bool                    `json:"canActivate"`
	ActivationDiagnostic string                  `json:"activationDiagnostic,omitempty"`
	UpgradePlan          *ReleaseUpgradePlan     `json:"upgradePlan,omitempty"`
	From                 string                  `json:"from,omitempty"`
	PromotedAt           time.Time               `json:"promotedAt,omitzero"`
	PromotedTo           []PromotionTarget       `json:"promotedTo,omitempty"`
}

func (t *Tenant) SavedReleases(m platform.Member, offset, limit int) (ReleasePage, error) {
	if err := t.admits(m); err != nil {
		return ReleasePage{}, err
	}
	if m.Roles[build.ID] != build.Builder && m.Roles[build.ID] != build.Publisher {
		return ReleasePage{}, fmt.Errorf("builder or publisher role required")
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return ReleasePage{}, fmt.Errorf("release page needs a nonnegative offset and a limit from 1 to 100")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ids := t.releases.ids()
	reply := ReleasePage{Candidates: []ReleaseSummary{}, Total: len(ids), ActiveID: t.releases.active}
	start := min(offset, len(ids))
	for _, id := range ids[start : start+min(limit, len(ids)-start)] {
		candidate, err := t.releases.candidate(id)
		if err != nil {
			return ReleasePage{}, err
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
		summary := ReleaseSummary{ID: id, Title: title, Assets: len(candidate.Assets)}
		if origin, ok := t.releases.origin(id); ok {
			summary.From, summary.PromotedAt = origin.From, origin.At
		}
		reply.Candidates = append(reply.Candidates, summary)
	}
	return reply, nil
}

func (t *Tenant) ReviewSavedRelease(m platform.Member, id string) (SavedReleaseReview, error) {
	if err := t.admits(m); err != nil {
		return SavedReleaseReview{}, err
	}
	if m.Roles[build.ID] != build.Builder && m.Roles[build.ID] != build.Publisher {
		return SavedReleaseReview{}, fmt.Errorf("builder or publisher role required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, exists := t.releases.candidates[id]
	if !exists {
		return SavedReleaseReview{}, fmt.Errorf("saved release candidate not found")
	}
	saved, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return SavedReleaseReview{}, err
	}
	reply := SavedReleaseReview{Assets: saved.Assets, Active: t.releases.active == id, Preview: ReleasePreview{
		CandidateID: id, Included: []platform.AssetRef{}, Added: []platform.AssetRef{}, Removed: []platform.AssetRef{}, Changed: []platform.AssetRef{},
	}}
	for _, asset := range saved.Assets {
		reply.Preview.Included = append(reply.Preview.Included, asset.Ref)
	}
	if origin, ok := t.releases.origin(id); ok {
		reply.From, reply.PromotedAt = origin.From, origin.At
	}
	running, err := t.runningReleaseLocked(saved)
	if err != nil {
		reply.RunningDiagnostic = err.Error()
	} else {
		reply.Preview.CurrentID = running.ID
		reply.RunningMatches = running.ID == id
		reply.Preview.Added, reply.Preview.Removed, reply.Preview.Changed, err = platform.CandidateDiff(running, saved)
		if err != nil {
			return reply, err
		}
	}
	if reply.RunningMatches {
		err = t.pendingWorkFitsLocked(id, raw)
	} else {
		_, err = t.prepareReleaseActivationLocked(id, raw)
	}
	reply.CanActivate = err == nil
	if err != nil {
		reply.ActivationDiagnostic = err.Error()
		if plan, planErr := t.releaseUpgradePlanLocked(saved); planErr == nil && plan != nil {
			if _, checkErr := t.prepareReleaseActivationLocked(id, raw, true); checkErr == nil {
				reply.UpgradePlan = plan
			}
		}
	}
	return reply, nil
}
