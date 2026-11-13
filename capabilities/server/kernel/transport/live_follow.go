package transport

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

type LiveFollow struct {
	Mux       *http.ServeMux
	State     func() (int64, <-chan struct{})
	Version   func(query string) string
	SharedKey func(query, version, language string) (string, bool)
	Snapshot  func(key string, read func() (int, []byte)) (int, []byte)
}

func (f LiveFollow) Serve(w http.ResponseWriter, r *http.Request) {
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
		if err != nil || len(query) > 8<<10 || u.Host != "" || u.Scheme != "" || u.Fragment != "" || path.Clean(u.Path) != u.Path || !LiveReadAllowed(u.Path) || (post && ((!strings.HasPrefix(u.Path, "/v1/object-sets/") && u.Path != "/v1/page-model/read") || !json.Valid([]byte(body)))) {
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
		_, pattern := f.Mux.Handler(request)
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
		seq, wake := f.State()
		frame := LiveQueryFrame{Sequence: seq}
		for _, query := range queries {
			request := requests[query]
			version := f.Version(query)
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
				f.Mux.ServeHTTP(capture, read)
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
			if key, shared := f.SharedKey(query, version, r.Header.Get("Accept-Language")); shared {
				status, body = f.Snapshot(key, func() (int, []byte) {
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
