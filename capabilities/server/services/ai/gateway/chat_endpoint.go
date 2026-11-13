package aigateway

import (
	"encoding/json"
	"fmt"
	"net/http"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ChatEndpoint struct {
	Chat     func(req platform.ChatRequest, delta ...func(string)) (platform.ChatAnswer, *kernel.Error, *platform.AIError)
	MaxBytes int64
	Say      func(string) string
	Said     func(*kernel.Error) *kernel.Error
	Reply    func(http.ResponseWriter, *kernel.Error)
	JSON     func(http.ResponseWriter, int, any)
}

func (e ChatEndpoint) Serve(w http.ResponseWriter, r *http.Request) {
	var req platform.ChatRequest
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, e.MaxBytes)).Decode(&req) != nil {
		e.Reply(w, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT})
		return
	}
	if req.Stream {
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
		answer, err, failure := e.Chat(req, func(piece string) { event("delta", map[string]string{"content": piece}) })
		switch {
		case err != nil && !started:
			e.Reply(w, e.Said(err))
		case err != nil:
			event("error", map[string]any{"code": err.Code.String(), "detail": e.Say(err.Message)})
		case failure != nil && failure.Quota && !started:
			e.JSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": e.Say(failure.Detail)}})
		case failure != nil:
			event("error", map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": e.Say(failure.Detail), "usage": answer.Usage})
		default:
			event("done", map[string]any{"usage": answer.Usage, "toolCalls": answer.ToolCalls})
		}
		return
	}
	answer, err, failure := e.Chat(req)
	switch {
	case err != nil:
		e.Reply(w, e.Said(err))
	case failure != nil && failure.Quota:
		e.JSON(w, http.StatusTooManyRequests, map[string]any{"error": map[string]any{"code": "QUOTA", "detail": e.Say(failure.Detail)}})
	case failure != nil:
		e.JSON(w, http.StatusBadGateway, map[string]any{"error": map[string]any{"code": "PROVIDER_ERROR", "status": failure.Status, "detail": e.Say(failure.Detail)}, "usage": answer.Usage})
	default:
		e.JSON(w, http.StatusOK, answer)
	}
}
