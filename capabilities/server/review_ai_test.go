package platformserver

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/org"
	"platformserver/apps/work"
	"platformserver/platform"
)

// standInCall is one model answer: the tool call a test prescribes.
type standInCall struct {
	Tool      string
	Arguments map[string]any
}

// body is the answer on the OpenAI wire, built by the encoder rather than by
// hand so the test's model cannot be defeated by its own quoting.
func (c standInCall) body(t *testing.T) string {
	t.Helper()
	arguments, err := json.Marshal(c.Arguments)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "",
			"tool_calls": []any{map[string]any{"id": "c1", "type": "function",
				"function": map[string]any{"name": c.Tool, "arguments": string(arguments)}}}}}},
		"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5},
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// modelStandIn answers one model call per turn with the answer the test
// prescribes, so what an agent reads can be checked exactly.
func modelStandIn(t *testing.T, answer func(turn int) standInCall) func(*http.Request) (*http.Response, error) {
	t.Helper()
	turn := 0
	return func(*http.Request) (*http.Response, error) {
		call := answer(turn)
		turn++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(call.body(t)))}, nil
	}
}

// reviewTenant is a desk whose agents run for nobody or for a person, a stock
// app they must not read beyond their scope, and the ai app both paths need.
func reviewTenant(t *testing.T) (*Tenant, func(authority, schema, typ, id string, payload any) *pb.ChangeRecord) {
	t.Helper()
	tn, err := NewTenant("t-1", NewConsole("t-1",
		Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"desk": "clerk", "stock": "lead", ai.ID: ai.Admin, PlatformApp: Admin, AgentApp: AgentAdmin}}},
		Seat{Subjects: []string{"bo"}, Member: platform.Member{ID: "bo", Roles: map[string]string{"desk": "viewer", "stock": "clerk"}}}),
		org.New("t-1", platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Site", Kind: "site"}},
			Units:       []platform.Unit{{ID: "plant", Kind: "plant"}, {ID: "L1", Kind: "line"}},
			Edges:       []platform.Edge{{Structure: "site", Unit: "L1", Parent: "plant"}},
			Memberships: []platform.Membership{{Party: "member:ana", Unit: "plant", Role: "lead"}}}),
		newDesk("t-1"), newStock("t-1"), ai.New("t-1"), work.New("t-1"), flow.New("t-1"), NewAgents("t-1"))
	if err != nil {
		t.Fatal(err)
	}
	ana, _ := tn.Member("ana")
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	keys := 0
	submit := func(authority, schema, typ, id string, payload any) *pb.ChangeRecord {
		t.Helper()
		keys++
		raw, _ := json.Marshal(payload)
		record, err := tn.Submit(ana, &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now)
		if err != nil {
			t.Fatalf("%s %s: %v: %s", schema, id, err, err.Message)
		}
		return record
	}
	submit(PlatformApp, SchemaSettingSet, SettingType, "agent/model", map[string]string{"value": "local/stand-in"})
	submit(ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": "http://local.test/v1"})
	submit(ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/stand-in", map[string]string{"access": "everyone"})
	return tn, submit
}

// ADR-0050 D2, review AI-01: a run no person started (a flow's agent) reads as
// its own app, not as the host. The host holds "*" in every app, so the reads
// used to answer with any app's records; the agent's own records stay readable,
// another app's must not be.
func TestAutomationRunReadsItsOwnAppNotTheHost(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	tn, _ := reviewTenant(t)
	if err := tn.automation("stock", false).PutAt(now, "test.item", Item{Record: platform.Record{ID: "I1"}, Name: "Press", Line: "L1"}); err != nil {
		t.Fatal(err)
	}
	if err := tn.automation("desk", false).PutAt(now, "test.ticket", Ticket{Record: platform.Record{ID: "T1"}, Subject: "Wifi down in room 12", Status: "open"}); err != nil {
		t.Fatal(err)
	}
	// A flow's agent: a run with no person behind it.
	if err := tn.automation(AgentApp, false).PutAt(now, "test.run", AgentRunRecord{Record: platform.Record{ID: "R1"},
		Agent: "desk.triage", Goal: "Look into the wifi", State: "running", Steps: []RunStep{}}); err != nil {
		t.Fatal(err)
	}
	answers := []standInCall{
		{"context", map[string]any{"type": "stock.item", "id": "I1", "rationale": "r"}},
		{"search", map[string]any{"query": "Press", "rationale": "r"}},
		{"context", map[string]any{"type": "desk.ticket", "id": "T1", "rationale": "r"}},
		{"finish", map[string]any{"result": "read what I may", "rationale": "r"}},
	}
	tn.AIClient = modelStandIn(t, func(turn int) standInCall {
		if turn >= len(answers) {
			return standInCall{"finish", map[string]any{"result": "done", "rationale": "r"}}
		}
		return answers[turn]
	})
	for range len(answers) {
		now = now.Add(time.Second)
		tn.Think(now)
	}
	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "R1")
	if len(run.Steps) != len(answers) {
		t.Fatalf("steps: %+v", run.Steps)
	}
	// Another app's record is refused, and its text is nowhere in the trace.
	if !strings.HasPrefix(run.Steps[0].Outcome, "refused:") || strings.Contains(run.Steps[0].Outcome, "Press") {
		t.Fatalf("reading another app's record: %q", run.Steps[0].Outcome)
	}
	if strings.Contains(run.Seen, "Press") || strings.Contains(run.Seen, "L1") {
		t.Fatalf("the run saw another app's record: %q", run.Seen)
	}
	if run.Steps[1].Outcome != "[]" {
		t.Fatalf("searching another app's records: %q", run.Steps[1].Outcome)
	}
	// Its own app's record is what it reads: the agent is not merely blind.
	if !strings.Contains(run.Steps[2].Outcome, "Wifi down in room 12") {
		t.Fatalf("its own app's record: %q", run.Steps[2].Outcome)
	}
	if run.State != "done" || !strings.Contains(run.Result, "read what I may") {
		t.Fatalf("run: %+v", run)
	}
}

