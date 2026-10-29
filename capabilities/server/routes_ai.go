package platformserver

import transport "platformserver/kernel/transport"

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// routesAI serves model calls and the AI catalog.
func (h *Host) routesAI(rt *routes) {
	rt.handle(transport.Route{Pattern: "GET /v1/ai/training/export", Summary: "Export a fixed currently qualified SFT or DPO dataset without external calls", Answer: platform.AITrainingExport{}, Query: []transport.Param{{Name: "format", Description: "sft or dpo"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, refusal := recordRead(t, context.Background(), func() (platform.AITrainingExport, *kernel.Error) {
			owner, ok := t.ai.(platform.AITrainingOwner)
			if !ok {
				return platform.AITrainingExport{}, platform.Refuse(pb.ErrorCode_ERROR_CODE_NOT_FOUND, "Training data is unavailable")
			}
			return owner.TrainingExport(platform.NewCaller(runtime{t}, m, platform.AIApp, false, false), r.URL.Query().Get("format"), h.Now())
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
	rt.handle(transport.Route{Pattern: "GET /v1/ai/knowledge-index", Summary: "Original knowledge vector index preparation and current retrieval mode", Answer: platform.KnowledgeIndexStatus{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		status, err := t.KnowledgeIndexStatus(m)
		if err != nil {
			Reply(w, nil, err)
		} else {
			status.Problem = t.i18n.Say(t.Language(m, r), status.Problem)
			WriteJSON(w, http.StatusOK, status)
		}
	})
	rt.handle(transport.Route{Pattern: "GET /v1/ai/model-selection", Summary: "Purpose-compatible models and effective configuration from the original AI and settings owners", Answer: ModelSelection{}, Query: []transport.Param{{Name: "purpose", Description: "generation, agent or embedding"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		selection, err := t.ModelSelection(m, r.URL.Query().Get("purpose"))
		if err != nil {
			Reply(w, nil, err)
		} else {
			selection.DefaultProblem = t.i18n.Say(t.Language(m, r), selection.DefaultProblem)
			for i := range selection.Models {
				selection.Models[i].Problem = t.i18n.Say(t.Language(m, r), selection.Models[i].Problem)
			}
			WriteJSON(w, http.StatusOK, selection)
		}
	})
	for _, endpoint := range []struct {
		path   string
		design bool
	}{{"POST /v1/ai/chat", false}, {"POST /v1/build/ai/chat", true}} {
		rt.handle(transport.Route{Pattern: endpoint.path, Summary: "Call an authorized model in the endpoint execution stage; the call is metered", Body: platform.ChatRequest{}, Answer: platform.ChatAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
			chat := t.Chat
			if endpoint.design {
				chat = t.DesignChat
			}
			var req platform.ChatRequest
			// A chat body carries a conversation and its tool declarations; the
			// platform bounds it like every other request body (review AI-03).
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, chatMaxBytes)).Decode(&req) != nil {
				Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
				return
			}
			if req.Stream { // server-sent events: delta, then done or error
				flusher, _ := w.(http.Flusher)
				started := false
				event := func(name string, v any) {
					if !started {
						w.Header().Set("Content-Type", "text/event-stream")
						w.Header().Set("Cache-Control", "no-cache")
						w.WriteHeader(http.StatusOK)
						started = true
					}
					raw, _ := json.Marshal(v)
					fmt.Fprintf(w, "event: %s\ndata: %s\n\n", name, raw)
					if flusher != nil {
						flusher.Flush()
					}
				}
				req.OnStream = func(piece platform.ChatStreamEvent) {
					if piece.ToolCall != nil {
						event("tool_delta", piece.ToolCall)
					}
				}
				answer, err, failure := chat(m, req, h.Now(), func(piece string) { event("delta", map[string]string{"content": piece}) })
				switch {
				case err != nil && !started:
					Reply(w, nil, err)
				case err != nil:
					event("error", map[string]any{"code": err.Code.String(), "detail": err.Message})
				case failure != nil && failure.Quota && !started:
					WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
				case failure != nil:
					event("error", map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": t.i18n.Say(t.Language(m, r), failure.Detail), "usage": answer.Usage})
				default:
					event("done", map[string]any{"usage": answer.Usage, "toolCalls": answer.ToolCalls})
				}
				return
			}
			answer, err, failure := chat(m, req, h.Now())
			switch {
			case err != nil:
				Reply(w, nil, err)
			case failure != nil && failure.Quota:
				WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
			case failure != nil:
				WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}, "usage": answer.Usage})
			default:
				WriteJSON(w, http.StatusOK, answer)
			}
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
	rt.handle(transport.Route{Pattern: "GET /v1/capabilities/calls/ai/{id}", Summary: "Read the original typed AI call result after member and source checks", Answer: platform.OperationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReadCapabilityAI(m, r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
}
