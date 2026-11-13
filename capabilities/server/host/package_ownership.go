package host

import (
	"net/http"

	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/devops/packages"
	build "platformserver/services/resources/authoring"
)

func (h *Host) routesPackageOwnership(rt *routes) {
	rt.recordRead(transport.Route{Pattern: "GET /v1/packages/{id}/ownership", Summary: "Review an explicit resource ownership transfer before installing a split product", Answer: packages.PackageOwnership{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		c := t.caller(m, t.App("platform"), false)
		if refusal := authorizationRefusal(t.authorizeNative(c, "GET /v1/packages/{id}/ownership", "read", []string{"admin"}, true)); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		catalog := t.App("platform").(interface{ PackageViews() []platform.PackageView })
		plan, err := packages.OwnershipFor(catalog.PackageViews(), r.PathValue("id"), r.URL.Query().Get("version"), t.Projection, t.builders[build.ID])
		if err != nil {
			WriteJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
			return
		}
		if plan == nil {
			WriteJSON(w, http.StatusNotFound, map[string]string{"error": "The exact product version is unavailable"})
			return
		}
		WriteJSON(w, http.StatusOK, plan)
	})
}
