package kernel

import "sync"

type ChangeFeed struct {
	mu          sync.Mutex
	seq         int64
	data        int64
	wake        chan struct{}
	global      int64
	owners      map[string]int64
	opsOwners   map[string]int64
	pendingData map[string]bool
}

func (c *ChangeFeed) Mark(data bool, owner string, deferred func() bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if data && deferred() {
		if c.pendingData == nil {
			c.pendingData = map[string]bool{}
		}
		c.pendingData[owner] = true
		return false
	}
	c.seq++
	switch {
	case !data:
		if c.opsOwners == nil {
			c.opsOwners = map[string]int64{}
		}
		c.opsOwners[owner]++
	case owner == "":
		c.data++
		c.global++
	default:
		c.data++
		c.bump(owner)
	}
	c.release()
	return true
}

func (c *ChangeFeed) Flush() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := len(c.pendingData) > 0
	for owner := range c.pendingData {
		c.seq++
		c.data++
		if owner == "" {
			c.global++
		} else {
			c.bump(owner)
		}
	}
	c.pendingData = nil
	c.release()
	return pending
}

func (c *ChangeFeed) bump(owner string) {
	if c.owners == nil {
		c.owners = map[string]int64{}
	}
	c.owners[owner]++
}

func (c *ChangeFeed) release() {
	if c.wake != nil {
		close(c.wake)
		c.wake = nil
	}
}

func (c *ChangeFeed) State() (int64, int64, <-chan struct{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wake == nil {
		c.wake = make(chan struct{})
	}
	return c.seq, c.data, c.wake
}

func (c *ChangeFeed) Operations() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.seq + int64(len(c.pendingData))
}

func (c *ChangeFeed) Versions() (global, meta, seq int64, owners, ops map[string]int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	owners, ops = make(map[string]int64, len(c.owners)), make(map[string]int64, len(c.opsOwners))
	for k, v := range c.owners {
		owners[k] = v
	}
	for k, v := range c.opsOwners {
		ops[k] = v
	}
	return c.global, c.owners["platform"] + c.owners["enterprise"] + c.owners["build"], c.seq, owners, ops
}
