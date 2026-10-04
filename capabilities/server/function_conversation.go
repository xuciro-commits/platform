package platformserver

import (
	"encoding/json"
	"fmt"
	"platformserver/apps/build"
	"platformserver/platform"
	"strings"
	"unicode/utf8"
)

// The caller holds store.mu. History consists of accepted call identities;
// assistant text is read from the original owner, never supplied by a client.
func (t *Tenant) functionConversation(store *recordStore, member platform.Member, request platform.FunctionRequest, f platform.AIFunction, owner, source, definition string, version int, input map[string]any) (any, []string, error) {
	if !f.Conversation {
		if request.Question != "" || len(request.History) > 0 {
			return nil, nil, fmt.Errorf("this function has no conversation input")
		}
		return input, nil, nil
	}
	if strings.TrimSpace(request.Question) == "" || len(request.Question) > 4096 || !utf8.ValidString(request.Question) || len(request.History) > 8 {
		return nil, nil, fmt.Errorf("conversation needs a bounded question and history")
	}
	if request.Reply != build.SchemaFunctionAnswer {
		return nil, nil, fmt.Errorf("conversation history needs the original function call owner")
	}
	type turn struct {
		Question string          `json:"question"`
		Answer   json.RawMessage `json:"answer"`
	}
	history := []turn{}
	refs := []string{}
	seen := map[string]bool{}
	et := store.types[build.FunctionCallType]
	for _, id := range request.History {
		if id == "" || len(id) > 1024 || seen[id] || et == nil {
			return nil, nil, fmt.Errorf("conversation history is unavailable")
		}
		seen[id] = true
		row := et.rows[id]
		if row == nil {
			return nil, nil, fmt.Errorf("conversation history is unavailable")
		}
		run, ok := row.value.Interface().(build.FunctionRun)
		if !ok || run.Archived || run.State != "ready" || run.Member != member.ID || run.App != owner || run.Function != f.Name || run.Version != version || run.Definition != definition || run.Source != source || f.ValidateOutput([]byte(run.Output)) != nil {
			return nil, nil, fmt.Errorf("conversation history belongs to another context")
		}
		history = append(history, turn{Question: run.Question, Answer: json.RawMessage(run.Output)})
		refs = append(refs, build.FunctionCallType+"/"+id+"#question", build.FunctionCallType+"/"+id+"#output")
	}
	return struct {
		Record   map[string]any `json:"record"`
		Question string         `json:"question"`
		History  []turn         `json:"history"`
	}{input, request.Question, history}, refs, nil
}
