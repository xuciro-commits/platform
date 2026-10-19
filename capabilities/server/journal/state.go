package journal

import (
	"context"
	"encoding/json"
)

// StateDelta is supplied by the authority that sealed the accepted result.
// The journal stores opaque images and history positions, never business rules.
type StateDelta struct {
	Records []StateRecord
	Changes []StateChange
	Values  []StateValue
}

type StateRecord struct {
	Type, ID    string
	Image       json.RawMessage
	HistoryBase int // -1: full historical image; >=0: appended suffix
	History     []json.RawMessage
}

type StateChange struct {
	App, ID, Key string
	Index        int // copied prefix ordinal, or zero to append in accepted order
	Image        []byte
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

// StateFilter only reduces the candidate IDs an owner examines. The original
// owner still evaluates its full predicate and permission rules on every row.
type StateFilter struct {
	Path   []string
	Values []string
	List   bool
}
