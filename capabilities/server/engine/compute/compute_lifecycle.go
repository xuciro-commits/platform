package compute

import (
	"context"
	"sync"
)

// Cancels are the running operations' cancel handles, by effect id: volatile,
// never ownership — cancelling is decided in the ledger (compute_cancel.go),
// this only reaches the goroutine.
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

// cancel stops a running operation, if it is one.
func (c *Cancels) Cancel(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if cancel := c.running[id]; cancel != nil {
		cancel()
		delete(c.running, id)
	}
}
