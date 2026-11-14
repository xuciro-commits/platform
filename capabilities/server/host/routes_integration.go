package host

import (
	"encoding/json"
	"io"
	"net/http"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/transport"
	"platformserver/platform"
	"platformserver/services/actions"
	dataowner "platformserver/services/data"
	"platformserver/services/data/lineage"
)

func (h *Host) routesIntegration(rt *routes) {
	rt.handle(transport.Route{Pattern: "GET /v1/observations", Summary: "Declared external observation sources available through original member and model permissions", Answer: []dataowner.ObservationInfo{}}, translating(func(_ *http.Request, m platform.Member, t *Tenant) ([]dataowner.ObservationInfo, *kernel.Error) {
		return t.Observations(m)
	}))
	rt.handle(transport.Route{Pattern: "POST /v1/observations/{app}/{name}/read", Summary: "Read a current protocol point or bounded external window without storing points in tenant state", Body: dataowner.ObservationRead{}, Answer: platform.ObservationResult{}}, observationRoute(func(r *http.Request, m platform.Member, t *Tenant, ref platform.AssetRef, request dataowner.ObservationRead) (platform.ObservationResult, *kernel.Error) {
		return t.ReadObservation(r.Context(), m, ref, request, h.Now())
	}))
	rt.handle(transport.Route{Pattern: "POST /v1/observations/{app}/{name}/evaluate", Summary: "Evaluate one external summary with the shared rule language and submit only compact results through the original business action", Body: dataowner.ObservationEvaluate{}, Answer: ObservationEvaluation{}}, observationRoute(func(r *http.Request, m platform.Member, t *Tenant, ref platform.AssetRef, request dataowner.ObservationEvaluate) (ObservationEvaluation, *kernel.Error) {
		return t.EvaluateObservation(r.Context(), m, ref, request, h.Now())
	}))
	rt.recordRead(transport.Route{Pattern: "GET /v1/schedules", Summary: "Independent pipeline schedules visible to the caller, with their next due time and last build", Answer: []dataowner.PipelineSchedule{}}, transport.Answering(func(_ *http.Request, m platform.Member, t *Tenant) ([]dataowner.PipelineSchedule, *kernel.Error) {
		now := h.Now()
		page, refusal := t.objectOwner(t.Projection).SelectionFrom(t.Projection, m, platform.ScheduleType, platform.Query{Sort: []string{"id"}}, now)
		if refusal != nil {
			return nil, refusal
		}
		return t.DataOwner().PipelineSchedules(page.Records, func(id string) (dataowner.Pipeline, bool) {
			view, refusal := t.recordOf(m, dataowner.PipelineType, id, now)
			if refusal != nil {
				return dataowner.Pipeline{}, false
			}
			return view.Record.(dataowner.Pipeline), true
		})
	}))
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
	rt.handle(transport.Route{Pattern: "POST /v1/protocols/{protocol}/{version}/{action}", Summary: "Call a protocol's action at the provider the tenant binds", Body: transport.ProtocolCall{}, Answer: SubmissionAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var call transport.ProtocolCall
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
		answer, err := t.InvokeCapability(r.Context(), m, q, h.Now())
		if err != nil {
			Reply(w, nil, t.i18n.Said(err, t.Language(m, r)), t.localizedFieldIssues(m, answer.Issues, t.Language(m, r))...)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/capabilities/calls/compute/{id}", Summary: "Read a retained compute result after rechecking member and protected sources", Answer: platform.OperationResult{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.OperationResult, *kernel.Error) {
		return t.ReadOperation(r.Context(), m, r.PathValue("id"))
	}))
	rt.public(transport.Route{Pattern: "GET /a2a/{tenant}/{agent}/.well-known/agent-card.json", Summary: "A published agent's A2A 1.0 card"}, func(w http.ResponseWriter, r *http.Request) {
		tenant := h.tenantByID(r.PathValue("tenant"))
		if tenant == nil || !tenant.published(r.PathValue("agent")) {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		d, _ := tenant.agents.AgentDeclaration(r.PathValue("agent"))
		WriteJSON(w, http.StatusOK, actions.A2ACard(d, r.PathValue("agent"), tenant.ID, r.Host, r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https", h.Issuer))
	})
	rt.public(transport.Route{Pattern: "POST /a2a/{tenant}/{agent}", Summary: "A2A 1.0 JSON-RPC to a published agent; the caller signs in with the host's issuer", Body: json.RawMessage{}}, h.serveA2A)
	rt.handle(transport.Route{Pattern: "POST /mcp", Summary: "MCP: the caller's catalog as tools (JSON-RPC)", Body: json.RawMessage{}}, h.mcp)
	rt.recordRead(transport.Route{Pattern: "GET /v1/lineage", Summary: "Data lineage across connections, sources, datasets, pipelines, object types and writebacks the caller may read, with dataset versions, stale inputs and propagated markings", Query: []transport.Param{{Name: "mode", Description: "overview or focus; defaults from focus/ref"}, {Name: "project", Description: "a project RID used as the overview scope"}, {Name: "focus", Description: "a resource RID or lineage node ID"}, {Name: "ref", Description: "an app/kind/name resource reference"}, {Name: "node", Description: "a legacy lineage node ID"}, {Name: "branch", Description: "a branch ID; reads its staged definitions and no main run evidence"}, {Name: "direction", Description: "upstream, downstream or both"}, {Name: "depth", Description: "1 to 3"}, {Name: "nodeLimit", Description: "response node cap, at most 2000"}, {Name: "edgeLimit", Description: "response edge cap, at most 5000"}, {Name: "fields", Description: "off or on: include field nodes and edges"}, {Name: "field", Description: "a field node ID to trace upstream and downstream"}}, Answer: lineage.Lineage{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (lineage.Lineage, *kernel.Error) {
		return lineage.NewReader(t).ReadValues(m, r.URL.Query())
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/integration-effects", Summary: "Build writeback delivery status for builders and integrators, excluding payloads", Answer: []dataowner.IntegrationEffect{}}, transport.Answering(func(_ *http.Request, m platform.Member, t *Tenant) ([]dataowner.IntegrationEffect, *kernel.Error) {
		return t.integrationEffects(m, h.Now())
	}))
}

func observationRoute[B, R any](f func(*http.Request, platform.Member, *Tenant, platform.AssetRef, B) (R, *kernel.Error)) func(http.ResponseWriter, *http.Request, platform.Member, *Tenant) {
	return func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var request B
		if refusal := dataowner.DecodeObservationRequest(r.Body, &request); refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		out, refusal := f(r, m, t, platform.AssetRef{App: r.PathValue("app"), Kind: platform.AssetObservation, Name: r.PathValue("name")}, request)
		transport.Answer(w, out, t.i18n.Said(refusal, t.Language(m, r)))
	}
}
