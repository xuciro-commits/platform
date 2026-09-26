package platformserver

import (
	"slices"
	"time"
)

// TenantHealth is how a tenant's work stands (ADR-0027 D6), for its
// administrators and for metrics: its queues, what gave up, what waits past a
// quota, its breakers, and its connectors and endpoints.
type TenantHealth struct {
	Status       string        `json:"status"` // ok, or degraded when something waits for a person or a destination fails
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
	h := TenantHealth{Status: "ok", Apps: len(t.apps), Queues: []QueueHealth{}, Deferred: t.Deferred(now), Breakers: t.Breakers(now)}
	if h.Deferred == nil {
		h.Deferred = []string{}
	}
	t.opsMu.Lock()
	for _, a := range t.apps {
		q := t.queues[a.Manifest().ID]
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
	h.Failed = len(t.failed)
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
