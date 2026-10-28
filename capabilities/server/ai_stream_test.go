package platformserver

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"platformserver/aigateway/providers"
	"platformserver/apps/ai"
	"platformserver/platform"
)

func modelStream(events ...string) string {
	var out strings.Builder
	for _, event := range events {
		var envelope struct{ Type string }
		if json.Unmarshal([]byte(event), &envelope) == nil && envelope.Type != "" {
			out.WriteString("event: " + envelope.Type + "\n")
		}
		out.WriteString("data: " + event + "\n\n")
	}
	return out.String()
}

func modelWire(t *testing.T, contentType, body string, inspect func(map[string]any)) func(*http.Request) (*http.Response, error) {
	t.Helper()
	return func(req *http.Request) (*http.Response, error) {
		var request map[string]any
		if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
			t.Fatal(err)
		}
		if inspect != nil {
			inspect(request)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{contentType}}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}
}

func TestOpenAIToolStreamPreservesCallsAndFinalUsage(t *testing.T) {
	wire := modelStream(
		`{"model":"routed-model","choices":[{"delta":{"content":"已读取"}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","function":{"name":"select","arguments":"{\"yes\":"}},{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"target\":\"工"}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"单一\"}"}},{"index":1,"function":{"arguments":"true}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":30,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":0},"cost":0}}`,
		`[DONE]`,
	)
	tn := &Tenant{AIClient: modelWire(t, "text/event-stream", wire, func(req map[string]any) {
		if req["stream"] != true || req["stream_options"].(map[string]any)["include_usage"] != true {
			t.Fatalf("stream usage not requested: %+v", req)
		}
		tool := req["tools"].([]any)[0].(map[string]any)["function"].(map[string]any)
		if tool["parameters"].(map[string]any)["properties"].(map[string]any)["yes"].(map[string]any)["type"] != "boolean" {
			t.Fatalf("tool input was flattened: %+v", tool)
		}
	})}
	var text string
	var deltas []platform.ToolCallDelta
	answer, usage, failure := tn.modelProvider(platform.AIProvider{BaseURL: "http://model/v1"}).Complete(platform.AIProvider{BaseURL: "http://model/v1"}, "requested", ChatRequest{
		Messages: []Message{{Role: "user", Content: "读取工单"}}, Stream: true,
		Tools: []Tool{{Name: "select", Properties: map[string]any{"yes": map[string]any{"type": "boolean"}}}},
		Delta: func(piece string) { text += piece }, OnStream: func(event platform.ChatStreamEvent) {
			if event.ToolCall != nil {
				deltas = append(deltas, *event.ToolCall)
			}
		},
	})
	want := []ToolCall{{ID: "c1", Name: "read", Arguments: json.RawMessage(`{"target":"工单一"}`)}, {ID: "c2", Name: "select", Arguments: json.RawMessage(`{"yes":true}`)}}
	if failure != nil || answer.Content != "已读取" || text != answer.Content || !reflect.DeepEqual(answer.ToolCalls, want) || len(deltas) != 4 {
		t.Fatalf("incomplete tool stream: %+v; deltas=%+v; failure=%v", answer, deltas, failure)
	}
	if !usage.TokensReported || usage.Input != 100 || usage.Output != 30 || usage.Served != "routed-model" || usage.CacheReadInput == nil || *usage.CacheReadInput != 80 ||
		usage.CacheWriteInput == nil || *usage.CacheWriteInput != 0 || !usage.CostReported || usage.Cost != 0 {
		t.Fatalf("final usage changed: %+v", usage)
	}
}

func TestModelCacheUsageDistinguishesMissingFromZero(t *testing.T) {
	for _, test := range []struct {
		name, usage string
		reported    bool
		valid       bool
	}{
		{"missing", `{"prompt_tokens":100,"completion_tokens":20}`, false, true},
		{"zero", `{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":0}}`, true, true},
		{"negative", `{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":-1}}`, false, false},
		{"exceeds total", `{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":101}}`, false, false},
		{"combined exceeds total", `{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":21}}`, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, usage, failure := providers.Completion(200, []byte(`{"choices":[{"message":{"content":"ok"}}],"usage":`+test.usage+`}`), "model")
			if (failure == nil) != test.valid || (usage.CacheReadInput != nil) != test.reported {
				t.Fatalf("cache usage presence or validity changed: %+v %v", usage, failure)
			}
		})
	}
}

