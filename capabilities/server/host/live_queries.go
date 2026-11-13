package host

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"strings"
	"time"
)

type LiveQueryResult struct {
	Path   string          `json:"path"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}
type LiveQueryFrame struct {
	Sequence int64             `json:"sequence"`
	Results  []LiveQueryResult `json:"results"`
}

const liveReadBudget = 8 << 20

type liveCapture struct {
	header    http.Header
	status    int
	body      bytes.Buffer
	oversized bool
}

func (c *liveCapture) Header() http.Header { return c.header }
func (c *liveCapture) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}
func (c *liveCapture) Write(p []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	if c.body.Len()+len(p) > liveReadBudget {
		c.oversized = true
		return 0, fmt.Errorf("live read exceeds budget")
	}
	return c.body.Write(p)
}

func followLiveQueries(w http.ResponseWriter, r *http.Request, t *Tenant, m platform.Member, mux *http.ServeMux) {
	encoded := r.URL.Query().Get("watch")
	var queries []string
	if len(encoded) > 64<<10 || json.Unmarshal([]byte(encoded), &queries) != nil || len(queries) > 32 || len(queries) == 0 {
		http.Error(w, "invalid live query registration", http.StatusBadRequest)
		return
	}
	requests := make(map[string]*http.Request)
	for _, query := range queries {
		target, body, post := strings.Cut(query, "\n")
		u, err := url.Parse(target)
		if err != nil || len(query) > 8<<10 || u.Host != "" || u.Scheme != "" || u.Fragment != "" || path.Clean(u.Path) != u.Path || !transport.LiveReadAllowed(u.Path) || (post && ((!strings.HasPrefix(u.Path, "/v1/object-sets/") && u.Path != "/v1/page-model/read") || !json.Valid([]byte(body)))) {
			http.Error(w, "unsupported live query", http.StatusBadRequest)
			return
		}
		request := r.Clone(r.Context())
		request.Method = http.MethodGet
		request.URL = u
		request.RequestURI = query
		request.Header.Del("Accept-Encoding")
		request.Header.Del("If-None-Match")
		request.Body = nil
		if post {
			request.Method = http.MethodPost
			request.Body = io.NopCloser(strings.NewReader(body))
			request.Header = request.Header.Clone()
			request.Header.Set("Content-Type", "application/json")
		}
		_, pattern := mux.Handler(request)
		if !strings.HasPrefix(pattern, request.Method+" ") || post && !strings.HasPrefix(pattern, "POST /v1/object-sets/") && pattern != "POST /v1/page-model/read" {
			http.Error(w, "unsupported live query route", http.StatusBadRequest)
			return
		}
		if requests[query] != nil {
			http.Error(w, "duplicate live query", http.StatusBadRequest)
			return
		}
		requests[query] = request
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	digests := make(map[string][32]byte)
	versions := make(map[string]string)
	for {
		seq, _, wake := t.changeState()
		frame := LiveQueryFrame{Sequence: seq}
		for _, query := range queries {
			request := requests[query]
			version := t.liveVersion(query)
			if previous, seen := versions[query]; seen && previous == version {
				continue
			}
			versions[query] = version
			captureRead := func(ctx context.Context) (int, []byte) {
				capture := &liveCapture{header: make(http.Header)}
				read := request.Clone(ctx)
				if _, body, post := strings.Cut(query, "\n"); post {
					read.Body = io.NopCloser(strings.NewReader(body))
				}
				mux.ServeHTTP(capture, read)
				status := capture.status
				if status == 0 {
					status = http.StatusOK
				}
				body := capture.body.Bytes()
				if capture.oversized {
					status = http.StatusRequestEntityTooLarge
					body = []byte(`{"error":"Live query exceeds the result budget."}`)
				}
				return status, body
			}
			var status int
			var body []byte
			if key, shared := build.PublicLiveKey(m, query, version, r.Header.Get("Accept-Language"), t.Entity); shared {
				status, body = t.viewCache().LiveSnapshot(key, func() (int, []byte) {
					return captureRead(context.WithoutCancel(r.Context()))
				})
			} else {
				status, body = captureRead(r.Context())
			}
			if !json.Valid(body) {
				body, _ = json.Marshal(map[string]string{"error": strings.TrimSpace(string(body))})
			}
			result := LiveQueryResult{Path: query, Status: status, Body: body}
			raw, _ := json.Marshal(result)
			digest := sha256.Sum256(raw)
			if previous, seen := digests[query]; !seen || previous != digest {
				frame.Results = append(frame.Results, result)
				digests[query] = digest
			}
		}
		if len(frame.Results) > 0 {
			raw, _ := json.Marshal(frame)
			fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", raw)
			flusher.Flush()
		}
		select {
		case <-r.Context().Done():
			return
		case <-wake:
			time.Sleep(150 * time.Millisecond)
		case <-time.After(25 * time.Second):
			fmt.Fprint(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

func (t *Tenant) liveVersion(query string) string {
	c := &t.change
	c.mu.Lock()
	global, meta, seq := c.global, c.owners["platform"]+c.owners["enterprise"]+c.owners["build"], c.seq
	owners := make(map[string]int64, len(c.owners))
	for k, v := range c.owners {
		owners[k] = v
	}
	ops := make(map[string]int64)
	for k, v := range c.opsOwners {
		ops[k] = v
	}
	c.mu.Unlock()
	if image := t.readImage.Load(); image != nil {
		return image.state.liveReadVersion(query, global, meta, seq, image.operations, owners, ops)
	}
	t.Lane.Lock()
	defer t.Lane.Unlock()
	return t.liveReadVersion(query, global, meta, seq, seq, owners, ops)
}

func (t *Tenant) liveReadVersion(query string, global, meta, seq, published int64, owners, ops map[string]int64) string {
	return transport.LiveVersions{BranchType: build.BranchType,
		Lock:        func() func() { t.Projection.Lock(); return t.Projection.Unlock },
		Generation:  t.Projection.Generation,
		LiveVersion: t.Projection.LiveVersion,
		Scoped: func(typ string) (bool, bool) {
			et := t.Projection.Type(typ)
			return et != nil, et != nil && (et.Info().Scope.Through != nil || et.Info().HasReadSources())
		},
		ComputeOwners: func(ref platform.AssetRef) ([]string, error) {
			_, graphs, err := t.applicationContext().Graphs(ref)
			relevant := []string{}
			for _, graph := range graphs {
				for r := range graph {
					if r.Kind == platform.AssetCompute {
						relevant = append(relevant, r.App)
					}
				}
			}
			return relevant, err
		},
		ReadOwner: func(name string) string {
			if app := t.App(t.owner["read:"+name]); app != nil {
				return app.Manifest().ID
			}
			return ""
		}}.Of(query, global, meta, seq, published, owners, ops)
}
