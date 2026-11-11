package kernel

import (
	"sync"
	"sync/atomic"
	"time"
)

const runIndexFresh = 5 * time.Second

type RunIndexCache[C any] struct {
	mu         sync.Mutex
	built      map[string]runIndexBuild[C]
	generation atomic.Int64
}

type runIndexBuild[C any] struct {
	at         time.Time
	generation int64
	value      C
}

func (c *RunIndexCache[C]) Changed() { c.generation.Add(1) }

func (c *RunIndexCache[C]) CopyGeneration(from *RunIndexCache[C]) {
	c.generation.Store(from.generation.Load())
}

func (c *RunIndexCache[C]) Refresh(key string, now time.Time, rebuild func() (C, bool)) C {
	c.mu.Lock()
	defer c.mu.Unlock()
	generation := c.generation.Load()
	if cached, ok := c.built[key]; ok && cached.generation == generation && now.Sub(cached.at) < runIndexFresh {
		return cached.value
	}
	value, ok := rebuild()
	if !ok {
		return value
	}
	if c.built == nil {
		c.built = map[string]runIndexBuild[C]{}
	}
	c.built[key] = runIndexBuild[C]{at: now, generation: generation, value: value}
	return value
}
