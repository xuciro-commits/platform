package platformserver

import (
	"encoding/json"
	"fmt"
	"github.com/anthropics/anthropic-sdk-go"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/work"
	"platformserver/journal"
	"platformserver/platform"
)

func TestKnowledgeConfigurationResumesPreparedVectorsAfterRestart(t *testing.T) {
	at := time.Now().UTC()
	calls := 0
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 2 {
			w.WriteHeader(503)
			fmt.Fprint(w, `{"error":"temporary failure"}`)
			return
		}
		var body struct {
			Input []string `json:"input"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		data := []map[string]any{}
		for i := range body.Input {
			data = append(data, map[string]any{"index": i, "embedding": []float32{1, 2}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data, "usage": map[string]int{"prompt_tokens": len(body.Input)}})
	}))
	defer service.Close()
	build := func() *Tenant {
		tn := composeTenant(t, "vector-restart", []Seat{seatOf("admin", "platform:admin", "ai:admin", "knowledge:editor")}, ai.New("vector-restart"), knowledge.New("vector-restart"))
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
		return tn
	}
	directory := t.TempDir()
	store, err := journal.OpenFile(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	tn := build()
	tn.Store = store
	decide(t, tn, "admin", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": service.URL}, at)
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/embed", map[string]any{"access": "users", "capabilities": platform.ModelCapabilities{Source: "synthetic embedding fixture", ObservedAt: at, Purposes: []string{"embedding"}}}, at)
	for i := 0; i < embedBatch+1; i++ {
		decide(t, tn, "admin", knowledge.ID, knowledge.DocumentType+".create", knowledge.DocumentType, fmt.Sprint(i), map[string]any{"title": fmt.Sprintf("Document %d", i), "text": fmt.Sprintf("Knowledge passage %d", i)}, at)
	}
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "knowledge/embedding-next-model", map[string]any{"value": "local/embed"}, at)
	tn.Embed(at)
	status := tn.knowledgeIndexStatus()
	if status.Completed != embedBatch || status.Total != embedBatch+1 || status.Ready || status.Problem == "" {
		t.Fatal("interrupted rebuild did not retain progress", status)
	}
	image, _, err := tn.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = journal.OpenFile(directory, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restored := build()
	restored.Store = store
	if err := restored.Restore(image); err != nil {
		t.Fatal(err)
	}
	restored.Embed(at.Add(time.Second))
	status = restored.knowledgeIndexStatus()
	if !status.Ready || status.Completed != embedBatch+1 || calls != 3 {
		t.Fatal("restart repeated completed embeddings or lost the target", status, calls)
	}
	decide(t, restored, "admin", PlatformApp, SchemaSettingSet, SettingType, "knowledge/embedding-model", map[string]any{"value": "local/embed"}, at)
	status = restored.knowledgeIndexStatus()
	if status.Active != "local/embed" || status.Preparing != "" || status.NeedsRebuild {
		t.Fatal("prepared index activation did not retain its space", status)
	}
	decide(t, restored, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/embed", map[string]any{"access": "users", "dailyTokens": 10000}, at)
	if !restored.knowledgeIndexStatus().NeedsRebuild {
		t.Fatal("a changed model silently switched the active vector space")
	}
	restored.Embed(at.Add(2 * time.Second))
	admin := memberOf(t, restored, "admin")
	if len(restored.Knowledge(&admin, "", "Knowledge", 3, at)) == 0 || calls != 3 {
		t.Fatal("reconfiguration lost keyword search or automatically called the changed model", calls)
	}
}

func TestAnthropicAdaptiveConfigurationProtectsThinkingState(t *testing.T) {
	var sent map[string]any
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&sent)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"m","type":"message","role":"assistant","model":"fixture","content":[{"type":"thinking","thinking":"private reasoning","signature":"signed"},{"type":"text","text":"visible"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":2}}`)
	}))
	defer service.Close()
	tn := composeTenant(t, "anthropic-options", []Seat{seatOf("admin", "ai:admin")}, ai.New("anthropic-options"))
	tn.Secrets = func(string) ([]byte, bool) { return []byte("synthetic-credential"), true }
	request := ChatRequest{Options: platform.ModelOptions{Thinking: "adaptive", Effort: "high"}, MaxTokens: 128, Messages: []Message{
		{Role: "assistant", Content: "previous", Protocol: json.RawMessage(`[{"type":"thinking","thinking":"retained private state","signature":"signed"}]`)},
		{Role: "user", Content: "continue"},
	}}
	answer, _, failure := tn.completeAnthropic(platform.AIProvider{ID: "local", Kind: "local", BaseURL: service.URL, Wire: "anthropic", Secret: "fixture"}, "fixture", request)
	if failure != nil {
		t.Fatal(failure)
	}
	if sent["max_tokens"] != float64(128) || sent["thinking"].(map[string]any)["type"] != "adaptive" || sent["output_config"].(map[string]any)["effort"] != "high" {
		t.Fatal("adaptive options were not sent", sent)
	}
	content := sent["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if content[0].(map[string]any)["signature"] != "signed" {
		t.Fatal("signed thinking state was not retained")
	}
	public, _ := json.Marshal(answer)
	if strings.Contains(string(public), "private reasoning") || len(answer.Protocol) == 0 {
		t.Fatal("thinking state was exposed or lost")
	}
	// Exercise SDK parsing as well as response projection; it is the same
	// signed-block representation used by the streaming accumulator.
	var message anthropic.Message
	if err := json.Unmarshal([]byte(`{"content":[{"type":"redacted_thinking","data":"protected"},{"type":"text","text":"visible"}],"usage":{"input_tokens":1,"output_tokens":1}}`), &message); err != nil {
		t.Fatal(err)
	}
	redacted, _, failure := anthropicAnswer(message, "")
	if failure != nil || !strings.Contains(string(redacted.Protocol), "protected") {
		t.Fatal("redacted continuation was lost", failure)
	}
}

