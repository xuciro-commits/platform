package releases

import (
	"encoding/json"
	"fmt"
	"platformserver/platform"

	"time"
)

type ReleaseSummary struct {
	ID         string            `json:"id"`
	Title      string            `json:"title"`
	Assets     int               `json:"assets"`
	From       string            `json:"from,omitempty"`
	PromotedAt time.Time         `json:"promotedAt,omitzero"`
	PromotedTo []PromotionTarget `json:"promotedTo,omitempty"`
}

type PromotionTarget struct {
	Tenant string    `json:"tenant"`
	Member string    `json:"member"`
	At     time.Time `json:"at"`
	Active bool      `json:"active"`
}

type ReleasePage struct {
	Candidates []ReleaseSummary `json:"candidates"`
	Total      int              `json:"total"`
	ActiveID   string           `json:"activeId"`
}

type ReleaseCandidateSaved struct {
	ID string `json:"id"`
}

type SavedReleaseReview struct {
	AI                   []platform.ApplicationAI `json:"ai,omitempty"`
	Preview              ReleasePreview           `json:"preview"`
	Assets               []platform.ReleaseAsset  `json:"assets"`
	Active               bool                     `json:"active"`
	RunningMatches       bool                     `json:"runningMatches"`
	RunningDiagnostic    string                   `json:"runningDiagnostic,omitempty"`
	CanActivate          bool                     `json:"canActivate"`
	ActivationDiagnostic string                   `json:"activationDiagnostic,omitempty"`
	UpgradePlan          *ReleaseUpgradePlan      `json:"upgradePlan,omitempty"`
	Extensions           []ReleaseExtensionReview `json:"extensions,omitempty"`
	From                 string                   `json:"from,omitempty"`
	PromotedAt           time.Time                `json:"promotedAt,omitzero"`
	PromotedTo           []PromotionTarget        `json:"promotedTo,omitempty"`
}

func (s *Store) Page(offset, limit int, application *platform.AssetRef) (ReleasePage, error) {
	if offset < 0 || limit < 1 || limit > 100 {
		return ReleasePage{}, fmt.Errorf("release page needs a nonnegative offset and a limit from 1 to 100")
	}
	ids := s.Ids()
	if application != nil {
		ref := *application
		filtered := make([]string, 0, len(ids))
		for _, id := range ids {
			candidate, err := s.Candidate(id)
			if err != nil {
				return ReleasePage{}, err
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
	reply := ReleasePage{Candidates: []ReleaseSummary{}, Total: len(ids), ActiveID: s.Active()}
	start := min(offset, len(ids))
	for _, id := range ids[start : start+min(limit, len(ids)-start)] {
		candidate, err := s.Candidate(id)
		if err != nil {
			return ReleasePage{}, err
		}
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
		if origin, ok := s.Origin(id); ok {
			summary.From, summary.PromotedAt = origin.From, origin.At
		}
		reply.Candidates = append(reply.Candidates, summary)
	}
	return reply, nil
}

func (s *Store) Review(id string, context ReviewContext) (SavedReleaseReview, error) {
	raw, exists := s.candidates[id]
	if !exists {
		return SavedReleaseReview{}, fmt.Errorf("saved release candidate not found")
	}
	saved, err := platform.ReadCandidate(id, raw)
	if err != nil {
		return SavedReleaseReview{}, err
	}
	visible := map[platform.AssetRef]bool{}
	for _, asset := range saved.Assets {
		visible[asset.Ref] = true
	}
	ai, err := context.AI(saved.Assets, visible)
	if err != nil {
		return SavedReleaseReview{}, err
	}
	reply := SavedReleaseReview{AI: ai, Assets: saved.Assets, Active: s.Active() == id, Preview: ReleasePreview{
		CandidateID: id, Included: []platform.AssetRef{}, Added: []platform.AssetRef{}, Removed: []platform.AssetRef{}, Changed: []platform.AssetRef{},
	}}
	for _, asset := range saved.Assets {
		reply.Preview.Included = append(reply.Preview.Included, asset.Ref)
	}
	if origin, ok := s.Origin(id); ok {
		reply.From, reply.PromotedAt = origin.From, origin.At
	}
	reply.Extensions, err = context.Extensions(saved)
	if err != nil {
		return reply, err
	}
	running, err := context.Running(saved)
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
	if context.ActivationDiagnostic != "" {
		reply.ActivationDiagnostic = context.ActivationDiagnostic
		return reply, nil
	}
	if reply.RunningMatches {
		err = context.CheckExisting(id, raw)
	} else {
		err = context.Prepare(saved, false)
	}
	reply.CanActivate = err == nil
	if err != nil {
		reply.ActivationDiagnostic = err.Error()
		if plan, planErr := context.Upgrade(saved); planErr == nil && plan != nil {
			if checkErr := context.Prepare(saved, true); checkErr == nil {
				reply.UpgradePlan = plan
			}
		}
	}
	return reply, nil
}

type ReviewContext struct {
	AI                   func([]platform.ReleaseAsset, map[platform.AssetRef]bool) ([]platform.ApplicationAI, error)
	Extensions           func(platform.ReleaseCandidate) ([]ReleaseExtensionReview, error)
	Running              func(platform.ReleaseCandidate) (platform.ReleaseCandidate, error)
	CheckExisting        func(string, []byte) error
	Prepare              func(platform.ReleaseCandidate, bool) error
	Upgrade              func(platform.ReleaseCandidate) (*ReleaseUpgradePlan, error)
	ActivationDiagnostic string
}
