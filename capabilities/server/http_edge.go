package platformserver

// The edge every request crosses: security headers, compression, revalidation,
// the counters /metrics reads, and the access log. It wraps the mux, so the
// workspace's files and the API are measured and served the same way.

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// gzipMinBytes is where compression starts paying for itself: below it the
// gzip header costs more than it saves.
const gzipMinBytes = 512

var gzipPool = sync.Pool{New: func() any {
	w, _ := gzip.NewWriterLevel(io.Discard, 5)
	return w
}}

func compressible(contentType string) bool {
	switch {
	case strings.HasPrefix(contentType, "text/"),
		strings.HasPrefix(contentType, "application/json"),
		strings.HasPrefix(contentType, "application/javascript"),
		strings.HasPrefix(contentType, "application/xml"),
		strings.HasPrefix(contentType, "image/svg+xml"):
		return true
	}
	return false
}

// edgeWriter holds a response until it knows whether to compress and revalidate
// it. JSON answers are buffered whole (they are one write), files stream, and
// server-sent events are passed through untouched.
type edgeWriter struct {
	http.ResponseWriter
	request     *http.Request
	acceptGzip  bool
	status      int
	started     bool
	gz          *gzip.Writer
	buffer      []byte // a buffered body that may become a 304 or a compressed answer
	buffering   bool   // the response is JSON and small enough to hold
	noCompress  bool   // the handler encoded the body itself
	writeFailed bool
}

func (w *edgeWriter) WriteHeader(status int) {
	if w.started || w.status != 0 {
		return
	}
	w.status = status
	// JSON answers are held so a matching If-None-Match costs no body at all.
	if w.request.Method == http.MethodGet && status == http.StatusOK && strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.buffering = true
	}
}

func (w *edgeWriter) Write(p []byte) (int, error) {
	if w.buffering {
		if len(w.buffer)+len(p) > 4<<20 { // too large to hold: send what is buffered, then stream
			w.buffering = false
			if err := w.start(len(w.buffer) + len(p)); err != nil {
				return 0, err
			}
			if len(w.buffer) > 0 {
				if _, err := w.body(w.buffer); err != nil {
					return 0, err
				}
				w.buffer = nil
			}
			return w.body(p)
		}
		w.buffer = append(w.buffer, p...)
		return len(p), nil
	}
	if !w.started {
		if err := w.start(len(p)); err != nil {
			return 0, err
		}
	}
	return w.body(p)
}

func (w *edgeWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// gzAllowed reports whether a body of this size would be compressed. It is
// what tells Close whether it still has to say the body's length.
func (w *edgeWriter) gzAllowed(size int) bool {
	return !w.noCompress && w.acceptGzip && size >= gzipMinBytes && compressible(w.Header().Get("Content-Type"))
}

// start sends the headers, choosing compression for this response.
func (w *edgeWriter) start(size int) error {
	if w.started {
		return nil
	}
	w.started = true
	header := w.Header()
	if !w.noCompress && w.acceptGzip && size >= gzipMinBytes && compressible(header.Get("Content-Type")) && header.Get("Content-Encoding") == "" &&
		w.statusCode() >= 200 && w.statusCode() != http.StatusNoContent && w.statusCode() != http.StatusNotModified {
		header.Set("Content-Encoding", "gzip")
		header.Del("Content-Length")
		header.Add("Vary", "Accept-Encoding")
		w.ResponseWriter.WriteHeader(w.statusCode())
		w.gz = gzipPool.Get().(*gzip.Writer)
		w.gz.Reset(w.ResponseWriter)
		return nil
	}
	w.ResponseWriter.WriteHeader(w.statusCode())
	return nil
}

func (w *edgeWriter) body(p []byte) (int, error) {
	if w.gz != nil {
		return w.gz.Write(p)
	}
	return w.ResponseWriter.Write(p)
}

// Close finishes the response: the buffered body becomes a 304 when the client
// already holds it, or a compressed body when it is worth compressing.
func (w *edgeWriter) Close() {
	if w.buffering && !w.started {
		body := w.buffer
		w.buffer = nil
		w.buffering = false
		sum := sha256.Sum256(body)
		etag := `"` + hex.EncodeToString(sum[:16]) + `"`
		w.Header().Set("ETag", etag)
		if match := w.request.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
			w.started = true
			w.Header().Del("Content-Type")
			w.ResponseWriter.WriteHeader(http.StatusNotModified)
			return
		}
		if !w.gzAllowed(len(body)) && w.Header().Get("Content-Length") == "" {
			w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		}
		_ = w.start(len(body))
		_, _ = w.body(body)
	} else if !w.started {
		_ = w.start(-1)
	}
	if w.gz != nil {
		_ = w.gz.Close()
		w.gz.Reset(io.Discard)
		gzipPool.Put(w.gz)
		w.gz = nil
	}
}

