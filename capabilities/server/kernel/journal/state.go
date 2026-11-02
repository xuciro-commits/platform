package journal

import (
	"context"
	"encoding/json"
)

type StateDelta struct {
	LegacyUnique bool
	LegacyImages map[string]json.RawMessage
	LegacyValues map[string]json.RawMessage
	Unique       []StateUnique
	Records      []StateRecord
	Replacements []StateReplacement
	Changes      []StateChange
	Values       []StateValue
	ValueUpdates []StateValueUpdate
}

type StateReplacement struct {
	Type, ID, Before string
	Value            json.RawMessage
}

type StateRecord struct {
	Type, ID     string
	Image        json.RawMessage
	HistoryPrior string
	HistoryHead  string
	HistoryBase  int
	History      []json.RawMessage
}

type StateChange struct {
	App, ID, Key string
	Index        int
	Image        []byte
}

type StateValueUpdate struct {
	Owner, Key string
	Apply      func(json.RawMessage) (json.RawMessage, error)
}

type StateValue struct {
	Owner, Key string
	Image      json.RawMessage
}

type RuntimeState interface {
	InitializeState(context.Context, string, int64, StateDelta) error
	StateRecord(context.Context, string, string, string) (json.RawMessage, error)
	StateCandidates(context.Context, string, string, []StateFilter, string, int, int64) ([]string, error)
	StateHistory(context.Context, string, string, string, int, int) ([]json.RawMessage, error)
	StateChanges(context.Context, string, string, string, string, int) ([][]byte, error)
	StateValue(context.Context, string, string, string, int64) (json.RawMessage, error)
	StateValues(context.Context, string, string, int64) (map[string]json.RawMessage, error)
}

type StateCurrent struct {
	Image        json.RawMessage
	HistoryCount int
	HistoryHead  string
	Position     int64
}

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

type StateFilter struct {
	Path   []string
	Values []string
	List   bool
}

type StateCheckpoint struct {
	Position int64
	Delta    StateDelta
}

type RuntimeStateRecovery interface {
	StatePosition(context.Context, string) (int64, bool, error)
	RestoreState(context.Context, string, int64, StateCheckpoint, StateDelta) error
}

type StateUnique struct {
	Type, ID string
	Keys     map[string]string
}

type UniqueState interface {
	StateUnique(context.Context, string, string, string, string, int64) ([]string, error)
}

type RuntimeStateUpgrade interface {
	StateNeedsUpgrade(context.Context, string) (bool, error)
}