// ADR-0050 D2: a run for a person reads as that person, so what the same agent
// may read differs by who it runs for — the fix narrows automation, not people.
func TestRunForAPersonStillReadsAsThatPerson(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 0, 0, 0, time.UTC)
	tn, _ := reviewTenant(t)
	item := Item{Record: platform.Record{ID: "I1"}, Name: "Press", Line: "L1", Secret: "the tolerance is 0.02mm"} // L1 is below ana's plant
	if err := tn.automation("stock", false).PutAt(now, "test.tolerance", item); err != nil {
		t.Fatal(err)
	}
	// ana is a lead in stock; bo, who also has the desk role this agent
	// belongs to, holds none — the same read differs between them.
	for _, who := range []string{"ana", "bo"} {
		if err := tn.automation(AgentApp, false).PutAt(now, "test.run"+who, AgentRunRecord{Record: platform.Record{ID: "R-" + who},
			Agent: "desk.triage", OnBehalf: who, Goal: "check the press", State: "running", Steps: []RunStep{}}); err != nil {
			t.Fatal(err)
		}
	}
	// Each run is answered by whose goal it is, not by call order: the runs are
	// due together.
	turns := map[string]int{}
	tn.AIClient = func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		who := "ana"
		if strings.Contains(string(raw), "On behalf of: bo") {
			who = "bo"
		}
		call := standInCall{"context", map[string]any{"type": "stock.item", "id": "I1", "rationale": "r"}}
		if turns[who] > 0 {
			call = standInCall{"finish", map[string]any{"result": "checked", "rationale": "r"}}
		}
		turns[who]++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(call.body(t)))}, nil
	}
	for range 2 {
		now = now.Add(time.Second)
		tn.Think(now)
		tn.Think(now)
	}
	read := func(who string) string {
		run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "R-"+who)
		if len(run.Steps) == 0 {
			t.Fatalf("%s's run: %+v", who, run)
		}
		return run.Steps[0].Outcome
	}
	if out := read("ana"); !strings.Contains(out, "the tolerance is 0.02mm") {
		t.Fatalf("a lead's own grants: %q", out)
	}
	if out := read("bo"); !strings.HasPrefix(out, "refused:") || strings.Contains(out, "0.02mm") {
		t.Fatalf("a viewer's read: %q", out)
	}
}