func (w *edgeWriter) Flush() {
	if w.buffering {
		w.buffering = false
		_ = w.start(len(w.buffer))
		if len(w.buffer) > 0 {
			_, _ = w.body(w.buffer)
			w.buffer = nil
		}
	} else if !w.started {
		_ = w.start(-1)
	}
	if w.gz != nil {
		_ = w.gz.Flush()
	}
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// NoCompression says the body is already encoded for this request: the edge
// writes it as it is.
func (w *edgeWriter) NoCompression() { w.noCompress = true }

// Unwrap lets http.ResponseController reach the underlying writer.
func (w *edgeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// ---------------------------------------------------------------------------
// Counters

// edgeStats is what /metrics reads: how many requests, how long they took, what
// is in flight. Labels stay bounded: a route's pattern, never its parameters.
type edgeStats struct {
	inflight atomic.Int64
	total    atomic.Int64
	mu       sync.Mutex
	counts   map[string]*atomic.Int64
	latency  map[string]*latencyHist
}

type latencyHist struct {
	mu      sync.Mutex
	buckets [len(latencyBounds) + 1]atomic.Int64
	sumNs   atomic.Int64
	count   atomic.Int64
}

var latencyBounds = [...]float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

func (h *latencyHist) observe(d time.Duration) {
	h.count.Add(1)
	h.sumNs.Add(int64(d))
	seconds := d.Seconds()
	i := len(latencyBounds)
	for j, bound := range latencyBounds {
		if seconds <= bound {
			i = j
			break
		}
	}
	h.buckets[i].Add(1)
}

func newEdgeStats() *edgeStats {
	return &edgeStats{counts: map[string]*atomic.Int64{}, latency: map[string]*latencyHist{}}
}

// patternOf names a route without its parameters: /v1/records/x/query becomes
// /v1/records/*/query. Cardinality stays what the router declares.
func patternOf(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	if len(segments) < 2 {
		return "/" + strings.Join(segments, "/")
	}
	if segments[0] == "v1" {
		for i := 2; i < len(segments); i++ {
			segments[i] = "*"
		}
	} else {
		for i := 1; i < len(segments); i++ {
			segments[i] = "*"
		}
	}
	return "/" + strings.Join(segments, "/")
}

func (s *edgeStats) observe(pattern string, status int, d time.Duration) {
	s.total.Add(1)
	key := pattern
	s.mu.Lock()
	counter, ok := s.counts[key]
	if !ok {
		counter = &atomic.Int64{}
		s.counts[key] = counter
	}
	hist, ok := s.latency[key]
	if !ok {
		hist = &latencyHist{}
		s.latency[key] = hist
	}
	s.mu.Unlock()
	counter.Add(1)
	_ = status
	hist.observe(d)
}

// writePrometheus writes the counters in the exposition format Prometheus
// scrapes. No tenant or member names appear: the labels are route patterns.
func (s *edgeStats) writePrometheus(w io.Writer) {
	var b strings.Builder
	b.WriteString("# HELP platform_requests_total Requests served, by route pattern.\n# TYPE platform_requests_total counter\n")
	s.mu.Lock()
	patterns := make([]string, 0, len(s.counts))
	for pattern := range s.counts {
		patterns = append(patterns, pattern)
	}
	s.mu.Unlock()
	for _, pattern := range patterns {
		s.mu.Lock()
		counter, hist := s.counts[pattern], s.latency[pattern]
		s.mu.Unlock()
		if counter == nil {
			continue
		}
		b.WriteString("platform_requests_total{route=\"" + pattern + "\"} " + strconv.FormatInt(counter.Load(), 10) + "\n")
		if hist == nil {
			continue
		}
		cumulative := int64(0)
		for i, bound := range latencyBounds {
			cumulative += hist.buckets[i].Load()
			b.WriteString("platform_request_duration_seconds_bucket{route=\"" + pattern + "\",le=\"" + strconv.FormatFloat(bound, 'g', -1, 64) + "\"} " + strconv.FormatInt(cumulative, 10) + "\n")
		}
		cumulative += hist.buckets[len(latencyBounds)].Load()
		b.WriteString("platform_request_duration_seconds_bucket{route=\"" + pattern + "\",le=\"+Inf\"} " + strconv.FormatInt(cumulative, 10) + "\n")
		b.WriteString("platform_request_duration_seconds_sum{route=\"" + pattern + "\"} " + strconv.FormatFloat(float64(hist.sumNs.Load())/1e9, 'f', 6, 64) + "\n")
		b.WriteString("platform_request_duration_seconds_count{route=\"" + pattern + "\"} " + strconv.FormatInt(hist.count.Load(), 10) + "\n")
	}
	b.WriteString("# HELP platform_requests_in_flight Requests being served now.\n# TYPE platform_requests_in_flight gauge\nplatform_requests_in_flight " + strconv.FormatInt(s.inflight.Load(), 10) + "\n")
	b.WriteString("# HELP platform_requests " + "All requests served since the process started.\n# TYPE platform_requests counter\nplatform_requests " + strconv.FormatInt(s.total.Load(), 10) + "\n")
	_, _ = io.WriteString(w, b.String())
}

// edge wraps the host's handler.
func (h *Host) edge(next http.Handler) http.Handler {
	if h.stats == nil {
		h.stats = newEdgeStats()
	}
	accessLog := os.Getenv("PLATFORM_ACCESS_LOG") == "1"
	limiter := h.limiter
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		h.stats.inflight.Add(1)
		defer h.stats.inflight.Add(-1)

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept-Language, "+TenantHeader+", "+SchemaVersionHeader+", If-None-Match")
		w.Header().Set("Access-Control-Expose-Headers", "ETag")
		w.Header().Set("Vary", "Accept-Language")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		pattern := patternOf(r.URL.Path)
		if limiter != nil && strings.HasPrefix(r.URL.Path, "/v1/") {
			if retry, ok := limiter.allow(r); !ok {
				w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds()+0.5)))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = io.WriteString(w, `{"error":{"code":"ERROR_CODE_RATE_LIMITED","message":"Too many requests; retry shortly."}}`)
				h.stats.observe(pattern, http.StatusTooManyRequests, time.Since(started))
				return
			}
		}

		edge := &edgeWriter{ResponseWriter: w, request: r, acceptGzip: acceptsGzip(r)}
		next.ServeHTTP(edge, r)
		edge.Close()
		h.stats.observe(pattern, edge.statusCode(), time.Since(started))
		if accessLog {
			log.Printf("%s %s %d %s %s", r.Method, r.URL.Path, edge.statusCode(), time.Since(started).Round(time.Microsecond), remoteOf(r))
		}
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.SplitN(encoding, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

func remoteOf(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

// ---------------------------------------------------------------------------
// Rate limiting

// limiter is a per-credential token bucket. A person never reaches it; a loop
// or a leaked token does, and the host answers 429 instead of spending its
// capacity on one caller.
type limiter struct {
	rate  float64 // tokens per second
	burst float64
	mu    sync.Mutex
	state map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rate, burst float64) *limiter {
	if rate <= 0 || burst <= 0 {
		return nil
	}
	return &limiter{rate: rate, burst: burst, state: map[string]*bucket{}}
}

func (l *limiter) allow(r *http.Request) (time.Duration, bool) {
	key := r.Header.Get("Authorization")
	if key == "" {
		key = remoteOf(r)
	} else {
		sum := sha256.Sum256([]byte(key))
		key = hex.EncodeToString(sum[:8])
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.state[key]
	if b == nil {
		if len(l.state) > 10000 { // a flood of distinct credentials: start over
			l.state = map[string]*bucket{}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.state[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return time.Duration((1 - b.tokens) / l.rate * float64(time.Second)), false
	}
	b.tokens--
	return 0, true
}
