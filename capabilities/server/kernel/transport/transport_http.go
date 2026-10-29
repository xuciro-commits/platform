package transport

import (
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

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

type edgeWriter struct {
	http.ResponseWriter
	request     *http.Request
	acceptGzip  bool
	status      int
	started     bool
	gz          *gzip.Writer
	buffer      []byte
	buffering   bool
	noCompress  bool
	writeFailed bool
}

func (w *edgeWriter) WriteHeader(status int) {
	if w.started || w.status != 0 {
		return
	}
	w.status = status
	if w.request.Method == http.MethodGet && status == http.StatusOK && strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
		w.buffering = true
	}
}

func (w *edgeWriter) Write(p []byte) (int, error) {
	if w.buffering {
		if len(w.buffer)+len(p) > 4<<20 {
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

func (w *edgeWriter) gzAllowed(size int) bool {
	return !w.noCompress && w.acceptGzip && size >= gzipMinBytes && compressible(w.Header().Get("Content-Type"))
}

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

func (w *edgeWriter) NoCompression() { w.noCompress = true }

func (w *edgeWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

type Stats struct {
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

func NewStats() *Stats {
	return &Stats{counts: map[string]*atomic.Int64{}, latency: map[string]*latencyHist{}}
}

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

func (s *Stats) observe(pattern string, status int, d time.Duration) {
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

func (s *Stats) WritePrometheus(w io.Writer) {
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

func Edge(next http.Handler, stats *Stats, limiter *Limiter, tenantHeader, schemaVersionHeader string) http.Handler {
	accessLog := os.Getenv("PLATFORM_ACCESS_LOG") == "1"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		stats.inflight.Add(1)
		defer stats.inflight.Add(-1)

		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Accept-Language, "+tenantHeader+", "+schemaVersionHeader+", If-None-Match")
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
				stats.observe(pattern, http.StatusTooManyRequests, time.Since(started))
				return
			}
		}

		edge := &edgeWriter{ResponseWriter: w, request: r, acceptGzip: AcceptsGzip(r)}
		next.ServeHTTP(edge, r)
		edge.Close()
		stats.observe(pattern, edge.statusCode(), time.Since(started))
		if accessLog {
			log.Printf("%s %s %d %s %s", r.Method, r.URL.Path, edge.statusCode(), time.Since(started).Round(time.Microsecond), RemoteOf(r))
		}
	})
}

func AcceptsGzip(r *http.Request) bool {
	for _, encoding := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.SplitN(encoding, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

func RemoteOf(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

type Limiter struct {
	rate  float64
	burst float64
	mu    sync.Mutex
	state map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func NewLimiter(rate, burst float64) *Limiter {
	if rate <= 0 || burst <= 0 {
		return nil
	}
	return &Limiter{rate: rate, burst: burst, state: map[string]*bucket{}}
}

func (l *Limiter) allow(r *http.Request) (time.Duration, bool) {
	key := r.Header.Get("Authorization")
	if key == "" {
		key = RemoteOf(r)
	} else {
		sum := sha256.Sum256([]byte(key))
		key = hex.EncodeToString(sum[:8])
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.state[key]
	if b == nil {
		if len(l.state) > 10000 {
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

type webAsset struct {
	body      []byte
	etag      string
	mediaType string
	immutable bool
}

type WebFiles struct {
	files map[string]webAsset
	index webAsset
	total int64
}

const maxWebBytes = 64 << 20

var mediaTypes = map[string]string{
	".html":  "text/html; charset=utf-8",
	".js":    "text/javascript; charset=utf-8",
	".mjs":   "text/javascript; charset=utf-8",
	".css":   "text/css; charset=utf-8",
	".json":  "application/json",
	".svg":   "image/svg+xml",
	".png":   "image/png",
	".jpg":   "image/jpeg",
	".jpeg":  "image/jpeg",
	".webp":  "image/webp",
	".ico":   "image/x-icon",
	".woff2": "font/woff2",
	".woff":  "font/woff",
	".wasm":  "application/wasm",
	".txt":   "text/plain; charset=utf-8",
	".map":   "application/json",
}

func hashedName(name string) bool {
	base := strings.TrimSuffix(name, path.Ext(name))
	_, hash, ok := strings.Cut(base, "-")
	return ok && len(hash) >= 8 && !strings.Contains(hash, ".")
}

func LoadWeb(dir string) *WebFiles {
	files := &WebFiles{files: map[string]webAsset{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return files
	}
	for _, entry := range entries {
		if entry.IsDir() {
			sub := filepath.Join(dir, entry.Name())
			assets, err := os.ReadDir(sub)
			if err != nil {
				continue
			}
			for _, asset := range assets {
				if asset.IsDir() {
					continue
				}
				body, err := os.ReadFile(filepath.Join(sub, asset.Name()))
				if err != nil {
					continue
				}
				key := "/" + entry.Name() + "/" + asset.Name()
				files.add(key, asset.Name(), body)
			}
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		files.add("/"+entry.Name(), entry.Name(), body)
	}
	if index, ok := files.files["/index.html"]; ok {
		files.index = index
	} else {
		files.index = webAsset{body: []byte("<!doctype html><title>Platform</title><p>The workspace is not built."), mediaType: "text/html; charset=utf-8"}
		files.index.etag = `"index"`
	}
	return files
}

func (f *WebFiles) add(key, name string, body []byte) {
	if f.total+int64(len(body)) > maxWebBytes {
		return
	}
	sum := sha256.Sum256(body)
	mediaType := mediaTypes[path.Ext(name)]
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	f.files[key] = webAsset{body: body, etag: `"` + hex.EncodeToString(sum[:16]) + `"`, mediaType: mediaType,
		immutable: hashedName(name) && strings.HasPrefix(key, "/assets/")}
	f.total += int64(len(body))
}

func (f *WebFiles) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	asset, ok := f.files[path.Clean("/"+r.URL.Path)]
	if !ok || r.URL.Path == "/" {
		asset = f.index
	}
	if asset.immutable {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	w.Header().Set("ETag", asset.etag)
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, asset.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", asset.mediaType)
	w.Header().Set("Content-Length", strconv.Itoa(len(asset.body)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(asset.body)
}

func ProfileRoutes(mux *http.ServeMux) {
	if os.Getenv("PLATFORM_PPROF") != "1" {
		return
	}
	loopback := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if host := RemoteOf(r); host != "127.0.0.1" && host != "::1" {
				http.Error(w, "profiles are for the host's operator", http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /debug/pprof/", loopback(pprof.Index))
	mux.HandleFunc("GET /debug/pprof/cmdline", loopback(pprof.Cmdline))
	mux.HandleFunc("GET /debug/pprof/profile", loopback(pprof.Profile))
	mux.HandleFunc("GET /debug/pprof/symbol", loopback(pprof.Symbol))
	mux.HandleFunc("GET /debug/pprof/trace", loopback(pprof.Trace))
}
