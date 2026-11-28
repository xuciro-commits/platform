package transport

import (
	"net/http"
	"platformserver/kernel/protocol"
	"platformserver/platform"
	"slices"
)

type IdentityEntrance struct {
	Entry  func(string) (protocol.IdentityEntry, bool)
	Accept func(*http.Request, string, string) (MeTenant, *platform.Error)
	Create func(protocol.WorkspaceCreate, string) (MeTenant, int)
}

func (e IdentityEntrance) Routes(register func(Route, http.HandlerFunc)) {
	route := func(spec Route, handler func(http.ResponseWriter, *http.Request, protocol.IdentityEntry)) {
		register(spec, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			identity, ok := e.Entry(r.Header.Get("Authorization"))
			if !ok {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			handler(w, r, identity)
		})
	}
	route(Route{Pattern: "GET /v1/auth/me", Summary: "Authenticated identity and available workspaces before choosing a tenant", Answer: protocol.IdentityEntry{}}, func(w http.ResponseWriter, r *http.Request, identity protocol.IdentityEntry) {
		WriteJSON(w, 200, identity)
	})
	route(Route{Pattern: "POST /v1/auth/invitations/{tenant}/accept", Summary: "Accept your verified identity's pending workspace invitation", Answer: MeTenant{}}, func(w http.ResponseWriter, r *http.Request, identity protocol.IdentityEntry) {
		id := r.PathValue("tenant")
		if !slices.ContainsFunc(identity.Invitations, func(inv protocol.PendingWorkspace) bool { return inv.ID == id }) {
			w.WriteHeader(403)
			return
		}
		answer, refusal := e.Accept(r, id, identity.Subject)
		Answer(w, answer, refusal)
	})
	route(Route{Pattern: "POST /v1/auth/workspaces", Summary: "Create a workspace with the authenticated identity as its first administrator", Body: protocol.WorkspaceCreate{}, Answer: MeTenant{}}, func(w http.ResponseWriter, r *http.Request, identity protocol.IdentityEntry) {
		if !identity.CanCreate {
			w.WriteHeader(403)
			return
		}
		var body protocol.WorkspaceCreate
		if DecodeBodyLimit(r, &body, 16384) != nil {
			WriteJSON(w, 400, map[string]string{"error": "Invalid workspace request"})
			return
		}
		answer, status := e.Create(body, identity.Subject)
		if status != 201 {
			WriteJSON(w, status, map[string]string{"error": "Workspace could not be created"})
			return
		}
		WriteJSON(w, status, answer)
	})
}
