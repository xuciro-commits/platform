package protocol

import (
	"context"
	"encoding/json"
	"time"
)

type Transcript struct {
	Tenant  string          `json:"-"`
	At      time.Time       `json:"at"`
	Member  string          `json:"member"`
	Model   string          `json:"model"`
	Run     string          `json:"run,omitempty"`
	Request json.RawMessage `json:"request"`
	Answer  json.RawMessage `json:"answer"`
	Outcome string          `json:"outcome"`
}

type VectorSource struct {
	Source   string
	Passages []VectorPassage
}

type VectorPassage struct {
	Chunk int
	Hash  string
	Scope string
	Apps  []string
}

type VectorHit struct {
	Source string
	Chunk  int
	Hash   string
	Score  float64
}

type VectorIndex interface {
	SyncVectorSources(context.Context, string, string, []VectorSource, bool) error
	MissingVectors(context.Context, string, string, []string) ([]string, error)
	NearestVectors(context.Context, string, string, []float32, []string, int) ([]VectorHit, error)
}
