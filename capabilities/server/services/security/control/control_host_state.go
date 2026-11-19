package control

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/kernel/protocol"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

type HostState struct {
	lifecycle        atomic.Pointer[string]
	mu               sync.Mutex
	support          []protocol.SupportGrant
	migrations       []protocol.MigrationManifest
	requests         map[string]protocol.HostControlReceipt
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

func (h *HostState) SupportGrants() []protocol.SupportGrant {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := append([]protocol.SupportGrant{}, h.support...)
	slices.Reverse(out)
	return out
}

func (h *HostState) UsableSupport(tenant, id string, now time.Time) (protocol.SupportGrant, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	index := slices.IndexFunc(h.support, func(g protocol.SupportGrant) bool { return g.ID == id })
	if index < 0 {
		return protocol.SupportGrant{}, fmt.Errorf("no support session %s in %s", id, tenant)
	}
	if !h.support[index].Expires.After(now) {
		return protocol.SupportGrant{}, fmt.Errorf("support session %s has ended", id)
	}
	return h.support[index], nil
}

func (h *HostState) Migrations() []protocol.MigrationManifest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.migrations)
}

func (h *HostState) SnapshotState() protocol.HostControlImage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return protocol.HostControlImage{Lifecycle: h.State(), Support: slices.Clone(h.support), Migrations: slices.Clone(h.migrations), Requests: maps.Clone(h.requests), RuntimeSelection: maps.Clone(h.runtimeSelection), DisabledOwners: slices.Clone(h.disabledOwners)}
}

func (h *HostState) Restore(image protocol.HostControlImage) {
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

func (h *HostState) Image() (protocol.HostControlImage, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	raw, err := json.Marshal(protocol.HostControlImage{Lifecycle: h.State(), Support: h.support, Migrations: h.migrations, Requests: h.requests, RuntimeSelection: h.runtimeSelection, DisabledOwners: h.disabledOwners})
	var image protocol.HostControlImage
	if err == nil {
		err = json.Unmarshal(raw, &image)
	}
	return image, err
}
