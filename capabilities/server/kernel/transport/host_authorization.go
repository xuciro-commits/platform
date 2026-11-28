package transport

import (
	"context"
	"net/http"
	"platformserver/kernel/acceptedformat"
	"platformserver/kernel/protocol"
	"platformserver/platform"
)

type hostAuthorizationKey struct{}

func HostRequestAuthorization(r *http.Request, tenant string) []acceptedformat.AuthorizationEvidence {
	evidence, found := r.Context().Value(hostAuthorizationKey{}).(acceptedformat.AuthorizationEvidence)
	if !found {
		return nil
	}
	evidence.Request.Tenant = tenant
	return []acceptedformat.AuthorizationEvidence{evidence}
}

func HostAuthorized[T comparable](pattern string, lookup func(string) T, authorize func(*http.Request) (string, platform.AuthzDecision, *platform.Error), f func(http.ResponseWriter, *http.Request, string, T)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subject, decision, refusal := authorize(r)
		if refusal != nil {
			WriteJSON(w, http.StatusUnauthorized, map[string]any{"error": protocol.ErrorBody{Code: refusal.Error(), Message: refusal.Message}})
			return
		}
		id := r.PathValue("tenant")
		tenant := lookup(id)
		var zero T
		if tenant == zero && id != "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		question := platform.AuthzRequest{Principal: "host:" + subject, Tenant: id, Kind: "action", Action: pattern}
		evidence := acceptedformat.AuthorizationEvidence{Request: question, Decision: decision, Roles: []string{"host-admin"}}
		r = r.WithContext(context.WithValue(r.Context(), hostAuthorizationKey{}, evidence))
		f(w, r, subject, tenant)
	}
}
