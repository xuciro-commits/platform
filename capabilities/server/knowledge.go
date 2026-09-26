package platformserver

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"platformserver/apps/files"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	"platformserver/apps/ai"
	"platformserver/apps/knowledge"
	"platformserver/platform"
)

// Knowledge search (ADR-0022), the host's engine over the knowledge app's documents: documents people upload, and fields apps
// declare as knowledge, cut into passages and searched by words and, when an
// embedding model is set, by meaning — only what the reader may read. Vectors
// are derived: embedded as owned work outside the journal and kept by the
// passage's hash, so losing them costs only embedding again. What an agent
// found is journaled with its step (agent_engine.go), so replay never searches.
// Passage is one piece of knowledge a search found, with where it comes from.
type Passage struct {
	Document string  `json:"document"` // knowledge.document/<id>, or <type>/<id>#<field>
	Title    string  `json:"title"`
	Chunk    int     `json:"chunk"`
	Text     string  `json:"text"`
	Score    float64 `json:"score"`
}

type chunk struct {
	doc, title, text, hash string
	n                      int
	apps                   []string
	terms                  map[string]int
	length                 int
}

// Store keeps what is derived from the journal outside it: passages' vectors
// and model calls' transcripts (ADR-0022 D2, D8). The PostgreSQL journal
// implements it; without one the host keeps both in memory.
type Store interface {
	Vectors(tenant, model string, hashes []string) map[string][]float32
	SaveVectors(tenant, model string, vectors map[string][]float32)
	SaveTranscript(x Transcript)
	Transcripts(tenant, run string, limit int) []Transcript
	PurgeTranscripts(tenant string, before time.Time)
}

const (
	passageChars = 3200 // about 800 tokens
	embedBatch   = 32
)

// glossary is the knowledge app as search and agents read it (ADR-0023 D1).
type glossary interface {
	Terms(app string) []knowledge.Term
	TermsFor(declaration string) []string
}

// index is the passages cut from the tenant's documents and knowledge fields.
type index struct {
	mu     sync.Mutex
	docs   map[string]string   // source key → the revision indexed
	chunks map[string][]*chunk // by source key
	texts  map[string]string   // attached text files by content hash
}

// source is one text to index: a document, or an app's knowledge field.
type source struct {
	key, title, text, revision string
	apps                       []string
}

// sources are the tenant's documents and the fields apps declare as knowledge.
func (t *Tenant) sources() []source {
	var out []source
	docs, _, _ := platform.Find[knowledge.Document](t.automation(knowledge.ID, false), platform.Query{Limit: 100000})
	for _, d := range docs {
		out = append(out, source{key: knowledge.DocumentType + "/" + d.ID, title: d.Title, text: d.Text, revision: fmt.Sprint(d.Revision), apps: d.Apps})
	}
	out = append(out, t.fileSources(docs)...)
	t.records.mu.Lock()
	types := slices.Collect(func(yield func(*entityType) bool) {
		for _, et := range t.records.types {
			if slices.ContainsFunc(et.info.Fields, func(f platform.FieldInfo) bool { return f.Knowledge }) && !yield(et) {
				return
			}
		}
	})
	t.records.mu.Unlock()
	for _, et := range types {
		page, err := t.Records(t.host(), et.info.Type, platform.Query{Limit: 100000}, time.Now())
		if err != nil {
			continue
		}
		for _, r := range page.Records {
			v := reflect.ValueOf(r)
			rec := v.FieldByName("Record").Interface().(platform.Record)
			title := rec.ID
			if f, ok := et.info.Field(et.info.Display); ok {
				title = fmt.Sprint(v.FieldByIndex(f.Index).Interface())
			}
			for _, f := range et.info.Fields {
				if text := fmt.Sprint(v.FieldByIndex(f.Index).Interface()); f.Knowledge && strings.TrimSpace(text) != "" {
					out = append(out, source{key: et.info.Type + "/" + rec.ID + "#" + f.Name, title: et.info.Title + " " + title + ": " + f.Title,
						text: text, revision: fmt.Sprint(rec.Revision), apps: []string{et.info.App}})
				}
			}
		}
	}
	return out
}

// sync cuts new and changed sources into passages and forgets removed ones.
func (t *Tenant) sync() {
	k := &t.index
	srcs := t.sources()
	if k.docs == nil {
		k.docs, k.chunks = map[string]string{}, map[string][]*chunk{}
	}
	k.mu.Lock()
	defer k.mu.Unlock()
	seen := map[string]bool{}
	for _, s := range srcs {
		seen[s.key] = true
		if k.docs[s.key] == s.revision {
			continue
		}
		var cs []*chunk
		for i, text := range passages(s.text) {
			sum := sha256.Sum256([]byte(text))
			c := &chunk{doc: s.key, title: s.title, text: text, hash: hex.EncodeToString(sum[:12]), n: i, apps: s.apps, terms: map[string]int{}}
			for _, w := range words(s.title + " " + text) {
				c.terms[w]++
				c.length++
			}
			cs = append(cs, c)
		}
		k.docs[s.key], k.chunks[s.key] = s.revision, cs
	}
	for key := range k.docs {
		if !seen[key] {
			delete(k.docs, key)
			delete(k.chunks, key)
		}
	}
}