func TestModelToolStreamRejectsIncompleteCalls(t *testing.T) {
	for _, test := range []struct {
		name, body string
		wantUsage  bool
	}{
		{"interrupted", modelStream(`{"choices":[{"delta":{"content":"处理中","tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"target\":"}}]}}]}`), false},
		{"cut off", modelStream(
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"target\":\"one\"}"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"length"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":10,"completion_tokens":20}}`, `[DONE]`), true},
		{"invalid JSON", modelStream(
			`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"target\":"}}]}}]}`,
			`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`, `[DONE]`), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			tn := &Tenant{AIClient: modelWire(t, "text/event-stream", test.body, nil)}
			answer, usage, failure := tn.modelProvider(platform.AIProvider{BaseURL: "http://model/v1"}).Complete(platform.AIProvider{BaseURL: "http://model/v1"}, "model", ChatRequest{Stream: true})
			if failure == nil || len(answer.ToolCalls) != 0 || usage.TokensReported != test.wantUsage {
				t.Fatalf("partial calls became executable or usage was lost: %+v %+v %v", answer, usage, failure)
			}
		})
	}
	answer, usage, failure := providers.Completion(200, []byte(`{"choices":[{"finish_reason":"length","message":{"tool_calls":[{"id":"c1","function":{"name":"read","arguments":"{}"}}]}}],"usage":{"prompt_tokens":10,"completion_tokens":20}}`), "model")
	if failure == nil || len(answer.ToolCalls) != 0 || !usage.TokensReported {
		t.Fatalf("non-streaming cutoff became executable: %+v %+v %v", answer, usage, failure)
	}
}

func TestAnthropicToolStreamPreservesCacheAndConversation(t *testing.T) {
	wire := modelStream(
		`{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"served-model","content":[],"usage":{"input_tokens":5,"output_tokens":1,"cache_read_input_tokens":80,"cache_creation_input_tokens":10}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"已读取工单"}}`,
		`{"type":"content_block_stop","index":0}`,
		`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"c1","name":"read","input":{}}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"target\":\"工"}}`,
		`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"单一\"}"}}`,
		`{"type":"content_block_stop","index":1}`,
		`{"type":"content_block_start","index":2,"content_block":{"type":"tool_use","id":"c2","name":"read","input":{}}}`,
		`{"type":"content_block_delta","index":2,"delta":{"type":"input_json_delta","partial_json":"{\"target\":\"工单二\"}"}}`,
		`{"type":"content_block_stop","index":2}`,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":30,"cache_read_input_tokens":81}}`,
		`{"type":"message_stop"}`,
	)
	tn := &Tenant{Secrets: func(string) ([]byte, bool) { return []byte("test-key"), true }}
	tn.AIClient = modelWire(t, "text/event-stream", wire, func(req map[string]any) {
		if req["stream"] != true || req["system"].([]any)[0].(map[string]any)["cache_control"].(map[string]any)["type"] != "ephemeral" {
			t.Fatalf("stable prefix was not cached: %+v", req)
		}
		messages := req["messages"].([]any)
		results := messages[2].(map[string]any)["content"].([]any)
		if len(messages) != 3 || len(results) != 2 || results[0].(map[string]any)["tool_use_id"] != "old1" || results[1].(map[string]any)["tool_use_id"] != "old2" {
			t.Fatalf("tool results lost their calls: %+v", messages)
		}
		calls := messages[1].(map[string]any)["content"].([]any)
		if _, ok := calls[0].(map[string]any)["input"].(map[string]any); !ok {
			t.Fatalf("tool input was sent as text: %+v", calls)
		}
		tool := req["tools"].([]any)[0].(map[string]any)
		if tool["input_schema"].(map[string]any)["properties"].(map[string]any)["target"].(map[string]any)["type"] != "string" {
			t.Fatalf("tool input changed: %+v", tool)
		}
	})
	var text string
	var deltas []platform.ToolCallDelta
	answer, usage, failure := tn.modelProvider(platform.AIProvider{BaseURL: "http://model", Secret: "model"}).CompleteAnthropic(platform.AIProvider{BaseURL: "http://model", Secret: "model"}, "requested", ChatRequest{
		Tools:  []Tool{{Name: "read", Properties: map[string]any{"target": map[string]any{"type": "string"}}, Required: []string{"target"}}},
		Stream: true, PromptCache: true, Messages: []Message{
			{Role: "system", Content: "读取工单"}, {Role: "user", Content: "继续"},
			{Role: "assistant", ToolCalls: []ToolCall{{ID: "old1", Name: "read", Arguments: json.RawMessage(`{}`)}, {ID: "old2", Name: "read", Arguments: json.RawMessage(`{}`)}}},
			{Role: "tool", ToolCallID: "old1", Content: "一"}, {Role: "tool", ToolCallID: "old2", Content: "二"},
		},
		Delta: func(piece string) { text += piece }, OnStream: func(event platform.ChatStreamEvent) {
			if event.ToolCall != nil {
				deltas = append(deltas, *event.ToolCall)
			}
		},
	})
	if failure != nil || text != "已读取工单" || answer.Content != text || len(answer.ToolCalls) != 2 || answer.ToolCalls[0].ID != "c1" ||
		string(answer.ToolCalls[0].Arguments) != `{"target":"工单一"}` || string(answer.ToolCalls[1].Arguments) != `{"target":"工单二"}` || len(deltas) != 5 ||
		deltas[0].Index != 0 || deltas[2].Index != 0 || deltas[3].Index != 1 {
		t.Fatalf("tool stream changed: %+v; deltas=%+v; failure=%v", answer, deltas, failure)
	}
	if !usage.TokensReported || usage.Input != 96 || usage.Output != 30 || usage.Served != "served-model" || usage.CacheReadInput == nil || *usage.CacheReadInput != 81 ||
		usage.CacheWriteInput == nil || *usage.CacheWriteInput != 10 || usage.CostReported {
		t.Fatalf("Anthropic cache tokens were double counted or cost invented: %+v", usage)
	}
	// Interrupted streams retain the text already shown, with no executable
	// calls and no claim that the initial output count is the final usage.
	tn.AIClient = modelWire(t, "text/event-stream", modelStream(
		`{"type":"message_start","message":{"id":"m1","type":"message","role":"assistant","model":"requested","content":[],"usage":{"input_tokens":5,"output_tokens":1}}}`,
		`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"处理中"}}`,
	), nil)
	answer, usage, failure = tn.modelProvider(platform.AIProvider{BaseURL: "http://model", Secret: "model"}).CompleteAnthropic(platform.AIProvider{BaseURL: "http://model", Secret: "model"}, "requested", ChatRequest{Stream: true})
	if failure == nil || answer.Content != "处理中" || len(answer.ToolCalls) != 0 || usage.TokensReported {
		t.Fatalf("interrupted Anthropic message changed: %+v %+v %v", answer, usage, failure)
	}
}

func TestChatToolStreamMetersOnceAndReportsAcceptanceFailure(t *testing.T) {
	approveTestInference(t, "http://localhost:1", "lm")
	at := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	build := func() *Tenant { return composeTenant(t, "t-1", []Seat{seatOf("ana", "ai:admin")}, ai.New("t-1")) }
	tn := build()
	var entries []Entry
	tn.Record = func(entry Entry) { entries = append(entries, entry) }
	decide(t, tn, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": "http://localhost:1/v1"}, at)
	decide(t, tn, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "lm/tools", map[string]any{"access": "everyone"}, at)
	tn.AIClient = modelWire(t, "text/event-stream", modelStream(
		`{"choices":[{"delta":{"content":"已读取","tool_calls":[{"index":0,"id":"c1","function":{"name":"read","arguments":"{\"target\":\"工单一\"}"}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
		`{"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":20,"prompt_tokens_details":{"cached_tokens":80,"cache_write_tokens":0}}}`, `[DONE]`,
	), nil)
	h := NewHost(Tokens(map[string]string{"token": "ana"}), tn)
	chat := func() string {
		req := httptest.NewRequest("POST", "/v1/ai/chat", strings.NewReader(`{"model":"lm/tools","messages":[{"role":"user","content":"读取工单"}],"stream":true}`))
		req.Header.Set("Authorization", "Bearer token")
		recorder := httptest.NewRecorder()
		h.Handler().ServeHTTP(recorder, req)
		return recorder.Body.String()
	}
	body := chat()
	if !strings.Contains(body, "event: tool_delta") || !strings.Contains(body, `"arguments":"{\"target\":\"工单一\"}"`) ||
		!strings.Contains(body, "event: done") || !strings.Contains(body, `"toolCalls":[{"id":"c1"`) {
		t.Fatalf("HTTP stream lost its final tools: %s", body)
	}
	usage := tn.ai.Usage()
	if len(usage) != 1 || !usage[0].TokensReported || usage[0].CacheReadInput == nil || *usage[0].CacheReadInput != 80 || usage[0].CacheWriteInput == nil || usage[0].CostReported {
		t.Fatalf("stream was metered incorrectly: %+v", usage)
	}
	CheckReplay(t, tn, entries, build)
	tn.AcceptResult = func(entry Entry, key, hash string) ([]byte, error) {
		var envelope struct{ Kind string }
		_ = json.Unmarshal(entry.Body, &envelope)
		if envelope.Kind == "usage-result" {
			return nil, errors.New("append failed")
		}
		return entry.Body, nil
	}
	body = chat()
	if !strings.Contains(body, "event: error") || strings.Contains(body, "event: done") || len(tn.ai.Usage()) != 1 {
		t.Fatalf("failed usage acceptance became a successful stream: %s", body)
	}
}
