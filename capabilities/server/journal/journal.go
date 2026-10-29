package journal

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Vectors(tenant, model string, hashes []string) map[string][]float32
	SaveVectors(tenant, model string, vectors map[string][]float32)
	SaveTranscript(x Transcript)
	Transcripts(tenant, run string, limit int) []Transcript
	PurgeTranscripts(tenant string, before time.Time)
}

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

type Identity struct {
	Tenant, App, Scope, Key, Hash string
}

type Identify func(raw []byte) (Identity, error)

func EncodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func DecodeVector(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

type Journals interface {
	Store
	Entries(ctx context.Context, tenant string, after int64) ([]Entry, error)
	Position(tenant string) int64
	SaveSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error
	RepairSnapshot(ctx context.Context, tenant string, seq int64, code string, state []byte) error
	Snapshot(ctx context.Context, tenant, code string) (int64, []byte, bool, error)
	Append(ctx context.Context, tenant string, e Entry) error
	AppendAccepted(ctx context.Context, tenant string, e Entry, key, hash string) ([]byte, error)
	Close()
}

func Pool(j Journals) (*pgxpool.Pool, bool) {
	pg, ok := j.(interface{ Pool() *pgxpool.Pool })
	if !ok {
		return nil, false
	}
	return pg.Pool(), true
}

func (j *Postgres) Pool() *pgxpool.Pool { return j.pool }
