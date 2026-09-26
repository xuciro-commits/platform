package platformserver

import (
	"fmt"
	"net/http"
	"sync"
	"time"
)

// changes tells whoever follows a tenant that its state moved: every accepted
// input passes through Tenant.record, which counts it and wakes the followers
// (F-32). Clients learn only that something changed, and read again what they
// show, within their own grants.
type changes struct {
	mu   sync.Mutex
	seq  int64
	wake chan struct{}
}

func (t *Tenant) changed() {
	c := &t.change
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	if c.wake != nil {
		close(c.wake)
		c.wake = nil
	}
}

// Changes is how many inputs the tenant has taken since it started serving,
// and a channel closed at the next one.
func (t *Tenant) Changes() (int64, <-chan struct{}) {
	c := &t.change
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wake == nil {
		c.wake = make(chan struct{})
	}
	return c.seq, c.wake
}

// followChanges streams "changed" events (server-sent events) until the client
// goes: one at once, then one per burst of inputs, and a comment every 25
// seconds to keep proxies from closing the stream.
func followChanges(w http.ResponseWriter, r *http.Request, t *Tenant) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	for {
		seq, wake := t.Changes()
		fmt.Fprintf(w, "event: changed\ndata: %d\n\n", seq)
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
