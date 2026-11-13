package host

import (
	"context"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"platformserver/services/resources/monitoring"
	"slices"
	"time"
)

type TenantHealth struct {
	Status        string                       `json:"status"`
	RecoveryError string                       `json:"recoveryError,omitempty"`
	Started       time.Time                    `json:"started"`
	Built         string                       `json:"built,omitempty"`
	Apps          int                          `json:"apps"`
	Queues        []monitoring.QueueHealth     `json:"queues"`
	Failed        int                          `json:"failed"`
	Breakers      []platform.Breaker           `json:"breakers"`
	OpenBreakers  int                          `json:"openBreakers"`
	Connectors    int                          `json:"connectorsFailing"`
	Endpoints     int                          `json:"endpointsFailing"`
	Observations  monitoring.ObservationHealth `json:"observations"`
	Readiness     []monitoring.HealthCondition `json:"readiness"`
	Lane          LaneHealth                   `json:"lane"`
}

type LaneHealth struct {
	Holder      string                  `json:"holder,omitempty"`
	HeldSeconds float64                 `json:"heldSeconds"`
	Waiters     int64                   `json:"waiters"`
	LastSlow    *runtimekernel.LaneHold `json:"lastSlow,omitempty"`
}

func (t *Tenant) Health(now time.Time) TenantHealth {
	if t.quarantined() {
		return t.health(now)
	}
	health, refusal := recordRead(t, context.Background(), func(t *Tenant) (TenantHealth, *kernel.Error) { return t.health(now), nil })
	if refusal != nil {
		return TenantHealth{Status: "degraded", Readiness: []monitoring.HealthCondition{{Name: "journal", Ready: false, Detail: refusal.Error()}}, Lane: t.laneHealth(now, true)}
	}
	return health
}

func (t *Tenant) health(now time.Time) TenantHealth {
	h := t.healthState(now)
	t.finishHealth(&h, now)
	h.Lane = t.laneHealth(now, false)
	return h
}

func (t *Tenant) laneHealth(now time.Time, outside bool) LaneHealth {
	if source := t.scopeSource(); source != nil {
		return source.laneHealth(now, true)
	}
	state := t.Lane.State()
	lane := LaneHealth{Waiters: state.Waiters, LastSlow: t.LastSlowHold.Load()}
	if outside && !state.Since.IsZero() {
		lane.Holder, lane.HeldSeconds = state.Holder, now.Sub(state.Since).Seconds()
	}
	return lane
}

func (t *Tenant) healthState(now time.Time) TenantHealth {
	operational := t
	if source := t.scopeSource(); source != nil {
		operational = source
	}
	if fault := operational.Fault.Load(); fault != nil {
		return TenantHealth{Status: "quarantined", RecoveryError: fault.Reason, Started: monitoring.Started,
			Built: monitoring.Built(), Apps: len(t.apps), Queues: []monitoring.QueueHealth{}, Breakers: []platform.Breaker{},
			Observations: monitoring.ObservationHealth{Dropped: t.ObservationBuffer.Dropped()},
			Readiness:    monitoring.Quarantined(fault.Reason)}
	}
	h := TenantHealth{Status: "ok", Started: monitoring.Started, Built: monitoring.Built(), Apps: len(t.apps), Queues: []monitoring.QueueHealth{}, Breakers: t.Breakers(now)}
	h.Queues, h.Failed = t.queueHealth(now)
	h.OpenBreakers = monitoring.OpenBreakers(h.Breakers)
	for _, c := range t.Connectors(now) {
		if !slices.Contains([]string{"healthy", "unspecified", "unknown"}, c.Health) && !c.Disabled {
			h.Connectors++
		}
	}
	for _, e := range t.Endpoints() {
		if e.Health != "ok" {
			h.Endpoints++
		}
	}
	if h.Failed > 0 || h.OpenBreakers > 0 || h.Connectors > 0 || h.Endpoints > 0 {
		h.Status = "degraded"
	}
	if t.console.Suspended() {
		h.Status = "suspended"
	}
	counts := operational.ObservationBuffer.Counts()
	h.Observations = monitoring.ObservationHealth{
		Enabled: t.logging().RetentionDays() > 0, Pending: counts.Pending, Dropped: counts.Dropped, Written: counts.Written,
		WriteFailures: counts.WriteFailures, Purged: counts.Purged, QuotaBytes: t.logging().QuotaBytes(),
		LastWrite: counts.LastWrite, LastPurge: counts.LastPurge,
	}
	return h
}

func (t *Tenant) finishHealth(h *TenantHealth, now time.Time) {
	if h.Status == "quarantined" {
		return
	}
	if page, err := t.Log().QueryObservations(platform.ObservationQuery{Tenant: t.ID, To: now, Limit: 1}); err == nil {
		h.Observations.Available = true
		h.Observations.RetainedFrom = page.Coverage.RetainedFrom
	}
	journalReady := t.Log() != nil
	journalDetail := ""
	if t.Store == nil {
		journalDetail = "in-memory test store"
	}
	h.Readiness = monitoring.Readiness(journalReady, journalDetail, h.Observations.Available, !t.businessStopped())
	if !h.Observations.Available {
		h.Status = "degraded"
	}
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
