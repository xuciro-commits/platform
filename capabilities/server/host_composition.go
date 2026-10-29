package platformserver

import transport "platformserver/kernel/transport"

import (
	"fmt"
	"maps"
	"net/http"
	controlowner "platformserver/kernel/control"
	"platformserver/studio/packages"
	"slices"
	"strings"
	"time"

	"platformserver/platform"
)

type CompositionRequest struct {
	Selection      map[string]string `json:"selection"`
	Reason         string            `json:"reason"`
	At             time.Time         `json:"at"`
	DisabledOwners []string          `json:"disabledOwners,omitempty"`
}

type HostComposition struct {
	Current               map[string]string           `json:"current"`
	Desired               map[string]string           `json:"desired"`
	Available             []platform.CapabilityModule `json:"available"`
	State                 string                      `json:"state"`
	Reason                string                      `json:"reason,omitempty"`
	Owners                []platform.CapabilityModule `json:"owners"`
	DisabledOwners        []string                    `json:"disabledOwners"`
	DesiredDisabledOwners []string                    `json:"desiredDisabledOwners"`
	Storage               platform.CapabilityModule   `json:"storage"`
}

func (t *Tenant) desiredRuntime() map[string]string {
	selection := t.console.RuntimeSelection()
	if selection == nil {
		return t.services.Selection()
	}
	return selection
}

func (t *Tenant) compositionView(providers platform.RuntimeProviders) HostComposition {
	view := HostComposition{Current: t.services.Selection(), Desired: t.desiredRuntime(), Available: providers.Modules(), State: "ready", Storage: t.services.StorageModule}
	view.DisabledOwners, view.DesiredDisabledOwners = t.currentDisabledOwners(), t.desiredDisabledOwners()
	view.Owners = []platform.CapabilityModule{}
	for _, module := range t.baseModules() {
		if strings.HasPrefix(module.ID, "platform.owner.") {
			view.Owners = append(view.Owners, module)
		}
	}
	if missingRuntime(view.Desired) || slices.Contains(view.DesiredDisabledOwners, controlowner.PlatformApp) {
		view.State, view.Reason = "maintenance", "A required implementation is removed; business access remains stopped"
	} else if !maps.Equal(view.Current, view.Desired) || !slices.Equal(view.DisabledOwners, view.DesiredDisabledOwners) {
		view.State, view.Reason = "restart-required", "The selection is saved; restart this host to activate the checked composition"
	} else if t.console.Suspended() {
		view.State = "suspended"
	}
	return view
}

func (h *Host) hostCompositionRoutes(mux *http.ServeMux) {
	providers := h.RuntimeProviders
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/packages/inspect", Summary: "Inspect package contents during tenant maintenance without installing or granting access", Body: platform.PackageInspectRequest{}, Answer: platform.PackagePreview{}}, func(w http.ResponseWriter, r *http.Request, _ string, t *Tenant) {
		var body platform.PackageInspectRequest
		if err := decodeBodyLimit(r, &body, packages.MaxPackageImportBytes+4096); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		preview, err := t.inspectPackage(r.Context(), body)
		if err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, preview)
	})
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/composition", Summary: "Current and saved foundational provider selection and supported maintenance mode", Answer: HostComposition{}}, func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.compositionView(providers))
	})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/composition", Summary: "Save a checked provider selection and stop business access until supported restart and recovery", Body: CompositionRequest{}, Answer: HostComposition{}}, func(w http.ResponseWriter, r *http.Request, subject string, t *Tenant) {
		var body CompositionRequest
		if err := decodeBody(r, &body); err != nil {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if body.Reason == "" || body.At.IsZero() {
			WriteJSON(w, http.StatusBadRequest, map[string]string{"error": "A reason and stable request time are required"})
			return
		}
		if err := providers.CheckSelection(body.Selection); err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		t.mu.Lock()
		if err := t.checkOwnerRemoval(body.DisabledOwners); err != nil {
			t.mu.Unlock()
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		_, err := t.acceptHostControl("composition", "host:"+subject, body, body.At, platform.AuditEntry{Action: "host.composition", Target: t.ID + ":" + body.Reason}, func(image *platform.HostControlImage) (platform.SupportGrant, error) {
			if t.quarantined() {
				return platform.SupportGrant{}, fmt.Errorf("Repair this tenant before changing its implementation")
			}
			image.RuntimeSelection = maps.Clone(body.Selection)
			image.DisabledOwners = slices.Clone(body.DisabledOwners)
			image.Lifecycle = "suspend"
			return platform.SupportGrant{}, nil
		}, hostRequestAuthorization(r, t.ID)...)
		t.mu.Unlock()
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, t.compositionView(providers))
	})
}

func missingRuntime(selection map[string]string) bool {
	for _, provider := range selection {
		if provider == "" {
			return true
		}
	}
	return false
}
