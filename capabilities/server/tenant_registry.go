package platformserver

import (
	"fmt"
	"sync"
)

// tenantRegistry keeps the host, work runner, telemetry and snapshotter on the
// same tenant generation. Rebuilding never reuses a partially restored Tenant.
type tenantRegistry struct {
	mu      sync.RWMutex
	tenants []*Tenant
}

func newTenantRegistry(tenants []*Tenant) *tenantRegistry {
	return &tenantRegistry{tenants: append([]*Tenant(nil), tenants...)}
}

func (r *tenantRegistry) list() []*Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]*Tenant(nil), r.tenants...)
}

func (r *tenantRegistry) current(id string) *Tenant {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, t := range r.tenants {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func (r *tenantRegistry) replace(id string, old, fresh *Tenant) error {
	if fresh == nil || fresh.ID != id || fresh.quarantined() || old == nil || !old.quarantined() {
		return fmt.Errorf("tenant %s: recovery candidate or predecessor is not eligible", id)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, t := range r.tenants {
		if t.ID == id {
			if t != old {
				return fmt.Errorf("tenant %s: another recovery already replaced this generation", id)
			}
			r.tenants[i] = fresh
			return nil
		}
	}
	return fmt.Errorf("tenant %s: not hosted", id)
}
