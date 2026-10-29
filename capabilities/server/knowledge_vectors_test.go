package platformserver

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/knowledge"
	"platformserver/journal"
	"platformserver/platform"
)

type indexedKnowledgeStore struct {
	journal.Memory
	hits     []journal.VectorHit
	requests []int
}

func (s *indexedKnowledgeStore) Vectors(string, string, []string) map[string][]float32 {
	panic("production search fetched corpus vectors")
}
func (s *indexedKnowledgeStore) SyncVectorSources(_ context.Context, _, _ string, sources []journal.VectorSource, _ bool) error {
	slices.SortFunc(sources, func(a, b journal.VectorSource) int { return strings.Compare(a.Source, b.Source) })
	s.hits = nil
	for i, source := range sources {
		for _, passage := range source.Passages {
			s.hits = append(s.hits, journal.VectorHit{Source: source.Source, Chunk: passage.Chunk, Hash: passage.Hash, Score: 1 - float64(i)/100})
		}
	}
	return nil
}
func (s *indexedKnowledgeStore) MissingVectors(context.Context, string, string, []string) ([]string, error) {
	return nil, nil
}
func (s *indexedKnowledgeStore) NearestVectors(_ context.Context, _, _ string, _ []float32, _ []string, limit int) ([]journal.VectorHit, error) {
	s.requests = append(s.requests, limit)
	return s.hits[:min(limit, len(s.hits))], nil
}

func TestKnowledgeVectorSearchContinuesAfterAuthorization(t *testing.T) {
	tn := stockTenant(t, knowledge.New("t-1"))
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	store := &indexedKnowledgeStore{}
	tn.Store = store
	for i := range 33 {
		id := fmt.Sprintf("D%02d", i)
		apps := []string{"other"}
		if i == 32 {
			apps = []string{"stock"}
		}
		doc := platform.KnowledgeDocument{Record: platform.Record{ID: id}, Title: id, Text: "中文工单维护", Apps: apps}
		if err := tn.automation(platform.KnowledgeApp, true).PutAt(now, "test.document", doc); err != nil {
			t.Fatal(err)
		}
	}
	reader := memberOf(t, tn, "ana")
	context := tn.knowledgeContext()
	context.Active = func() string { return "local/embed" }
	context.NeedsRebuild = func(string) bool { return false }
	context.Space = func(name string) string { return name }
	context.Embed = func(string, []string, time.Time) ([][]float32, error) { return [][]float32{{1}}, nil }
	found := tn.index.Search(context, &reader, "", "中文工单维护", 1, now)
	if len(found) != 1 || len(store.requests) != 2 || store.requests[0] != 32 || store.requests[1] != 64 {
		t.Fatalf("did not continue after filtering: %v %v", found, store.requests)
	}
	for _, passage := range found {
		if passage.Document != "knowledge.document/D32" {
			t.Fatalf("private content escaped: %s", passage.Document)
		}
	}
}
