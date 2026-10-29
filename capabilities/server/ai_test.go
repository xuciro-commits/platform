package platformserver

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"platformserver/engine/integrations"
	controlowner "platformserver/kernel/control"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/aigateway/providers"
	"platformserver/apps/ai"
	"platformserver/platform"
	"platformserver/studio/build"
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
			Messages []platform.Message
			Stream   bool
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
		if req.Stream && req.Model != "flat" { // server-sent events: the answer in two pieces, then the usage; "flat" never streams
			w.Header().Set("Content-Type", "text/event-stream")
			for _, piece := range []string{"echo: ", last} {
				fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", piece)
			}
			fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\ndata: [DONE]\n\n", len(last), len(last)+6)
			return
		}
		fmt.Fprintf(w, `{"model":"echo-v2","choices":[{"message":{"role":"assistant","content":%q}}],"usage":{"prompt_tokens":%d,"completion_tokens":%d,"cost":0.0001}}`,
			"echo: "+last, len(last), len(last)+6)
	}))
	approveTestInference(t, s.URL, "lm", "local")
	t.Cleanup(s.Close)
	return s
}

func TestModelUsageDistinguishesUnreportedAndZeroCost(t *testing.T) {
	for _, test := range []struct {
		name, body   string
		tokens, cost bool
	}{
		{"unreported", `{"choices":[{"message":{"content":"ok"}}]}`, false, false},
		{"tokens only", `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":0,"completion_tokens":0}}`, true, false},
		{"cost only", `{"choices":[{"message":{"content":"ok"}}],"usage":{"cost":0}}`, false, true},
		{"reported free", `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"cost":0}}`, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, usage, failure := providers.Completion(200, []byte(test.body), "probe")
			if failure != nil || usage.TokensReported != test.tokens || usage.CostReported != test.cost {
				t.Fatalf("provider usage presence changed: %+v %v", usage, failure)
			}
		})
	}
	_, _, failure := providers.Completion(200, []byte(`{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"cost":-1}}`), "probe")
	if failure == nil {
		t.Fatal("negative provider cost was accepted as a release measurement")
	}
}

// an administrator adds a local provider and enables models, each
// with its access; members call them through the host, and every call's usage
// is journaled and replayed without calling the model again.
func TestAIProviders(t *testing.T) {
	var calls atomic.Int32
	server := fakeModels(t, &calls)
	var journal []Entry
	build := func() *Tenant {
		console := newConsole("t-1",
			platform.Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{ai.ID: ai.Admin}}},
			platform.Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{ai.ID: ai.User}}},
			platform.Seat{Subjects: []string{"cy"}, Member: platform.Member{ID: "cy", Roles: map[string]string{}}},
			platform.Seat{Subjects: []string{"client:bot"}, Member: platform.Member{ID: "bot", Roles: map[string]string{}, Agent: true}})
		tn, err := NewTenant("t-1", console, ai.New("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member {
		m, _ := tn.app(controlowner.PlatformApp).(*controlowner.Console).MemberBySubject(id)
		return m
	}
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
		answer, err, failure := tn.Chat(m, platform.ChatRequest{Model: model, Messages: []platform.Message{{Role: "user", Content: text}}}, now)
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
	if visible := models.([]platform.AIModel); len(visible) != 1 || visible[0].Name() != "lm/busy" || visible[0].Access != "everyone" || visible[0].Revision != 1 {
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

// The Anthropic adapter: the official SDK against a stand-in
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
	console := newConsole("t-1", platform.Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{ai.ID: ai.Admin, "build": "builder"}}})
	tn, err := NewTenant("t-1", console, ai.New("t-1"), build.New("t-1"))
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
	ana, _ := console.MemberBySubject("ana")
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
	if kerr != nil || failure != nil || len(catalog) != 1 || catalog[0].Name != "Claude Opus 5" || catalog[0].Capabilities == nil || catalog[0].Capabilities.InputTokens == nil || *catalog[0].Capabilities.InputTokens != 1000000 || catalog[0].Capabilities.ContextTokens != nil {
		t.Fatalf("catalog %+v %v %v", catalog, kerr, failure)
	}
	answer, kerr, failure := tn.chatForStage(ana, platform.ChatRequest{Model: "claude/claude-opus-5", Messages: []platform.Message{
		{Role: "system", Content: "be brief"}, {Role: "user", Content: "hi"}, {Role: "assistant", Content: "hello"}, {Role: "user", Content: "again"}}}, now, platform.AIStageDesign)
	if kerr != nil || failure != nil || answer.Content != "be brief|again|3|8192" || answer.Usage.Input != 12 || answer.Usage.Output != 7 {
		t.Fatalf("answer %+v %v %v", answer, kerr, failure)
	}
	// The platform's ceiling, not the adapter's own default, is what an answer
	// asking for more is capped to (review AI-03); a smaller ask is kept.
	capped, _, _ := tn.chatForStage(ana, platform.ChatRequest{Model: "claude/claude-opus-5", MaxTokens: 100000, Messages: []platform.Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "x"}}}, now, platform.AIStageDesign)
	small, _, _ := tn.chatForStage(ana, platform.ChatRequest{Model: "claude/claude-opus-5", MaxTokens: 512, Messages: []platform.Message{{Role: "system", Content: "be brief"}, {Role: "user", Content: "x"}}}, now, platform.AIStageDesign)
	if !strings.HasSuffix(capped.Content, "|8192") || !strings.HasSuffix(small.Content, "|512") {
		t.Fatalf("max tokens ceiling: %q, %q", capped.Content, small.Content)
	}
	if _, _, failure := tn.chatForStage(ana, platform.ChatRequest{Model: "claude/claude-busy", Messages: []platform.Message{{Role: "user", Content: "x"}}}, now, platform.AIStageDesign); failure == nil ||
		failure.Status != 429 || failure.Detail != "rate limited" {
		t.Fatalf("a rate limit: %+v", failure)
	}
}

