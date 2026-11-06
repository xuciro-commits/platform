package host

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (h *Host) routesIntegration(rt *routes) {
	h.routesObservations(rt)
	h.routesSchedules(rt)
	rt.handle(transport.Route{Pattern: "POST /v1/listeners/{path}", Summary: "Deliver an inbound webhook as the connector member with X-Listener-Secret verification", Body: json.RawMessage{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		out, refusal := t.listenerInput(r.Context(), m, r.PathValue("path"), r.Header.Get("X-Listener-Secret"), scheme+"://"+r.Host, body, h.Now())
		record, _ := out.(*pb.ChangeRecord)
		Reply(w, record, refusal)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/protocols/{protocol}/{version}/{action}/{target}", Summary: "Call a protocol action with a record target and stable Idempotency-Key", Body: json.RawMessage{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, err := io.ReadAll(r.Body)
		if err != nil || !json.Valid(body) || r.Header.Get("Idempotency-Key") == "" {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		_, record, refusal, issues := t.invokeDiagnosed(m, r.PathValue("protocol")+"/"+r.PathValue("version"), r.PathValue("action"), r.PathValue("target"), body, r.Header.Get("Idempotency-Key"), h.Now())
		Reply(w, record, t.i18n.Said(refusal, t.Language(m, r)), t.localizedFieldIssues(m, issues, t.Language(m, r))...)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/connectors/{input}", Summary: "Deliver a connector's batch or page as the connector's member (K8)", Body: json.RawMessage{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		body, _ := io.ReadAll(r.Body)
		out, err := t.Input(m, r.PathValue("input"), body, h.Now())
		record, _ := out.(*pb.ChangeRecord)
		Reply(w, record, err)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/protocols/{protocol}/{version}/{action}", Summary: "Call a protocol's action at the provider the tenant binds", Body: ProtocolCall{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var call struct {
			Target, IdempotencyKey string
			Payload                json.RawMessage
		}
		if json.NewDecoder(r.Body).Decode(&call) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		_, record, err, issues := t.invokeDiagnosed(m, r.PathValue("protocol")+"/"+r.PathValue("version"), r.PathValue("action"), call.Target, call.Payload, call.IdempotencyKey, h.Now())
		Reply(w, record, t.i18n.Said(err, t.Language(m, r)), t.localizedFieldIssues(m, issues, t.Language(m, r))...)
	})
	rt.handle(transport.Route{Pattern: "POST /v1/capabilities/invoke", Summary: "Route a typed call to its canonical query, action, AI or compute owner", Body: platform.CapabilityInvocation{}, Answer: platform.CapabilityResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var q platform.CapabilityInvocation
		if !readCapabilityBody(w, r, &q) {
			return
		}
		answer, err := t.InvokeCapability(m, q, h.Now())
		if err != nil {
			Reply(w, nil, t.i18n.Said(err, t.Language(m, r)), t.localizedFieldIssues(m, answer.Issues, t.Language(m, r))...)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/capabilities/calls/compute/{id}", Summary: "Read a retained compute result after rechecking member and protected sources", Answer: platform.OperationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReadOperation(m, r.PathValue("id"))
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.public(transport.Route{Pattern: "GET /a2a/{tenant}/{agent}/.well-known/agent-card.json", Summary: "A published agent's A2A 1.0 card"}, func(w http.ResponseWriter, r *http.Request) {
		tenants := h.currentTenants()
		i := slices.IndexFunc(tenants, func(t *Tenant) bool { return t.ID == r.PathValue("tenant") })
		if i < 0 || !tenants[i].published(r.PathValue("agent")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		WriteJSON(w, http.StatusOK, h.agentCard(r, tenants[i], r.PathValue("agent")))
	})
	rt.public(transport.Route{Pattern: "POST /a2a/{tenant}/{agent}", Summary: "A2A 1.0 JSON-RPC to a published agent; the caller signs in with the host's issuer", Body: json.RawMessage{}}, h.serveA2A)
	rt.handle(transport.Route{Pattern: "POST /mcp", Summary: "MCP: the caller's catalog as tools (JSON-RPC)", Body: json.RawMessage{}}, h.mcp)
	rt.recordRead(transport.Route{Pattern: "GET /v1/lineage",
		Summary: "Data lineage across connections, sources, datasets, pipelines, object types and writebacks the caller may read, with dataset versions, stale inputs and propagated markings",
		Query: []transport.Param{
			{Name: "mode", Description: "overview or focus; defaults from focus/ref"},
			{Name: "project", Description: "a project RID used as the overview scope"},
			{Name: "focus", Description: "a resource RID or lineage node ID"},
			{Name: "ref", Description: "an app/kind/name resource reference"},
			{Name: "node", Description: "a legacy lineage node ID"},
			{Name: "branch", Description: "a branch ID; reads its staged definitions and no main run evidence"},
			{Name: "direction", Description: "upstream, downstream or both"},
			{Name: "depth", Description: "1 to 3"},
			{Name: "nodeLimit", Description: "response node cap, at most 500"},
			{Name: "edgeLimit", Description: "response edge cap, at most 1000"},
			{Name: "fields", Description: "off or on: include field nodes and edges"},
			{Name: "field", Description: "a field node ID to trace upstream and downstream"},
		}, Answer: Lineage{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		depth, err := lineageInt(r, "depth", 0)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		nodeLimit, err := lineageInt(r, "nodeLimit", 0)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		edgeLimit, err := lineageInt(r, "edgeLimit", 0)
		if err != nil {
			Reply(w, nil, err)
			return
		}
		focus := r.URL.Query().Get("focus")
		if focus == "" {
			focus = r.URL.Query().Get("node")
		}
		out, refusal := t.lineageLocked(m, LineageRequest{
			Mode: r.URL.Query().Get("mode"), Project: r.URL.Query().Get("project"),
			Focus: focus, Ref: r.URL.Query().Get("ref"), Direction: r.URL.Query().Get("direction"),
			Depth: depth, NodeLimit: nodeLimit, EdgeLimit: edgeLimit, Branch: r.URL.Query().Get("branch"),
			Fields: r.URL.Query().Get("fields") == "on", Field: r.URL.Query().Get("field"),
		})
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/integration-effects", Summary: "Build writeback delivery status for builders and integrators, excluding payloads", Answer: []IntegrationEffect{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		out, err := t.integrationEffects(m, h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, out)
	})
}

func lineageInt(r *http.Request, name string, fallback int) (int, *kernel.Error) {
	raw := r.URL.Query().Get(name)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < 0 {
		return 0, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, name+" is a non-negative integer")
	}
	return value, nil
}
