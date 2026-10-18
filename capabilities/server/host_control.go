package platformserver

import (
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// hostControl is what the host console holds over a tenant (ADR-0016 §6.5):
// its lifecycle ("" or "open" runs, "suspend" and "decommission" block
// ordinary requests), the support sessions it authorized, and the migrations
// it moved in or out. The lifecycle is read without any lock (health, the
// tenant record); the rest has its own.
type hostControl struct {
	lifecycle  atomic.Pointer[string]
	mu         sync.Mutex
	support    []SupportGrant
	migrations []MigrationManifest
	requests   map[string]hostControlReceipt
}

func (h *hostControl) state() string {
	if p := h.lifecycle.Load(); p != nil {
		return *p
	}
	return ""
}

func (h *hostControl) setState(v string) { h.lifecycle.Store(&v) }

// suspended reports whether the console stopped the tenant.
func (h *hostControl) suspended() bool {
	l := h.state()
	return l != "" && l != "open"
}

// supportGrants are the sessions, newest first.
func (h *hostControl) supportGrants() []SupportGrant {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := slices.Clone(h.support)
	slices.Reverse(out)
	return out
}

// usableSupport resolves an open session without using it.
func (h *hostControl) usableSupport(tenant, id string, now time.Time) (SupportGrant, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	index := slices.IndexFunc(h.support, func(g SupportGrant) bool { return g.ID == id })
	if index < 0 {
		return SupportGrant{}, fmt.Errorf("no support session %s in %s", id, tenant)
	}
	if !h.support[index].Expires.After(now) {
		return SupportGrant{}, fmt.Errorf("support session %s has ended", id)
	}
	return h.support[index], nil
}

func (h *hostControl) migrationList() []MigrationManifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.migrations)
}

func (h *hostControl) snapshot(s *tenantState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	s.HostLifecycle, s.Support, s.Migrations = h.state(), slices.Clone(h.support), slices.Clone(h.migrations)
	s.HostRequests = maps.Clone(h.requests)
}

func (h *hostControl) restore(s *tenantState) {
	h.setState(s.HostLifecycle)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.support, h.migrations = slices.Clone(s.Support), slices.Clone(s.Migrations)
	h.requests = maps.Clone(s.HostRequests)
}