// ADR-0050 D3, review AI-02: a model call happens outside the tenant's lock, so
// a run may be cancelled while its answer is coming. The late answer must not
// run its tool — and what the call cost must still be metered.
func TestLateModelAnswerCannotActAfterCancel(t *testing.T) {
	now := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC)
	tn, submit := reviewTenant(t)
	started, release := make(chan struct{}), make(chan struct{})
	var once bool
	late := standInCall{"finish", map[string]any{"result": "late", "rationale": "late"}}
	tn.AIClient = func(*http.Request) (*http.Response, error) {
		if !once {
			once = true
			close(started)
		}
		<-release // the answer arrives only after the run is cancelled
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(late.body(t)))}, nil
	}
	if err := tn.automation(AgentApp, false).PutAt(now, "test.run", AgentRunRecord{Record: platform.Record{ID: "R1"},
		Agent: "desk.triage", Goal: "Answer ticket T1: wifi", State: "running", Steps: []RunStep{}}); err != nil {
		t.Fatal(err)
	}
	think := make(chan struct{})
	go func() { tn.Think(now.Add(time.Second)); close(think) }()
	<-started
	submit(AgentApp, SchemaRunCancel, RunType, "R1", struct{}{}) // the run stops in flight
	close(release)
	<-think

	run, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "R1")
	if run.State != "stopped" || !strings.Contains(run.Stopped, "stopped by ana") || len(run.Steps) != 0 || run.Result != "" {
		t.Fatalf("a late answer was acted on: %+v", run)
	}
	// The call happened: its tokens are accounted for, not lost.
	if spent := tn.ai.Spent("agent:desk.triage", now.Add(time.Second)); spent != 15 {
		t.Fatalf("late usage metered: %d", spent)
	}
	if tn.agents.busy["R1"] {
		t.Fatal("the cancelled run stayed busy")
	}
}

// ADR-0050 D4, review AI-03: one answer may not ask for more than the tenant's
// setting allows, and a call asking for nothing gets that ceiling rather than
// an adapter's own larger default.
func TestAnswersAreCapped(t *testing.T) {
	now := time.Date(2026, 10, 5, 11, 0, 0, 0, time.UTC)
	tenant, submit := reviewTenant(t)
	ana, _ := tenant.Member("ana")
	var asked []int
	tenant.AIClient = func(r *http.Request) (*http.Response, error) {
		var body struct {
			MaxTokens int `json:"max_tokens"`
		}
		raw, _ := io.ReadAll(r.Body)
		json.Unmarshal(raw, &body)
		asked = append(asked, body.MaxTokens)
		answer, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
			"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1}})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(string(answer)))}, nil
	}
	for _, want := range []int{0, 100000, 512} {
		if _, err, failure := tenant.Chat(ana, ChatRequest{Model: "local/stand-in", MaxTokens: want,
			Messages: []Message{{Role: "user", Content: "hi"}}}, now); err != nil || failure != nil {
			t.Fatalf("chat asking %d: %v %v", want, err, failure)
		}
	}
	if fmt.Sprint(asked) != "[8192 8192 512]" {
		t.Fatalf("what the provider was asked for: %v", asked)
	}
	// An administrator may lower the tenant's ceiling.
	submit(PlatformApp, SchemaSettingSet, SettingType, ai.ID+"/"+ai.SettingMaxTokens, map[string]string{"value": "256"})
	if _, err, failure := tenant.Chat(ana, ChatRequest{Model: "local/stand-in", MaxTokens: 4096,
		Messages: []Message{{Role: "user", Content: "hi"}}}, now); err != nil || failure != nil {
		t.Fatalf("chat under a lower ceiling: %v %v", err, failure)
	}
	if got := asked[len(asked)-1]; got != 256 {
		t.Fatalf("the tenant's own ceiling: %d", got)
	}
}

