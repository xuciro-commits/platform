package journal

import (
	"context"
	"encoding/json"
)

// StateDelta is supplied by the authority that sealed the accepted result.
// The journal stores opaque images and history positions, never business rules.
type StateDelta struct {
	// LegacyUnique identifies an old result without owner-supplied key changes.
	// Only verified retained-suffix recovery may rebuild those derived keys.
	LegacyUnique bool
	// LegacyImages contains only original accepted record images whose later
	// installations the first SQL writer did not project into current records.
	LegacyImages map[string]json.RawMessage
	LegacyValues map[string]json.RawMessage
	Unique       []StateUnique
	Records      []StateRecord
	Replacements []StateReplacement
	Changes      []StateChange
	Values       []StateValue
	ValueUpdates []StateValueUpdate
}

// StateReplacement is a sealed metadata installation: only its value changes;
// the existing history, image format and projections remain unchanged.
type StateReplacement struct {
	Type, ID, Before string
	Value            json.RawMessage
}

type StateRecord struct {
	Type, ID     string
	Image        json.RawMessage
	HistoryPrior string
	HistoryHead  string
	HistoryBase  int // -1: full historical image; >=0: appended suffix
	History      []json.RawMessage
}

type StateChange struct {
	App, ID, Key string
	Index        int // copied prefix ordinal, or zero to append in accepted order
	Image        []byte
}

// StateValueUpdate is an owner-provided pure projection of a saved observation
// onto its opaque state. It runs after the result's complete value replacements.
// No live owner, permission decision or outside request is reachable here.
type StateValueUpdate struct {
	Owner, Key string
	Apply      func(json.RawMessage) (json.RawMessage, error)
}

type StateValue struct {
	Owner, Key string
	Image      json.RawMessage
}

// RuntimeState is optional: file and memory deployments retain their existing
// storage while the same accepted-result application implements their rules.
type RuntimeState interface {
	InitializeState(context.Context, string, int64, StateDelta) error
	StateRecord(context.Context, string, string, string) (json.RawMessage, error)
	StateCandidates(context.Context, string, string, []StateFilter, string, int, int64) ([]string, error)
	StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error)
	StateChanges(context.Context, string, string, string, string, int) ([][]byte, error)
	StateValue(context.Context, string, string, string, int64) (json.RawMessage, error)
	StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error)
}

// StateCurrent preserves the original image and its incremental history header.
// Position is the commit that last replaced this row, not the read-view position.
type StateCurrent struct {
	Image        json.RawMessage
	HistoryCount int
	HistoryHead  string
	Position     int64
}

// StateReadViews fixes SQL records, histories, owner values and derived keys to
// the tenant position already applied by the host coordinator.
type StateReadViews interface {
	BeginStateRead(context.Context, string, int64) (StateReadView, error)
}

type StateDigest struct {
	Records int64  `json:"records"`
	Digest  string `json:"digest"`
}

type StateReadView interface {
	Checkpoint(context.Context, string) (StateDigest, error)
	Position() int64
	Close() error
	StateCurrent(context.Context, string, string, string) (StateCurrent, error)
	StateRecord(context.Context, string, string, string) (json.RawMessage, error)
	StateCandidates(context.Context, string, string, []StateFilter, string, int, int64) ([]string, error)
	StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error)
	StateChanges(context.Context, string, string, string, string, int) ([][]byte, error)
	StateValue(context.Context, string, string, string, int64) (json.RawMessage, error)
	StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error)
	StateUnique(context.Context, string, string, string, string, int64) ([]string, error)
}

// StateFilter only reduces the candidate IDs an owner examines. The original
// owner still evaluates its full predicate and permission rules on every row.
type StateFilter struct {
	Path   []string
	Values []string
	List   bool
}

// StateCheckpoint is the original restored state at the SQL position, before
// any retained accepted-result suffix is applied.
type StateCheckpoint struct {
	Position int64
	Delta    StateDelta
}

// RuntimeStateRecovery only repairs a verified accepted-result suffix before
// the tenant serves. Ordinary accepted writes still use AppendAccepted.
type RuntimeStateRecovery interface {
	StatePosition(context.Context, string) (int64, bool, error)
	RestoreState(context.Context, string, int64, StateCheckpoint, StateDelta) error
}

// StateUnique replaces the complete derived key set of one owner record.
// Multiple rows may occupy the same key: the business owner, rather than a
// database uniqueness constraint, decides whether a new write is admissible.
type StateUnique struct {
	Type, ID string
	Keys     map[string]string
}

// UniqueState is the optional lookup for owner-derived business constraints.
// Reads require the same position as the owner's applied private read view.
type UniqueState interface {
	StateUnique(context.Context, string, string, string, string, int64) ([]string, error)
}

// RuntimeStateUpgrade identifies the one-time conversion of the first SQL
// projection. Conversion compares its original accepted preimages before writes.
type RuntimeStateUpgrade interface {
	StateNeedsUpgrade(context.Context, string) (bool, error)
}
