package host

import "sync"

type changes struct {
	mu          sync.Mutex
	seq         int64
	data        int64
	wake        chan struct{}
	global      int64
	owners      map[string]int64
	opsOwners   map[string]int64
	pendingData map[string]bool
}

func (t *Tenant) changed() {
	t.runIndex.Changed()
	t.markChanged(true, "")
}

func (t *Tenant) operationsChanged(owner ...string) {
	name := ""
	if len(owner) > 0 {
		name = owner[0]
	}
	t.markChanged(false, name)
}

func (t *Tenant) changedOwner(owner string) {
	t.runIndex.Changed()
	t.markChanged(true, owner)
}

func (t *Tenant) markChanged(data bool, owner string) {
	c := &t.change
	c.mu.Lock()
	defer c.mu.Unlock()
	if data && t.readOrigin == nil && t.readImage.Load() != nil && !t.quarantined() {
		if c.pendingData == nil {
			c.pendingData = map[string]bool{}
		}
		c.pendingData[owner] = true
		return
	}
	c.seq++
	if data {
		c.data++
		if owner == "" {
			c.global++
		} else {
			if c.owners == nil {
				c.owners = map[string]int64{}
			}
			c.owners[owner]++
		}
	}
	if !data {
		if c.opsOwners == nil {
			c.opsOwners = map[string]int64{}
		}
		c.opsOwners[owner]++
	}

	if c.wake != nil {
		close(c.wake)
		c.wake = nil
	}
	wakeWork()
}

func (t *Tenant) Changes() (int64, <-chan struct{}) {
	seq, _, wake := t.changeState()
	return seq, wake
}

func (t *Tenant) changeState() (int64, int64, <-chan struct{}) {
	c := &t.change
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wake == nil {
		c.wake = make(chan struct{})
	}
	return c.seq, c.data, c.wake
}
