package store

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"math"
	"platformserver/platform"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Store interface {
	Vectors(tenant, model string, hashes []string) map[string][]float32
	SaveVectors(tenant, model string, vectors map[string][]float32)
	SaveTranscript(x Transcript)
	Transcripts(tenant, run string, limit int) []Transcript
	PurgeTranscripts(tenant string, before time.Time)
	SaveObservations(events []platform.Observation) error
	QueryObservations(query platform.ObservationQuery) (platform.ObservationPage, error)
	PurgeObservations(tenant string, before time.Time) (int, error)
	ObservationUsage(tenant string, from time.Time) (int64, error)
	SummarizeObservations(tenant, member string, from, to time.Time) (platform.ObservationSummary, error)
	SaveAuditEntries(tenant string, entries []platform.AuditEntry) error
	QueryAuditEntries(query platform.AuditQuery) (platform.AuditPage, error)
	SavePersonalReads(tenant string, reads []platform.PersonalRead) error
	QueryPersonalReads(query platform.PersonalReadQuery) (platform.PersonalReadPage, error)
	ReplaceRunIndex(tenant, scope string, entries []platform.RunIndexEntry) error
	QueryRunIndex(query platform.RunIndexQuery) (platform.RunIndexPage, error)
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