func TestAIModelConfigurationPostgresCheckpoint(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE needs an isolated PostgreSQL database")
	}
	ctx := t.Context()
	store, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { store.Close() }()
	id := "model-configuration-checkpoint"
	build := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("admin", "platform:admin", "ai:admin", "agent:admin", "desk:clerk")}, ai.New(id), NewAgents(id), newDesk(id), work.New(id), flow.New(id))
	}
	deployment := &Deployment{SnapshotEvery: 1}
	tn := build()
	if err := deployment.restoreTenants(ctx, store, "model-configuration", []*Tenant{tn}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if store.Position(id) != 0 {
		t.Fatal("model configuration rehearsal database is not empty")
	}
	tn.attachJournal(ctx, store)
	at := time.Now().UTC()
	decide(t, tn, "admin", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://127.0.0.1:1/v1"}, at)
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/one", map[string]any{"access": "users", "capabilities": platform.ModelCapabilities{Source: "synthetic protocol fixture", ObservedAt: at, Adapter: "deepseek", ThinkingModes: []string{"enabled", "disabled"}, EffortLevels: []string{"high", "max"}}, "defaults": platform.ModelOptions{MaxTokens: 128, Thinking: "enabled", Effort: "high"}}, at)
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/one"}, at)
	decide(t, tn, "admin", AgentApp, SchemaRunStart, RunType, "r", map[string]any{"agent": "desk.triage", "goal": "retained model configuration"}, at)
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/one", map[string]any{"access": "users", "defaults": platform.ModelOptions{MaxTokens: 256, Thinking: "enabled", Effort: "max"}}, at)
	if _, err := tn.checkpointRuntime(ctx, store, "model-configuration"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	store, err = OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	restored := build()
	restored.AIClient = func(*http.Request) (*http.Response, error) {
		t.Error("recovery called a model")
		return nil, fmt.Errorf("unexpected provider request")
	}
	if err := deployment.restoreTenants(ctx, store, "model-configuration", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil {
		t.Fatal(err)
	}
	if restored.quarantined() {
		t.Fatal("model owner or accepted run failed to restore", restored.fault.Load())
	}
	restored.attachJournal(ctx, store)
	value, refusal := restored.RecordOf(memberOf(t, restored, "admin"), RunType, "r", at)
	if refusal != nil {
		t.Fatal(refusal)
	}
	raw, _ := json.Marshal(value.Record)
	var run AgentRunRecord
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	if run.ModelBinding == nil || run.ModelBinding.Options.MaxTokens != 128 || run.ModelBinding.Options.Effort != "high" {
		t.Fatal("SQL checkpoint changed the accepted model binding", run.ModelBinding)
	}
	selection, refusal := restored.ModelSelection(memberOf(t, restored, "admin"), "agent")
	if refusal != nil || len(selection.Models) != 1 || selection.Models[0].Effective.Options.Effort != "max" {
		t.Fatal("restored current defaults differ", selection, refusal)
	}
}

func TestAgentAcceptedConfigurationSurvivesDefaultChanges(t *testing.T) {
	at := time.Now()
	build := func() *Tenant {
		return composeTenant(t, "agent-binding", []Seat{seatOf("admin", "platform:admin", "ai:admin", "agent:admin", "desk:clerk")}, ai.New("agent-binding"), NewAgents("agent-binding"), newDesk("agent-binding"), work.New("agent-binding"), flow.New("agent-binding"))
	}
	tn := build()
	var entries []Entry
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
	tn.Record = func(e Entry) { entries = append(entries, e) }
	decide(t, tn, "admin", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://127.0.0.1:1/v1"}, at)
	for _, model := range []string{"one", "two"} {
		decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/"+model, map[string]any{"access": "users", "defaults": platform.ModelOptions{MaxTokens: 128}}, at)
	}
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/one"}, at)
	decide(t, tn, "admin", AgentApp, SchemaRunStart, RunType, "r", map[string]any{"agent": "desk.triage", "goal": "a bounded task"}, at)
	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "r")
	if run.ModelBinding == nil || run.ModelBinding.Model != "local/one" {
		t.Fatal("new accepted run did not pin its model", run.ModelBinding)
	}
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]any{"value": "local/two"}, at)
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/one", map[string]any{"access": "users", "defaults": platform.ModelOptions{MaxTokens: 256}}, at)
	turns := tn.agents.DueAgentTurns(at)
	if len(turns) != 1 || turns[0].Request.Binding == nil || turns[0].Request.Binding.Model != "local/one" || turns[0].Request.Binding.Options.MaxTokens != 128 {
		t.Fatal("running task changed configuration", turns)
	}
	tn.agents.IdleAgentTurn("r")
	decide(t, tn, "admin", ai.ID, ai.SchemaModelDisable, ai.ModelType, "local/one", map[string]any{}, at)
	turns = tn.agents.DueAgentTurns(at)
	if len(turns) != 1 || turns[0].Stop == "" {
		t.Fatal("disabled model continued a pinned run")
	}
	CheckReplay(t, tn, entries, build)
}

func TestModelConfigurationDirectoryAndAcceptedBinding(t *testing.T) {
	at := time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)
	pages := 0
	var sent map[string]any
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			pages++
			if r.URL.Query().Get("after_id") == "a" {
				fmt.Fprint(w, `{"data":[{"id":"b"}],"has_more":false}`)
			} else {
				fmt.Fprint(w, `{"data":[{"id":"a","context_window":32000,"max_output_tokens":4000,"effort":{"supported_levels":["high","max"]}}],"has_more":true,"last_id":"a"}`)
			}
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&sent)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":4,"completion_tokens":2}}`)
	}))
	defer service.Close()
	build := func() *Tenant {
		return composeTenant(t, "model-config", []Seat{seatOf("admin", "platform:admin", "ai:admin"), seatOf("user", "ai:user")}, ai.New("model-config"))
	}
	tn := build()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	decide(t, tn, "admin", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": service.URL + "/v1"}, at)
	admin := memberOf(t, tn, "admin")
	models, err, failure := tn.ProviderModels(admin, "local", false)
	if err != nil || failure != nil || len(models) != 2 || pages != 2 || models[1].Capabilities.OutputTokens != nil || models[1].PriceKnown || models[1].Free {
		t.Fatalf("paginated/unknown catalog: %+v %v %v pages=%d", models, err, failure, pages)
	}
	_, _, _ = tn.ProviderModels(admin, "local", false)
	if pages != 2 {
		t.Fatal("unchanged provider did not reuse directory cache")
	}
	deep, problem := catalogModel(platform.AIProvider{Vendor: "deepseek", BaseURL: service.URL + "/v1"}, json.RawMessage(`{"id":"a","context_window":32000,"max_output_tokens":4000,"effort":{"supported_levels":["high","max"]}}`), at)
	if problem != nil || len(deep.Capabilities.EffortLevels) != 2 {
		t.Fatal("DeepSeek capability evidence lost", problem)
	}
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/a", map[string]any{"access": "everyone", "capabilities": deep.Capabilities, "defaults": platform.ModelOptions{MaxTokens: 128, Thinking: "enabled", Effort: "high"}}, at)
	rec := decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]any{"value": "local/a"}, at)
	caller := platform.NewCaller(&stagedDecision{tenant: tn, records: tn.records}, admin, ai.ID, false, true)
	effect := tn.planModelRequest(caller, rec, platform.Request{Target: "test", Payload: platform.Prompt{User: "hello"}}, nil)
	var saved modelAsk
	_ = json.Unmarshal([]byte(effect.Body), &saved)
	if saved.Binding == nil || saved.Binding.Options.Effort != "high" || saved.Binding.Options.MaxTokens != 128 {
		t.Fatal("accepted binding did not retain effective defaults")
	}
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/a", map[string]any{"access": "everyone", "expectedRevision": 1, "defaults": platform.ModelOptions{MaxTokens: 256, Thinking: "enabled", Effort: "max"}}, at)
	out, _ := tn.sendModel(effect, at)
	if out.Result != "delivered" || sent["max_tokens"] != float64(128) || sent["reasoning_effort"] != "high" {
		t.Fatalf("queued request changed with defaults: %+v %+v", out, sent)
	}
	if result := refuse(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/a", map[string]any{"access": "everyone", "expectedRevision": 1}, at); !strings.Contains(result, "CONFLICT") {
		t.Fatal("stale model edit was accepted", result)
	}
	CheckReplay(t, tn, entries, build)
}

