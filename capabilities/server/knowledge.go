package platformserver

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The knowledge app (ADR-0022): documents people upload, and fields apps
// declare as knowledge, cut into passages and searched by words and, when an
// embedding model is set, by meaning — only what the reader may read. Vectors
// are derived: embedded as owned work outside the journal and kept by the
// passage's hash, so losing them costs only embedding again. What an agent
// found is journaled with its step (agent_engine.go), so replay never searches.
const (
	KnowledgeApp          = "knowledge"
	DocumentType          = "knowledge.document"
	KnowledgeEditor       = "editor"
	SettingEmbeddingModel = "embedding-model"
	passageChars          = 3200 // about 800 tokens
	embedBatch            = 32
)

// Document is a text people upload for agents and members to find.
type Document struct {
	platform.Record
	Title  string   `json:"title" field:"required,search"`
	Text   string   `json:"text" field:"required" type:"longtext"`
	Source string   `json:"source,omitempty" title:"Where it comes from"`
	Apps   []string `json:"apps,omitempty" title:"Read by members of"` // the apps whose members may read it; none: every member
}

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

type Knowledge struct {
	t      *Tenant
	ledger *platform.Ledger
	mu     sync.Mutex
	docs   map[string]string   // source key → the revision indexed
	chunks map[string][]*chunk // by source key
}

func NewKnowledge(tenant string) *Knowledge {
	k := &Knowledge{docs: map[string]string{}, chunks: map[string][]*chunk{}}
	k.ledger = platform.NewLedger(tenant, KnowledgeApp, platform.NewCatalog(platform.EntityActions(knowledgeEntities()[0])...), DocumentType)
	return k
}

func knowledgeEntities() []platform.Entity {
	return []platform.Entity{{Type: DocumentType, Title: "Document", Model: Document{}, Display: "title",
		Standard: platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{KnowledgeEditor}, Capability: "documents"}}}
}

func (k *Knowledge) Manifest() platform.Manifest {
	return platform.Manifest{ID: KnowledgeApp, Title: "Knowledge", Version: "1", Actions: k.ledger.Catalog, Entities: knowledgeEntities(),
		Settings: []platform.Setting{{Name: SettingEmbeddingModel, Title: "Embedding model", Type: "text", Default: "",
			Description: "The enabled model that embeds passages, <provider>/<model>, on the OpenAI wire. Empty: knowledge is searched by words only."}}}
}

func (k *Knowledge) Declarations() []*pb.AuthorityDeclaration { return k.ledger.Declarations() }
func (k *Knowledge) Snapshot() (json.RawMessage, error)       { return k.ledger.Snapshot() }
func (k *Knowledge) Restore(raw json.RawMessage) error        { return k.ledger.Restore(raw) }
func (k *Knowledge) Read(platform.Caller, string) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
}
func (k *Knowledge) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (k *Knowledge) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if record, err, ok := k.ledger.Generated(c, s, now, nil, knowledgeEntities()...); ok {
		return record, err
	}
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

// source is one text to index: a document, or an app's knowledge field.
type source struct {
	key, title, text, revision string
	apps                       []string
}

// sources are the tenant's documents and the fields apps declare as knowledge.
func (k *Knowledge) sources() []source {
	t := k.t
	var out []source
	docs, _, _ := platform.Find[Document](t.automation(KnowledgeApp, false), platform.Query{Limit: 100000})
	for _, d := range docs {
		out = append(out, source{key: DocumentType + "/" + d.ID, title: d.Title, text: d.Text, revision: fmt.Sprint(d.Revision), apps: d.Apps})
	}
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
func (k *Knowledge) sync() {
	srcs := k.sources()
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
	k := t.knowledge
	if k == nil || strings.TrimSpace(q) == "" {
		return out
	}
	k.sync()
	var query []float32
	model := t.setting(t.automation(KnowledgeApp, false), SettingEmbeddingModel)
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
	k := t.knowledge
	if k == nil || t.ai == nil {
		return
	}
	model := t.setting(t.automation(KnowledgeApp, false), SettingEmbeddingModel)
	if model == "" {
		return
	}
	k.sync()
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
	model, pv, err := t.ai.model(name)
	t.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("the model %s is not enabled", name)
	}
	if pv.Wire == "anthropic" {
		return nil, fmt.Errorf("the provider %s does not embed", pv.ID)
	}
	body, _ := json.Marshal(map[string]any{"model": model.Model, "input": input})
	started := time.Now()
	status, answer, failure := t.aiRequest(pv, http.MethodPost, "/embeddings", body, aiTimeout)
	var out struct {
		Data []struct {
			Embedding []float32 `json:"embedding"`
			Index     int       `json:"index"`
		} `json:"data"`
		Usage struct {
			Prompt int `json:"prompt_tokens"`
		} `json:"usage"`
	}
	u := Usage{At: now, Member: "app:" + KnowledgeApp, Model: name, Millis: time.Since(started).Milliseconds(), Outcome: "ok"}
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
