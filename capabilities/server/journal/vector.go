package journal

import "context"

// VectorSource replaces the derived passages of one source. An empty list
// removes its index entries; authoritative documents and grants stay elsewhere.
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

// VectorIndex is the PostgreSQL search surface. File deployments retain their
// small local vector store; production never retrieves the corpus's vectors.
type VectorIndex interface {
	SyncVectorSources(context.Context, string, string, []VectorSource, bool) error
	MissingVectors(context.Context, string, string, []string) ([]string, error)
	NearestVectors(context.Context, string, string, []float32, []string, int) ([]VectorHit, error)
}
