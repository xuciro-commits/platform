package platformserver

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

// LiveQueryResult is the original authorized GET result, never a fabricated row delta.
type LiveQueryResult struct {
	Path   string          `json:"path"`
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}
type LiveQueryFrame struct {
	Sequence int64             `json:"sequence"`
	Results  []LiveQueryResult `json:"results"`
}

const liveReadBudget = 1 << 20

func liveReadAllowed(p string) bool {
	if strings.HasPrefix(p, "/v1/applications/") && strings.HasSuffix(p, "/runs") {
		return true
	}
	if strings.HasPrefix(p, "/v1/capabilities/") {
		return true
	}
	if strings.HasPrefix(p, "/v1/records/") || strings.HasPrefix(p, "/v1/aggregates/") || strings.HasPrefix(p, "/v1/capabilities/calls/compute/") {
		return true
	}
	return slices.Contains([]string{"/v1/members", "/v1/organization", "/v1/enterprise", "/v1/enterprise-metamodel", "/v1/enterprise-patterns", "/v1/enterprise-published", "/v1/packages", "/v1/settings", "/v1/audit", "/v1/personal-reads", "/v1/inbox", "/v1/requests", "/v1/capabilities", "/v1/release-profile", "/v1/ai-limits", "/v1/ai-models", "/v1/ai-providers", "/v1/ai/vendors", "/v1/me", "/v1/actions", "/v1/entities", "/v1/definitions", "/v1/apps", "/v1/protocols", "/v1/notifications", "/v1/views", "/v1/flows", "/v1/agents", "/v1/runs", "/v1/memories", "/v1/releases/active", "/v1/releases/candidates", "/v1/health", "/v1/work", "/v1/deliveries", "/v1/effects", "/v1/integration-effects", "/v1/endpoints", "/v1/connectors", "/v1/agent-overview", "/v1/ai-usage"}, p) || strings.HasPrefix(p, "/v1/releases/candidates/")
}
func liveOperationRead(p string) bool {
	return strings.HasPrefix(p, "/v1/applications/") && strings.HasSuffix(p, "/runs") || strings.HasPrefix(p, "/v1/capabilities/calls/compute/") || slices.Contains([]string{"/v1/health", "/v1/work", "/v1/deliveries", "/v1/effects", "/v1/integration-effects", "/v1/endpoints", "/v1/connectors", "/v1/protocols", "/v1/agent-overview", "/v1/ai-usage"}, p)
}

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

func followLiveQueries(w http.ResponseWriter, r *http.Request, t *Tenant, mux *http.ServeMux) {
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
		if err != nil || len(query) > 8<<10 || u.Host != "" || u.Scheme != "" || u.Fragment != "" || path.Clean(u.Path) != u.Path || !liveReadAllowed(u.Path) || (post && (!strings.HasSuffix(u.Path, "/query") || !json.Valid([]byte(body)))) {
			http.Error(w, "unsupported live query", http.StatusBadRequest)
			return
		}
		request := r.Clone(r.Context())
		request.Method = http.MethodGet
		request.URL = u
		request.RequestURI = query
		request.Body = nil
		if post {
			request.Method = http.MethodPost
			request.Body = io.NopCloser(strings.NewReader(body))
			request.Header = request.Header.Clone()
			request.Header.Set("Content-Type", "application/json")
		}
		_, pattern := mux.Handler(request)
		if !strings.HasPrefix(pattern, request.Method+" ") || post && pattern != "POST /v1/records/{type}/query" && pattern != "POST /v1/aggregates/{type}/query" {
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
			capture := &liveCapture{header: make(http.Header)}
			// ServeMux dispatches the original route, including authentication,
			// member resolution, field privacy, query limits and version binding.
			read := request.Clone(r.Context())
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

// liveVersion includes query membership and authorization dependencies, not
// only the rows currently returned. Unknown detail/Through dependencies are
// conservatively tied to the store generation; simple lists/aggregates are typed.
func (t *Tenant) liveVersion(query string) string {
	target, body, _ := strings.Cut(query, "\n")
	u, _ := url.Parse(target)
	p := u.Path
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
	if strings.HasPrefix(p, "/v1/applications/") && strings.HasSuffix(p, "/runs") {
		t.mu.Lock()
		defer t.mu.Unlock()
		parts := strings.Split(p, "/")
		if len(parts) != 6 {
			return fmt.Sprint(seq)
		}
		ref := platform.AssetRef{App: parts[3], Kind: platform.AssetApp, Name: parts[4]}
		_, graphs, err := t.applicationRunGraphsLocked(ref)
		if err != nil {
			return fmt.Sprint(global, meta, seq)
		}
		relevant := []string{}
		for _, graph := range graphs {
			for r := range graph {
				if r.Kind == platform.AssetCompute {
					relevant = append(relevant, r.App)
				}
			}
		}
		slices.Sort(relevant)
		relevant = slices.Compact(relevant)
		operationVersion := ops[""]
		for _, owner := range relevant {
			operationVersion += ops[owner]
		}
		t.records.mu.Lock()
		a, b := t.records.liveVersions["flow.instance"], t.records.liveVersions["agent.run"]
		t.records.mu.Unlock()
		return fmt.Sprintf("%d/%d/%d/%d/%d", global, meta, a, b, operationVersion)
	}
	if liveOperationRead(p) || p == "/v1/notifications" || p == "/v1/audit" || p == "/v1/inbox" || p == "/v1/personal-reads" {
		return fmt.Sprint(seq)
	}
	for _, prefix := range []string{"/v1/records/", "/v1/aggregates/"} {
		if rest, ok := strings.CutPrefix(p, prefix); ok {
			typ, tail, _ := strings.Cut(rest, "/")
			t.records.mu.Lock()
			defer t.records.mu.Unlock()
			version := t.records.liveVersions[typ]
			if et := t.records.types[typ]; et != nil && (et.info.Scope.Through != nil || len(et.info.Derived) > 0 || (tail != "" && tail != "query") || strings.Contains(body, `"set"`) || strings.Contains(body, `"traversal"`)) {
				version = t.records.generation
			}
			return fmt.Sprintf("%d/%d/%d", global, meta, version)
		}
	}
	owner := "platform"
	switch p {
	case "/v1/releases/active", "/v1/releases/candidates", "/v1/release-profile", "/v1/capabilities":
		owner = "build"
	case "/v1/ai-limits", "/v1/ai-models", "/v1/ai-providers", "/v1/ai/vendors":
		owner = "ai"
	case "/v1/organization", "/v1/enterprise", "/v1/enterprise-metamodel", "/v1/enterprise-patterns", "/v1/enterprise-published":
		owner = "enterprise"
	case "/v1/flows":
		owner = "flow"
	case "/v1/agents", "/v1/runs", "/v1/memories":
		owner = "agent"
	case "/v1/me", "/v1/actions", "/v1/entities", "/v1/definitions", "/v1/apps", "/v1/protocols", "/v1/views":
		return fmt.Sprintf("%d/%d", global, meta)
	}
	return fmt.Sprintf("%d/%d/%d", global, meta, owners[owner])
}
