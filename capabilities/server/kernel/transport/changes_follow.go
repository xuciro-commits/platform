package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

type changeEvent struct {
	Seq   int64    `json:"seq"`
	Types []string `json:"types,omitempty"`
}

func FollowChanges(w http.ResponseWriter, r *http.Request, state func() (int64, int64, <-chan struct{}), versions ...func() map[string]uint64) {
	flusher, ok := flushWriter(w)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	var data int64 = -1
	var seen map[string]uint64
	var lastPush time.Time
	readVersions := func() map[string]uint64 {
		if len(versions) == 0 || versions[0] == nil {
			return nil
		}
		return versions[0]()
	}
	for {
		seq, nextData, wake := state()
		event := "operations"
		payload := changeEvent{Seq: seq}
		if data != nextData {
			event = "changed"
			if current := readVersions(); current != nil {
				if seen != nil {
					for typ, ver := range current {
						if seen[typ] != ver {
							payload.Types = append(payload.Types, typ)
						}
					}
					slices.Sort(payload.Types)
				}
				seen = current
			}
		}
		data = nextData
		raw, _ := json.Marshal(payload)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, raw)
		flusher.Flush()
		lastPush = time.Now()
		for waiting := true; waiting; {
			select {
			case <-r.Context().Done():
				return
			case <-wake:
				waiting = false
				if since := time.Since(lastPush); since < 150*time.Millisecond {
					time.Sleep(150*time.Millisecond - since)
				}
			case <-time.After(25 * time.Second):
				fmt.Fprint(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	}
}
