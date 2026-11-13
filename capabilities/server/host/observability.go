package host

import (
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

func (t *Tenant) bindAuditStore() {
	t.AuditLog.SetSink(func(entry platform.AuditEntry) {
		if err := t.Log().SaveAuditEntries(t.ID, []platform.AuditEntry{entry}); err != nil {
			log.Printf("audit %s: %v", t.ID, err)
		}
	}, func(read platform.PersonalRead) {
		if err := t.Log().SavePersonalReads(t.ID, []platform.PersonalRead{read}); err != nil {
			log.Printf("personal read audit %s: %v", t.ID, err)
		}
	})
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
	if err := t.ObservationBuffer.Record(t.Log(), event, logging.QuotaBytes()); err != nil {
		log.Printf("observation usage %s: %v", t.ID, err)
	}
}

func (t *Tenant) observeOwnerRun(at time.Time, source, runID, app, resource, status, title, detail string) {
	t.recordObservation(monitoring.OwnerRunObservation(at, source, runID, app, resource, status, title, detail))
}

func (t *Tenant) FlushObservations() {
	if err := t.ObservationBuffer.Flush(t.Log(), Now()); err != nil {
		log.Printf("observation %s: %v", t.ID, err)
	}
}

func (h *Host) observeRequest(t *Tenant, m platform.Member, correlation transport.RequestCorrelation, route string, status int, duration time.Duration) {
	if status < http.StatusBadRequest && duration < time.Second && mathrand.IntN(100) >= t.logging().RequestPercent() {
		return
	}
	t.recordObservation(monitoring.RequestObservation(h.Now(), m.ID, route, correlation.RequestID, correlation.TraceID, correlation.SpanID, status, duration))
}

func (t *Tenant) observeSubmission(r *http.Request, m platform.Member, sub *pb.Submission, record *pb.ChangeRecord, refusal *kernel.Error, at time.Time) {
	correlation := transport.CorrelationOf(r.Context())
	t.recordObservation(monitoring.SubmissionObservation(at, m.ID, correlation.RequestID, correlation.TraceID, correlation.SpanID, sub, record, refusal))
}

func (t *Tenant) PurgeObservations(now time.Time) {
	days := t.logging().RetentionDays()
	if days == 0 {
		return
	}
	t.FlushObservations()
	removed, err := t.ObservationBuffer.Purge(t.Log(), t.ID, now, now.AddDate(0, 0, -days))
	if err != nil {
		log.Printf("purge observations %s: %v", t.ID, err)
		return
	}
	if removed > 0 {
		log.Printf("purged %d observations for %s", removed, t.ID)
	}
}
