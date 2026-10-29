package knowledge

import (
	"context"

	"fmt"
	"log"

	"platformserver/journal"
	"platformserver/platform"

	"slices"
	"strings"

	"time"
)

const (
	passageChars = 3200
	embedBatch   = 32
)

func (k *Index) Sync(load func(bool) ([]Source, map[string]bool)) {
	k.mu.Lock()
	walked := k.walked
	k.mu.Unlock()
	srcs, changed := load(walked)
	if walked && len(changed) == 0 {
		return
	}
	if k.docs == nil {
		k.docs, k.chunks = map[string]string{}, map[string][]*chunk{}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	seen := map[string]bool{}
	for _, s := range srcs {
		seen[s.Key] = true
		if k.docs[s.Key] == s.Revision {
			continue
		}
		k.keep(s.Key, cut(s))
		k.docs[s.Key] = s.Revision
	}
	for key := range k.docs {
		if seen[key] {
			continue
		}
		record, _, isField := strings.Cut(key, "#")
		if !walked || isField && changed[record] || changed[key] {
			k.keep(key, nil)
			delete(k.docs, key)
		}
	}
	k.walked = true
}

func (k *Index) syncVectorSources(ctx Context, store journal.VectorIndex, model string) error {
	k.vectorMu.Lock()
	defer k.vectorMu.Unlock()
	k.mu.Lock()
	reset := k.vectorModel != model
	dirty := k.vectorDirty
	if dirty == nil {
		dirty = map[string]bool{}
	}
	if reset {
		for source := range k.chunks {
			dirty[source] = true
		}
	}
	sources := make([]journal.VectorSource, 0, len(dirty))
	for source := range dirty {
		item := journal.VectorSource{Source: source}
		for _, c := range k.chunks[source] {
			item.Passages = append(item.Passages, journal.VectorPassage{Chunk: c.n, Hash: c.hash, Scope: c.scope, Apps: slices.Clone(c.apps)})
		}
		sources = append(sources, item)
	}
	k.vectorDirty = map[string]bool{}
	k.mu.Unlock()
	err := store.SyncVectorSources(context.Background(), ctx.Tenant, model, sources, reset)
	k.mu.Lock()
	if err == nil {
		k.vectorModel = model
	} else {
		for source := range dirty {
			k.vectorDirty[source] = true
		}
	}
	k.mu.Unlock()
	return err
}

func (k *Index) Nearest(ctx Context, model string, query []float32, reader *platform.Member, app string, limit int, now time.Time) map[*chunk]float64 {
	store := ctx.Store.(journal.VectorIndex)
	out := map[*chunk]float64{}
	if err := k.syncVectorSources(ctx, store, model); err != nil {
		log.Printf("knowledge sources %s: %v", ctx.Tenant, err)
		return out
	}
	apps := []string{app}
	if reader != nil {
		apps = nil
		for name, role := range reader.Roles {
			if role != "" {
				apps = append(apps, name)
			}
		}
	}
	for count := max(32, limit*4); ; count *= 2 {
		hits, err := store.NearestVectors(context.Background(), ctx.Tenant, model, query, apps, count)
		if err != nil {
			log.Printf("knowledge vectors %s: %v", ctx.Tenant, err)
			return out
		}
		for _, hit := range hits {
			k.mu.Lock()
			var c *chunk
			if cs := k.chunks[hit.Source]; hit.Chunk >= 0 && hit.Chunk < len(cs) && !cs[hit.Chunk].gone && cs[hit.Chunk].hash == hit.Hash {
				c = cs[hit.Chunk]
			}
			k.mu.Unlock()
			if c != nil && hit.Score > 0 && k.current(ctx, c, reader, app, now) {
				out[c] = hit.Score
			}
		}
		if len(out) >= limit || len(hits) < count {
			return out
		}
	}
}

func (k *Index) current(ctx Context, c *chunk, reader *platform.Member, app string, at time.Time) bool {
	allowed := false
	ctx.Read(func() { allowed = k.currentInLane(ctx, c, reader, app, at) })
	return allowed
}
func (k *Index) currentInLane(ctx Context, c *chunk, reader *platform.Member, app string, at time.Time) bool {
	k.mu.Lock()
	gone := c.gone
	k.mu.Unlock()
	return !gone && readable(c, reader, app) && ctx.Visible(c.doc, c.scope, c.apps, reader, app, at)
}
func (k *Index) Text(hash string, load func() (string, bool)) (string, bool) {
	k.mu.Lock()
	text, ok := k.texts[hash]
	k.mu.Unlock()
	if ok {
		return text, true
	}
	text, ok = load()
	if !ok {
		return "", false
	}
	k.mu.Lock()
	if k.texts == nil {
		k.texts = map[string]string{}
	}
	k.texts[hash] = text
	k.mu.Unlock()
	return text, true
}

func (k *Index) Embed(ctx Context, now time.Time) {
	if ctx.Stopped() {
		return
	}
	model := ctx.Preparing()
	if model == "" {
		model = ctx.Active()
		if ctx.NeedsRebuild(model) {
			return
		}
	}
	if model == "" {
		return
	}
	space := ctx.Space(model)
	ctx.Sync()
	k.mu.Lock()
	k.embeddingError = ""
	k.mu.Unlock()
	fail := func(err error) {
		k.mu.Lock()
		k.embeddingError = err.Error()
		k.mu.Unlock()
		ctx.Changed()
	}
	k.mu.Lock()
	var hashes []string
	texts := map[string]string{}
	for _, cs := range k.chunks {
		for _, c := range cs {
			if texts[c.hash] == "" {
				hashes = append(hashes, c.hash)
				texts[c.hash] = c.title + "\n" + c.text
			}
		}
	}
	k.mu.Unlock()
	slices.Sort(hashes)
	var missing []string
	if search, ok := ctx.Store.(journal.VectorIndex); ok {
		if err := k.syncVectorSources(ctx, search, space); err != nil {
			return
		}
		var err error
		missing, err = search.MissingVectors(context.Background(), ctx.Tenant, space, hashes)
		if err != nil {
			return
		}
	} else {
		have := ctx.Store.Vectors(ctx.Tenant, space, hashes)
		for _, h := range hashes {
			if have[h] == nil {
				missing = append(missing, h)
			}
		}
	}
	expected := 0
	for _, vector := range ctx.Store.Vectors(ctx.Tenant, space, hashes) {
		if len(vector) > 0 {
			expected = len(vector)
			break
		}
	}
	for len(missing) > 0 {
		if ctx.Space(model) != space {
			fail(fmt.Errorf("Model configuration changed; rebuild with the current revision"))
			return
		}
		batch := missing[:min(embedBatch, len(missing))]
		missing = missing[len(batch):]
		var in []string
		for _, h := range batch {
			in = append(in, texts[h])
		}
		vs, err := ctx.Embed(model, in, now)
		if err != nil {
			fail(err)
			return
		}
		if err := ConsistentVectors(vs, expected); err != nil {
			fail(err)
			return
		}
		if expected == 0 && len(vs) > 0 {
			expected = len(vs[0])
		}
		if ctx.Space(model) != space {
			fail(fmt.Errorf("Model configuration changed; rebuild with the current revision"))
			return
		}
		saved := map[string][]float32{}
		for i, h := range batch {
			saved[h] = vs[i]
		}
		ctx.Store.SaveVectors(ctx.Tenant, space, saved)
		ctx.Changed()
	}
}

func (k *Index) Status(ctx Context) platform.KnowledgeIndexStatus {
	out := platform.KnowledgeIndexStatus{Active: ctx.Active(), Preparing: ctx.Preparing()}
	if ctx.NeedsRebuild(out.Active) {
		out.NeedsRebuild = true
		out.Problem = "Model configuration changed; prepare and activate a new vector index to resume semantic search"
	}
	if out.Preparing == "" {
		return out
	}
	k.mu.Lock()
	hashes := []string{}
	seen := map[string]bool{}
	for _, chunks := range k.chunks {
		for _, c := range chunks {
			if !c.gone && !seen[c.hash] {
				seen[c.hash] = true
				hashes = append(hashes, c.hash)
			}
		}
	}
	out.Problem = k.embeddingError
	k.mu.Unlock()
	out.Total = len(hashes)
	for _, vector := range ctx.Store.Vectors(ctx.Tenant, ctx.Space(out.Preparing), hashes) {
		if len(vector) == 0 {
			continue
		}
		if out.Dimensions != 0 && len(vector) != out.Dimensions {
			out.Problem = "Vector dimensions changed. Configure a new model revision and rebuild the index."
			return out
		}
		out.Dimensions = len(vector)
		out.Completed++
	}
	out.Ready = out.Completed == out.Total && out.Problem == ""
	return out
}

func ConsistentVectors(vectors [][]float32, expected int) error {
	for _, v := range vectors {
		if len(v) == 0 {
			return fmt.Errorf("The provider returned an empty embedding")
		}
		if expected == 0 {
			expected = len(v)
		}
		if len(v) != expected {
			return fmt.Errorf("Vector dimensions changed. Configure a new model revision and rebuild the index.")
		}
	}
	return nil
}
