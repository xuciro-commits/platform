package host

import (
	"context"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/actions"
	"platformserver/services/resources/monitoring"
	"time"
)

type TenantHealth struct {
	monitoring.Health
	Lane runtimekernel.LaneHealth `json:"lane"`
}

func (t *Tenant) Health(now time.Time) TenantHealth {
	if t.quarantined() {
		return t.health(now)
	}
	health, refusal := recordRead(t, context.Background(), func(t *Tenant) (TenantHealth, *kernel.Error) { return t.health(now), nil })
	if refusal != nil {
		return TenantHealth{Health: monitoring.Health{Status: "degraded", Readiness: []monitoring.HealthCondition{{Name: "journal", Ready: false, Detail: refusal.Error()}}}, Lane: t.laneHealth(now, true)}
	}
	return health
}

func (t *Tenant) health(now time.Time) TenantHealth {
	h := t.healthState(now)
	t.finishHealth(&h, now)
	return TenantHealth{Health: h, Lane: t.laneHealth(now, false)}
}

func (t *Tenant) laneHealth(now time.Time, outside bool) runtimekernel.LaneHealth {
	if source := t.readOrigin; source != nil {
		return source.laneHealth(now, true)
	}
	return t.Sequence.LaneHealth(now, outside)
}

func (t *Tenant) healthState(now time.Time) monitoring.Health {
	operational := t
	if source := t.readOrigin; source != nil {
		operational = source
	}
	return monitoring.HealthSource{
		Apps: len(t.apps),
		Fault: func() (string, bool) {
			fault := operational.Fault.Load()
			if fault == nil {
				return "", false
			}
			return fault.Reason, true
		},
		Dropped:    t.ObservationBuffer.Dropped,
		Breakers:   func() []platform.Breaker { return t.Breakers(now) },
		Queues:     func() ([]monitoring.QueueHealth, int) { return t.queueHealth(now) },
		Connectors: func() int { return t.ConnectorRoster.Failing(now) },
		Endpoints:  func() int { return actions.FailingEndpoints(t.Endpoints()) },
		Suspended:  t.console.Suspended,
		Observations: func() monitoring.ObservationHealth {
			counts := operational.ObservationBuffer.Counts()
			return monitoring.ObservationHealth{
				Enabled: t.logging().RetentionDays() > 0, Pending: counts.Pending, Dropped: counts.Dropped, Written: counts.Written,
				WriteFailures: counts.WriteFailures, Purged: counts.Purged, QuotaBytes: t.logging().QuotaBytes(),
				LastWrite: counts.LastWrite, LastPurge: counts.LastPurge,
			}
		},
	}.Assess()
}

func (t *Tenant) finishHealth(h *monitoring.Health, now time.Time) {
	journalDetail := ""
	if t.Store == nil {
		journalDetail = "in-memory test store"
	}
	h.Complete(func() (time.Time, bool) {
		page, err := t.Log().QueryObservations(platform.ObservationQuery{Tenant: t.ID, To: now, Limit: 1})
		return page.Coverage.RetainedFrom, err == nil
	}, t.Log() != nil, journalDetail, !t.businessStopped())
}

func (t *Tenant) queueHealth(now time.Time) ([]monitoring.QueueHealth, int) {
	t.work.Lock()
	defer t.work.Unlock()
	apps := make([]string, 0, len(t.apps))
	for _, a := range t.apps {
		apps = append(apps, a.Manifest().ID)
	}
	queues := monitoring.Queues(now, apps, func(app string) []time.Time {
		var dues []time.Time
		for _, x := range t.work.Queued(app) {
			dues = append(dues, x.Due)
		}
		return dues
	})
	return queues, t.work.FailedCount()
}

func (t *Tenant) Breakers(now time.Time) []platform.Breaker { return t.breakers.View(now) }
