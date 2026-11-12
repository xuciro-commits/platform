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
	runtimekernel "platformserver/kernel"
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

func (t *Tenant) bindAuditStore() {
	t.audit.SetSink(func(entry platform.AuditEntry) {
		if err := t.Log().SaveAuditEntries(t.ID, []platform.AuditEntry{entry}); err != nil {
			log.Printf("audit %s: %v", t.ID, err)
		}
	}, func(read platform.PersonalRead) {
		if err := t.Log().SavePersonalReads(t.ID, []platform.PersonalRead{read}); err != nil {
			log.Printf("personal read audit %s: %v", t.ID, err)
		}
	})
}

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

func (t *Tenant) logging() controlowner.Logging {
	l := controlowner.Logging{Settings: &t.settings, Rank: runtimekernel.ObservationLevelRank}
	if app := t.App(controlowner.PlatformApp); app != nil {
		l.Defaults = app.Manifest().Settings
	}
	return l
}

func (t *Tenant) LogPolicies() []controlowner.LogPolicyView {
	apps := make([]string, 0, len(t.apps))
	for _, app := range t.apps {
		apps = append(apps, app.Manifest().ID)
	}
	return t.logging().Policies(apps)
}

func (t *Tenant) recordObservation(event platform.Observation) {
	logging := t.logging()
	if logging.RetentionDays() == 0 || !logging.Allows(event.Level, event.App, Now()) {
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
	if err := t.observations.Record(t.Log(), event, logging.QuotaBytes()); err != nil {
		log.Printf("observation usage %s: %v", t.ID, err)
	}
}

func (t *Tenant) observeOwnerRun(at time.Time, source, runID, app, resource, status, title, detail string) {
	level, outcome := "info", "ok"
	switch status {
	case "failed":
		level, outcome = "error", "failed"
	case "cancelled":
		level, outcome = "warn", "refused"
	case "pending", "running":
		level, outcome = "info", "pending"
	}
	message := title + " " + status
	attributes := map[string]string{"status": status}
	if detail != "" {
		if len(detail) > 300 {
			detail = detail[:300]
		}
		attributes["error"] = detail
	}
	t.recordObservation(platform.Observation{
		At: at, Level: level, Source: source, App: app, RunID: runID,
		Resource: resource, Outcome: outcome, Message: message, Attributes: attributes,
	})
}

func (t *Tenant) FlushObservations() {
	if err := t.observations.Flush(t.Log(), Now()); err != nil {
		log.Printf("observation %s: %v", t.ID, err)
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
	if status < http.StatusBadRequest && duration < time.Second && mathrand.IntN(100) >= t.logging().RequestPercent() {
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
	days := t.logging().RetentionDays()
	if days == 0 {
		return
	}
	t.FlushObservations()
	removed, err := t.observations.Purge(t.Log(), t.ID, now, now.AddDate(0, 0, -days))
	if err != nil {
		log.Printf("purge observations %s: %v", t.ID, err)
		return
	}
	if removed > 0 {
		log.Printf("purged %d observations for %s", removed, t.ID)
	}
}
