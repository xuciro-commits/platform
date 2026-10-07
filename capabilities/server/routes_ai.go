package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

// routesAI serves model calls and the AI catalog.
func (h *Host) routesAI(rt *routes) {
	rt.handle(Route{Pattern: "POST /v1/ai/chat", Summary: "Call a model the caller may use; the call is metered (ADR-0015)", Body: ChatRequest{}, Answer: ChatAnswer{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		var req ChatRequest
		// A chat body carries a conversation and its tool declarations; the
		// platform bounds it like every other request body (review AI-03).
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, chatMaxBytes)).Decode(&req) != nil {
			Reply(w, nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
			return
		}
		if req.Stream { // server-sent events: delta, then done or error (ADR-0029 D2)
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
			answer, err, failure := t.Chat(m, req, h.Now(), func(piece string) { event("delta", map[string]string{"content": piece}) })
			switch {
			case err != nil && !started:
				Reply(w, nil, err)
			case failure != nil && failure.Quota && !started:
				WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
			case failure != nil:
				event("error", map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": failure.Detail, "usage": answer.Usage})
			default:
				event("done", map[string]any{"usage": answer.Usage})
			}
			return
		}
		answer, err, failure := t.Chat(m, req, h.Now())
		switch {
		case err != nil:
			Reply(w, nil, err)
		case failure != nil && failure.Quota:
			WriteJSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": t.i18n.Say(t.Language(m, r), failure.Detail)}})
		case failure != nil:
			WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": failure.Detail}, "usage": answer.Usage})
		default:
			WriteJSON(w, http.StatusOK, answer)
		}
	})
	rt.handle(Route{Pattern: "GET /v1/ai/providers/{id}/models", Summary: "A provider's catalog of models", Answer: []CatalogModel{}, Query: []Param{{"refresh", "true reads the catalog from the provider again"}}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		models, err, failure := t.ProviderModels(m, r.PathValue("id"), r.URL.Query().Get("refresh") == "true")
		switch {
		case err != nil:
			Reply(w, nil, err)
		case failure != nil:
			WriteJSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": failure.Detail}})
		default:
			WriteJSON(w, http.StatusOK, models)
		}
	})
	rt.handle(Route{Pattern: "GET /v1/ai/vendors", Summary: "The vendors a provider may be", Answer: []ai.Vendor{}}, func(w http.ResponseWriter, _ *http.Request, _ platform.Member, _ *Tenant) {
		WriteJSON(w, http.StatusOK, ai.Vendors)
	})
	rt.handle(Route{Pattern: "GET /v1/capabilities/calls/ai/{id}", Summary: "Read the original typed AI call result after member and source checks", Answer: platform.OperationResult{}}, func(w http.ResponseWriter, r *http.Request, m platform.Member, t *Tenant) {
		answer, err := t.ReadCapabilityAI(m, r.PathValue("id"), h.Now())
		if err != nil {
			Reply(w, nil, err)
			return
		}
		WriteJSON(w, http.StatusOK, answer)
	})
}
