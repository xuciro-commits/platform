package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"
)

type changeEvent struct {
	Seq    int64    `json:"seq"`
	Types  []string `json:"types,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}

type ChangeFeedSources struct {
	State   func() (int64, int64, <-chan struct{})
	Types   func() map[string]uint64
	Owners  func() (int64, int64, int64, map[string]int64, map[string]int64)
	Notices func() int64
}

func FollowChanges(w http.ResponseWriter, r *http.Request, src ChangeFeedSources) {
	flusher, ok := flushWriter(w)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	var data int64 = -1
	var seenTypes map[string]uint64
	var seenOwners map[string]int64
	var seenNotices int64 = -1
	var lastPush time.Time
	for {
		seq, nextData, wake := src.State()
		event := "operations"
		payload := changeEvent{Seq: seq}
		if data != nextData {
			event = "changed"
			if src.Types != nil {
				current := src.Types()
				if seenTypes != nil {
					for typ, ver := range current {
						if seenTypes[typ] != ver {
							payload.Types = append(payload.Types, typ)
						}
					}
					slices.Sort(payload.Types)
				}
				seenTypes = current
			}
			if src.Owners != nil {
				_, _, _, owners, _ := src.Owners()
				if seenOwners != nil {
					for owner, ver := range owners {
						if owner != "" && seenOwners[owner] != ver {
							payload.Scopes = append(payload.Scopes, "owner:"+owner)
						}
					}
				}
				seenOwners = owners
			}
			if src.Notices != nil {
				ver := src.Notices()
				if seenNotices >= 0 && ver != seenNotices {
					payload.Scopes = append(payload.Scopes, "notifications")
				}
				seenNotices = ver
			}
			slices.Sort(payload.Scopes)
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