// ADR-0050 D4, review AI-03: the chat body is bounded like every other request
// body, before it is decoded.
func TestChatBodyIsBounded(t *testing.T) {
	tenant, _ := reviewTenant(t)
	answer, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": "ok"}}},
		"usage": map[string]any{"prompt_tokens": 1, "completion_tokens": 1}})
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(string(answer)))}, nil
	}
	h := NewHost(Tokens(map[string]string{"ana-token": "ana"}), tenant)
	post := func(body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/v1/ai/chat", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer ana-token")
		rec := httptest.NewRecorder()
		h.Handler().ServeHTTP(rec, req)
		return rec
	}
	if rec := post(`{"model":"local/stand-in","messages":[{"role":"user","content":"hi"}]}`); rec.Code != http.StatusOK {
		t.Fatalf("a chat within the bound: %d %.200s", rec.Code, rec.Body.String())
	}
	big := `{"model":"local/stand-in","messages":[{"role":"user","content":"` + strings.Repeat("x", chatMaxBytes) + `"}]}`
	if rec := post(big); rec.Code != http.StatusBadRequest {
		t.Fatalf("a chat over the bound: %d %.200s", rec.Code, rec.Body.String())
	}
}

// wireAnswer is one answer on the OpenAI wire, with exactly the usage a test
// wants the provider to report: nil says nothing, a map is reported as given.
func wireAnswer(t *testing.T, content string, usage map[string]any) *http.Response {
	t.Helper()
	body := map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}}}
	if usage != nil {
		body["usage"] = usage
	}
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
		Body: io.NopCloser(strings.NewReader(string(raw)))}
}

