package control

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type HostState struct {
	lifecycle        atomic.Pointer[string]
	mu               sync.Mutex
	support          []platform.SupportGrant
	migrations       []platform.MigrationManifest
	requests         map[string]platform.HostControlReceipt
	runtimeSelection map[string]string
	disabledOwners   []string
}

func (h *HostState) State() string {
	if p := h.lifecycle.Load(); p != nil {
		return *p
	}
	return ""
}

func (h *HostState) Suspended() bool {
	l := h.State()
	return l != "" && l != "open"
}

func (h *HostState) SupportGrants() []platform.SupportGrant {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := slices.Clone(h.support)
	slices.Reverse(out)
	return out
}

func (h *HostState) UsableSupport(tenant, id string, now time.Time) (platform.SupportGrant, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	index := slices.IndexFunc(h.support, func(g platform.SupportGrant) bool { return g.ID == id })
	if index < 0 {
		return platform.SupportGrant{}, fmt.Errorf("no support session %s in %s", id, tenant)
	}
	if !h.support[index].Expires.After(now) {
		return platform.SupportGrant{}, fmt.Errorf("support session %s has ended", id)
	}
	return h.support[index], nil
}

func (h *HostState) Migrations() []platform.MigrationManifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.migrations)
}

func (h *HostState) SnapshotState() platform.HostControlImage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return platform.HostControlImage{Lifecycle: h.State(), Support: slices.Clone(h.support), Migrations: slices.Clone(h.migrations), Requests: maps.Clone(h.requests), RuntimeSelection: maps.Clone(h.runtimeSelection), DisabledOwners: slices.Clone(h.disabledOwners)}
}

func (h *HostState) Restore(image platform.HostControlImage) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.lifecycle.Store(&image.Lifecycle)
	h.support, h.migrations = slices.Clone(image.Support), slices.Clone(image.Migrations)
	h.requests = maps.Clone(image.Requests)
	h.runtimeSelection = maps.Clone(image.RuntimeSelection)
	h.disabledOwners = slices.Clone(image.DisabledOwners)
}

func (h *HostState) RuntimeSelection() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return maps.Clone(h.runtimeSelection)
}

func (h *HostState) DisabledOwners() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.disabledOwners)
}

func (h *HostState) Image() (platform.HostControlImage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	raw, err := json.Marshal(platform.HostControlImage{Lifecycle: h.State(), Support: h.support, Migrations: h.migrations, Requests: h.requests, RuntimeSelection: h.runtimeSelection, DisabledOwners: h.disabledOwners})
	var image platform.HostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &image)
	}
	return image, err
}
