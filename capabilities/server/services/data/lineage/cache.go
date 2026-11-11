package lineage

import (
	"sync"
	"time"
)

type Snapshot struct {
	Generation uint64
	Raw        Input
	Base       map[string]Meta
	Preview    Graph
	Guard      Graph
	BuiltAt    time.Time
}

type Cache[W any] struct {
	mu       sync.Mutex
	main     *Snapshot
	branches map[string]*Snapshot
	worlds   map[string]W
}

const cacheLimit = 8

func keep[V any](m *map[string]V, key string, value V) {
	if *m == nil {
		*m = map[string]V{}
	}
	if len(*m) >= cacheLimit {
		clear(*m)
	}
	(*m)[key] = value
}

func (c *Cache[W]) Main(generation uint64) (Snapshot, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.main == nil || c.main.Generation != generation {
		return Snapshot{}, false
	}
	return *c.main, true
}

func (c *Cache[W]) KeepMain(s Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.main == nil || c.main.Generation <= s.Generation {
		c.main = &s
	}
}

func (c *Cache[W]) Branch(key string) *Snapshot {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.branches[key]
}

func (c *Cache[W]) KeepBranch(key string, s Snapshot) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keep(&c.branches, key, &s)
}

func (c *Cache[W]) World(key string) (W, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	w, ok := c.worlds[key]
	return w, ok
}

func (c *Cache[W]) KeepWorld(key string, w W) {
	c.mu.Lock()
	defer c.mu.Unlock()
	keep(&c.worlds, key, w)
}