// ADR-0050 D7, review AI-05: a provider that reports no usage must not be
// booked as spending nothing, and a provider that reports a real zero must not
// be booked as unknown.
func TestUnreportedUsageIsEstimatedAndZeroIsZero(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	tenant, submit := reviewTenant(t)
	ana, _ := tenant.Member("ana")
	chat := func() {
		t.Helper()
		if _, err, failure := tenant.Chat(ana, ChatRequest{Model: "local/stand-in",
			Messages: []Message{{Role: "user", Content: "how long is the lead time of P-200?"}}}, now); err != nil || failure != nil {
			t.Fatalf("chat: %v %v", err, failure)
		}
	}
	// A provider that says nothing about usage: the platform estimates, so the
	// call counts against the day's tokens instead of reading as free.
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		return wireAnswer(t, "the lead time is two weeks", nil), nil
	}
	chat()
	usage := tenant.ai.Usage()
	if len(usage) != 1 || usage[0].TokensReported || !usage[0].TokensEstimated || usage[0].Input == 0 {
		t.Fatalf("unreported usage: %+v", usage)
	}
	spent := tenant.ai.Spent("ana", now)
	if spent != usage[0].Input+usage[0].Output || spent == 0 {
		t.Fatalf("the day's tokens with unreported usage: %d", spent)
	}
	// A cap at exactly what was estimated refuses the next call: the estimate
	// is what the cap is held against.
	submit(PlatformApp, SchemaSettingSet, SettingType, ai.ID+"/"+ai.SettingDailyTokens, map[string]string{"value": fmt.Sprint(spent)})
	if _, err, failure := tenant.Chat(ana, ChatRequest{Model: "local/stand-in",
		Messages: []Message{{Role: "user", Content: "again"}}}, now); err != nil || failure == nil || !failure.Quota {
		t.Fatalf("a call past the estimated spend: err %v, failure %v", err, failure)
	}
	// A provider that reports a real zero is reported, and adds zero.
	submit(PlatformApp, SchemaSettingSet, SettingType, ai.ID+"/"+ai.SettingDailyTokens, map[string]string{"value": "100000"})
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		return wireAnswer(t, "no tokens at all", map[string]any{"prompt_tokens": 0, "completion_tokens": 0}), nil
	}
	chat()
	usage = tenant.ai.Usage()
	last := usage[len(usage)-1]
	if !last.TokensReported || last.TokensEstimated || last.Input+last.Output != 0 {
		t.Fatalf("a reported zero: %+v", last)
	}
	if got := tenant.ai.Spent("ana", now); got != spent {
		t.Fatalf("a reported zero added %d tokens", got-spent)
	}
	// The totals keep the two apart, and say how many calls never had a cost
	// reported — a cost of 0 is not the same as no cost.
	out, err := tenant.Read(ana, "ai-usage")
	if err != nil {
		t.Fatal(err)
	}
	var totals []ai.Total
	raw, _ := json.Marshal(out)
	var parsed struct {
		Totals []ai.Total `json:"totals"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		t.Fatal("usage totals")
	}
	totals = parsed.Totals
	if len(totals) != 1 || totals[0].Estimated != spent || totals[0].Reported != 0 || totals[0].CostUnknown != 2 {
		t.Fatalf("totals: %+v", totals)
	}
}

// ADR-0050 D7/D9, review AI-05: an agent run's budget counts the estimated
// tokens of a provider that reports none, and its USD budget stops a run only
// on cost that was actually reported.
func TestRunBudgetCountsUnreportedUsageAndCost(t *testing.T) {
	now := time.Date(2026, 10, 5, 13, 0, 0, 0, time.UTC)
	tenant, _ := reviewTenant(t)
	def := *tenant.agents.defs["desk.triage"]
	tenant.agents.defs["desk.triage"] = &def
	seed := func(id string) {
		t.Helper()
		if err := tenant.automation(AgentApp, false).PutAt(now, "test.run"+id, AgentRunRecord{Record: platform.Record{ID: "R" + id},
			Agent: "desk.triage", Goal: "look around", State: "running", Steps: []RunStep{}}); err != nil {
			t.Fatal(err)
		}
	}
	// A provider that reports no usage: with a small token budget, the run must
	// still run out of tokens (the estimate is what counts).
	def.Budget = platform.Budget{Steps: 5, Tokens: 120, Actions: 3}
	seed("1")
	calls := 0
	tenant.AIClient = func(*http.Request) (*http.Response, error) { calls++; return wireAnswer(t, "", nil), nil }
	tenant.Think(now.Add(time.Second))
	run, _ := platform.Get[AgentRunRecord](tenant.automation(AgentApp, false), "R1")
	if run.State != "stopped" || !strings.Contains(run.Stopped, "over its budget") || run.TokensUsed == 0 {
		t.Fatalf("a budget that ignored unreported usage: %+v", run)
	}
	if calls != 1 {
		t.Fatalf("calls made past the budget: %d", calls)
	}
	// A provider that reports a cost over the USD budget stops the run, and
	// says so in USD.
	def.Budget = platform.Budget{Steps: 5, Tokens: 100000, Cost: 0.50, Actions: 3}
	seed("2")
	call := standInCall{"search", map[string]any{"query": "S", "rationale": "r"}}
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		calls++
		raw, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": "",
			"tool_calls": []any{map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": call.Tool, "arguments": `{"query":"S","rationale":"r"}`}}}}}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.60}})
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
	}
	tenant.Think(now.Add(time.Second))
	run, _ = platform.Get[AgentRunRecord](tenant.automation(AgentApp, false), "R2")
	if run.State != "stopped" || !strings.Contains(run.Stopped, "USD") || run.Cost != 0.60 {
		t.Fatalf("a cost budget: %+v", run)
	}
	// A cost no provider reported is unknown, not 0: it must not stop the run
	// as if it were free, and must not be invented as if it were reported.
	seed("3")
	calls = 0
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(strings.NewReader(call.body(t)))}, nil
	}
	tenant.Think(now.Add(time.Second))
	tenant.Think(now.Add(2 * time.Second))
	run, _ = platform.Get[AgentRunRecord](tenant.automation(AgentApp, false), "R3")
	if run.Cost != 0 || strings.Contains(run.Stopped, "USD") || len(run.Steps) != 2 {
		t.Fatalf("an unreported cost: %+v", run)
	}
	if calls != 2 {
		t.Fatalf("calls with an unreported cost: %d", calls)
	}
}

// ADR-0050 D4, review AI-03: a call counts against its limit from the moment
// it passes the door, so calls that start together cannot each pass it — and
// the booking is released by the meter reading that follows the call.
func TestCallsInFlightHoldTheirLimit(t *testing.T) {
	now := time.Date(2026, 10, 5, 14, 0, 0, 0, time.UTC)
	tenant, submit := reviewTenant(t)
	ana, _ := tenant.Member("ana")
	submit(PlatformApp, SchemaSettingSet, SettingType, ai.ID+"/"+ai.SettingPerMinute, map[string]string{"value": "1"})
	started, release := make(chan struct{}), make(chan struct{})
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		close(started)
		<-release
		return wireAnswer(t, "the first answer", map[string]any{"prompt_tokens": 5, "completion_tokens": 4}), nil
	}
	first := make(chan string)
	go func() {
		_, err, failure := tenant.Chat(ana, ChatRequest{Model: "local/stand-in", Messages: []Message{{Role: "user", Content: "first"}}}, now)
		switch {
		case err != nil:
			first <- err.Error()
		case failure != nil:
			first <- failure.Detail
		default:
			first <- "ok"
		}
	}()
	<-started
	// The first call is in flight: the minute's one call is taken.
	if _, err, failure := tenant.Chat(ana, ChatRequest{Model: "local/stand-in", Messages: []Message{{Role: "user", Content: "second"}}}, now); err != nil || failure == nil || !failure.Quota {
		t.Fatalf("a call while the minute's one call was in flight: err %v, failure %v", err, failure)
	}
	close(release)
	if got := <-first; got != "ok" {
		t.Fatalf("the first call: %s", got)
	}
	if calls := tenant.ai.Usage(); len(calls) != 1 || calls[0].Input+calls[0].Output != 9 {
		t.Fatalf("metered calls: %+v", calls)
	}
}

// ADR-0050 D8, review AI-04: transcripts are kept only when the tenant keeps
// them; 0 keeps none and forgets what was kept, and a person's own chat calls
// are served without the per-run check that guards a run's trace.
func TestTranscriptsAreKeptOnlyWhenAsked(t *testing.T) {
	now := time.Date(2026, 10, 5, 15, 0, 0, 0, time.UTC)
	tenant, submit := reviewTenant(t)
	ana, _ := tenant.Member("ana")
	bo, _ := tenant.Member("bo")
	tenant.AIClient = func(*http.Request) (*http.Response, error) {
		return wireAnswer(t, "kept or not", map[string]any{"prompt_tokens": 1, "completion_tokens": 1}), nil
	}
	chat := func(who platform.Member) {
		t.Helper()
		if _, err, failure := tenant.Chat(who, ChatRequest{Model: "local/stand-in", Messages: []Message{{Role: "user", Content: "hello"}}}, now); err != nil || failure != nil {
			t.Fatalf("chat: %v %v", err, failure)
		}
	}
	chat(ana) // the default keeps 30 days
	if kept := tenant.Transcripts("", 10); len(kept) != 1 {
		t.Fatalf("the default kept %d transcripts", len(kept))
	}
	// 0 is a switch, not a period: nothing new is kept, and what was kept is
	// forgotten on the next round.
	submit(PlatformApp, SchemaSettingSet, SettingType, AgentApp+"/"+SettingTranscriptDays, map[string]string{"value": "0"})
	chat(ana)
	if kept := tenant.Transcripts("", 10); len(kept) != 1 {
		t.Fatalf("0 kept something new: %d", len(kept))
	}
	tenant.PurgeTranscripts(now)
	if kept := tenant.Transcripts("", 10); len(kept) != 0 {
		t.Fatalf("0 forgot nothing: %d", len(kept))
	}
	// Kept again, a person's own calls are readable by an agent administrator
	// without a per-run check; another member still may not read them.
	submit(PlatformApp, SchemaSettingSet, SettingType, AgentApp+"/"+SettingTranscriptDays, map[string]string{"value": "7"})
	chat(ana)
	if _, err := tenant.TranscriptsFor(bo, "", 10, now); err == nil {
		t.Fatal("a non-administrator read transcripts")
	}
	kept, err := tenant.TranscriptsFor(ana, "", 10, now)
	if err != nil || len(kept) != 1 || kept[0].Member != "ana" {
		t.Fatalf("a person's own calls: %v %+v", err, kept)
	}
}
