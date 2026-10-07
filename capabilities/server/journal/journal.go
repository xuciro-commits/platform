package journal

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Journals is a tenant journal a host runs on: PostgreSQL on the delivery path
// (ADR-0019), or one local file on the lightweight one (ADR-0049 D2). Both keep
// the same semantics — entries in order per tenant, appends expect the read
// position, an accepted result is committed once under its idempotency key,
// snapshots are checkpoints of this code — and both carry the derived store
// (vectors and transcripts), which lives wherever the journal lives.
// Store keeps the derived state the journal owns besides entries: embedding
// vectors and model calls' transcripts (ADR-0022 D2, D8). The PostgreSQL
// journal implements it; without one the host keeps both in memory.
type Store interface {
	Vectors(tenant, model string, hashes []string) map[string][]float32
	SaveVectors(tenant, model string, vectors map[string][]float32)
	SaveTranscript(x Transcript)
	Transcripts(tenant, run string, limit int) []Transcript
	PurgeTranscripts(tenant string, before time.Time)
}

// Transcript is one model call kept for review (ADR-0022 D8).
type Transcript struct {
	Tenant  string          `json:"-"`
	At      time.Time       `json:"at"`
	Member  string          `json:"member"`
	Model   string          `json:"model"`
	Run     string          `json:"run,omitempty"` // the agent run or evaluation it was for
	Request json.RawMessage `json:"request"`
	Answer  json.RawMessage `json:"answer"`
	Outcome string          `json:"outcome"`
}

// Identity is what an accepted result is indexed by: the idempotency key and
// request hash under its tenant, app and scope. The host decodes it — the
// journal only stores result bytes — so it is injected as Identify.
type Identity struct {
	Tenant, App, Scope, Key, Hash string
}

// Identify decodes an accepted result's identity from its stored bytes.
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

// pool is the PostgreSQL pool behind a journal, for the reads only the delivery
// path has (the projection for outside tools, ADR-0019 D7). A local file
// journal has none; the deployment refuses what needs one.
func Pool(j Journals) (*pgxpool.Pool, bool) {
	pg, ok := j.(*Postgres)
	if !ok {
		return nil, false
	}
	return pg.pool, true
}

// Pool is the connection pool the PostgreSQL journal runs on, for the host's
// own tables and for tests that clean up after themselves.
func (j *Postgres) Pool() *pgxpool.Pool { return j.pool }
