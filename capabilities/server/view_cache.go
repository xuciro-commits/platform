package platformserver

import (
	"encoding/json"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"platformserver/platform"
)

// A member-derived read depends on two things only: the rights the member
// holds and the definitions the tenant has installed. Both are cheap to
// describe and expensive to apply — the catalog hashes every action's schema,
// the definition list re-describes every asset — so a tenant keeps the answers
// per rights signature and drops all of them when its definitions move.
//
// The values are shared, never mutated: the JSON encoder and the translator
// both read them as they are.
type viewCache struct {
	mu    sync.Mutex
	gen   uint64
	entry map[string]*viewEntry
}

// maxViewEntries bounds what a tenant holds. A tenant has a handful of role
// combinations; a flood of distinct ones is a bug or an attack, and the cache
// then starts over rather than grow without limit.
const maxViewEntries = 8

// rights is the member's answer to "what may I be shown": the same roles,
// grants and AI context produce the same catalog, entities and definitions.
func rights(m platform.Member) string {
	// Authorization implementations may use any caller attribute. Sharing by
	// roles alone leaks scoped metadata across principals and personal tokens.
	raw, _ := json.Marshal(m)
	return strconv.FormatBool(m.AIContext) + string(raw)
}

// definitionsGeneration moves when the tenant's installed definitions do; it
// is the cache's only invalidation key.
func (t *Tenant) definitionsFingerprint() uint64 { return t.defGen.Load() }

// setDefinitions installs a definition set and moves the generation: every
// cached catalog, entity view and encoded answer becomes stale at once.
func (t *Tenant) setDefinitions(definitions []platform.Definition) {
	t.definitions = definitions
	t.defGen.Add(1)
}

// generation resets the cache when the definitions moved and answers whether
// the entry for this generation and rights signature is kept.
func (c *viewCache) settle(gen uint64) bool {
	if c.gen == gen {
		return true
	}
	c.gen, c.entry = gen, nil
	return false
}

func (c *viewCache) current(gen uint64, key string) *viewEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.settle(gen) {
		return nil
	}
	return c.entry[key]
}

// store applies value to the entry, creating it when it is missing. A stale
// generation stores nothing: the caller still returns what it computed.
func (c *viewCache) store(gen uint64, key string, value func(*viewEntry)) {
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

func (t *Tenant) cachedCatalog(m platform.Member, gen uint64, key string) ([]platform.Action, bool) {
	t.views.mu.Lock()
	defer t.views.mu.Unlock()
	if !t.views.settle(gen) {
		return nil, false
	}
	if e := t.views.entry[key]; e != nil && e.catalog != nil {
		return e.catalog, true
	}
	return nil, false
}

func (t *Tenant) storeCatalog(gen uint64, key string, value []platform.Action) {
	t.views.store(gen, key, func(e *viewEntry) { e.catalog = value })
}

func (t *Tenant) cachedEntities(m platform.Member, gen uint64, key string) ([]platform.EntityInfo, bool) {
	t.views.mu.Lock()
	defer t.views.mu.Unlock()
	if !t.views.settle(gen) {
		return nil, false
	}
	if e := t.views.entry[key]; e != nil && e.entities != nil {
		return e.entities, true
	}
	return nil, false
}

func (t *Tenant) storeEntities(gen uint64, key string, value []platform.EntityInfo) {
	t.views.store(gen, key, func(e *viewEntry) { e.entities = value })
}

func (t *Tenant) cachedDefinitions(m platform.Member, gen uint64, key string) ([]platform.Definition, bool) {
	t.views.mu.Lock()
	defer t.views.mu.Unlock()
	if !t.views.settle(gen) {
		return nil, false
	}
	if e := t.views.entry[key]; e != nil && e.definitions != nil {
		return e.definitions, true
	}
	return nil, false
}

func (t *Tenant) storeDefinitions(gen uint64, key string, value []platform.Definition) {
	t.views.store(gen, key, func(e *viewEntry) { e.definitions = value })
}

// body returns the encoded answer kept for a kind and language.
func (c *viewCache) body(gen uint64, key, bodyKey string) (*viewBody, bool) {
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

func (c *viewCache) putBody(gen uint64, key, bodyKey string, body *viewBody) {
	c.store(gen, key, func(e *viewEntry) {
		if e.bodies == nil {
			e.bodies = map[string]*viewBody{}
		}
		e.bodies[bodyKey] = body
	})
}

// manifestCache memoizes an app's manifest per definition generation. Building
// one assembles every declared entity, action, protocol and page; the answer
// changes only when the tenant installs something, which moves the generation.
// The key is the app itself, so a draft's fork (a different value) builds its
// own manifest instead of inheriting a stale one.
var manifestCache sync.Map // platform.App -> manifestEntry

type manifestEntry struct {
	gen      uint64
	manifest platform.Manifest
}

var manifestCacheSize atomic.Int64

func (t *Tenant) manifestOf(a platform.App, gen uint64) platform.Manifest {
	// The cache is keyed by the app value itself; an app whose value type is
	// uncomparable (a struct carrying manifest slices or maps) can never be a
	// map key, so it skips memoization instead of panicking.
	if key := reflect.ValueOf(a); !key.IsValid() || !key.Type().Comparable() {
		return a.Manifest()
	}
	if entry, ok := manifestCache.Load(a); ok {
		if cached := entry.(manifestEntry); cached.gen == gen {
			return cached.manifest
		}
	}
	manifest := a.Manifest()
	if manifestCacheSize.Add(1) > 512 { // forks and reinstalls; start over rather than grow
		manifestCache.Range(func(key, _ any) bool { manifestCache.Delete(key); return true })
		manifestCacheSize.Store(1)
	}
	manifestCache.Store(a, manifestEntry{gen: gen, manifest: manifest})
	return manifest
}
