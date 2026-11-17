package host

import (
	"net/http"

	transport "platformserver/kernel/transport"
	"platformserver/platform"
	dataowner "platformserver/services/data"
)

func (h *Host) routesConnectionKinds(rt *routes) {
	roles := []string{dataowner.Builder, dataowner.Integrator}
	rt.authorized(transport.Route{Pattern: "GET /v1/connection-kinds", Summary: "Built-in transports a connection can use", Answer: dataowner.ConnectionKindPage{}}, dataowner.App, roles, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(dataowner.ConnectionKinds(), t.Language(m, r)))
	})
}
