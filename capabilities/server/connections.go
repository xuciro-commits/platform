package platformserver

import transport "platformserver/kernel/transport"

import (
	"net/http"

	"platformserver/platform"
	"platformserver/studio/build"
)

// ConnectionKind is one way the tenant can reach an external system.
type ConnectionKind struct {
	ID                 string `json:"id"`
	Title              string `json:"title"`
	Kind               string `json:"kind"`
	AddressPlaceholder string `json:"addressPlaceholder,omitempty"`
}
type ConnectionKindPage struct {
	Items []ConnectionKind `json:"items"`
}

func (t *Tenant) connectionKinds() ConnectionKindPage {
	return ConnectionKindPage{Items: []ConnectionKind{
		{ID: "http", Kind: "http", Title: "HTTP / REST / files", AddressPlaceholder: "https://api.example.com/v1/"},
		{ID: "odata", Kind: "odata", Title: "OData (SAP Gateway)", AddressPlaceholder: "https://sap.example.com/sap/opu/odata/sap/API_PRODUCT_SRV/"},
		{ID: "postgres", Kind: "postgres", Title: "PostgreSQL (read-only)", AddressPlaceholder: "postgres://reader@db.example.com:5432/app?sslmode=require"},
	}}
}

func (h *Host) routesConnectionKinds(rt *routes) {
	roles := []string{build.Builder, build.Integrator}
	// GET /v1/connection-kinds answers the built-in transports.
	rt.authorized(transport.Route{Pattern: "GET /v1/connection-kinds", Summary: "Built-in transports a connection can use", Answer: ConnectionKindPage{}}, build.ID, roles, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		WriteJSON(w, http.StatusOK, t.i18n.Translate(t.connectionKinds(), t.Language(m, r)))
	})
}
