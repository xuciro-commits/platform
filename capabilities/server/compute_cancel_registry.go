package platformserver

import (
	"context"
	"sync"
)

// cancels are the running operations' cancel handles, by effect id: volatile,
// never ownership — cancelling is decided in the ledger (compute_cancel.go),
// this only reaches the goroutine.
type cancels struct {
	mu      sync.Mutex
	running map[string]context.CancelFunc
}

func (c *cancels) add(id string, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.running == nil {
		c.running = map[string]context.CancelFunc{}
	}
	c.running[id] = cancel
}

func (c *cancels) remove(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.running, id)
}

// cancel stops a running operation, if it is one.
func (c *cancels) cancel(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.running[id]; cancel != nil {
		cancel()
		delete(c.running, id)
	}
}
