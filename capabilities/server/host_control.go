package platformserver

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// hostControl is what the host console holds over a tenant (ADR-0047 §6.5):
// its lifecycle ("" or "open" runs, "suspended" and "decommissioned" block
// ordinary requests), the support sessions it authorized, and the migrations
// it moved in or out. The lifecycle is read without any lock (health, the
// tenant record); the rest has its own.
type hostControl struct {
	lifecycle  atomic.Pointer[string]
	mu         sync.Mutex
	support    []SupportGrant
	migrations []MigrationManifest
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

func (h *hostControl) addSupport(grant SupportGrant) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.support = append(h.support, grant)
}

// supportGrants are the sessions, newest first.
func (h *hostControl) supportGrants() []SupportGrant {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := slices.Clone(h.support)
	slices.Reverse(out)
	return out
}

// useSupport counts one use of an open session and answers it; an unknown
// session is errUnknownGrant, an ended one says so.
func (h *hostControl) useSupport(id string, now time.Time) (SupportGrant, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	index := slices.IndexFunc(h.support, func(g SupportGrant) bool { return g.ID == id })
	if index < 0 {
		return SupportGrant{}, errUnknownGrant
	}
	if !h.support[index].Expires.After(now) {
		return SupportGrant{}, errSupport("the support session has ended")
	}
	h.support[index].Uses++
	return h.support[index], nil
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

func (h *hostControl) addMigration(m MigrationManifest) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.migrations = append(h.migrations, m)
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
}

func (h *hostControl) restore(s *tenantState) {
	h.setState(s.HostLifecycle)
	h.mu.Lock()
	defer h.mu.Unlock()
	h.support, h.migrations = slices.Clone(s.Support), slices.Clone(s.Migrations)
}
