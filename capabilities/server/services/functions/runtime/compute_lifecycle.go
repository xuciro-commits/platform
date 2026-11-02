package compute

import (
	"context"
	"sync"
)

type Cancels struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func (c *Cancels) Add(id string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil {
		c.running = map[string]context.CancelFunc{}
	}
	c.running[id] = cancel
}

func (c *Cancels) Remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.running, id)
}

func (c *Cancels) Cancel(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.running[id]; cancel != nil {
		cancel()
		delete(c.running, id)
	}
}
