package host

import transport "platformserver/kernel/transport"

import (
	"fmt"
	"maps"
	"net/http"
	"platformserver/kernel/acceptedformat"
	"platformserver/services/devops/packages"
	controlowner "platformserver/services/security/control"
	"slices"

	"platformserver/platform"
)

func (t *Tenant) desiredRuntime() map[string]string {
	selection := t.console.RuntimeSelection()
	if selection == nil {
		return t.services.Selection()
	}
	return selection
}

func (t *Tenant) compositionView(providers platform.RuntimeProviders) packages.HostComposition {
	return packages.CompositionState{Current: t.services.Selection(), Desired: t.desiredRuntime(), Available: providers.Modules(), Base: t.baseModules(),
		Storage: t.services.StorageModule, DisabledOwners: t.currentDisabledOwners(), DesiredDisabledOwners: t.desiredDisabledOwners(),
		Suspended: t.console.Suspended(), PlatformApp: controlowner.PlatformApp}.View()
}

func (h *Host) hostCompositionRoutes(mux *http.ServeMux) {
	providers := h.RuntimeProviders
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/packages/inspect", Summary: "Inspect package contents during tenant maintenance without installing or granting access", Body: platform.PackageInspectRequest{}, Answer: platform.PackagePreview{}}, func(w http.ResponseWriter, r *http.Request, _ string, t *Tenant) {
		var body platform.PackageInspectRequest
		if err := transport.DecodeBodyLimit(r, &body, packages.MaxPackageImportBytes+4096); err != nil {
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
	h.hostRoute(mux, transport.Route{Pattern: "GET /v1/host/tenants/{tenant}/composition", Summary: "Current and saved foundational provider selection and supported maintenance mode", Answer: packages.HostComposition{}}, func(w http.ResponseWriter, _ *http.Request, _ string, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.compositionView(providers))
	})
	h.hostRoute(mux, transport.Route{Pattern: "POST /v1/host/tenants/{tenant}/composition", Summary: "Save a checked provider selection and stop business access until supported restart and recovery", Body: acceptedformat.CompositionRequest{}, Answer: packages.HostComposition{}}, func(w http.ResponseWriter, r *http.Request, subject string, t *Tenant) {
		var body acceptedformat.CompositionRequest
		if err := transport.DecodeBodyLimit(r, &body, 1<<20); err != nil {
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
		t.Lane.Lock()
		if err := t.checkOwnerRemoval(body.DisabledOwners); err != nil {
			t.Lane.Unlock()
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
		t.Lane.Unlock()
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		WriteJSON(w, http.StatusOK, t.compositionView(providers))
	})
}

func missingRuntime(selection map[string]string) bool { return packages.MissingRuntime(selection) }