// passages cuts a text at its headings, then into pieces of about 800 tokens
// on paragraph boundaries; each piece after the first repeats the paragraph
// before it, and carries its section's heading.
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

// words are lower-cased letters and digits; each CJK character is a word.
func words(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 1 || len(cur) == 1 && unicode.IsDigit(cur[0]) {
			out = append(out, string(cur))
		}
		cur = cur[:0]
	}
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			flush()
			out = append(out, string(r))
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			cur = append(cur, r)
		default:
			flush()
		}
	}
	flush()
	return out
}

// readable says whether reader (or, for none, the agent's app) may read a passage.
func readable(c *chunk, reader *platform.Member, app string) bool {
	if len(c.apps) == 0 {
		return true
	}
	return slices.ContainsFunc(c.apps, func(a string) bool {
		if reader != nil {
			return reader.Roles[a] != ""
		}
		return a == app
	})
}

// Knowledge finds the passages reader may read (for no reader: those of app,
// an agent's app) that answer q: by words (BM25) and, with an embedding model,
// by meaning, the two fused by rank. It calls the embedding model for q, so it
// runs outside the tenant's lock.
func (t *Tenant) Knowledge(reader *platform.Member, app, q string, limit int, now time.Time) []Passage {
	out := []Passage{}
	k := &t.index
	if t.knowledge == nil || strings.TrimSpace(q) == "" {
		return out
	}
	t.sync()
	var query []float32
	model := t.setting(t.automation(knowledge.ID, false), knowledge.SettingEmbeddingModel)
	if model != "" {
		if vs, err := t.embed(model, []string{q}, now); err == nil {
			query = vs[0]
		}
	}
	k.mu.Lock()
	var all []*chunk
	for _, cs := range k.chunks {
		for _, c := range cs {
			if readable(c, reader, app) {
				all = append(all, c)
			}
		}
	}
	k.mu.Unlock()
	if len(all) == 0 {
		return out
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
		slices.SortStableFunc(list, func(a, b *chunk) int { return cmpFloat(scored[b], scored[a]) })
		for i, c := range list {
			ranks[c] += 1 / float64(60+i+1) // reciprocal rank fusion
		}
	}
	// BM25 over the passages the reader may read.
	avg, df := 0.0, map[string]int{}
	for _, c := range all {
		avg += float64(c.length)
		for w := range c.terms {
			df[w]++
		}
	}
	avg /= float64(len(all))
	bm := map[*chunk]float64{}
	for _, w := range slices.Compact(slices.Sorted(slices.Values(words(q)))) {
		if df[w] == 0 {
			continue
		}
		idf := math.Log(1 + (float64(len(all))-float64(df[w])+0.5)/(float64(df[w])+0.5))
		for _, c := range all {
			if f := float64(c.terms[w]); f > 0 {
				bm[c] += idf * f * 2.2 / (f + 1.2*(0.25+0.75*float64(c.length)/avg))
			}
		}
	}
	fuse(bm)
	if query != nil {
		hashes := make([]string, len(all))
		for i, c := range all {
			hashes[i] = c.hash
		}
		vectors := t.vectors(model, hashes)
		cos := map[*chunk]float64{}
		for _, c := range all {
			if v := vectors[c.hash]; v != nil {
				cos[c] = cosine(query, v)
			}
		}
		fuse(cos)
	}
	for c, r := range ranks {
		out = append(out, Passage{Document: c.doc, Title: c.title, Chunk: c.n, Text: c.text, Score: math.Round(r*10000) / 10000})
	}
	slices.SortStableFunc(out, func(a, b Passage) int {
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

// Embed gives passages without a vector theirs, as owned work outside the
// tenant's lock; the host calls it apart from other work.
func (t *Tenant) Embed(now time.Time) {
	k := &t.index
	if t.knowledge == nil || t.ai == nil {
		return
	}
	model := t.setting(t.automation(knowledge.ID, false), knowledge.SettingEmbeddingModel)
	if model == "" {
		return
	}
	t.sync()
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
	have := t.vectors(model, hashes)
	var missing []string
	for _, h := range hashes {
		if have[h] == nil {
			missing = append(missing, h)
		}
	}
	for len(missing) > 0 {
		batch := missing[:min(embedBatch, len(missing))]
		missing = missing[len(batch):]
		var in []string
		for _, h := range batch {
			in = append(in, texts[h])
		}
		vs, err := t.embed(model, in, now)
		if err != nil {
			return // tried again on the next round
		}
		saved := map[string][]float32{}
		for i, h := range batch {
			saved[h] = vs[i]
		}
		t.saveVectors(model, saved)
	}
}

// embed calls an embedding model on the OpenAI wire and meters the call to the knowledge app.
func (t *Tenant) embed(name string, input []string, now time.Time) ([][]float32, error) {
	t.mu.Lock()
	model, pv, err := t.ai.Model(name)
	t.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("the model %s is not enabled", name)
	}
	if pv.Wire == "anthropic" {
		return nil, fmt.Errorf("the provider %s does not embed", pv.ID)
	}
	if !t.breakers.allow("ai:"+pv.ID, now) {
		return nil, fmt.Errorf("the provider %s failed repeatedly; embedding waits", pv.ID)
	}
	body, _ := json.Marshal(map[string]any{"model": model.Model, "input": input})
	started := time.Now()
	status, answer, failure := t.aiRequest(pv, http.MethodPost, "/embeddings", body, aiTimeout)
	t.breakers.report("ai:"+pv.ID, failure == nil && status < 500 && status != http.StatusTooManyRequests, now)
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Usage struct {
			Prompt int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	u := ai.Usage{At: now, Member: "app:" + knowledge.ID, Model: name, Millis: time.Since(started).Milliseconds(), Outcome: "ok"}
	switch {
	case failure != nil:
		u.Outcome = failure.Detail
	case status/100 != 2 || json.Unmarshal(answer, &out) != nil || len(out.Data) != len(input):
		u.Outcome = fmt.Sprintf("HTTP %d: no embeddings", status)
	}
	u.Input = out.Usage.Prompt
	t.meter(platform.Member{ID: u.Member, Tenant: t.ID}, u)
	if u.Outcome != "ok" {
		return nil, fmt.Errorf("%s", u.Outcome)
	}
	vs := make([][]float32, len(input))
	for _, d := range out.Data {
		if d.Index >= 0 && d.Index < len(vs) {
			vs[d.Index] = d.Embedding
		}
	}
	return vs, nil
}

// vectors and saveVectors use the Store, or the tenant's memory without one.
func (t *Tenant) vectors(model string, hashes []string) map[string][]float32 {
	if t.Store != nil {
		return t.Store.Vectors(t.ID, model, hashes)
	}
	t.derivedMu.Lock()
	defer t.derivedMu.Unlock()
	out := map[string][]float32{}
	for _, h := range hashes {
		if v := t.vectorMemory[model+"/"+h]; v != nil {
			out[h] = v
		}
	}
	return out
}

func (t *Tenant) saveVectors(model string, vs map[string][]float32) {
	if t.Store != nil {
		t.Store.SaveVectors(t.ID, model, vs)
		return
	}
	t.derivedMu.Lock()
	defer t.derivedMu.Unlock()
	if t.vectorMemory == nil {
		t.vectorMemory = map[string][]float32{}
	}
	for h, v := range vs {
		t.vectorMemory[model+"/"+h] = v
	}
}

// encodeVector and decodeVector keep a vector as little-endian float32s.
func encodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[4*i:], math.Float32bits(x))
	}
	return out
}

