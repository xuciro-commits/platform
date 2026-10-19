package journal

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestPostgresVectorSourcesAndFilteredRecall(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenPostgres(ctx, dsn, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	tenant := fmt.Sprintf("vectors-%d", time.Now().UnixNano())
	defer func() {
		j.pool.Exec(ctx, `delete from embedding_sources where tenant=$1`, tenant)
		j.pool.Exec(ctx, `delete from embeddings where tenant=$1`, tenant)
	}()
	const model = "local/three"
	vectors := map[string][]float32{}
	var sources []VectorSource
	// Most of the nearest items are private to another application. Filtering
	// must happen before the result limit, including with a low ANN scan budget.
	for i := range 256 {
		hash := fmt.Sprintf("h%03d", i)
		vectors[hash] = []float32{1, float32(i+1) / 1000, 0}
		apps := []string{"other"}
		if i >= 240 {
			apps = []string{"desk"}
		}
		sources = append(sources, VectorSource{Source: "doc/" + hash, Passages: []VectorPassage{{Chunk: 0, Hash: hash, Apps: apps}}})
	}
	j.SaveVectors(tenant, model, vectors)
	if err := j.SyncVectorSources(ctx, tenant, model, sources, true); err != nil {
		t.Fatal(err)
	}
	hits, err := j.NearestVectors(ctx, tenant, model, []float32{1, 0, 0}, []string{"desk"}, 10)
	if err != nil || len(hits) != 10 {
		t.Fatalf("filtered recall: %d hits, %v", len(hits), err)
	}
	for i, hit := range hits {
		if want := fmt.Sprintf("h%03d", 240+i); hit.Hash != want {
			t.Fatalf("wrong filtered neighbor %d: %+v want %s", i, hit, want)
		}
	}
	if missing, err := j.MissingVectors(ctx, tenant, model, []string{"h000", "absent"}); err != nil || len(missing) != 1 || missing[0] != "absent" {
		t.Fatalf("missing check: %v %v", missing, err)
	}
	if hits, err := j.NearestVectors(ctx, tenant+"-other", model, []float32{1, 0, 0}, []string{"desk"}, 10); err != nil || len(hits) != 0 {
		t.Fatalf("tenant escaped: %v %v", hits, err)
	}
	if hits, err := j.NearestVectors(ctx, tenant, model, []float32{1, 0}, []string{"desk"}, 10); err != nil || len(hits) != 0 {
		t.Fatalf("dimensions mixed: %v %v", hits, err)
	}
	if err := j.SyncVectorSources(ctx, tenant, model, []VectorSource{{Source: "doc/h240"}}, false); err != nil {
		t.Fatal(err)
	}
	hits, err = j.NearestVectors(ctx, tenant, model, []float32{1, 0, 0}, []string{"desk"}, 20)
	if err != nil || len(hits) != 15 || hits[0].Hash != "h241" {
		t.Fatalf("source retirement/exhaustion: %v %v", hits, err)
	}
}
