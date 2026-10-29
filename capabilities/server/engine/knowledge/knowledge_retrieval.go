package knowledge

import (
	"crypto/sha256"
	"encoding/hex"

	"fmt"

	"math"

	"platformserver/journal"
	"platformserver/platform"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

type VectorStore interface {
	Vectors(string, string, []string) map[string][]float32
	SaveVectors(string, string, map[string][]float32)
}
type Context struct {
	Tenant        string
	Store         VectorStore
	Sync          func()
	Read          func(func())
	Visible       func(document, scope string, apps []string, reader *platform.Member, app string, at time.Time) bool
	ScopeReadable func(*platform.Member, string, time.Time) bool
	Active        func() string
	Preparing     func() string
	NeedsRebuild  func(string) bool
	Space         func(string) string
	Embed         func(string, []string, time.Time) ([][]float32, error)
	Stopped       func() bool
	Changed       func()
}

type chunk struct {
	doc, title, text, hash string
	scope                  string
	n                      int
	apps                   []string
	terms                  map[string]int
	length                 int
	gone                   bool
}

type Index struct {
	mu             sync.Mutex
	vectorMu       sync.Mutex
	vectorModel    string
	embeddingError string
	vectorDirty    map[string]bool
	docs           map[string]string
	chunks         map[string][]*chunk
	texts          map[string]string
	walked         bool
	postings       map[string][]*chunk
	total          int
	length         int
	stale          int
}

func (k *Index) keep(key string, cs []*chunk) {
	if k.vectorDirty == nil {
		k.vectorDirty = map[string]bool{}
	}
	k.vectorDirty[key] = true
	for _, c := range k.chunks[key] {
		c.gone = true
		k.total, k.length, k.stale = k.total-1, k.length-c.length, k.stale+1
	}
	if k.postings == nil {
		k.postings = map[string][]*chunk{}
	}
	if cs == nil {
		delete(k.chunks, key)
	} else {
		k.chunks[key] = cs
	}
	for _, c := range cs {
		k.total, k.length = k.total+1, k.length+c.length
		for w := range c.terms {
			k.postings[w] = append(k.postings[w], c)
		}
	}
	if k.stale > k.total {
		k.rebuild()
	}
}

func (k *Index) rebuild() {
	k.postings, k.stale = map[string][]*chunk{}, 0
	for _, cs := range k.chunks {
		for _, c := range cs {
			for w := range c.terms {
				k.postings[w] = append(k.postings[w], c)
			}
		}
	}
}

type Source struct {
	Key, Title, Text, Revision string
	Scope                      string
	Apps                       []string
}

func RecordSources(info platform.EntityInfo, v reflect.Value) []Source {
	var out []Source
	rec := v.FieldByName("Record").Interface().(platform.Record)
	title := rec.ID
	if f, ok := info.Field(info.Display); ok && len(f.Read) == 0 {
		title = fmt.Sprint(v.FieldByIndex(f.Index).Interface())
	}
	for _, f := range info.Fields {
		if text := fmt.Sprint(v.FieldByIndex(f.Index).Interface()); f.Knowledge && len(f.Read) == 0 && strings.TrimSpace(text) != "" {
			out = append(out, Source{Key: info.Type + "/" + rec.ID + "#" + f.Name, Title: info.Title + " " + title + ": " + f.Title,
				Text: text, Revision: fingerprint(title, text), Scope: info.Type + "/" + rec.ID, Apps: []string{info.App}})
		}
	}
	return out
}

func fingerprint(title, text string) string {
	sum := sha256.Sum256([]byte(title + "\x00" + text))
	return hex.EncodeToString(sum[:12])
}

func DocumentSource(d platform.KnowledgeDocument) Source {
	return Source{Key: platform.KnowledgeDocumentType + "/" + d.ID, Title: d.Title, Text: d.Text, Revision: fingerprint(d.Title, d.Text) + "/" + strings.Join(d.Apps, ","), Apps: d.Apps}
}

func cut(s Source) []*chunk {
	var out []*chunk
	for i, text := range passages(s.Text) {
		sum := sha256.Sum256([]byte(s.Title + "\n" + text))
		c := &chunk{doc: s.Key, title: s.Title, text: text, hash: hex.EncodeToString(sum[:12]), scope: s.Scope, n: i, apps: s.Apps, terms: map[string]int{}}
		for _, w := range platform.TokenizeWords(s.Title + " " + text) {
			c.terms[w]++
			c.length++
		}
		out = append(out, c)
	}
	return out
}

func passages(text string) []string {
	var out []string
	var sections [][]string
	heading := ""
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n\n") {
		para = strings.TrimSpace(para)
		if para == "" {
			continue
		}
		if strings.HasPrefix(para, "#") {
			heading = strings.SplitN(para, "\n", 2)[0]
			sections = append(sections, []string{})
		}
		if len(sections) == 0 {
			sections = append(sections, []string{})
		}
		if heading != "" && len(sections[len(sections)-1]) == 0 && !strings.HasPrefix(para, "#") {
			sections[len(sections)-1] = append(sections[len(sections)-1], heading)
		}
		sections[len(sections)-1] = append(sections[len(sections)-1], para)
	}
	for _, sec := range sections {
		cur, prev := "", ""
		for _, para := range sec {
			if cur != "" && len(cur)+len(para) > passageChars {
				out = append(out, cur)
				cur = prev
				if len(cur)+len(para) > passageChars {
					cur = ""
				}
			}
			if cur != "" {
				cur += "\n\n"
			}
			cur += para
			prev = para
		}
		if cur != "" {
			out = append(out, cur)
		}
	}
	return out
}

