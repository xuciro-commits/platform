package releases

import (
	"platformserver/platform"
	"platformserver/studio/build"

	"time"
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

type ReleasePage struct {
	Candidates []ReleaseSummary `json:"candidates"`
	Total      int              `json:"total"`
	ActiveID   string           `json:"activeId"`
}

// ReleaseCandidateSaved is the candidate a tenant just froze.
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

type ReleaseDraftChanges struct {
	Changes []build.DraftChange `json:"changes"`
}
