package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// routesIntegration serves connectors, protocols, typed capability calls, A2A, MCP and writebacks.
func (h *Host) routesIntegration(rt *routes) {
	rt.handle(Route{Pattern: "POST /v1/connectors/{input}", Summary: "Deliver a connector's batch or page as the connector's member (K8)", Body: json.RawMessage{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		out, err := t.Input(m, r.PathValue("input"), body, h.Now())
		record, _ := out.(*pb.ChangeRecord)
		Reply(w, record, err)
	})
	rt.handle(Route{Pattern: "POST /v1/protocols/{protocol}/{version}/{action}", Summary: "Call a protocol's action at the provider the tenant binds", Body: ProtocolCall{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var call struct {
			Target, IdempotencyKey string
			Payload                json.RawMessage
		}
		if json.NewDecoder(r.Body).Decode(&call) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		_, record, err := t.Invoke(m, r.PathValue("protocol")+"/"+r.PathValue("version"), r.PathValue("action"), call.Target, call.Payload, call.IdempotencyKey, h.Now())
		Reply(w, record, err)
	})
	rt.handle(Route{Pattern: "POST /v1/capabilities/invoke", Summary: "Route a typed call to its canonical query, action, AI or compute owner", Body: platform.CapabilityInvocation{}, Answer: platform.CapabilityResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var q platform.CapabilityInvocation
		if !readCapabilityBody(w, r, &q) {
			return
		}
		answer, err := t.InvokeCapability(m, q, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(Route{Pattern: "GET /v1/capabilities/calls/compute/{id}", Summary: "Read a retained compute result after rechecking member and protected sources", Answer: platform.OperationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReadOperation(m, r.PathValue("id"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.public(Route{Pattern: "GET /a2a/{tenant}/{agent}/.well-known/agent-card.json", Summary: "A published agent's A2A 1.0 card (ADR-0022)"}, func(w http.ResponseWriter, r *http.Request) {
		tenants := h.currentTenants()
		i := slices.IndexFunc(tenants, func(t *Tenant) bool { return t.ID == r.PathValue("tenant") })
		if i < 0 || !tenants[i].published(r.PathValue("agent")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		WriteJSON(w, http.StatusOK, h.agentCard(r, tenants[i], r.PathValue("agent")))
	})
	rt.public(Route{Pattern: "POST /a2a/{tenant}/{agent}", Summary: "A2A 1.0 JSON-RPC to a published agent; the caller signs in with the host's issuer", Body: json.RawMessage{}}, h.serveA2A)
	rt.handle(Route{Pattern: "POST /mcp", Summary: "MCP: the caller's catalog as tools (JSON-RPC)", Body: json.RawMessage{}}, h.mcp)
	rt.handle(Route{Pattern: "GET /v1/integration-effects", Summary: "Build writeback delivery status for builders and integrators, excluding payloads", Answer: []IntegrationEffect{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.integrationEffects(m, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}