func readable(c *chunk, reader *platform.Member, app string) bool {
	return AppsReadable(c.apps, reader, app)
}

func AppsReadable(apps []string, reader *platform.Member, app string) bool {
	if len(apps) == 0 {
		return true
	}
	return slices.ContainsFunc(apps, func(a string) bool {
		if reader != nil {
			return reader.Roles[a] != ""
		}
		return a == app
	})
}

func cmpFloat(a, b float64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func cosine(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

func (k *Index) Search(ctx Context, reader *platform.Member, app, q string, limit int, now time.Time) []platform.Passage {
	out := []platform.Passage{}
	if strings.TrimSpace(q) == "" || reader != nil && reader.Tenant != ctx.Tenant {
		return out
	}
	ctx.Sync()
	asked := slices.Compact(slices.Sorted(slices.Values(platform.TokenizeWords(q))))
	model := ctx.Active()
	if ctx.NeedsRebuild(model) {
		model = ""
	}
	k.mu.Lock()
	df, total, avg := map[string]int{}, k.total, 1.0
	if k.total > 0 {
		avg = float64(k.length) / float64(k.total)
	}
	found := map[*chunk]bool{}
	var candidates []*chunk
	take := func(c *chunk) {
		if c.gone || found[c] || !readable(c, reader, app) {
			return
		}
		found[c] = true
		candidates = append(candidates, c)
	}
	_, databaseSearch := ctx.Store.(journal.VectorIndex)
	if model == "" || databaseSearch {
		for _, w := range asked {
			for _, c := range k.postings[w] {
				if !c.gone {
					df[w]++
				}
				take(c)
			}
		}
	} else {
		for _, w := range asked {
			for _, c := range k.postings[w] {
				if !c.gone {
					df[w]++
				}
			}
		}
		for _, cs := range k.chunks {
			for _, c := range cs {
				take(c)
			}
		}
	}
	k.mu.Unlock()
	var all []*chunk
	ctx.Read(func() {
		allowed := map[string]bool{}
		for _, c := range candidates {
			visible, known := allowed[c.scope]
			if !known {
				visible = c.scope == "" || ctx.ScopeReadable(reader, c.scope, now)
				allowed[c.scope] = visible
			}
			if visible {
				all = append(all, c)
			}
		}
	})
	if len(all) == 0 && (model == "" || !databaseSearch) {
		return out
	}
	var query []float32
	if model != "" {
		if vs, err := ctx.Embed(model, []string{q}, now); err == nil {
			query = vs[0]
		}
	}
	slices.SortFunc(all, func(a, b *chunk) int { return strings.Compare(a.doc+fmt.Sprint(a.n), b.doc+fmt.Sprint(b.n)) })
	ranks := map[*chunk]float64{}
	fuse := func(scored map[*chunk]float64) {
		list := slices.Collect(func(yield func(*chunk) bool) {
			for c, s := range scored {
				if s > 0 && !yield(c) {
					return
				}
			}
		})
		slices.SortFunc(list, func(a, b *chunk) int {
			if comparison := cmpFloat(scored[b], scored[a]); comparison != 0 {
				return comparison
			}
			return strings.Compare(a.doc+fmt.Sprint(a.n), b.doc+fmt.Sprint(b.n))
		})
		for i, c := range list {
			ranks[c] += 1 / float64(60+i+1)
		}
	}
	bm := map[*chunk]float64{}
	for _, w := range asked {
		if df[w] == 0 {
			continue
		}
		idf := math.Log(1 + (float64(total)-float64(df[w])+0.5)/(float64(df[w])+0.5))
		for _, c := range all {
			if f := float64(c.terms[w]); f > 0 {
				bm[c] += idf * f * 2.2 / (f + 1.2*(0.25+0.75*float64(c.length)/avg))
			}
		}
	}
	fuse(bm)
	if query != nil {
		cos := map[*chunk]float64{}
		if databaseSearch {
			cos = k.Nearest(ctx, ctx.Space(model), query, reader, app, limit, now)
		} else {
			hashes := make([]string, len(all))
			for i, c := range all {
				hashes[i] = c.hash
			}
			vectors := ctx.Store.Vectors(ctx.Tenant, ctx.Space(model), hashes)
			for _, c := range all {
				if v := vectors[c.hash]; v != nil {
					cos[c] = cosine(query, v)
				}
			}
		}
		fuse(cos)
	}
	ctx.Read(func() {
		for c, r := range ranks {
			if !k.currentInLane(ctx, c, reader, app, now) {
				continue
			}
			out = append(out, platform.Passage{Document: c.doc, Title: c.title, Chunk: c.n, Text: c.text, Score: math.Round(r*10000) / 10000})
		}
	})
	slices.SortStableFunc(out, func(a, b platform.Passage) int {
		if a.Score != b.Score {
			return cmpFloat(b.Score, a.Score)
		}
		return strings.Compare(a.Document+fmt.Sprint(a.Chunk), b.Document+fmt.Sprint(b.Chunk))
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
