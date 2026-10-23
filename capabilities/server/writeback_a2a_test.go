package platformserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"platformserver/internal/construction"
	"platformserver/platform"
)

func TestDeclaredA2AWritebackKeepsOriginalMessageAndAcceptedAnswer(t *testing.T) {
	const tenant = "declared-a2a"
	compose := func() *Tenant { return builderTenant(t, tenant) }
	tn := compose()
	var entries []Entry
	tn.AcceptResult = func(entry Entry, _, _ string) ([]byte, error) {
		entries = append(entries, entry)
		return entry.Body, nil
	}
	tn.Secrets = func(name string) ([]byte, bool) { return []byte("Bearer fixture-inquiry"), name == "supplier-inquiry" }
	var mu sync.Mutex
	var ids []string
	requests := map[string]json.RawMessage{}
	loseAnswer := true
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			WriteJSON(w, http.StatusOK, map[string]bool{"ready": true})
			return
		}
		var rpc struct {
			ID, Method string
			Params     struct {
				Message struct {
					MessageID string `json:"messageId"`
					Parts     []struct {
						Data json.RawMessage `json:"data"`
					} `json:"parts"`
				} `json:"message"`
			} `json:"params"`
		}
		if json.NewDecoder(r.Body).Decode(&rpc) != nil || rpc.Method != "SendMessage" || rpc.ID == "" || rpc.Params.Message.MessageID != rpc.ID || len(rpc.Params.Message.Parts) != 2 || r.Header.Get("A2A-Version") != A2AVersion || r.Header.Get("Authorization") != "Bearer fixture-inquiry" {
			t.Error("the declared request changed the original A2A envelope or connection credential")
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
		mu.Lock()
		ids = append(ids, rpc.ID)
		body := rpc.Params.Message.Parts[1].Data
		if previous := requests[rpc.ID]; previous != nil && !sameCompositePayload(previous, body) {
			t.Error("the same message changed its frozen payload")
		}
		requests[rpc.ID] = body
		lost := loseAnswer
		loseAnswer = false
		mu.Unlock()
		if lost {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = connection.Close()
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": map[string]any{"task": map[string]any{"id": rpc.ID, "status": map[string]any{"state": "TASK_STATE_COMPLETED"}, "artifacts": []any{map[string]any{"parts": []any{map[string]string{"text": "Delivery in three days"}}}}}}})
	}))
	defer server.Close()
	typ := publishObject(t, tn, "builder", "INQUIRY", map[string]any{"name": "inquiry", "title": "Inquiry", "fields": []construction.Field{{Name: "question", Title: "Question", Type: "text", Required: true}, {Name: "answer", Title: "Supplier answer", Type: "longtext", SourceType: "external-input", Write: []string{construction.Builder}}}, "access": []construction.Access{{Role: construction.User, Read: "all", Create: true}}}, txnAt)
	decide(t, tn, "builder", "build", "build.connection.create", construction.ConnectionType, "supplier", map[string]any{"name": "supplier", "title": "Supplier agent", "kind": "http", "address": server.URL, "secret": "supplier-inquiry", "allowPrivate": true}, txnAt)
	decide(t, tn, "builder", "build", "build.connection.check", construction.ConnectionType, "supplier", nil, txnAt)
	tn.CheckConnections(txnAt)
	decide(t, tn, "builder", "build", "build.writeback.create", construction.WritebackType, "inquiry", map[string]any{"name": "asksupplier", "title": "Ask supplier", "connection": "supplier", "object": typ, "on": "create", "transport": "a2a", "method": "POST", "path": "rpc", "mapping": []construction.SourceField{{From: "question", To: "message"}}, "result": []construction.SourceField{{From: "artifacts.0.parts.0.text", To: "answer"}}}, txnAt)
	decide(t, tn, "builder", "build", "build.writeback.publish", construction.WritebackType, "inquiry", nil, txnAt)
	sub := submission(tn, memberOf(t, tn, "user"), "build", typ+".create", typ, "delivery", map[string]any{"question": "When can the supplier deliver this item?"})
	if _, err := tn.Submit(memberOf(t, tn, "user"), sub, txnAt); err != nil {
		t.Fatal(err)
	}
	// A subsequent publication cannot turn the pending A2A message into HTTP.
	decide(t, tn, "builder", "build", "build.writeback.edit", construction.WritebackType, "inquiry", map[string]any{"transport": "", "method": "PUT"}, txnAt)
	decide(t, tn, "builder", "build", "build.writeback.publish", construction.WritebackType, "inquiry", nil, txnAt)
	tn.Dispatch(txnAt)
	row, problem := tn.RecordOf(memberOf(t, tn, "user"), typ, "delivery", txnAt)
	if problem != nil || strings.Contains(string(platform.Raw(row.Record)), "Delivery in three days") {
		t.Fatal("an unconfirmed delivery answer became a business result")
	}
	tn.Dispatch(txnAt.Add(time.Minute))
	row, problem = tn.RecordOf(memberOf(t, tn, "user"), typ, "delivery", txnAt)
	if problem != nil || !strings.Contains(string(platform.Raw(row.Record)), "Delivery in three days") {
		t.Fatalf("the original callback did not keep the supplier answer: %s %v", platform.Raw(row.Record), problem)
	}
	mu.Lock()
	seen := append([]string(nil), ids...)
	unique := len(requests)
	mu.Unlock()
	if len(seen) != 2 || seen[0] != seen[1] || unique != 1 {
		t.Fatalf("the lost answer created a new inquiry: %v", seen)
	}
	count := len(entries)
	if _, err := tn.Submit(memberOf(t, tn, "user"), sub, txnAt.Add(2*time.Minute)); err != nil || len(entries) != count {
		t.Fatal("the original request was accepted twice")
	}
	CheckReplay(t, tn, entries, compose)
}

func TestDeclaredA2AWritebackRefusesIncompleteAnswersAndInvalidTransport(t *testing.T) {
	answer := `{"result":{"message":{"parts":[{"text":"Complete answer"}]}}}`
	for _, name := range []string{"oversized", "incomplete"} {
		t.Run(name, func(t *testing.T) {
			tn := builderTenant(t, "bounded-a2a")
			tn.Outbound = func(*http.Request, bool) (*http.Response, error) {
				var body io.Reader = strings.NewReader(answer + strings.Repeat(" ", 64<<10))
				if name == "incomplete" {
					body = io.MultiReader(strings.NewReader(answer), iotest.ErrReader(io.ErrUnexpectedEOF))
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(body)}, nil
			}
			req := construction.WritebackRequest{Transport: "a2a", Method: "POST", URL: "https://supplier.example/rpc", Body: map[string]any{"message": "Ask for availability"}}
			out := tn.sendWriteback(platform.Effect{ID: "inquiry", Body: string(platform.Raw(req))})
			if out.Result != "unknown" || len(out.Answer) != 0 {
				t.Fatalf("an incomplete answer was accepted: %+v", out)
			}
		})
	}
	for _, transport := range []string{"script", "a2a"} {
		request := construction.Writeback{Transport: transport, Method: "PUT"}
		if _, err := request.Request(construction.Connection{Kind: "http", Address: "https://supplier.example"}, "inquiry", platform.Raw(map[string]string{"question": "Availability?"})); err == nil {
			t.Fatal("an undeclared transport or A2A method was accepted")
		}
	}
}
