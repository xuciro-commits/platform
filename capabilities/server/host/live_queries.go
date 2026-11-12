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
	"platformserver/platform"
	"platformserver/services/resources/authoring"
	"slices"
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

func liveReadAllowed(p string) bool {
	if strings.HasPrefix(p, "/v1/packages/") && (strings.HasSuffix(p, "/preparation") || strings.HasSuffix(p, "/ownership")) {
		return true
	}
	if strings.HasPrefix(p, "/v1/queries/") || strings.HasPrefix(p, "/v1/object-sets/") {
		return true
	}
	if p == "/v1/page-model/read" {
		return true
	}
	if strings.HasPrefix(p, "/v1/applications/") && (strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runtime") || strings.HasSuffix(p, "/diagnostic")) {
		return true
	}
	if strings.HasPrefix(p, "/v1/dispatches/") && strings.HasSuffix(p, "/targets") {
		return true
	}
	if strings.HasPrefix(p, "/v1/capabilities/") || strings.HasPrefix(p, "/v1/releases/candidates/") {
		return true
	}
	if strings.HasPrefix(p, "/v1/records/") || strings.HasPrefix(p, "/v1/chain/") || p == "/v1/branches" || strings.HasPrefix(p, "/v1/branches/") || p == "/v1/resources" || strings.HasPrefix(p, "/v1/resources/") || strings.HasPrefix(p, "/v1/working-state/") {
		return true
	}
	if rest, ok := strings.CutPrefix(p, "/v1/"); ok && rest != "" && !strings.Contains(rest, "/") {
		return !slices.Contains([]string{"changes", "sign-in", "openapi.json"}, rest)
	}
	return slices.Contains([]string{"/v1/authz/explain", "/v1/ai/model-selection", "/v1/ai/knowledge-index", "/v1/ai/vendors", "/v1/releases/active", "/v1/releases/candidates", "/v1/releases/drafts/working", "/v1/build/drafts/actions", "/v1/page-model/rules"}, p)
}
func liveOperationRead(p string) bool {
	return strings.HasPrefix(p, "/v1/applications/") && (strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runtime") || strings.HasSuffix(p, "/diagnostic")) || strings.HasPrefix(p, "/v1/dispatches/") && strings.HasSuffix(p, "/targets") || strings.HasPrefix(p, "/v1/capabilities/calls/compute/") || slices.Contains([]string{"/v1/health", "/v1/work", "/v1/deliveries", "/v1/effects", "/v1/integration-effects", "/v1/endpoints", "/v1/connectors", "/v1/protocols", "/v1/agent-overview", "/v1/ai-usage", "/v1/ai/knowledge-index"}, p)
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
		if err != nil || len(query) > 8<<10 || u.Host != "" || u.Scheme != "" || u.Fragment != "" || path.Clean(u.Path) != u.Path || !liveReadAllowed(u.Path) || (post && ((!strings.HasPrefix(u.Path, "/v1/object-sets/") && u.Path != "/v1/page-model/read") || !json.Valid([]byte(body)))) {
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
	target, body, _ := strings.Cut(query, "\n")
	u, _ := url.Parse(target)
	p := u.Path
	if p == "/v1/page-model/read" {
		return fmt.Sprintf("%d/%d/%d", global, meta, t.Projection.Generation())
	}
	if strings.HasPrefix(p, "/v1/releases/candidates/") {
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["ai"], owners["agent"])
	}
	if p == "/v1/ai/model-selection" {
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["ai"], owners["agent"])
	}
	if strings.HasPrefix(p, "/v1/packages/") && (strings.HasSuffix(p, "/preparation") || strings.HasSuffix(p, "/ownership")) {
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["ai"], owners["agent"])
	}
	if strings.HasPrefix(p, "/v1/queries/") {
		t.Projection.Lock()
		generation := t.Projection.Generation()
		t.Projection.Unlock()
		return fmt.Sprintf("%d/%d/%d", global, meta, generation)
	}
	if strings.HasPrefix(p, "/v1/applications/") && (strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runtime") || strings.HasSuffix(p, "/diagnostic")) {
		parts := strings.Split(p, "/")
		if len(parts) != 6 {
			return fmt.Sprintf("%d/%d", seq, published)
		}
		ref := platform.AssetRef{App: parts[3], Kind: platform.AssetApp, Name: parts[4]}
		_, graphs, err := t.applicationContext().Graphs(ref)
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
		t.Projection.Lock()
		a, b := t.Projection.LiveVersion("flow.instance"), t.Projection.LiveVersion("agent.run")
		t.Projection.Unlock()
		return fmt.Sprintf("%d/%d/%d/%d/%d/%d", global, meta, a, b, operationVersion, published)
	}
	if liveOperationRead(p) || strings.HasPrefix(p, "/v1/chain/") || p == "/v1/business-refusals" || p == "/v1/notifications" || p == "/v1/audit" || p == "/v1/inbox" || p == "/v1/personal-reads" {
		return fmt.Sprintf("%d/%d", seq, published)
	}
	if strings.HasPrefix(p, "/v1/object-sets/") {
		var request ObjectSetRequest
		typ := ""
		var walk func(platform.ObjectSet) bool
		walk = func(set platform.ObjectSet) bool {
			if set.Interface != "" || set.Around != nil || set.Object != "" && typ != "" && set.Object != typ {
				return false
			}
			if set.Object != "" {
				typ = set.Object
			}
			_, operands := set.Operation()
			for _, operand := range operands {
				if !walk(operand) {
					return false
				}
			}
			return true
		}
		t.Projection.Lock()
		defer t.Projection.Unlock()
		version := t.Projection.Generation()
		if json.Unmarshal([]byte(body), &request) == nil && walk(request.Set) && request.Collection == nil {
			if et := t.Projection.Type(typ); et != nil && et.Info().Scope.Through == nil && !et.Info().HasReadSources() {
				version = t.Projection.LiveVersion(typ)
			}
		}
		return fmt.Sprintf("%d/%d/%d", global, meta, version)
	}
	if p == "/v1/branches" || strings.HasPrefix(p, "/v1/branches/") || p == "/v1/resources" || strings.HasPrefix(p, "/v1/resources/") || strings.HasPrefix(p, "/v1/working-state/") {
		t.Projection.Lock()
		defer t.Projection.Unlock()
		if strings.HasSuffix(p, "/changes") {
			return fmt.Sprintf("%d/%d/%d", global, meta, t.Projection.LiveVersion(build.BranchType))
		}
		return fmt.Sprintf("%d/%d/%d", global, meta, t.Projection.Generation())
	}
	if rest, ok := strings.CutPrefix(p, "/v1/records/"); ok {
		typ, tail, _ := strings.Cut(rest, "/")
		t.Projection.Lock()
		defer t.Projection.Unlock()
		version := t.Projection.LiveVersion(typ)
		if et := t.Projection.Type(typ); et != nil && (et.Info().Scope.Through != nil || et.Info().HasReadSources() || tail != "") {
			version = t.Projection.Generation()
		}
		return fmt.Sprintf("%d/%d/%d", global, meta, version)
	}
	if p == "/v1/enterprise" || p == "/v1/enterprise-references" || p == "/v1/enterprise-usage" {
		t.Projection.Lock()
		generation := t.Projection.Generation()
		t.Projection.Unlock()
		return fmt.Sprintf("%d/%d/%d/%d", global, meta, owners["enterprise"], generation)
	}
	owner := "platform"
	if name, ok := strings.CutPrefix(p, "/v1/"); ok && !strings.Contains(name, "/") {
		if app := t.App(t.owner["read:"+name]); app != nil {
			owner = app.Manifest().ID
		}
	}
	switch p {
	case "/v1/releases/active", "/v1/releases/candidates", "/v1/releases/drafts/working", "/v1/build/drafts/actions", "/v1/release-profile", "/v1/capabilities", "/v1/page-model/rules":
		owner = "build"
	case "/v1/ai-limits", "/v1/ai-models", "/v1/ai-providers", "/v1/ai/vendors":
		owner = "ai"
	case "/v1/organization", "/v1/enterprise", "/v1/enterprise-metamodel", "/v1/enterprise-published", "/v1/enterprise-references", "/v1/enterprise-usage",
		"/v1/enterprise-query", "/v1/enterprise-resolve":
		owner = "enterprise"
	case "/v1/flows":
		owner = "flow"
	case "/v1/agents", "/v1/runs", "/v1/memories":
		owner = "agent"
	case "/v1/me", "/v1/actions", "/v1/entities", "/v1/definitions", "/v1/apps", "/v1/protocols":
		return fmt.Sprintf("%d/%d", global, meta)
	}
	return fmt.Sprintf("%d/%d/%d", global, meta, owners[owner])
}
