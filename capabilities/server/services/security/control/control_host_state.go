package control

import (
	"encoding/json"
	"fmt"
	"maps"
	"platformserver/platform"
	"slices"
	"strings"
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

func ValidateHostImage(tenant string, image platform.HostControlImage) error {
	owners := map[string]bool{}
	for _, id := range image.DisabledOwners {
		if id == "" || len(id) > 128 || owners[id] {
			return fmt.Errorf("invalid inactive owner")
		}
		owners[id] = true
	}
	if image.RuntimeSelection != nil {
		if len(image.RuntimeSelection) != 3 {
			return fmt.Errorf("invalid runtime selection")
		}
		for _, id := range []string{"platform.rules", "platform.authorization", "platform.kernel"} {
			if _, found := image.RuntimeSelection[id]; !found {
				return fmt.Errorf("missing runtime contract %s", id)
			}
		}
	}
	if !slices.Contains([]string{"", "open", "suspend", "decommission", "suspended", "decommissioned"}, image.Lifecycle) {
		return fmt.Errorf("invalid host lifecycle")
	}
	ids := map[string]bool{}
	for _, grant := range image.Support {
		if grant.ID == "" || ids[grant.ID] || grant.Tenant != tenant || grant.Member == "" || grant.Reason == "" || grant.Opened == "" || grant.Expires.IsZero() || grant.Uses < 0 {
			return fmt.Errorf("invalid host support grant")
		}
		ids[grant.ID] = true
	}
	for _, m := range image.Migrations {
		if m.At.IsZero() || m.Key == "" || m.From == "" || m.To != tenant || m.From == m.To || m.Member == "" {
			return fmt.Errorf("invalid host migration manifest")
		}
		for _, part := range m.Types {
			if part.Type == "" || part.Rows < 0 || part.Written < 0 || part.Written > part.Rows {
				return fmt.Errorf("invalid host migration counts")
			}
		}
	}
	for key, receipt := range image.Requests {
		tag, _, found := strings.Cut(strings.TrimPrefix(key, "host:"), ":")
		if !strings.HasPrefix(key, "host:") || !found || !slices.Contains([]string{"lifecycle", "support.open", "support.use", "migration", "composition"}, tag) || len(receipt.RequestHash) != 64 || !strings.HasSuffix(key, ":"+receipt.RequestHash) {
			return fmt.Errorf("invalid host receipt")
		}
		if tag == "support.open" || tag == "support.use" {
			i := slices.IndexFunc(image.Support, func(g platform.SupportGrant) bool { return g.ID == receipt.Answer.ID })
			if i < 0 {
				return fmt.Errorf("host receipt names no support grant")
			}
			current := image.Support[i]
			if receipt.Answer.Uses < 0 || receipt.Answer.Uses > current.Uses {
				return fmt.Errorf("host receipt has an invalid support count")
			}
			current.Uses = receipt.Answer.Uses
			if current != receipt.Answer {
				return fmt.Errorf("host receipt rewrites its support authority")
			}
		} else if receipt.Answer != (platform.SupportGrant{}) {
			return fmt.Errorf("host receipt has an unrelated answer")
		}
	}
	return nil
}
