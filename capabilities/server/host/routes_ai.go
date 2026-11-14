package host

import transport "platformserver/kernel/transport"

import (
	"encoding/json"
	"net/http"
	"platformserver/services/ai/gateway"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (h *Host) routesAI(rt *routes) {
	rt.handle(transport.Route{Pattern: "GET /v1/ai/training/export", Summary: "Export a fixed currently qualified SFT or DPO dataset without external calls", Answer: platform.AITrainingExport{}, Query: []transport.Param{{Name: "format", Description: "sft or dpo"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, refusal := recordRead(t, r.Context(), func(t *Tenant) (platform.AITrainingExport, *kernel.Error) {
			owner, ok := t.ai.(platform.AITrainingOwner)
			if !ok {
				return platform.AITrainingExport{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Training data is unavailable")
			}
			return owner.TrainingExport(platform.NewCaller(runtime{t: t}, m, platform.AIApp, false, false), r.URL.Query().Get("format"), h.Now())
		})
		if refusal != nil {
			Reply(w, nil, refusal)
			return
		}
		artifact, _ := json.Marshal(answer)
		if err := t.files().Put(r.Context(), "training/"+t.ID+"/"+answer.ID+".json", artifact, "application/json"); err != nil {
			http.Error(w, t.i18n.Say(t.Language(m, r), "Training export could not be saved"), http.StatusInternalServerError)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/ai/knowledge-index", Summary: "Original knowledge vector index preparation and current retrieval mode", Answer: platform.KnowledgeIndexStatus{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.KnowledgeIndexStatus, *kernel.Error) {
		status, err := t.KnowledgeIndexStatus(r.Context(), m)
		status.Problem = t.i18n.Say(t.Language(m, r), status.Problem)
		return status, err
	}))
	rt.handle(transport.Route{Pattern: "GET /v1/ai/model-selection", Summary: "Purpose-compatible models and effective configuration from the original AI and settings owners", Answer: aigateway.ModelSelection{}, Query: []transport.Param{{Name: "purpose", Description: "generation, agent or embedding"}}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (aigateway.ModelSelection, *kernel.Error) {
		selection, err := t.ModelSelection(r.Context(), m, r.URL.Query().Get("purpose"))
		selection.Localize(func(s string) string { return t.i18n.Say(t.Language(m, r), s) })
		return selection, err
	}))
	for _, endpoint := range []struct {
		path   string
		design bool
	}{{"POST /v1/ai/chat", false}, {"POST /v1/build/ai/chat", true}} {
		rt.handle(transport.Route{Pattern: endpoint.path, Summary: "Call an authorized model in the endpoint execution stage; the call is metered", Body: platform.ChatRequest{}, Answer: platform.ChatAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			chat := t.Chat
			if endpoint.design {
				chat = t.DesignChat
			}
			aigateway.ChatEndpoint{MaxBytes: chatMaxBytes, Say: func(s string) string { return t.i18n.Say(t.Language(m, r), s) },
				Chat: func(req platform.ChatRequest, delta ...func(string)) (platform.ChatAnswer, *kernel.Error, *platform.AIError) {
					return chat(m, req, h.Now(), delta...)
				},
				Said:  func(err *kernel.Error) *kernel.Error { return t.i18n.Said(err, t.Language(m, r)) },
				Reply: func(w http.ResponseWriter, err *kernel.Error) { Reply(w, nil, err) },
				JSON:  WriteJSON,
			}.Serve(w, r)
		})
	}
	rt.handle(transport.Route{Pattern: "GET /v1/ai/providers/{id}/models", Summary: "A provider's catalog of models", Answer: []platform.CatalogModel{}, Query: []transport.Param{{Name: "refresh", Description: "true reads the catalog from the provider again"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		models, err, failure := t.ProviderModels(m, r.PathValue("id"), r.URL.Query().Get("refresh") == "true")
		switch {
		case err != nil:
			Reply(w, nil, err)
		case failure != nil:
			WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
		default:
			WriteJSON(w, http.StatusOK, models)
		}
	})
	rt.handle(transport.Route{Pattern: "GET /v1/ai/vendors", Summary: "The vendors a provider may be", Answer: []platform.AIVendor{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, _ *Tenant) {
		WriteJSON(w, http.StatusOK, platform.AIVendors)
	})
	rt.handle(transport.Route{Pattern: "GET /v1/capabilities/calls/ai/{id}", Summary: "Read the original typed AI call result after member and source checks", Answer: platform.OperationResult{}}, transport.Answering(func(r *http.Request, m platform.Member, t *Tenant) (platform.OperationResult, *kernel.Error) {
		return t.ReadCapabilityAI(r.Context(), m, r.PathValue("id"), h.Now())
	}))
}
