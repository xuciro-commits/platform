package host

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log"
	mathrand "math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	controlowner "platformserver/services/security/control"
)

type requestCorrelation struct {
	RequestID string
	TraceID   string
	SpanID    string
}

type requestCorrelationKey struct{}

var fallbackCorrelationID atomic.Uint64

const observationPendingLimit = 2000

func randomHex(size int) string {
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

func traceparent(value string) (string, string) {
	parts := strings.Split(value, "-")
	if len(parts) < 4 || len(parts[0]) != 2 || len(parts[3]) != 2 || parts[0] == "ff" || parts[0] == "00" && len(parts) != 4 {
		return "", ""
	}
	if _, err := hex.DecodeString(parts[0]); err != nil {
		return "", ""
	}
	if _, err := hex.DecodeString(parts[3]); err != nil {
		return "", ""
	}
	if len(parts[1]) != 32 || len(parts[2]) != 16 {
		return "", ""
	}
	if _, err := hex.DecodeString(parts[1]); err != nil {
		return "", ""
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", ""
	}
	if strings.Trim(parts[1], "0") == "" || strings.Trim(parts[2], "0") == "" {
		return "", ""
	}
	return strings.ToLower(parts[1]), strings.ToLower(parts[2])
}

func newRequestCorrelation(r *http.Request) requestCorrelation {
	correlation := requestCorrelation{RequestID: randomHex(16)}
	if traceID, _ := traceparent(r.Header.Get("Traceparent")); traceID != "" {
		correlation.TraceID, correlation.SpanID = traceID, randomHex(8)
	} else {
		correlation.TraceID, correlation.SpanID = randomHex(16), randomHex(8)
	}
	return correlation
}

func correlationOf(ctx context.Context) requestCorrelation {
	correlation, _ := ctx.Value(requestCorrelationKey{}).(requestCorrelation)
	return correlation
}

func withRequestCorrelation(r *http.Request, correlation requestCorrelation) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), requestCorrelationKey{}, correlation))
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	if w.status != 0 {
		return
	}
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Flush() {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

func (w *statusWriter) statusCode() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

func (t *Tenant) platformSetting(name string) string {
	if value, ok := t.settings.Get(controlowner.PlatformApp + "/" + name); ok {
		return value
	}
	app := t.app(controlowner.PlatformApp)
	if app == nil {
		return ""
	}
	for _, setting := range app.Manifest().Settings {
		if setting.Name == name {
			return setting.Default
		}
	}
	return ""
}

func (t *Tenant) logLevel() string {
	level := t.platformSetting(controlowner.SettingLogLevel)
	if levelRank(level) < 0 {
		return "info"
	}
	return level
}

func levelRank(level string) int {
	switch level {
	case "debug":
		return 0
	case "info":
		return 1
	case "warn":
		return 2
	case "error":
		return 3
	}
	return -1
}

func (t *Tenant) logLevelAllows(level string) bool {
	rank := levelRank(level)
	if rank < 0 {
		rank = levelRank("info")
	}
	return rank >= levelRank(t.logLevel())
}

func (t *Tenant) logRequestPercent() int {
	value, err := strconv.Atoi(t.platformSetting(controlowner.SettingLogRequests))
	if err != nil {
		return 10
	}
	return max(0, min(value, 100))
}

func (t *Tenant) logRetentionDays() int {
	value, err := strconv.Atoi(t.platformSetting(controlowner.SettingLogRetention))
	if err != nil {
		return 30
	}
	return max(0, min(value, 3650))
}

func (t *Tenant) recordObservation(event platform.Observation) {
	if t.logRetentionDays() == 0 || !t.logLevelAllows(event.Level) {
		return
	}
	if event.ID == "" {
		event.ID = randomHex(16)
	}
	if event.At.IsZero() {
		event.At = Now()
	}
	event.At = event.At.UTC()
	event.Tenant = t.ID
	t.observationMu.Lock()
	defer t.observationMu.Unlock()
	if len(t.observationPending) >= observationPendingLimit {
		drop := -1
		for index, pending := range t.observationPending {
			if pending.Level == "debug" || pending.Level == "info" {
				drop = index
				break
			}
		}
		if drop < 0 && levelRank(event.Level) < levelRank("error") {
			t.observationDropped.Add(1)
			return
		}
		if drop >= 0 {
			t.observationPending = append(t.observationPending[:drop], t.observationPending[drop+1:]...)
		} else {
			t.observationPending = t.observationPending[1:]
		}
		t.observationDropped.Add(1)
	}
	t.observationPending = append(t.observationPending, event)
}

func (t *Tenant) FlushObservations() {
	t.observationMu.Lock()
	events := t.observationPending
	t.observationPending = nil
	t.observationMu.Unlock()
	if len(events) == 0 {
		return
	}
	if err := t.store().SaveObservations(events); err != nil {
		log.Printf("observation %s: %v", t.ID, err)
		t.observationMu.Lock()
		lost := len(events) + len(t.observationPending) - observationPendingLimit
		if lost > 0 {
			t.observationDropped.Add(int64(lost))
		}
		kept := append(events, t.observationPending...)
		if len(kept) > observationPendingLimit {
			kept = kept[len(kept)-observationPendingLimit:]
		}
		t.observationPending = kept
		t.observationMu.Unlock()
	}
}

func (h *Host) observeRequest(t *Tenant, m platform.Member, correlation requestCorrelation, route string, status int, duration time.Duration) {
	level, outcome := "info", "ok"
	switch {
	case status >= http.StatusInternalServerError:
		level, outcome = "error", "failed"
	case status >= http.StatusBadRequest:
		level, outcome = "warn", "refused"
	}
	if status < http.StatusBadRequest && duration < time.Second && mathrand.IntN(100) >= t.logRequestPercent() {
		return
	}
	t.recordObservation(platform.Observation{
		At:        h.Now(),
		Level:     level,
		Source:    "request",
		Member:    m.ID,
		Route:     route,
		RequestID: correlation.RequestID,
		TraceID:   correlation.TraceID,
		SpanID:    correlation.SpanID,
		Outcome:   outcome,
		Message:   fmt.Sprintf("Request completed with status %d", status),
		Attributes: map[string]string{
			"status":     strconv.Itoa(status),
			"durationMs": strconv.FormatInt(duration.Milliseconds(), 10),
		},
	})
}

func (t *Tenant) observeSubmission(r *http.Request, m platform.Member, sub *pb.Submission, record *pb.ChangeRecord, refusal *kernel.Error, at time.Time) {
	level, outcome := "info", "ok"
	if refusal != nil {
		level, outcome = "warn", "refused"
	}
	correlation := correlationOf(r.Context())
	attributes := map[string]string{}
	if schema := sub.GetSchema().GetName(); schema != "" {
		attributes["schema"] = schema
	}
	if refusal != nil {
		attributes["errorCode"] = refusal.Error()
	}
	event := platform.Observation{
		At:         at,
		Level:      level,
		Source:     "submit",
		Member:     m.ID,
		App:        sub.GetAuthority(),
		Route:      "POST /v1/submissions",
		RequestID:  correlation.RequestID,
		TraceID:    correlation.TraceID,
		SpanID:     correlation.SpanID,
		Outcome:    outcome,
		Message:    "Submission accepted",
		Attributes: attributes,
	}
	if refusal != nil {
		event.Message = "Submission refused"
	}
	if record != nil {
		event.ChangeID = record.GetChangeId()
	}
	t.recordObservation(event)
}

func (t *Tenant) PurgeObservations(now time.Time) {
	days := t.logRetentionDays()
	if days == 0 {
		return
	}
	t.FlushObservations()
	t.observationPurgeMu.Lock()
	if !t.lastObservationPurge.IsZero() && now.Sub(t.lastObservationPurge) < time.Hour {
		t.observationPurgeMu.Unlock()
		return
	}
	t.lastObservationPurge = now
	t.observationPurgeMu.Unlock()
	removed, err := t.store().PurgeObservations(t.ID, now.AddDate(0, 0, -days))
	if err != nil {
		log.Printf("purge observations %s: %v", t.ID, err)
		return
	}
	if removed > 0 {
		log.Printf("purged %d observations for %s", removed, t.ID)
	}
}
