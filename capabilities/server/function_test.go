package platformserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/platform"
)

type functionStock struct {
	*stock
	function platform.AIFunction
}

// Refusals must invalidate the whole accepted decision, including callers
// that ignore RequestFunction's return value inside their apply callback.
func TestAIFunctionRefusalsAndModelGates(t *testing.T) {
	at := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	for _, reason := range []string{"private field", "input budget", "no model", "disabled model", "app quota"} {
		t.Run(reason, func(t *testing.T) {
			fields := []string{"name", "qty"}
			if reason == "private field" {
				fields = []string{"secret"}
			}
			tn := functionTenant(t, "", fields)
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { return e.Body, nil }
			ana, _ := tn.Member("ana")
			keys := 0
			do := func(app, schema, typ, id string, payload any) *kernel.Error {
				keys++
				_, err := tn.Submit(ana, &pb.Submission{TenantId: tn.ID, PrincipalId: ana.ID, Authority: app, IdempotencyKey: fmt.Sprint(keys), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at)
				return err
			}
			must := func(app, schema, typ, id string, payload any) {
				t.Helper()
				if err := do(app, schema, typ, id, payload); err != nil {
					t.Fatal(err)
				}
			}
			if reason != "no model" {
				must(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": "http://localhost:1"})
				must(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]string{"access": "users"})
				must(PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": "local/probe"})
			}
			name := "Part"
			if reason == "input budget" {
				name = strings.Repeat("x", 4096)
			}
			must("stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": name, "qty": 2})
			err := do("stock", "stock.item.advise", "stock.item", "I1", struct{}{})
			switch reason {
			case "private field", "input budget", "no model":
				if err == nil || len(tn.outbound) != 0 {
					t.Fatal("refusal left a committed function effect")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if reason == "disabled model" {
				must(ai.ID, ai.SchemaModelDisable, ai.ModelType, "local/probe", struct{}{})
			} else {
				must(ai.ID, ai.SchemaLimitSet, ai.LimitType, "app:stock", map[string]int{"dailyTokens": 1})
				tn.ai.Meter(ai.Usage{Member: "app:stock", Model: "local/probe", Input: 1, At: at})
			}
			called := false
			tn.AIClient = func(*http.Request) (*http.Response, error) { called = true; return nil, errors.New("must not send") }
			out, usage := tn.sendModel(tn.outbound[0].Effect, at.Add(time.Second))
			if out.Result == "delivered" || usage != nil || called {
				t.Fatalf("%s escaped the model gate: %+v", reason, out)
			}
		})
	}
}

func TestJournalAcceptedFunctionCrashBeforeApplication(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("function-crash-%d", time.Now().UnixNano())
	defer journal.pool.Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	compose := func() *Tenant { return functionTenantFor(t, id, "", []string{"name", "qty"}) }
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": `{"summary":"Saved advice","category":"review","review":true}`}}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 12}})
	}))
	defer provider.Close()
	live := compose()
	live.Record = func(e Entry) {
		if err := journal.Append(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) { return journal.AppendAccepted(ctx, id, e, key, hash) }
	at := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	ana, _ := live.Member("ana")
	key := 0
	do := func(app, schema, typ, target string, payload any) {
		t.Helper()
		key++
		_, err := live.Submit(ana, &pb.Submission{TenantId: id, PrincipalId: ana.ID, Authority: app, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: target}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at)
		if err != nil {
			t.Fatal(err)
		}
	}
	do(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": provider.URL})
	do(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/probe", map[string]string{"access": "users"})
	do(PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": "local/probe"})
	do("stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Part", "qty": 2, "note": "original"})
	do("stock", "stock.item.advise", "stock.item", "I1", struct{}{})
	out, usage := live.sendModel(live.outbound[0].Effect, at.Add(time.Second))
	if out.Result != "delivered" || usage == nil {
		t.Fatalf("function did not validate: %+v", out)
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) {
		if _, err := journal.AppendAccepted(ctx, id, e, key, hash); err != nil {
			t.Fatal(err)
		}
		panic("crash after function result append")
	}
	effectID := live.outbound[0].ID
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("crash was not injected")
			}
		}()
		live.settleWithUsage(effectID, out, usage, at.Add(time.Second))
	}()
	if live.outbound[0].State != "pending" || len(live.ai.Usage()) != 0 || live.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "original" {
		t.Fatal("unapplied result escaped")
	}
	entries, err := journal.Entries(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	restored := functionTenantFor(t, id, "New instructions", []string{"name", "qty"})
	restored.AIClient = func(*http.Request) (*http.Response, error) {
		t.Error("recovery called a provider")
		return nil, errors.New("must not send")
	}
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	if restored.outbound[0].State != "delivered" || len(restored.ai.Usage()) != 1 || restored.records.types["stock.item"].rows["I1"].value.Interface().(Item).Note != "Saved advice" {
		t.Fatal("saved answer, reply and usage did not recover together")
	}
	CheckReplay(t, restored, entries, func() *Tenant { return functionTenantFor(t, id, "New instructions", []string{"name", "qty"}) })
}

func (a *functionStock) Manifest() platform.Manifest {
	m := a.stock.Manifest()
	m.Functions = []platform.AIFunction{a.function}
	m.Entities[1].Scope.Levels["lead"] = platform.ScopeTenant
	return m
}
func (*functionStock) AcceptedActionSchemas() []string {
	return []string{"stock.item.advise", "stock.item.advice-answer"}
}
func (a *functionStock) Submit(c platform.Caller, s *pb.Submission, at time.Time) (*pb.ChangeRecord, *kernel.Error) {
	if s.GetSchema().GetName() != "stock.item.advise" && s.GetSchema().GetName() != "stock.item.advice-answer" {
		return a.stock.Submit(c, s, at)
	}
	return a.ledger.Receive(c, s, at, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		item, ok := platform.Get[Item](c, s.GetTarget().GetId())
		if !ok {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		}
		if s.GetSchema().GetName() == "stock.item.advise" {
			return func(r *pb.ChangeRecord) {
				c.RequestFunction(r, platform.FunctionRequest{Name: "record-advice", Reply: "stock.item.advice-answer"})
			}, nil
		}
		var answer platform.Answer
		json.Unmarshal(s.GetPayload(), &answer)
		if answer.Outcome == "accepted" {
			var result platform.RecordAdvice
			if json.Unmarshal([]byte(answer.Text), &result) != nil {
				return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
			}
			item.Note = result.Summary
		}
		return func(r *pb.ChangeRecord) { c.Put(r, item) }, nil
	})
}