func TestModelConfigurationConstraintsAndProtectedContinuation(t *testing.T) {
	window, output := 4096, 1024
	m := platform.AIModel{Provider: "p", Model: "m", Capabilities: &platform.ModelCapabilities{ContextTokens: &window, OutputTokens: &output, Adapter: "deepseek", ThinkingModes: []string{"enabled", "disabled"}, EffortLevels: []string{"high"}}}
	for _, options := range []platform.ModelOptions{{InputTokens: 4000, MaxTokens: 128}, {MaxTokens: 1025}, {Thinking: "disabled", Effort: "high"}, {Thinking: "enabled", Effort: "low"}, {InputTokens: -1}} {
		if _, err := m.Resolve("generation", options, 8192); err == nil {
			t.Fatalf("invalid combination accepted: %+v", options)
		}
	}
	b, err := m.Resolve("generation", platform.ModelOptions{}, 8192)
	if err != nil || b.Options.MaxTokens != 1024 {
		t.Fatal("inherited generation budget did not respect reported limits", b, err)
	}
	reply, _, failure := completion(200, []byte(`{"choices":[{"message":{"content":"visible","reasoning_content":"private","tool_calls":[{"id":"c","function":{"name":"tool","arguments":"{}"}}]}}]}`), "m")
	if failure != nil || len(reply.Protocol) == 0 {
		t.Fatal("protected continuation was lost", failure)
	}
	public, _ := json.Marshal(reply)
	if strings.Contains(string(public), "private") {
		t.Fatal("reasoning leaked through public answer")
	}
	wire := openAIMessages([]Message{{Role: "assistant", Content: reply.Content, ToolCalls: reply.ToolCalls, Protocol: reply.Protocol}})
	if wire[0]["reasoning_content"] != "private" {
		t.Fatal("tool continuation omitted provider reasoning state")
	}
	var nativeBudget map[string]any
	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&nativeBudget)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"reasoning_content\":\"private\",\"content\":\"visible\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer stream.Close()
	tn := composeTenant(t, "continuation", []Seat{seatOf("admin", "platform:admin", "agent:admin")}, NewAgents("continuation"))
	answer, _, failure := tn.complete(platform.AIProvider{ID: "local", Kind: "local", BaseURL: stream.URL, Wire: "openai", Vendor: "openai"}, "m", ChatRequest{MaxTokens: 64, Stream: true, Messages: []Message{{Role: "user", Content: "hello"}}})
	if failure != nil || string(answer.Protocol) != `"private"` {
		t.Fatal("stream continuation lost", failure)
	}
	if nativeBudget["max_completion_tokens"] != float64(64) || nativeBudget["max_tokens"] != nil {
		t.Fatal("native OpenAI generation budget used the deprecated field", nativeBudget)
	}
	run := AgentRunRecord{Record: platform.Record{ID: "r"}, State: "done", OnBehalf: "admin", Protocol: map[int]json.RawMessage{0: reply.Protocol}, Steps: []RunStep{}}
	if err := tn.automation(AgentApp, true).PutAt(time.Now(), "test.run", run); err != nil {
		t.Fatal(err)
	}
	view, refusal := tn.RecordOf(memberOf(t, tn, "admin"), RunType, "r", time.Now())
	if refusal != nil {
		t.Fatal(refusal)
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "private") {
		t.Fatal("protected session state leaked through record read")
	}
}

