package platformserver

import (
	"runtime/debug"
	"slices"
	"time"
)

// TenantHealth is how a tenant's work stands (ADR-0027 D6), for its
// administrators and for metrics: its queues, what gave up, what waits past a
// quota, its breakers, and its connectors and endpoints.
type TenantHealth struct {
	Status        string `json:"status"`                  // ok, or degraded when something waits for a person or a destination fails
	RecoveryError string `json:"recoveryError,omitempty"` // diagnostic for an isolated tenant; no work runs
	// Started is when this process began, and Built the code it was built from:
	// what answers "is this host running the code I just changed?" — the
	// question that cost three walks before it was on screen.
	Started      time.Time     `json:"started"`
	Built        string        `json:"built,omitempty"`
	Apps         int           `json:"apps"`
	Queues       []QueueHealth `json:"queues"`
	Failed       int           `json:"failed"`   // owned work that gave up
	Deferred     []string      `json:"deferred"` // apps past their quota now
	Breakers     []Breaker     `json:"breakers"`
	OpenBreakers int           `json:"openBreakers"`
	Connectors   int           `json:"connectorsFailing"` // connectors neither healthy nor unknown
	Endpoints    int           `json:"endpointsFailing"`
}

// QueueHealth is one app's waiting deliveries.
type QueueHealth struct {
	App    string        `json:"app"`
	Depth  int           `json:"depth"`
	Oldest time.Duration `json:"-"`
	Age    float64       `json:"oldestSeconds"` // how long the oldest has waited since it was due
}

// Health is the tenant's health at now.
func (t *Tenant) Health(now time.Time) TenantHealth {
	if fault := t.fault.Load(); fault != nil {
		return TenantHealth{Status: "quarantined", RecoveryError: fault.Reason, Started: Started,
			Built: Built(), Apps: len(t.apps), Queues: []QueueHealth{}, Deferred: []string{}, Breakers: []Breaker{}}
	}
	h := TenantHealth{Status: "ok", Started: Started, Built: Built(), Apps: len(t.apps), Queues: []QueueHealth{}, Deferred: t.Deferred(now), Breakers: t.Breakers(now)}
	if h.Deferred == nil {
		h.Deferred = []string{}
	}
	t.opsMu.Lock()
	for _, a := range t.apps {
		q := t.work.queued(a.Manifest().ID)
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
		h.Queues = append(h.Queues, qh)
	}
	h.Failed = t.work.failedCount()
	t.opsMu.Unlock()
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
	if h.Failed > 0 || h.OpenBreakers > 0 || h.Connectors > 0 || h.Endpoints > 0 || len(h.Deferred) > 0 {
		h.Status = "degraded"
	}
	return h
}

// Started is when this process began: every tenant reports it, so a person can
// tell a host running yesterday's code from one they just built.
var Started = time.Now().UTC().Truncate(time.Second)

// Built is the code this process was built from, when the build stamped it:
// the version control revision from Go's build info.
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
	if modified { // git describe's marker: built from uncommitted changes
		return revision + "-dirty"
	}
	return revision
}
