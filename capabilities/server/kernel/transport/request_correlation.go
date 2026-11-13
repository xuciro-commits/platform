package transport

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
)

var fallbackCorrelationID atomic.Uint64

func RandomHex(size int) string {
	raw := make([]byte, size)
	if _, err := rand.Read(raw); err == nil {
		return hex.EncodeToString(raw)
	}
	var fallback [16]byte
	binary.BigEndian.PutUint64(fallback[:8], uint64(time.Now().UnixNano()))
	binary.BigEndian.PutUint64(fallback[8:], fallbackCorrelationID.Add(1))
	copy(raw, fallback[:])
	return hex.EncodeToString(raw)
}

func Traceparent(value string) (string, string) {
	parts := strings.Split(value, "-")
	if len(parts) < 4 || len(parts[0]) != 2 || len(parts[3]) != 2 || parts[0] == "ff" || parts[0] == "00" && len(parts) != 4 {
		return "", ""
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 {
		return "", ""
	}
	for _, part := range []string{parts[0], parts[3], parts[1], parts[2]} {
		if _, err := hex.DecodeString(part); err != nil {
			return "", ""
		}
	}
	if strings.Trim(parts[1], "0") == "" || strings.Trim(parts[2], "0") == "" {
		return "", ""
	}
	return strings.ToLower(parts[1]), strings.ToLower(parts[2])
}

type RequestCorrelation struct {
	RequestID string
	TraceID   string
	SpanID    string
}

type requestCorrelationKey struct{}

func NewRequestCorrelation(r *http.Request) RequestCorrelation {
	correlation := RequestCorrelation{RequestID: RandomHex(16)}
	if traceID, _ := Traceparent(r.Header.Get("Traceparent")); traceID != "" {
		correlation.TraceID, correlation.SpanID = traceID, RandomHex(8)
	} else {
		correlation.TraceID, correlation.SpanID = RandomHex(16), RandomHex(8)
	}
	return correlation
}

func CorrelationOf(ctx context.Context) RequestCorrelation {
	correlation, _ := ctx.Value(requestCorrelationKey{}).(RequestCorrelation)
	return correlation
}

func WithRequestCorrelation(r *http.Request, correlation RequestCorrelation) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestCorrelationKey{}, correlation))
}

type StatusWriter struct {
	http.ResponseWriter
	status int
}

func (w *StatusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *StatusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *StatusWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *StatusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *StatusWriter) StatusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}