func TestKnowledgeConfigurationRequiresPreparedSpace(t *testing.T) {
	at := time.Now()
	tn := composeTenant(t, "embedding-config", []Seat{seatOf("admin", "platform:admin", "ai:admin", "knowledge:editor")}, ai.New("embedding-config"), knowledge.New("embedding-config"))
	decide(t, tn, "admin", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://127.0.0.1:1/v1"}, at)
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/embed", map[string]any{"access": "users", "capabilities": platform.ModelCapabilities{Purposes: []string{"embedding"}, Source: "test", ObservedAt: at}}, at)
	if result := refuse(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "knowledge/embedding-model", map[string]any{"value": "local/embed"}, at); !strings.Contains(result, "Build this model") {
		t.Fatal("unprepared vector space activated", result)
	}
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "knowledge/embedding-next-model", map[string]any{"value": "local/embed"}, at)
	tn.sync()
	status := tn.knowledgeIndexStatus()
	if !status.Ready || status.Total != 0 {
		t.Fatal("empty corpus should be ready without external calls", status)
	}
	decide(t, tn, "admin", PlatformApp, SchemaSettingSet, SettingType, "knowledge/embedding-model", map[string]any{"value": "local/embed"}, at)
	before := tn.vectorSpace("local/embed")
	decide(t, tn, "admin", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/embed", map[string]any{"access": "users"}, at)
	if before == tn.vectorSpace("local/embed") {
		t.Fatal("model revision reused an old vector space")
	}
	if consistentVectors([][]float32{{1, 2}, {1}}, 0) == nil {
		t.Fatal("mixed vector dimensions accepted")
	}
}
