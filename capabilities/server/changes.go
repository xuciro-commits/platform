package platformserver

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// changes wakes tenant followers (F-32). Data changes invalidate authorized
// reads; queue-only progress invalidates operation monitors. Neither event
// carries records, identities or private task details.
type changes struct {
	mu   sync.Mutex
	seq  int64
	data int64
	wake chan struct{}
}

func (t *Tenant) changed() {
	t.markChanged(true)
}

// Queue progress without record, private state or notification changes wakes
// operation monitors without invalidating every business read in the tenant.
func (t *Tenant) operationsChanged() {
	t.markChanged(false)
}

func (t *Tenant) markChanged(data bool) {
	c := &t.change
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	if data {
		c.data++
	}
	if c.wake != nil {
		close(c.wake)
		c.wake = nil
	}
}

// Changes is how many inputs the tenant has taken since it started serving,
// and a channel closed at the next one.
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

// followChanges sends an initial changed event, then changed or operations per
// burst. Comments every 25 seconds keep proxies from closing the stream.
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
				time.Sleep(150 * time.Millisecond) // a burst of inputs is one event
			case <-time.After(25 * time.Second):
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	}
}
