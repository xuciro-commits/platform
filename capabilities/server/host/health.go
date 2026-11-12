package host

import (
	"context"
	"platformkernel/kernel"
	runtimekernel "platformserver/kernel"
	"platformserver/platform"
	"runtime/debug"
	"slices"
	"time"
)

type TenantHealth struct {
	Status        string             `json:"status"`
	RecoveryError string             `json:"recoveryError,omitempty"`
	Started       time.Time          `json:"started"`
	Built         string             `json:"built,omitempty"`
	Apps          int                `json:"apps"`
	Queues        []QueueHealth      `json:"queues"`
	Failed        int                `json:"failed"`
	Breakers      []platform.Breaker `json:"breakers"`
	OpenBreakers  int                `json:"openBreakers"`
	Connectors    int                `json:"connectorsFailing"`
	Endpoints     int                `json:"endpointsFailing"`
	Observations  ObservationHealth  `json:"observations"`
	Readiness     []HealthCondition  `json:"readiness"`
	Lane          LaneHealth         `json:"lane"`
}

type LaneHealth struct {
	Holder      string                  `json:"holder,omitempty"`
	HeldSeconds float64                 `json:"heldSeconds"`
	Waiters     int64                   `json:"waiters"`
	LastSlow    *runtimekernel.LaneHold `json:"lastSlow,omitempty"`
}

type ObservationHealth struct {
	Enabled       bool      `json:"enabled"`
	Pending       int       `json:"pending"`
	Dropped       int64     `json:"dropped"`
	Written       int64     `json:"written"`
	WriteFailures int64     `json:"writeFailures"`
	Purged        int64     `json:"purged"`
	QuotaBytes    int64     `json:"quotaBytes"`
	RetainedFrom  time.Time `json:"retainedFrom,omitzero"`
	LastWrite     time.Time `json:"lastWrite,omitzero"`
	LastPurge     time.Time `json:"lastPurge,omitzero"`
	Available     bool      `json:"available"`
}

type HealthCondition struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

type QueueHealth struct {
	App    string        `json:"app"`
	Depth  int           `json:"depth"`
	Oldest time.Duration `json:"-"`
	Age    float64       `json:"oldestSeconds"`
}

func (t *Tenant) Health(now time.Time) TenantHealth {
	if t.quarantined() {
		return t.health(now)
	}
	health, refusal := recordRead(t, context.Background(), func(t *Tenant) (TenantHealth, *kernel.Error) { return t.health(now), nil })
	if refusal != nil {
		return TenantHealth{Status: "degraded", Readiness: []HealthCondition{{Name: "journal", Ready: false, Detail: refusal.Error()}}, Lane: t.laneHealth(now, true)}
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
		return TenantHealth{Status: "quarantined", RecoveryError: fault.Reason, Started: Started,
			Built: Built(), Apps: len(t.apps), Queues: []QueueHealth{}, Breakers: []platform.Breaker{},
			Observations: ObservationHealth{Dropped: t.observations.Dropped()},
			Readiness:    []HealthCondition{{Name: "journal", Ready: false, Detail: fault.Reason}, {Name: "observations", Ready: false, Detail: "tenant is quarantined"}, {Name: "work-loop", Ready: false, Detail: "tenant is quarantined"}}}
	}
	h := TenantHealth{Status: "ok", Started: Started, Built: Built(), Apps: len(t.apps), Queues: []QueueHealth{}, Breakers: t.Breakers(now)}
	h.Queues, h.Failed = t.queueHealth(now)
	for _, b := range h.Breakers {
		if b.State != "closed" {
			h.OpenBreakers++
		}
	}
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
	counts := operational.observations.Counts()
	h.Observations = ObservationHealth{
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
	} else {
		h.Observations.Available = false
	}
	journalReady := t.Log() != nil
	journalDetail := ""
	if t.Store == nil {
		journalDetail = "in-memory test store"
	}
	h.Readiness = []HealthCondition{
		{Name: "journal", Ready: journalReady, Detail: journalDetail},
		{Name: "observations", Ready: h.Observations.Available, Detail: map[bool]string{true: "", false: "operational log storage is unavailable"}[h.Observations.Available]},
		{Name: "work-loop", Ready: !t.businessStopped(), Detail: map[bool]string{true: "", false: "business access is stopped"}[!t.businessStopped()]},
	}
	if !h.Observations.Available {
		h.Status = "degraded"
	}
}

func (t *Tenant) queueHealth(now time.Time) ([]QueueHealth, int) {
	queues := []QueueHealth{}
	t.work.Lock()
	defer t.work.Unlock()
	for _, a := range t.apps {
		q := t.work.Queued(a.Manifest().ID)
		if len(q) == 0 {
			continue
		}
		qh := QueueHealth{App: a.Manifest().ID, Depth: len(q)}
		for _, x := range q {
			if age := now.Sub(x.Due); age > qh.Oldest {
				qh.Oldest = age
			}
		}
		qh.Age = qh.Oldest.Seconds()
		queues = append(queues, qh)
	}
	return queues, t.work.FailedCount()
}

var Started = time.Now().UTC().Truncate(time.Second)

func Built() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	revision, modified := "", false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified {
		return revision + "-dirty"
	}
	return revision
}

func (t *Tenant) Breakers(now time.Time) []platform.Breaker { return t.breakers.View(now) }