func functionTenant(t *testing.T, instructions string, fields []string) *Tenant {
	t.Helper()
	return functionTenantFor(t, "function", instructions, fields)
}

func functionTenantFor(t *testing.T, id, instructions string, fields []string) *Tenant {
	t.Helper()
	a := &functionStock{stock: newStock(id), function: platform.RecordAdviceFunction("stock.item", fields, []string{"clerk", "lead"})}
	if instructions != "" {
		a.function.Instructions = instructions
	}
	a.ledger.Catalog.Add(platform.Action{Schema: "stock.item.advise", Target: "stock.item", Title: "Advise", Description: "Advise", Payload: []platform.Field{}, Roles: []string{"clerk", "lead"}},
		platform.Action{Schema: "stock.item.advice-answer", Target: "stock.item", Title: "Answer", Description: "Answer", Payload: platform.AnswerFields(), Automation: true})
	a.ledger = platform.NewLedger(id, "stock", a.ledger.Catalog, "stock.bin", "stock.item")
	tn, err := NewTenant(id, NewConsole(id,
		Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"stock": "clerk", PlatformApp: Admin, ai.ID: ai.Admin}}},
		Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{"stock": "clerk"}}},
		Seat{Subjects: []string{"lead"}, Member: platform.Member{ID: "lead", Roles: map[string]string{"stock": "lead"}}}), ai.New(id), a)
	if err != nil {
		t.Fatal(err)
	}
	return tn
}

