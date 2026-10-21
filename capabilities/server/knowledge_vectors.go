package platformserver

import (
	"context"
	"log"
	"slices"
	"strings"
	"time"

	"platformkernel/kernel"
	"platformserver/journal"
	"platformserver/platform"
)

func (t *Tenant) syncVectorSources(store journal.VectorIndex, model string) error {
	k := &t.index
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
	err := store.SyncVectorSources(context.Background(), t.ID, model, sources, reset)
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

func (t *Tenant) nearestKnowledge(model string, query []float32, reader *platform.Member, app string, limit int, now time.Time) map[*chunk]float64 {
	store := t.store().(journal.VectorIndex)
	out := map[*chunk]float64{}
	if err := t.syncVectorSources(store, model); err != nil {
		log.Printf("knowledge sources %s: %v", t.ID, err)
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
	// Widen after final record/field authorization, rather than treating a
	// filtered global top-k as all the readable results that exist.
	for count := max(32, limit*4); ; count *= 2 {
		hits, err := store.NearestVectors(context.Background(), t.ID, model, query, apps, count)
		if err != nil {
			log.Printf("knowledge vectors %s: %v", t.ID, err)
			return out
		}
		for _, hit := range hits {
			k := &t.index
			k.mu.Lock()
			var c *chunk
			if cs := k.chunks[hit.Source]; hit.Chunk >= 0 && hit.Chunk < len(cs) && !cs[hit.Chunk].gone && cs[hit.Chunk].hash == hit.Hash {
				c = cs[hit.Chunk]
			}
			k.mu.Unlock()
			if c != nil && hit.Score > 0 && t.currentPassageReadable(c, reader, app, now) {
				out[c] = hit.Score
			}
		}
		if len(out) >= limit || len(hits) < count {
			return out
		}
	}
}

func (t *Tenant) currentPassageReadable(c *chunk, reader *platform.Member, app string, now time.Time) bool {
	out, _ := recordRead(t, context.Background(), func() (bool, *kernel.Error) { return t.currentPassageReadableInLane(c, reader, app, now), nil })
	return out
}
func (t *Tenant) currentPassageReadableInLane(c *chunk, reader *platform.Member, app string, now time.Time) bool {
	t.index.mu.Lock()
	gone := c.gone
	t.index.mu.Unlock()
	if gone {
		return false
	}
	if !readable(c, reader, app) {
		return false
	}
	if c.scope == "" {
		id, document := strings.CutPrefix(c.doc, platform.KnowledgeDocumentType+"/")
		t.records.mu.Lock()
		defer t.records.mu.Unlock()
		if !document {
			// Attached text uses its current document's content grant too.
			fileID, fileSource := strings.CutPrefix(c.doc, platform.FileType+"/")
			files := t.records.types[platform.FileType]
			if !fileSource || files == nil {
				return false
			}
			r := files.rows.Get(fileID)
			if r == nil {
				return false
			}
			file := r.Value.Interface().(platform.File)
			if file.Archived {
				return false
			}
			id, document = strings.CutPrefix(file.Target, platform.KnowledgeDocumentType+"/")
			if !document {
				return false
			}
		}
		et := t.records.types[platform.KnowledgeDocumentType]
		if et == nil {
			return false
		}
		r := et.rows.Get(id)
		if r == nil {
			return false
		}
		d := r.Value.Interface().(platform.KnowledgeDocument)
		return !d.Archived && knowledgeAppsReadable(d.Apps, reader, app)
	}
	m := t.host()
	if reader != nil {
		m = *reader
	}
	return t.readable(m, c.scope, now)
}
