package host

import (
	"context"
	"log"
	mathrand "math/rand/v2"
	"net/http"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/monitoring"
	controlowner "platformserver/services/security/control"
)

type requestCorrelation struct {
	RequestID string
	TraceID   string
	SpanID    string
}

type requestCorrelationKey struct{}

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

func newRequestCorrelation(r *http.Request) requestCorrelation {
	correlation := requestCorrelation{RequestID: transport.RandomHex(16)}
	if traceID, _ := transport.Traceparent(r.Header.Get("Traceparent")); traceID != "" {
		correlation.TraceID, correlation.SpanID = traceID, transport.RandomHex(8)
	} else {
		correlation.TraceID, correlation.SpanID = transport.RandomHex(16), transport.RandomHex(8)
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
		event.ID = transport.RandomHex(16)
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
	t.recordObservation(monitoring.OwnerRunObservation(at, source, runID, app, resource, status, title, detail))
}

func (t *Tenant) FlushObservations() {
	if err := t.observations.Flush(t.Log(), Now()); err != nil {
		log.Printf("observation %s: %v", t.ID, err)
	}
}

func (h *Host) observeRequest(t *Tenant, m platform.Member, correlation requestCorrelation, route string, status int, duration time.Duration) {
	if status < http.StatusBadRequest && duration < time.Second && mathrand.IntN(100) >= t.logging().RequestPercent() {
		return
	}
	t.recordObservation(monitoring.RequestObservation(h.Now(), m.ID, route, correlation.RequestID, correlation.TraceID, correlation.SpanID, status, duration))
}

func (t *Tenant) observeSubmission(r *http.Request, m platform.Member, sub *pb.Submission, record *pb.ChangeRecord, refusal *kernel.Error, at time.Time) {
	correlation := correlationOf(r.Context())
	t.recordObservation(monitoring.SubmissionObservation(at, m.ID, correlation.RequestID, correlation.TraceID, correlation.SpanID, sub, record, refusal))
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
