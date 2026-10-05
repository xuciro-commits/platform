package platformserver

import (
	"encoding/json"
	"fmt"
	"slices"

	"platformserver/apps/build"
	"platformserver/platform"
)

// Saved candidates are read from committed bytes, independently of today's drafts.
type ReleaseSummary struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Assets int    `json:"assets"`
}

type ReleasePage struct {
	Candidates []ReleaseSummary `json:"candidates"`
	Total      int              `json:"total"`
	ActiveID   string           `json:"activeId"`
}

type SavedReleaseReview struct {
	Preview              ReleasePreview      `json:"preview"`
	Active               bool                `json:"active"`
	RunningMatches       bool                `json:"runningMatches"`
	RunningDiagnostic    string              `json:"runningDiagnostic,omitempty"`
	CanActivate          bool                `json:"canActivate"`
	ActivationDiagnostic string              `json:"activationDiagnostic,omitempty"`
	UpgradePlan          *ReleaseUpgradePlan `json:"upgradePlan,omitempty"`
}

func (t *Tenant) SavedReleases(m platform.Member, offset, limit int) (ReleasePage, error) {
	if err := t.admits(m); err != nil {
		return ReleasePage{}, err
	}
	if m.Roles[build.ID] != build.Builder {
		return ReleasePage{}, fmt.Errorf("builder role required")
	}
	if offset < 0 || limit < 1 || limit > 100 {
		return ReleasePage{}, fmt.Errorf("release page needs a nonnegative offset and a limit from 1 to 100")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	ids := make([]string, 0, len(t.releaseCandidates))
	for id := range t.releaseCandidates {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	reply := ReleasePage{Candidates: []ReleaseSummary{}, Total: len(ids), ActiveID: t.activeRelease}
	start := min(offset, len(ids))
	for _, id := range ids[start : start+min(limit, len(ids)-start)] {
		candidate, err := platform.ReadCandidate(id, t.releaseCandidates[id])
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
		reply.Candidates = append(reply.Candidates, ReleaseSummary{ID: id, Title: title, Assets: len(candidate.Assets)})
	}
	return reply, nil
}

func (t *Tenant) ReviewSavedRelease(m platform.Member, id string) (SavedReleaseReview, error) {
	if err := t.admits(m); err != nil {
		return SavedReleaseReview{}, err
	}
	if m.Roles[build.ID] != build.Builder {
		return SavedReleaseReview{}, fmt.Errorf("builder role required")
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	raw, exists := t.releaseCandidates[id]
	if !exists {
		return SavedReleaseReview{}, fmt.Errorf("saved release candidate not found")
	}
	saved, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return SavedReleaseReview{}, err
	}
	reply := SavedReleaseReview{Active: t.activeRelease == id, Preview: ReleasePreview{
		CandidateID: id, Included: []platform.AssetRef{}, Added: []platform.AssetRef{}, Removed: []platform.AssetRef{}, Changed: []platform.AssetRef{},
	}}
	for _, asset := range saved.Assets {
		reply.Preview.Included = append(reply.Preview.Included, asset.Ref)
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
