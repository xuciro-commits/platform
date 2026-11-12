package transport

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"platformserver/platform"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
)

type ViewCache struct {
	mu    sync.Mutex
	gen   uint64
	entry map[string]*viewEntry
	live  map[string]*liveView
}

const maxViewEntries = 8

func Rights(m platform.Member) string {
	raw, _ := json.Marshal(m)
	return strconv.FormatBool(m.AIContext) + string(raw)
}

func (c *ViewCache) settle(gen uint64) bool {
	if c.gen == gen {
		return true
	}
	c.gen, c.entry = gen, nil
	return false
}

func (c *ViewCache) store(gen uint64, key string, value func(*viewEntry)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) || c.entry == nil && gen == 0 {
		return
	}
	if c.entry == nil {
		c.entry = map[string]*viewEntry{}
	}
	if len(c.entry) >= maxViewEntries {
		if _, ok := c.entry[key]; !ok {
			c.entry = map[string]*viewEntry{}
		}
	}
	e := c.entry[key]
	if e == nil {
		e = &viewEntry{}
		c.entry[key] = e
	}
	value(e)
}

func (c *ViewCache) Catalog(gen uint64, key string) ([]platform.Action, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.catalog != nil {
		return e.catalog, true
	}
	return nil, false
}

func (c *ViewCache) StoreCatalog(gen uint64, key string, value []platform.Action) {
	c.store(gen, key, func(e *viewEntry) { e.catalog = value })
}

func (c *ViewCache) Entities(gen uint64, key string) ([]platform.EntityInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.entities != nil {
		return e.entities, true
	}
	return nil, false
}

func (c *ViewCache) StoreEntities(gen uint64, key string, value []platform.EntityInfo) {
	c.store(gen, key, func(e *viewEntry) { e.entities = value })
}

func (c *ViewCache) Definitions(gen uint64, key string) ([]platform.Definition, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.definitions != nil {
		return e.definitions, true
	}
	return nil, false
}

func (c *ViewCache) StoreDefinitions(gen uint64, key string, value []platform.Definition) {
	c.store(gen, key, func(e *viewEntry) { e.definitions = value })
}

func (c *ViewCache) body(gen uint64, key, bodyKey string) (*viewBody, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil, false
	}
	if e := c.entry[key]; e != nil && e.bodies != nil {
		if body, ok := e.bodies[bodyKey]; ok {
			return body, true
		}
	}
	return nil, false
}

func (c *ViewCache) putBody(gen uint64, key, bodyKey string, body *viewBody) {
	c.store(gen, key, func(e *viewEntry) {
		if e.bodies == nil {
			e.bodies = map[string]*viewBody{}
		}
		e.bodies[bodyKey] = body
	})
}

var manifestCache sync.Map

type manifestEntry struct {
	gen      uint64
	manifest platform.Manifest
}

var manifestCacheSize atomic.Int64

func ManifestOf(a platform.App, gen uint64) platform.Manifest {
	if key := reflect.ValueOf(a); !key.IsValid() || !key.Type().Comparable() {
		return a.Manifest()
	}
	if entry, ok := manifestCache.Load(a); ok {
		if cached := entry.(manifestEntry); cached.gen == gen {
			return cached.manifest
		}
	}
	manifest := a.Manifest()
	if manifestCacheSize.Add(1) > 512 {
		manifestCache.Range(func(key, _ any) bool { manifestCache.Delete(key); return true })
		manifestCacheSize.Store(1)
	}
	manifestCache.Store(a, manifestEntry{gen: gen, manifest: manifest})
	return manifest
}

type viewBody struct {
	plain []byte
	etag  string
	once  sync.Once
	gz    []byte
}

type liveView struct {
	ready  chan struct{}
	status int
	body   []byte
}

func (c *ViewCache) LiveSnapshot(key string, build func() (int, []byte)) (int, []byte) {
	c.mu.Lock()
	if saved := c.live[key]; saved != nil {
		c.mu.Unlock()
		<-saved.ready
		return saved.status, saved.body
	}
	if len(c.live) >= 32 {
		c.live = map[string]*liveView{}
	}
	saved := &liveView{ready: make(chan struct{})}
	if c.live == nil {
		c.live = map[string]*liveView{}
	}
	c.live[key] = saved
	c.mu.Unlock()

	saved.status, saved.body = build()
	if len(saved.body) > 2<<20 {
		c.mu.Lock()
		delete(c.live, key)
		c.mu.Unlock()
	}
	close(saved.ready)
	return saved.status, saved.body
}

func (b *viewBody) compressed() []byte {
	b.once.Do(func() {
		var buf bytes.Buffer
		writer, _ := gzip.NewWriterLevel(&buf, 6)
		_, _ = writer.Write(b.plain)
		_ = writer.Close()
		b.gz = buf.Bytes()
	})
	return b.gz
}

type viewEntry struct {
	entities    []platform.EntityInfo
	catalog     []platform.Action
	definitions []platform.Definition
	bodies      map[string]*viewBody
}

func (c *ViewCache) cachedView(gen uint64, key, kind, lang string, source func() any, translate func(any, string) any) *viewBody {
	bodyKey := kind + ":" + lang
	if body, ok := c.body(gen, key, bodyKey); ok {
		return body
	}
	value := source()
	if lang != "" {
		value = translate(value, lang)
	}
	plain, err := json.Marshal(value)
	if err != nil {
		plain = []byte("null")
	}
	sum := sha256.Sum256(plain)
	body := &viewBody{plain: plain, etag: `"` + hex.EncodeToString(sum[:16]) + `"`}
	c.putBody(gen, key, bodyKey, body)
	return body
}

func (c *ViewCache) WriteView(gen uint64, key, kind, lang string, w http.ResponseWriter, r *http.Request, source func() any, translate func(any, string) any) {
	body := c.cachedView(gen, key, kind, lang, source, translate)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	w.Header().Set("ETag", body.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && contentTypeMatches(match, body.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if noCompression, ok := w.(interface{ NoCompression() }); ok {
		noCompression.NoCompression()
	}
	if AcceptsGzip(r) {
		compressed := body.compressed()
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Content-Length", itoa(len(compressed)))
		_, _ = w.Write(compressed)
		return
	}
	w.Header().Set("Content-Length", itoa(len(body.plain)))
	_, _ = w.Write(body.plain)
}

func contentTypeMatches(header, etag string) bool {
	for _, candidate := range bytes.Split([]byte(header), []byte(",")) {
		if bytes.Equal(bytes.TrimSpace(candidate), []byte(etag)) {
			return true
		}
	}
	return false
}