func TestAIFunctionUsesAcceptedInputAndStrictOutput(t *testing.T) {
	at := time.Date(2026, 9, 29, 9, 0, 0, 0, time.UTC)
	compose := func() *Tenant { return functionTenant(t, "", []string{"name", "qty"}) }
	tn := compose()
	var entries []Entry
	tn.Record = func(e Entry) { entries = append(entries, e) }
	fail := false
	tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if fail {
			return nil, errors.New("append unavailable")
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	ana, _ := tn.Member("ana")
	key := 0
	submit := func(m platform.Member, app, schema, typ, id string, payload any) *kernel.Error {
		key++
		_, err := tn.Submit(m, &pb.Submission{TenantId: tn.ID, PrincipalId: m.ID, Authority: app, IdempotencyKey: fmt.Sprint(key), Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: platform.Raw(payload)}, at)
		return err
	}
	must := func(app, schema, typ, id string, payload any) {
		t.Helper()
		if err := submit(ana, app, schema, typ, id, payload); err != nil {
			t.Fatal(err)
		}
	}
	var requests atomic.Int32
	var response atomic.Value
	type modelResponse struct {
		Output string
		Tokens int
		Tools  bool
	}
	output := `{"summary":"Two parts","category":"routine","review":false}`
	response.Store(modelResponse{Output: output, Tokens: 20})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		reply := response.Load().(modelResponse)
		var body struct {
			Model     string
			Messages  []Message
			MaxTokens int `json:"max_tokens"`
		}
		json.NewDecoder(r.Body).Decode(&body)
		var input map[string]any
		if json.Unmarshal([]byte(body.Messages[len(body.Messages)-1].Content), &input) != nil || len(input) != 2 || input["name"] != "Part" || input["qty"] != float64(2) || body.MaxTokens != 256 || body.Model != "chosen" {
			t.Errorf("provider received unbound or excess input: %+v", body)
		}
		message := map[string]any{"content": reply.Output}
		if reply.Tools {
			message["tool_calls"] = []any{map[string]any{"id": "outside", "function": map[string]string{"name": "undeclared", "arguments": "{}"}}}
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": message}}, "usage": map[string]int{"prompt_tokens": 10, "completion_tokens": reply.Tokens}})
	}))
	defer server.Close()
	must(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]string{"kind": "local", "baseUrl": server.URL})
	must(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/chosen", map[string]string{"access": "users"})
	must(PlatformApp, SchemaSettingSet, SettingType, "ai/app-model", map[string]string{"value": "local/chosen"})
	must("stock", "stock.item.create", "stock.item", "I1", map[string]any{"name": "Part", "qty": 2, "note": "original"})
	bo, _ := tn.Member("bo")
	if err := submit(bo, "stock", "stock.item.advise", "stock.item", "I1", map[string]any{}); err == nil || len(tn.outbound) != 0 {
		t.Fatal("another owner sent private input")
	}
	foreign := ana
	foreign.Tenant = "other"
	if err := submit(foreign, "stock", "stock.item.advise", "stock.item", "I1", map[string]any{}); err == nil {
		t.Fatal("foreign member called the function")
	}
	fail = true
	if err := submit(ana, "stock", "stock.item.advise", "stock.item", "I1", map[string]any{}); err == nil || len(tn.outbound) != 0 || requests.Load() != 0 {
		t.Fatal("failed append leaked a function request")
	}
	fail = false
	must("stock", "stock.item.advise", "stock.item", "I1", map[string]any{})
	var ask modelAsk
	json.Unmarshal([]byte(tn.outbound[0].Body), &ask)
	if ask.Function == nil || ask.Function.Call.Definition == "" || ask.Function.Call.Dependencies == "" || len(ask.Function.Call.Sources) != 2 {
		t.Fatal("function lost its binding")
	}
	must("stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 3})
	CheckReplay(t, tn, entries, compose)
	// A code deployment changes the current prompt; old pending work retains
	// its saved definition and accepted input instead of looking it up again.
	changed := functionTenant(t, "New instructions for future calls", []string{"name", "qty"})
	if err := changed.Replay(entries); err != nil {
		t.Fatal(err)
	}
	changed.AIClient = func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "New instructions") {
			t.Fatal("old request followed new code")
		}
		r.Body = io.NopCloser(strings.NewReader(string(body)))
		return http.DefaultClient.Do(r)
	}
	if out, _ := changed.sendModel(changed.outbound[0].Effect, at.Add(time.Second)); out.Result != "delivered" {
		t.Fatalf("old definition did not dispatch: %+v", out)
	}
	must("stock", "stock.item.edit", "stock.item", "I1", map[string]any{"qty": 2})
	for i, tc := range []struct {
		output   string
		tokens   int
		tools    bool
		accepted bool
	}{
		{output, 20, false, true},
		{`{"summary":"bad","category":"routine","review":"false"}`, 20, false, false},
		{`{"summary":"bad","category":"routine","review":false,"extra":1}`, 20, false, false},
		{output, 257, false, false},
		{output, 20, true, false},
	} {
		if i > 0 {
			must("stock", "stock.item.advise", "stock.item", "I1", map[string]any{})
		}
		response.Store(modelResponse{Output: tc.output, Tokens: tc.tokens, Tools: tc.tools})
		x := tn.outbound[len(tn.outbound)-1].Effect
		out, usage := tn.sendModel(x, at.Add(time.Duration(i+1)*time.Second))
		if (out.Result == "delivered") != tc.accepted || usage == nil {
			t.Fatalf("incorrect type/budget outcome: %+v", out)
		}
		if i == 0 {
			fail = true
			tn.settleWithUsage(x.ID, out, usage, usage.At)
			if tn.outbound[0].State != "pending" || len(tn.ai.Usage()) != 0 {
				t.Fatal("failed settlement leaked answer or usage")
			}
			fail = false
		}
		tn.settleWithUsage(x.ID, out, usage, usage.At)
		if tn.quarantined() {
			t.Fatalf("function settlement quarantined: %+v", tn.fault.Load())
		}
		CheckReplay(t, tn, entries, compose)
	}
	must("stock", "stock.item.advise", "stock.item", "I1", map[string]any{})
	must(PlatformApp, SchemaRevoke, MemberType, "ana", map[string]string{"app": "stock"})
	before := requests.Load()
	out, usage := tn.sendModel(tn.outbound[len(tn.outbound)-1].Effect, at.Add(time.Minute))
	if out.Result != "rejected" || usage != nil || requests.Load() != before {
		t.Fatal("revoked source grant reached the provider")
	}
	tn.settleWithUsage(out.Effect, out, usage, at.Add(time.Minute))
	CheckReplay(t, tn, entries, compose)
}

