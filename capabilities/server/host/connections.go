package host

import (
	"net/http"

	transport "platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (h *Host) routesConnectionKinds(rt *routes) {
	roles := []string{build.Builder, build.Integrator}
	rt.authorized(transport.Route{Pattern: "GET /v1/connection-kinds", Summary: "Built-in transports a connection can use", Answer: build.ConnectionKindPage{}}, build.ID, roles, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(build.ConnectionKinds(), t.Language(m, r)))
	})
}
