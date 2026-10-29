package platformserver

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

type changes struct {
	mu        sync.Mutex
	seq       int64
	data      int64
	wake      chan struct{}
	global    int64
	owners    map[string]int64
	opsOwners map[string]int64
}

func (t *Tenant) changed() {
	t.markChanged(true, "")
}

func (t *Tenant) operationsChanged(owner ...string) {
	name := ""
	if len(owner) > 0 {
		name = owner[0]
	}
	t.markChanged(false, name)
}

func (t *Tenant) changedOwner(owner string) { t.markChanged(true, owner) }

func (t *Tenant) markChanged(data bool, owner string) {
	c := &t.change
	c.mu.Lock()
	defer c.mu.Unlock()
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

func followChanges(w http.ResponseWriter, r *http.Request, t *Tenant) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	var data int64 = -1
	for {
		seq, nextData, wake := t.changeState()
		event := "operations"
		if data != nextData {
			event = "changed"
		}
		data = nextData
		fmt.Fprintf(w, "event: %s\ndata: %d\n\n", event, seq)
		flusher.Flush()
		for waiting := true; waiting; {
			select {
			case <-r.Context().Done():
				return
			case <-wake:
				waiting = false
				time.Sleep(150 * time.Millisecond)
			case <-time.After(25 * time.Second):
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	}
}