// Limits at the door every call passes — a person's tokens a
// day, anyone's calls a minute, a model's cap for the tenant, and one
// member's own limit — and an answer streamed as it comes, metered once.
func TestAILimits(t *testing.T) {
	var calls atomic.Int32
	server := fakeModels(t, &calls)
	var journal []Entry
	build := func() *Tenant {
		console := newConsole("t-1",
			platform.Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{ai.ID: ai.Admin, controlowner.PlatformApp: controlowner.Admin}}},
			platform.Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{}}},
			platform.Seat{Subjects: []string{"cy"}, Member: platform.Member{ID: "cy", Roles: map[string]string{}}})
		tn, err := NewTenant("t-1", console, ai.New("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.Member(id); return m }
	now := time.Date(2026, 9, 27, 9, 0, 0, 0, time.UTC)
	keys := 0
	decide := func(authority, schema, typ, id string, payload any) {
		t.Helper()
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member("ana"), &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			t.Fatalf("%s %s: %v", schema, id, err)
		}
	}
	chat := func(who string) string {
		_, err, failure := tn.Chat(member(who), platform.ChatRequest{Model: "local/echo", MaxTokens: 11, Messages: []platform.Message{{Role: "user", Content: "hello"}}}, now)
		switch {
		case err != nil:
			return err.Error()
		case failure != nil && failure.Quota:
			return "quota: " + failure.Detail
		case failure != nil:
			return failure.Detail
		}
		return "ok"
	}
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: %s, want %s", what, got, want)
		}
	}
	decide(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": server.URL + "/v1"})
	decide(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]string{"access": "everyone"})
	decide(controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, ai.ID+"/"+ai.SettingDailyTokens, map[string]string{"value": "136"})

	// Each call costs 16 reported tokens. The fourth cannot reserve its
	// input bound and maximum output within the remaining daily budget.
	for i := 0; i < 3; i++ {
		expect("bo's call", chat("bo"), "ok")
	}
	before := calls.Load()
	expect("past the day", chat("bo"), "quota: bo used its 136 tokens for today")
	quota := tn.ai.Quota(member("bo"), tn.limits(), now)
	if quota.DailyTokens != 136 || quota.UsedTokens != 48 || quota.UsedCalls != 3 {
		t.Fatalf("quota view differs from admission: %+v", quota)
	}
	expect("the model was not called", fmt.Sprint(calls.Load()-before), "0")
	decide(ai.ID, ai.SchemaLimitSet, ai.LimitType, "bo", map[string]int{"dailyTokens": 1000})
	expect("his own limit", chat("bo"), "ok")
	if quota := tn.ai.Quota(member("bo"), tn.limits(), now); quota.DailyTokens != 1000 || quota.UsedTokens != 64 {
		t.Fatalf("quota did not follow the original limit override: %+v", quota)
	}
	expect("said in Chinese", tn.i18n.Say("zh-CN", "bo used its 136 tokens for today"), "bo 今天的 136 个 token 已用完")

	// Calls a minute, whatever the tokens.
	decide(controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, ai.ID+"/"+ai.SettingPerMinute, map[string]string{"value": "2"})
	decide(controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, ai.ID+"/"+ai.SettingDailyTokens, map[string]string{"value": "0"})
	expect("cy 1", chat("cy"), "ok")
	expect("cy 2", chat("cy"), "ok")
	expect("cy 3", chat("cy"), "quota: cy made its 2 calls this minute")
	now = now.Add(time.Minute)
	expect("a minute later", chat("cy"), "ok")

	// A model's cap for the whole tenant.
	decide(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone", "dailyTokens": 100})
	expect("the model's day", chat("cy"), "quota: The tenant used the 100 tokens a day of local/echo")
	decide(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone"})

	// Streamed: the pieces as they come, the whole answer, one usage entry.
	var pieces []string
	usage := len(journal)
	answer, err, failure := tn.Chat(member("cy"), platform.ChatRequest{Model: "local/echo", Messages: []platform.Message{{Role: "user", Content: "stream"}}}, now.Add(time.Minute),
		func(piece string) { pieces = append(pieces, piece) })
	if err != nil || failure != nil {
		t.Fatalf("stream: %v %v", err, failure)
	}
	expect("streamed", fmt.Sprint(pieces, " ", answer.Content, " ", answer.Usage.Input+answer.Usage.Output, " ", len(journal)-usage), "[echo:  stream] echo: stream 18 2")

	// A server that does not stream answers at once, and the answer comes as one piece.
	decide(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/flat", map[string]string{"access": "everyone"})
	pieces = nil
	answer, _, failure = tn.Chat(member("bo"), platform.ChatRequest{Model: "local/flat", MaxTokens: 8, Messages: []platform.Message{{Role: "user", Content: "hi"}}}, now.Add(2*time.Minute),
		func(piece string) { pieces = append(pieces, piece) })
	expect("flat", fmt.Sprint(pieces, " ", failure == nil), "[echo: hi] true")

	CheckReplay(t, tn, journal, build)
}

func TestAIReservationAdmissionSettlementAndRecovery(t *testing.T) {
	at := time.Date(2026, 10, 28, 10, 0, 0, 0, time.UTC)
	started, release := make(chan struct{}, 4), make(chan struct{}, 4)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		started <- struct{}{}
		<-release
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":1}}`)
	}))
	defer server.Close()
	approveTestInference(t, server.URL, "local")
	const id = "reservation-concurrent"
	build := func() *Tenant {
		return composeTenant(t, id, []platform.Seat{seatOf("ana", "ai:admin", "platform:admin")}, ai.New(id))
	}
	tenant := build()
	ana := memberOf(t, tenant, "ana")
	decide(t, tenant, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": server.URL}, at)
	decide(t, tenant, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone", "dailyTokens": 160}, at)
	request := platform.ChatRequest{Call: "original-call", Model: "local/echo", MaxTokens: 10, Messages: []platform.Message{{Role: "user", Content: "hello"}}}
	done := make(chan error, 1)
	go func() {
		_, err, failure := tenant.Chat(ana, request, at)
		if err != nil {
			done <- err
		} else if failure != nil {
			done <- failure
		} else {
			done <- nil
		}
	}()
	select {
	case <-started:
	case <-time.After(10 * time.Second):
		t.Fatal("first call did not reach the provider")
	}
	q := tenant.ai.Quota(ana, tenant.limits(), at)
	if q.UnsettledCalls != 1 || q.ReservedTokens <= 10 || q.UsedTokens != 0 {
		t.Errorf("inflight call was not reserved: %+v", q)
	}
	second := request
	second.Call = "other-call"
	_, err, failure := tenant.Chat(ana, second, at.Add(2*time.Minute))
	if err != nil || failure == nil || !failure.Quota || calls.Load() != 1 {
		t.Errorf("concurrent call escaped the token cap after a minute: %v %v calls=%d", err, failure, calls.Load())
	}
	duplicate := request
	duplicate.Messages = []platform.Message{{Role: "user", Content: "other"}}
	if _, err, failure := tenant.Chat(ana, duplicate, at); err != nil || failure == nil || !strings.Contains(failure.Detail, "different input") || calls.Load() != 1 {
		t.Errorf("same-key changed input was accepted: %v %v", err, failure)
	}
	// Snapshot while actual I/O is still unresolved. Restore must keep its
	// liability, and cannot dispatch the call just because the process changed.
	snapshot, _, snapshotErr := tenant.Snapshot(func() int64 { return 0 })
	if snapshotErr != nil {
		t.Error(snapshotErr)
	}
	restored := build()
	if err := restored.Restore(snapshot); err != nil {
		t.Error(err)
	}
	if actual := restored.ai.Quota(ana, restored.limits(), at.Add(24*time.Hour)); actual.ReservedTokens != q.ReservedTokens || actual.UnsettledCalls != 1 {
		t.Errorf("recovery or midnight forgot unresolved cost: %+v", actual)
	}
	release <- struct{}{}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	q = tenant.ai.Quota(ana, tenant.limits(), at)
	if q.ReservedTokens != 0 || q.UnsettledCalls != 0 || q.UsedTokens != 3 {
		t.Fatal("reported usage failed to replace its own reservation", q)
	}
	// Reservation failure is durable before any external call can occur.
	tenant.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append unavailable") }
	_, _, failure = tenant.Chat(ana, second, at.Add(time.Minute))
	if failure == nil || calls.Load() != 1 || tenant.ai.Quota(ana, tenant.limits(), at).UnsettledCalls != 0 {
		t.Fatal("failed reservation append escaped to provider", failure)
	}
}

func TestAIReservationUnknownAndOverreportedUsage(t *testing.T) {
	at := time.Date(2026, 10, 28, 11, 0, 0, 0, time.UTC)
	for _, kind := range []string{"unknown", "exceeded"} {
		t.Run(kind, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if kind == "unknown" {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
				} else {
					fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":2,"completion_tokens":50,"cost":0.25}}`)
				}
			}))
			defer server.Close()
			approveTestInference(t, server.URL, "local")
			id := "reservation-" + kind
			build := func() *Tenant { return composeTenant(t, id, []platform.Seat{seatOf("ana", "ai:admin")}, ai.New(id)) }
			tenant := build()
			ana := memberOf(t, tenant, "ana")
			decide(t, tenant, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": server.URL}, at)
			decide(t, tenant, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone"}, at)
			var entries []Entry
			before, _, err := tenant.Snapshot(func() int64 { return 0 })
			if err != nil {
				t.Fatal(err)
			}
			tenant.Record = func(e Entry) { entries = append(entries, e) }
			req := platform.ChatRequest{Run: "bounded-run", RunTokens: 160, Call: "original-call", Model: "local/echo", MaxTokens: 10, Messages: []platform.Message{{Role: "user", Content: "hello"}}}
			answer, kerr, failure := tenant.Chat(ana, req, at)
			if kerr != nil || (kind == "unknown" && failure != nil) || (kind == "exceeded" && failure == nil) {
				t.Fatal(kerr, failure)
			}
			r, ok := tenant.ai.Reservation(req.Call)
			if !ok || r.State != kind {
				t.Fatalf("unknown or excess usage was silently released: %+v", r)
			}
			if kind == "unknown" && (!answer.Usage.TokensEstimated || answer.Usage.CostReported || tenant.ai.Quota(ana, tenant.limits(), at).ReservedTokens == 0) {
				t.Fatal("unknown usage or cost was called free", answer.Usage)
			}
			if kind == "exceeded" && tenant.ai.Quota(ana, tenant.limits(), at).ReservedTokens != 0 {
				t.Fatal("known excess usage retained a fictional token liability")
			}
			if kind == "exceeded" && (answer.Usage.Output != 50 || answer.Usage.Cost != 0.25) {
				t.Fatal("provider excess was hidden", answer.Usage)
			}
			for _, next := range []platform.ChatRequest{req, {Call: "other-call", Model: req.Model, MaxTokens: 10, Messages: req.Messages}} {
				if next.Call != "original-call" {
					next.Run, next.RunTokens = req.Run, 160-answer.Usage.Input-answer.Usage.Output
				}
				if _, kerr, failure := tenant.Chat(ana, next, at.Add(2*time.Minute)); kerr != nil || failure == nil || calls.Load() != 1 {
					t.Fatal("duplicate call or provider excess was redispatched", kerr, failure, calls.Load())
				}
			}
			if err := tenant.meter(ana, answer.Usage); err != nil || len(tenant.ai.Usage()) != 1 {
				t.Fatal("retry metered a call twice", err)
			}
			image, _ := tenant.ai.(*ai.AI).AcceptedState()
			recovered := build()
			if err := recovered.Restore(before); err != nil {
				t.Fatal(err)
			}
			if err := recovered.Replay(entries); err != nil {
				t.Fatal(err)
			}
			actual, _ := recovered.ai.(*ai.AI).AcceptedState()
			if !bytes.Equal(image, actual) {
				t.Fatal("replay changed reservation/usage", string(actual))
			}
			if _, kerr := tenant.Input(ana, platform.AIReserveCall, []byte(`{}`), at); kerr == nil {
				t.Fatal("administrator acquired the private meter input")
			}
		})
	}
}