func decodeVector(b []byte) []float32 {
	out := make([]float32, len(b)/4)
	for i := range out {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return out
}

// fileSources are the text files attached to knowledge documents, and to
// records of types whose files are knowledge (ADR-0028 D3), read from the
// store once per content.
func (t *Tenant) fileSources(docs []knowledge.Document) []source {
	if t.app(files.ID) == nil {
		return nil
	}
	attached, _, _ := platform.Find[files.File](t.automation(files.ID, false), platform.Query{Limit: 100000})
	var out []source
	for _, f := range attached {
		if !strings.HasPrefix(f.ContentType, "text/") || f.Size > 1<<20 {
			continue
		}
		typ, id, _ := strings.Cut(f.Target, "/")
		var apps []string
		switch {
		case typ == knowledge.DocumentType:
			i := slices.IndexFunc(docs, func(d knowledge.Document) bool { return d.ID == id })
			if i < 0 {
				continue
			}
			apps = docs[i].Apps
		default:
			t.records.mu.Lock()
			et := t.records.types[typ]
			t.records.mu.Unlock()
			if et == nil || !et.info.KnowledgeFiles {
				continue
			}
			apps = []string{et.info.App}
		}
		text, ok := t.index.texts[f.Hash]
		if !ok {
			body, _, err := t.files().Get(context.Background(), t.ID+"/"+f.Hash)
			if err != nil {
				continue
			}
			raw, _ := io.ReadAll(io.LimitReader(body, 1<<20))
			body.Close()
			text = string(raw)
			if t.index.texts == nil {
				t.index.texts = map[string]string{}
			}
			t.index.texts[f.Hash] = text
		}
		out = append(out, source{key: files.FileType + "/" + f.ID, title: f.Name, text: text, revision: f.Hash, apps: apps})
	}
	return out
}