func TestAIFunctionDiscoveryAndDependencyBinding(t *testing.T) {
	tn := functionTenant(t, "", []string{"secret"})
	ref := platform.AssetRef{App: "stock", Kind: platform.AssetFunction, Name: "record-advice"}
	for _, id := range []string{"ana", "lead"} {
		member, _ := tn.Member(id)
		found := false
		for _, def := range tn.Definitions(member) {
			if def.Ref == ref {
				found = true
			}
		}
		if found != (id == "lead") {
			t.Fatalf("function discovery ignored source field grants for %s", id)
		}
	}
	first, err := tn.releaseCandidateLocked([]platform.AssetRef{ref})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := platform.ReadCandidate(first.ID, first.Bytes); err != nil {
		t.Fatal(err)
	}
	changed := functionTenant(t, "Changed instructions", []string{"secret"})
	second, err := changed.releaseCandidateLocked([]platform.AssetRef{ref})
	if err != nil || first.ID == second.ID {
		t.Fatalf("changed function retained dependency identity: %v", err)
	}
	for i, asset := range first.Assets {
		if asset.Ref == ref {
			first.Assets[i].Requires = nil
			if _, err := platform.Candidate([]platform.AssetRef{ref}, first.Assets); err == nil {
				t.Fatal("function omitted its source object dependency")
			}
			return
		}
	}
	t.Fatal("function was missing from its own closure")
}