func TestAIReservationReleasesOnlyUndispatchedCalls(t *testing.T) {
	at := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	tenant := composeTenant(t, "reservation-release", []platform.Seat{seatOf("ana", "ai:admin", "platform:admin")}, ai.New("reservation-release"))
	ana := memberOf(t, tenant, "ana")
	approveTenantInference(t, tenant, "http://localhost:1", "local")
	decide(t, tenant, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://localhost:1", "secret": "unconfigured-model-key"}, at)
	decide(t, tenant, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone"}, at)
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		t.Fatal("missing credentials reached external I/O")
		return nil, nil
	}
	req := platform.ChatRequest{Model: "local/echo", MaxTokens: 10, Messages: []platform.Message{{Role: "user", Content: "hello"}}}
	if _, err, failure := tenant.Chat(ana, req, at); err != nil || failure == nil {
		t.Fatal("missing credentials were not reported", err, failure)
	}
	if q := tenant.ai.Quota(ana, tenant.limits(), at); q.ReservedTokens != 0 || q.UnsettledCalls != 0 || q.UsedCalls != 0 || len(tenant.ai.Usage()) != 0 {
		t.Fatal("undispatched call was charged or retained", q)
	}
	decide(t, tenant, "ana", controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "ai/"+ai.SettingMaxTokens, map[string]string{"value": "0"}, at)
	req.MaxTokens = 0
	if _, err, failure := tenant.Chat(ana, req, at); err != nil || failure == nil || !strings.Contains(failure.Detail, "positive generation") {
		t.Fatal("unbounded call acquired a reservation", err, failure)
	}
	if q := tenant.ai.Quota(ana, tenant.limits(), at); q.UnsettledCalls != 0 {
		t.Fatal("invalid request left a reservation", q)
	}
}

func TestAIHalfOpenProbeSurvivesReadinessAndBudgetRefusal(t *testing.T) {
	at := time.Date(2026, 10, 28, 13, 0, 0, 0, time.UTC)
	var calls atomic.Int32
	server := fakeModels(t, &calls)
	tenant := composeTenant(t, "probe-reservation", []platform.Seat{seatOf("ana", "ai:admin")}, ai.New("probe-reservation"))
	decide(t, tenant, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": server.URL}, at)
	decide(t, tenant, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone", "dailyTokens": 1}, at)
	for range integrations.BreakerAfter {
		tenant.breakers.Report("ai:local", false, at)
	}
	ready := at.Add(integrations.BreakerPause)
	for range 2 {
		if !tenant.breakers.Allow("ai:local", ready, false) {
			t.Fatal("readiness consumed a probe")
		}
	}
	req := platform.ChatRequest{Model: "local/echo", MaxTokens: 16, Messages: []platform.Message{{Role: "user", Content: "hello"}}}
	if _, _, failure := tenant.Chat(memberOf(t, tenant, "ana"), req, ready); failure == nil || !failure.Quota || calls.Load() != 0 {
		t.Fatal("budget refusal consumed a provider call", failure)
	}
	decide(t, tenant, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/echo", map[string]any{"access": "everyone"}, ready)
	if _, err, failure := tenant.Chat(memberOf(t, tenant, "ana"), req, ready); err != nil || failure != nil || calls.Load() != 1 {
		t.Fatal("half-open provider could not recover", err, failure, calls.Load())
	}
	if len(tenant.Breakers(ready)) != 0 {
		t.Fatal("successful probe did not close the breaker")
	}
}
