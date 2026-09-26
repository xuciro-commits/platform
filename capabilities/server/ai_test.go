package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/platform"
)

// fakeModels speaks the OpenAI wire as LM Studio, Ollama or OpenRouter do:
// "echo" repeats the last message, "busy" is rate-limited upstream.
func fakeModels(t *testing.T, calls *atomic.Int32) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			fmt.Fprint(w, `{"data":[{"id":"echo","name":"Echo","context_length":4096,"pricing":{"prompt":"0","completion":"0"}},{"id":"busy"}]}`)
			return
		}
		calls.Add(1)
		var req struct {
			Model    string
			Messages []Message
		}
		json.NewDecoder(r.Body).Decode(&req)
		if r.Header.Get("Authorization") != "" {
			w.WriteHeader(http.StatusUnauthorized) // a local server needs no key
			return
		}
		if req.Model == "busy" {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":{"message":"busy is temporarily rate-limited upstream"}}`)
			return
		}
		last := req.Messages[len(req.Messages)-1].Content
		fmt.Fprintf(w, `{"model":"echo-v2","choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"cost":0.0001}}`,
			"echo: "+last, len(last), len(last)+6)
	}))
	t.Cleanup(s.Close)
	return s
}

// ADR-0015: an administrator adds a local provider and enables models, each
// with its access; members call them through the host, and every call's usage
// is journaled and replayed without calling the model again.
func TestAIProviders(t *testing.T) {
	var calls atomic.Int32
	server := fakeModels(t, &calls)
	var journal []Entry
	build := func() *Tenant {
		console := NewConsole("t-1",
			Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{ai.ID: ai.Admin}}},
			Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{ai.ID: ai.User}}},
			Seat{Subjects: []string{"cy"}, Member: platform.Member{ID: "cy", Roles: map[string]string{}}},
			Seat{Subjects: []string{"client:bot"}, Member: platform.Member{ID: "bot", Roles: map[string]string{}, Agent: true}})
		tn, err := NewTenant("t-1", console, ai.New("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	ana, bo, cy, bot := member("ana"), member("bo"), member("cy"), member("bot")
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	keys := 0
	decide := func(m platform.Member, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(m, &pb.Submission{TenantId: "t-1", PrincipalId: m.ID, Authority: ai.ID, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	for _, c := range []struct {
		who     platform.Member
		id      string
		payload map[string]any
		want    string
	}{
		{bo, "lm", map[string]any{"kind": "local", "baseUrl": server.URL + "/v1"}, "ERROR_CODE_POLICY_DENIED"},
		{ana, "or", map[string]any{"kind": "vendor", "vendor": "openrouter"}, "ERROR_CODE_INVALID_ARGUMENT"},         // a vendor needs a key
		{ana, "x", map[string]any{"kind": "vendor", "vendor": "nope", "secret": "k"}, "ERROR_CODE_INVALID_ARGUMENT"}, // unknown vendor
		{ana, "tp", map[string]any{"kind": "compatible", "baseUrl": "http://api.example.com/v1", "secret": "k"}, "ERROR_CODE_INVALID_ARGUMENT"},
		{ana, "or", map[string]any{"kind": "vendor", "vendor": "openrouter", "secret": "openrouter"}, "ok"},
		{ana, "lm", map[string]any{"kind": "local", "baseUrl": server.URL + "/v1/"}, "ok"},
	} {
		if got := decide(c.who, ai.SchemaProviderAdd, ai.ProviderType, c.id, c.payload); got != c.want {
			t.Fatalf("add %s %v: %s, want %s", c.id, c.payload, got, c.want)
		}
	}
	// The catalog is read live, for administrators.
	catalog, err, failure := tn.ProviderModels(ana, "lm", false)
	if err != nil || failure != nil || len(catalog) != 2 || !catalog[1].Free || catalog[1].Context != 4096 {
		t.Fatalf("catalog %+v %v %v", catalog, err, failure)
	}
	if _, err, _ := tn.ProviderModels(bo, "lm", false); err == nil {
		t.Fatal("a user read a provider's catalog")
	}
	for _, c := range []struct{ id, access, want string }{
		{"lm/echo", "users", "ok"}, {"lm/busy", "everyone", "ok"}, {"nope/echo", "users", "ERROR_CODE_NOT_FOUND"}, {"lm/echo", "all", "ERROR_CODE_INVALID_ARGUMENT"},
	} {
		if got := decide(ana, ai.SchemaModelEnable, ai.ModelType, c.id, map[string]string{"access": c.access}); got != c.want {
			t.Fatalf("enable %s: %s", c.id, got)
		}
	}
	chat := func(m platform.Member, model, text string) string {
		answer, err, failure := tn.Chat(m, ChatRequest{Model: model, Messages: []Message{{Role: "user", Content: text}}}, now)
		switch {
		case err != nil:
			return err.Error()
		case failure != nil:
			return fmt.Sprint(failure.Status, " ", failure.Detail)
		}
		return answer.Content
	}
	for _, c := range []struct {
		who         platform.Member
		model, want string
	}{
		{bo, "lm/echo", "echo: hello"},
		{cy, "lm/echo", "ERROR_CODE_POLICY_DENIED"},  // open to users only
		{bot, "lm/echo", "ERROR_CODE_POLICY_DENIED"}, // agents too
		{cy, "lm/busy", "429 busy is temporarily rate-limited upstream"},
		{bo, "lm/other", "ERROR_CODE_NOT_FOUND"}, // not enabled
		{ana, "lm/echo", "echo: hello"},
	} {
		if got := chat(c.who, c.model, "hello"); got != c.want {
			t.Fatalf("%s calls %s: %q, want %q", c.who.ID, c.model, got, c.want)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("%d calls reached the provider", calls.Load())
	}
	usage := func(m platform.Member) map[string]any {
		out, _ := tn.Read(m, "ai-usage")
		raw, _ := json.Marshal(out)
		var v map[string]any
		json.Unmarshal(raw, &v)
		return v
	}
	all, own := usage(ana), usage(bo)
	if len(all["calls"].([]any)) != 3 || len(own["calls"].([]any)) != 1 {
		t.Fatalf("usage: all %v, bo's %v", all, own)
	}
	first := own["calls"].([]any)[0].(map[string]any)
	if first["input"] != 5.0 || first["output"] != 11.0 || first["served"] != "echo-v2" || first["cost"] != 0.0001 {
		t.Fatalf("bo's call %v", first)
	}
	if !strings.Contains(fmt.Sprint(all["totals"]), "failed:1") {
		t.Fatalf("totals %v", all["totals"])
	}
	// Models a member may call; removing a provider disables its models.
	models, _ := tn.Read(cy, "ai-models")
	if fmt.Sprint(models) != "[{lm busy everyone}]" {
		t.Fatalf("cy's models %v", models)
	}
	CheckReplay(t, tn, journal, build)
	if got := decide(ana, ai.SchemaProviderRemove, ai.ProviderType, "lm", struct{}{}); got != "ok" {
		t.Fatal(got)
	}
	if got := chat(ana, "lm/echo", "again"); got != "ERROR_CODE_NOT_FOUND" {
		t.Fatalf("after removal: %s", got)
	}
}

// The Anthropic adapter (ADR-0015 point 3): the official SDK against a stand-in
// of the Messages and Models APIs, reached through the host's client.
func TestAnthropicProvider(t *testing.T) {
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("x-api-key") != "sk-ant-test" || r.Header.Get("anthropic-version") == "" {
			w.WriteHeader(http.StatusUnauthorized)
			fmt.Fprint(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key"}}`)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			fmt.Fprint(w, `{"data":[{"type":"model","id":"claude-opus-5","display_name":"Claude Opus 5","created_at":"2026-01-01T00:00:00Z","max_input_tokens":1000000}],"has_more":false,"first_id":"claude-opus-5","last_id":"claude-opus-5"}`)
		case "/v1/messages":
			var req struct {
				Model     string
				MaxTokens int `json:"max_tokens"`
				System    []struct{ Text string }
				Messages  []struct {
					Role    string
					Content []struct{ Text string }
				}
			}
			json.NewDecoder(r.Body).Decode(&req)
			if req.Model == "claude-busy" {
				w.WriteHeader(http.StatusTooManyRequests)
				fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"rate limited"}}`)
				return
			}
			text := fmt.Sprintf("%s|%s|%d|%d", req.System[0].Text, req.Messages[len(req.Messages)-1].Content[0].Text, len(req.Messages), req.MaxTokens)
			fmt.Fprintf(w, `{"id":"msg_1","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":%q}],"stop_reason":"end_turn","stop_sequence":null,"usage":{"input_tokens":12,"output_tokens":7}}`, req.Model, text)
		}
	}))
	defer api.Close()
	target, _ := url.Parse(api.URL)
	console := NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{ai.ID: ai.Admin}}})
	tn, err := NewTenant("t-1", console, ai.New("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Secrets = func(name string) ([]byte, bool) { return []byte("sk-ant-test"), name == "anthropic" }
	tn.AIClient = func(r *http.Request) (*http.Response, error) { // the vendor's fixed URL, answered by the stand-in
		if r.URL.Host != "api.anthropic.com" {
			return nil, fmt.Errorf("called %s", r.URL.Host)
		}
		r.URL.Scheme, r.URL.Host = target.Scheme, target.Host
		return http.DefaultTransport.RoundTrip(r)
	}
	ana, _ := console.Member("ana")
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	decide := func(schema, typ, id, payload, key string) {
		t.Helper()
		if _, err := tn.Submit(ana, &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: ai.ID, IdempotencyKey: key,
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: []byte(payload)}, now); err != nil {
			t.Fatal(err)
		}
	}
	decide(ai.SchemaProviderAdd, ai.ProviderType, "claude", `{"kind":"vendor","vendor":"anthropic","secret":"anthropic"}`, "k1")
	decide(ai.SchemaModelEnable, ai.ModelType, "claude/claude-opus-5", `{"access":"everyone"}`, "k2")
	decide(ai.SchemaModelEnable, ai.ModelType, "claude/claude-busy", `{"access":"everyone"}`, "k3")
	catalog, kerr, failure := tn.ProviderModels(ana, "claude", true)
	if kerr != nil || failure != nil || len(catalog) != 1 || catalog[0].Name != "Claude Opus 5" || catalog[0].Context != 1000000 {
		t.Fatalf("catalog %+v %v %v", catalog, kerr, failure)
	}
	answer, kerr, failure := tn.Chat(ana, ChatRequest{Model: "claude/claude-opus-5", Messages: []Message{
		{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}, {Role: "user", Content: "again"}}}, now)
	if kerr != nil || failure != nil || answer.Content != "be brief|again|3|16000" || answer.Usage.Input != 12 || answer.Usage.Output != 7 {
		t.Fatalf("answer %+v %v %v", answer, kerr, failure)
	}
	if _, _, failure := tn.Chat(ana, ChatRequest{Model: "claude/claude-busy", Messages: []Message{{Role: "user", Content: "x"}}}, now); failure == nil ||
		failure.Status != 429 || failure.Detail != "rate limited" {
		t.Fatalf("a rate limit: %+v", failure)
	}
}
