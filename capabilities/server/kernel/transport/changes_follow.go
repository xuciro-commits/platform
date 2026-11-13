package transport

import (
	"fmt"
	"net/http"
	"time"
)

func FollowChanges(w http.ResponseWriter, r *http.Request, state func() (int64, int64, <-chan struct{})) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	var data int64 = -1
	for {
		seq, nextData, wake := state()
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
