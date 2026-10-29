package platformserver

import agentowner "platformserver/engine/agents"

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"platformserver/internal/agents"
	controlowner "platformserver/kernel/control"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/ai"
	"platformserver/apps/work"
	automation "platformserver/engine/automation"
	"platformserver/platform"
)

func TestAgentStepUsesJournalJSONOrder(t *testing.T) {
	got, err := agents.NormalizeStepJSON(json.RawMessage(`{"z":{"y":2,"x":1},"a":9007199254740993}`))
	if err != nil || string(got) != `{"a":9007199254740993,"z":{"x":1,"y":2}}` {
		t.Fatalf("model step does not use stable JSONB-compatible bytes: %s, %v", got, err)
	}
}

func TestAgentObservationBoundsPreserveJSONAndChinese(t *testing.T) {
	for _, input := range []any{
		map[string]any{"record": map[string]any{"id": "工单一", "note": strings.Repeat("中文备注", 2000)}, "links": []string{"a", "b"}},
		[]any{map[string]any{"text": strings.Repeat("生产进度", 1000)}, map[string]any{"id": "next"}},
	} {
		raw, _ := json.Marshal(input)
		got := agents.Observation(string(raw), 256)
		if !json.Valid([]byte(got)) || !utf8.ValidString(got) || len(got) > 256 || !strings.Contains(got, `"truncated":true`) {
			t.Fatalf("invalid bounded observation (%d bytes): %s", len(got), got)
		}
	}
	if got := agents.Clip(strings.Repeat("中文", 20), 5); !utf8.ValidString(got) || got != "中…" {
		t.Fatalf("text cut inside a character: %q", got)
	}
	for _, args := range []string{`{"id":`, `{"id":1,"id":2}`, `[]`} {
		if _, err := agents.CompleteToolCalls([]platform.ToolCall{{ID: "a", Name: "read", Arguments: json.RawMessage(args)}}, "r", 1); err == nil {
			t.Fatalf("accepted incomplete or ambiguous tool input: %s", args)
		}
	}
}

func TestAgentMultipleCallsResumeAccepted(t *testing.T) {
	now := time.Date(2026, 10, 19, 9, 0, 0, 0, time.UTC)
	var modelCalls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		modelCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"处理两张工单","tool_calls":[{"id":"first","type":"function","function":{"name":"desk_ticket_answer","arguments":"{\"target\":\"T1\",\"reply\":\"第一张\",\"rationale\":\"answer first\"}"}},{"id":"second","type":"function","function":{"name":"desk_ticket_answer","arguments":"{\"target\":\"T2\",\"reply\":\"第二张\",\"rationale\":\"answer second\"}"}},{"id":"end","type":"function","function":{"name":"finish","arguments":"{\"result\":\"完成\",\"rationale\":\"finished\"}"}}]}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`)
	}))
	approveTestInference(t, model.URL, "lm")
	t.Cleanup(model.Close)
	build := func() *Tenant {
		return composeTenant(t, "t-1", []platform.Seat{seatOf("ana", "desk:clerk", "platform:admin", "ai:admin", "agent:admin")},
			ai.New("t-1"), work.New("t-1"), automation.New("t-1"), agentowner.New("t-1"), newDesk("t-1"))
	}
	tn := build()
	var entries []Entry
	record := func(tn *Tenant) {
		tn.Record = func(e Entry) { entries = append(entries, e) }
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			entries = append(entries, e)
			return e.Body, nil
		}
	}
	record(tn)
	for _, id := range []string{"T1", "T2"} {
		decide(t, tn, "ana", "desk", "desk.ticket.open", "desk.ticket", id, map[string]any{"subject": id}, now)
	}
	decide(t, tn, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"}, now)
	decide(t, tn, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "lm/scripted", map[string]any{"access": "users"}, now)
	decide(t, tn, "ana", controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "agent/model", map[string]any{"value": "lm/scripted"}, now)
	decide(t, tn, "ana", platform.AgentApp, platform.SchemaRunStart, platform.RunType, "R1", map[string]any{"agent": "desk.triage", "goal": "answer both", "act": true}, now)
	tn.Think(now)
	run, _ := platform.Get[platform.AgentRunRecord](tn.automation(platform.AgentApp, false), "R1")
	if run.StepsUsed != 1 || len(run.Pending) != 2 || len(run.Steps[0].Calls) != 3 || run.Steps[0].CallID != "first" {
		t.Fatalf("model response was not retained: %+v", run)
	}
	checkpoint := slices.Clone(entries)
	for _, interruption := range []string{"cancel", "revoke"} {
		t.Run(interruption, func(t *testing.T) {
			interrupted := build()
			if err := interrupted.Replay(checkpoint); err != nil {
				t.Fatal(err)
			}
			if interruption == "cancel" {
				decide(t, interrupted, "ana", platform.AgentApp, platform.SchemaRunCancel, platform.RunType, "R1", map[string]any{}, now)
			} else {
				console := testConsoleOf(interrupted)
				rewriteMember(t, console, "ana", func(member *platform.Member) {
					delete(member.Roles, "desk")
				})
			}
			interrupted.Think(now.Add(time.Second))
			stopped, _ := platform.Get[platform.AgentRunRecord](interrupted.automation(platform.AgentApp, false), "R1")
			first, _ := platform.Get[Ticket](interrupted.automation("desk", false), "T1")
			second, _ := platform.Get[Ticket](interrupted.automation("desk", false), "T2")
			if first.Revision != 1 || second.Revision != 1 || first.Status != "open" || second.Status != "open" {
				t.Fatalf("a draft wrote before confirmation after %s: %+v %+v", interruption, first, second)
			}
			if interruption == "revoke" {
				// The person lost the role the draft would use: it waits, and the
				// confirmation is refused by the original authorization.
				if stopped.State != "waiting" {
					t.Fatalf("revoked run changed state: %+v", stopped)
				}
				if got := refuse(t, interrupted, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "R1", map[string]any{}, now.Add(time.Second)); !strings.HasPrefix(got, "ERROR_CODE_POLICY_DENIED") {
					t.Fatalf("revoked confirmation: %s", got)
				}
				return
			}
			if stopped.State != "stopped" || len(stopped.Pending) != 0 {
				t.Fatalf("pending call executed after %s: %+v", interruption, stopped)
			}
			results := map[string]bool{}
			for _, step := range stopped.Steps {
				if step.CallID != "" {
					results[step.CallID] = true
				}
			}
			if len(results) != 3 {
				t.Fatalf("interrupted calls lost their results: %+v", stopped.Steps)
			}
		})
	}
	// Restart before the first draft is confirmed. The other calls resume from
	// the saved response, without another model call or another first draft;
	// each write still waits for its own confirmation.
	restored := build()
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	record(restored)
	restored.AIClient = func(*http.Request) (*http.Response, error) {
		t.Fatal("resuming accepted calls requested the model again")
		return nil, nil
	}
	restored.Think(now.Add(time.Second))
	decide(t, restored, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "R1", map[string]any{}, now.Add(2*time.Second))
	restored.Think(now.Add(3 * time.Second))
	decide(t, restored, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "R1", map[string]any{}, now.Add(4*time.Second))
	restored.Think(now.Add(5 * time.Second))
	run, _ = platform.Get[platform.AgentRunRecord](restored.automation(platform.AgentApp, false), "R1")
	first, _ := platform.Get[Ticket](restored.automation("desk", false), "T1")
	second, _ := platform.Get[Ticket](restored.automation("desk", false), "T2")
	if run.State != "done" || len(run.Pending) != 0 || run.StepsUsed != 1 || run.TokensUsed != 120 || modelCalls.Load() != 1 ||
		first.Revision != 2 || second.Revision != 2 || first.Reply != "第一张" || second.Reply != "第二张" {
		t.Fatalf("bad resumed run: %+v; tickets: %+v / %+v", run, first, second)
	}
	req := restored.agents.AgentEvaluationRuntime().Prompt(restored.automation(platform.AgentApp, false), restored.agents.AgentEvaluationRuntime().Definition(run.Agent), run, "lm/scripted", now)
	if len(req.Messages) != 6 || len(req.Messages[2].ToolCalls) != 3 || req.Messages[3].ToolCallID != "first" || req.Messages[4].ToolCallID != "second" || req.Messages[5].ToolCallID != "end" {
		t.Fatalf("model call/result pairing was lost: %+v", req.Messages)
	}
	CheckReplay(t, restored, entries, build)
}

// desk is a test app: tickets answered by people or by its triage agent.
type Ticket struct {
	platform.Record
	Subject string `json:"subject" field:"required,search" help:"What the customer asks, in their words" synonyms:"topic"`
	Status  string `json:"status" field:"readonly" choices:"open,answered"`
	Reply   string `json:"reply,omitempty" field:"readonly" type:"longtext" knowledge:"true"`
}

type desk struct{ ledger *platform.Ledger }

func newDesk(tenant string) *desk {
	clerk := []string{"clerk"}
	return &desk{ledger: platform.NewLedger(tenant, "desk", platform.NewCatalog(
		platform.Action{Schema: "desk.ticket.open", Target: "desk.ticket", Capability: "tickets", Title: "Open ticket", Description: "Open a ticket.",
			Payload: []platform.Field{{Name: "subject", Type: "string", Required: true, Description: "Subject"}}, Roles: []string{"clerk", "viewer"}},
		platform.Action{Schema: "desk.ticket.answer", Target: "desk.ticket", Capability: "tickets", Title: "Answer ticket", Description: "Answer an open ticket.",
			Payload: []platform.Field{{Name: "reply", Type: "string", Required: true, Description: "The answer to the customer"}}, Roles: clerk},
	), "desk.ticket")}
}

var triage = platform.Agent{Name: "triage", Title: "Triage", Instructions: "Answer tickets.", Tools: []string{"desk.ticket.answer", "read:queue"},
	Budget: platform.Budget{Steps: 4},
	// Its suite: one case it answers, one it never does.
	Cases: []platform.Case{
		{Name: "answers wifi", Goal: "Answer ticket T1: wifi", Ref: "desk.ticket/T1", Check: func(r platform.CaseRun) string {
			if !slices.ContainsFunc(r.Actions, func(a string) bool { return strings.HasPrefix(a, "desk.ticket.answer T1 ") }) {
				return "no answer to T1"
			}
			return ""
		}},
		{Name: "says goodbye", Goal: "Answer ticket T1: wifi", Ref: "desk.ticket/T1", Check: func(r platform.CaseRun) string {
			if !slices.ContainsFunc(r.Actions, func(a string) bool { return strings.Contains(a, "Goodbye") }) {
				return "it did not say goodbye"
			}
			return ""
		}},
	},
	Guard: func(_ platform.Caller, _ platform.AgentRun, action, _ string, payload json.RawMessage) *kernel.Error {
		if action == "desk.ticket.answer" && strings.Contains(string(payload), "refund") {
			return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED} // refunds are for people
		}
		return nil
	},
	To: func(platform.Caller, platform.AgentRun) []platform.Recipient {
		return []platform.Recipient{{AppRole: "clerk"}}
	}}

// scout asks a partner's agent through an effect and waits for its answer.
var scout = platform.Agent{Name: "scout", Title: "Scout", Instructions: "Ask the partner.", Tools: []string{"emit:lookup"}}

// handle: a ticket whose subject says "auto" is drafted by the agent; stopped, a clerk answers.
var handle = platform.Flow{Name: "handle", Title: "Handle ticket", Version: 1,
	Start: platform.Start{On: []string{"desk.ticket.open"}, Begin: func(c platform.Caller, e platform.Event) (string, any, bool) {
		t, _ := platform.Get[Ticket](c, e.Record.GetSubmission().GetTarget().GetId())
		return t.ID, nil, strings.Contains(t.Subject, "auto")
	}},
	Steps: []platform.Step{
		{Name: "draft", Fault: "manual", Agent: &platform.AgentStep{Agent: "triage", To: clerksOf,
			Goal: func(c platform.Caller, r *platform.Run) string {
				t, _ := platform.Get[Ticket](c, r.Key)
				return "Answer ticket " + r.Key + ": " + t.Subject
			},
			Ref: func(_ platform.Caller, r *platform.Run) string { return "desk.ticket/" + r.Key }},
			Choose: func(c platform.Caller, r *platform.Run) (string, string) {
				if t, _ := platform.Get[Ticket](c, r.Key); strings.Contains(t.Subject, "undo") {
					return platform.Compensate, "the answer is not wanted"
				}
				return "", "the agent answered: " + r.Answer
			}},
		{Name: "manual", Ask: &platform.Ask{Title: func(_ platform.Caller, r *platform.Run) string { return "Answer " + r.Key }, To: clerksOf}},
	}}

var clerksOf = func(platform.Caller, *platform.Run) []platform.Recipient {
	return []platform.Recipient{{AppRole: "clerk"}}
}

func (d *desk) AcceptedLedger() *platform.Ledger { return d.ledger }

func (d *desk) Manifest() platform.Manifest {
	return platform.Manifest{ID: "desk", Version: "1", Actions: d.ledger.Catalog, Reads: []string{"queue"},
		Entities: []platform.Entity{{Type: "desk.ticket", Title: "Ticket", Model: Ticket{}, Display: "subject", Synonyms: "case,issue",
			Description: "A customer's request the desk answers."}},
		Agents: []platform.Agent{triage, scout}, Flows: []platform.Flow{handle},
		Emits: []platform.EffectKind{{Name: "lookup", Title: "Ask the partner", Description: "Ask the partner's agent a question."}}}
}
func (d *desk) Declarations() []*pb.AuthorityDeclaration { return d.ledger.Declarations() }
func (d *desk) Snapshot() (json.RawMessage, error)       { return d.ledger.Snapshot() }
func (d *desk) Restore(raw json.RawMessage) error        { return d.ledger.Restore(raw) }
func (d *desk) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}
func (d *desk) Read(c platform.Caller, _ string) (any, *kernel.Error) {
	out, _, _ := platform.Find[Ticket](c, platform.Query{Domain: json.RawMessage(`[["status","=","open"]]`)})
	return out, nil
}
func (d *desk) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	return d.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		var p struct{ Subject, Reply string }
		json.Unmarshal(s.GetPayload(), &p)
		t, known := platform.Get[Ticket](c, s.GetTarget().GetId())
		if s.GetSchema().GetName() == "desk.ticket.open" {
			t = Ticket{Record: platform.Record{ID: s.GetTarget().GetId()}, Subject: p.Subject, Status: "open"}
		} else if !known || t.Status != "open" {
			return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		} else {
			t.Status, t.Reply = "answered", p.Reply
		}
		return func(r *pb.ChangeRecord) { c.Put(r, t) }, nil
	})
}

// scriptedModel answers on the OpenAI wire with the tool calls a script
// chooses from the goal and the steps so far; "fail" goals get errors.
func scriptedModel(t *testing.T) *httptest.Server {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Messages []map[string]any }
		json.NewDecoder(r.Body).Decode(&req)
		goal, _ := req.Messages[1]["content"].(string)
		var results []string
		for _, m := range req.Messages {
			if m["role"] == "tool" {
				results = append(results, fmt.Sprint(m["content"]))
			}
		}
		call := func(name string, args map[string]any) {
			args["rationale"] = "because " + name
			raw, _ := json.Marshal(args)
			fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"c%d","type":"function","function":{"name":%q,"arguments":%q}}]}}],"usage":{"prompt_tokens":100,"completion_tokens":20}}`,
				len(results), name, string(raw))
		}
		w.Header().Set("Content-Type", "application/json")
		is := func(prefix string) bool { return strings.HasPrefix(goal, "Goal: "+prefix) }
		switch n := len(results); {
		case strings.Contains(strings.SplitN(goal, "\n", 2)[0], "fail"):
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, `{"error":{"message":"the model is down"}}`)
		case is("remember") && n == 0:
			call("remember", map[string]any{"fact": "ana signs replies with her first name", "about_person": true})
		case is("scout") && n == 0:
			call("emit_lookup", map[string]any{"message": "What is the lead time of P-200?"})
		case is("loop"):
			call("search", map[string]any{"query": "T"})
		case is("ask") && n == 0:
			call("ask", map[string]any{"question": "May I answer T3?", "answers": []string{"yes", "no"}})
		case is("speak"): // the run's language reaches the prompt
			system, _ := req.Messages[0]["content"].(string)
			call("finish", map[string]any{"result": map[bool]string{true: "中文", false: "English"}[strings.Contains(system, "Simplified Chinese")]})
		case is("refund") && n == 0:
			call("desk_ticket_answer", map[string]any{"target": "T4", "reply": "a refund"})
		case is("Answer ticket") && n == 0:
			call("context", map[string]any{"type": "desk.ticket", "id": strings.TrimSuffix(strings.Fields(goal)[3], ":")})
		case is("Answer ticket") && n == 1:
			call("desk_ticket_answer", map[string]any{"target": strings.TrimSuffix(strings.Fields(goal)[3], ":"), "reply": "Hello"})
		default:
			call("finish", map[string]any{"result": "after " + fmt.Sprint(n) + ": " + results[len(results)-1]})
		}
	}))
	approveTestInference(t, s.URL, "lm")
	t.Cleanup(s.Close)
	return s
}

// agents run in the host as governed principals; each step the
// model chose is journaled and replayed without calling it.
func testAgents(t *testing.T, accepted bool) {
	model := scriptedModel(t)
	var journal []Entry
	build := func() *Tenant {
		seat := func(id string, roles map[string]string) platform.Seat {
			return platform.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}}
		}
		tn, err := NewTenant("t-1", newConsole("t-1", seat("ana", map[string]string{"desk": "clerk", controlowner.PlatformApp: controlowner.Admin, ai.ID: ai.Admin, platform.AgentApp: platform.AgentAdmin}),
			seat("bo", map[string]string{"desk": "viewer"}),
			platform.Seat{Subjects: []string{"bot"}, Member: platform.Member{ID: "bot", Roles: map[string]string{"desk": "clerk"}, Agent: true}}), ai.New("t-1"), work.New("t-1"), automation.New("t-1"), agentowner.New("t-1"), newDesk("t-1"))
		if err != nil {
			t.Fatal(err)
		}
		tn.AIClient = func(*http.Request) (*http.Response, error) { t.Fatal("a replay called a model"); return nil, nil }
		return tn
	}
	tn := build()
	tn.AIClient = nil // the default client: the local stand-in
	tn.Record = func(e Entry) { journal = append(journal, e) }
	if accepted {
		tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			journal = append(journal, e)
			return e.Body, nil
		}
	}
	member := func(id string) platform.Member {
		m, _ := tn.app(controlowner.PlatformApp).(*controlowner.Console).MemberBySubject(id)
		return m
	}
	now := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	keys := 0
	do := func(who, authority, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		if _, err := tn.Submit(member(who), &pb.Submission{TenantId: "t-1", PrincipalId: who, Authority: authority, IdempotencyKey: fmt.Sprint("k", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	think := func(n int) {
		for range n {
			now = now.Add(time.Second)
			tn.Think(now)
			tn.Work(now)
		}
	}
	run := func(id string) platform.AgentRunRecord {
		r, _ := platform.Get[platform.AgentRunRecord](tn.automation(platform.AgentApp, false), id)
		return r
	}
	steps := func(id string) string {
		var out []string
		for _, s := range run(id).Steps {
			out = append(out, s.Tool+" "+strings.SplitN(s.Outcome, " ", 2)[0])
		}
		return strings.Join(out, ", ")
	}
	ticket := func(id string) Ticket { x, _ := platform.Get[Ticket](tn.automation("desk", false), id); return x }
	expect := func(what, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s:\n got %s\nwant %s", what, got, want)
		}
	}
	start := func(who, id, goal, ref string) string {
		return do(who, platform.AgentApp, platform.SchemaRunStart, platform.RunType, id, map[string]string{"agent": "desk.triage", "goal": goal, "ref": ref})
	}
	for _, id := range []string{"T1", "T2", "T3", "T4"} {
		do("ana", "desk", "desk.ticket.open", "desk.ticket", id, map[string]string{"subject": "Wifi down in room " + id})
	}

	// Without a model the run stops, and its goal goes to the person it ran for.
	expect("start", start("ana", "R0", "Answer ticket T1: wifi", "desk.ticket/T1"), "ok")
	think(1)
	expect("no model", run("R0").State+" "+run("R0").Stopped, "stopped no model is set for agents")
	do("ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "lm", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"})
	do("ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "lm/scripted", map[string]string{"access": "users"})
	do("ana", controlowner.PlatformApp, controlowner.SchemaSettingSet, controlowner.SettingType, "agent/model", map[string]string{"value": "lm/scripted"})

	// For ana, a clerk: it reads the ticket's context and drafts the answer; ana
	// confirms it, and it is done as ana, correlated to the run (D6). The agent finishes.
	expect("start", start("ana", "R1", "Answer ticket T1: wifi", "desk.ticket/T1"), "ok")
	think(4)
	expect("R1 drafts", run("R1").State+" / "+steps("R1"), "waiting / context {\"type\":\"desk.ticket\",\"record\":{\"id\":\"T1\",\"revision\":1,\"created\":{\"by\":\"ana\",\"at\":\"2026-10-01T09:00:00Z\",\"change\":\"chg-1\"},\"changed\":{\"by\":\"ana\",\"at\":\"2026-10-01T09:00:00Z\",\"change\":\"chg-1\"},\"subject\":\"Wifi, desk_ticket_answer drafted")
	expect("draft", fmt.Sprint(run("R1").Draft[0].Action, " ", run("R1").Draft[0].Target, " ", run("R1").Draft[0].Payload, " ", ticket("T1").Status), `desk.ticket.answer T1 {"reply":"Hello"} open`)
	expect("only ana answers her drafts", do("bo", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "R1", map[string]any{}), "ERROR_CODE_POLICY_DENIED")
	expect("confirm", do("ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "R1", map[string]any{"payload": map[string]string{"reply": "Hello, it works again"}}), "ok")
	expect("answered as ana", ticket("T1").Status+" "+ticket("T1").Reply+" "+ticket("T1").Changed.By, "answered Hello, it works again ana")
	think(2)
	expect("R1", run("R1").State+" "+run("R1").Signals[0].Kind+" "+run("R1").Signals[0].Value, `done changed {"reply":"Hello, it works again"}`)

	// In the person's language.
	do("ana", platform.AgentApp, platform.SchemaRunStart, platform.RunType, "RL1", map[string]string{"agent": "desk.triage", "goal": "speak", "language": "zh-CN"})
	do("ana", platform.AgentApp, platform.SchemaRunStart, platform.RunType, "RL2", map[string]string{"agent": "desk.triage", "goal": "speak"})
	think(2)
	expect("languages", run("RL1").Result+" "+run("RL2").Result, "中文 English")
	expect("rationale", run("R1").Steps[1].Rationale, "because desk_ticket_answer")
	// A rejected draft: the agent hears why and goes on.
	start("ana", "R6", "Answer ticket T4: wifi", "desk.ticket/T4")
	think(3)
	expect("reject", do("ana", platform.AgentApp, platform.SchemaRunReject, platform.RunType, "R6", map[string]string{"reason": "not this ticket"}), "ok")
	think(2)
	expect("R6", run("R6").State+" "+run("R6").Signals[0].Kind+" "+ticket("T4").Status+" "+fmt.Sprint(strings.Contains(run("R6").Result, "rejected by ana: not this ticket")), "done rejected open true")
	// For bo, a viewer who may not answer: the agent may not either (D2).
	expect("a viewer starts one", start("bo", "R2", "Answer ticket T2: wifi", "desk.ticket/T2"), "ok")
	think(4)
	expect("R2", run("R2").Steps[1].Outcome[:25]+" "+ticket("T2").Status, "refused: bo may not do th open")
	// An agent of an app the member has no role in cannot be asked.
	expect("a stranger", do("bo", platform.AgentApp, platform.SchemaRunStart, platform.RunType, "R9", map[string]string{"agent": "desk.nope", "goal": "x"}), "ERROR_CODE_POLICY_DENIED")

	// It asks a person, waits, and goes on with the answer.
	start("ana", "R3", "ask about T3", "")
	think(2)
	expect("waiting", run("R3").State, "waiting")
	inbox, _ := tn.Read(member("ana"), "inbox")
	tasks := inbox.([]work.WorkTask)
	task := tasks[slices.IndexFunc(tasks, func(w work.WorkTask) bool { return strings.HasPrefix(w.Title, "May I") })]
	expect("question", fmt.Sprint(task.Title, task.Answers), "May I answer T3?[yes no]")
	do("ana", work.ID, "work.task.complete", work.TaskType, task.ID, map[string]string{"answer": "yes"})
	think(3)
	expect("R3", run("R3").State+" "+run("R3").Result, "done after 1: asked: May I answer T3?\nanswered by ana: yes")

	// The guard refuses a refund; a loop runs out of budget and hands its goal back.
	start("ana", "R4", "refund T4", "")
	think(3)
	expect("guard", run("R4").Steps[0].Outcome[:24]+" "+ticket("T4").Status, "refused by its guard: ER open")
	// The budget is checked before the next call, so the loop stops at its
	// fourth step rather than spend a fifth to learn it is over;
	// a reply that crosses the budget is still counted.
	start("ana", "R5", "loop", "")
	think(6)
	expect("budget", run("R5").State+" "+run("R5").Stopped, "stopped over its budget (4 steps, 480 tokens)")
	inbox, _ = tn.Read(member("ana"), "inbox")
	expect("takeover", fmt.Sprint(slices.ContainsFunc(inbox.([]work.WorkTask), func(w work.WorkTask) bool { return w.Title == "Take over from the agent: loop" })), "true")

	// In a flow: the agent drafts, the flow goes on with its answer; a model
	// that keeps failing stops the run and the flow takes its fault path.
	do("ana", "desk", "desk.ticket.open", "desk.ticket", "T5", map[string]string{"subject": "auto wifi"})
	think(6)
	x, _ := platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), "desk.handle:T5")
	expect("flow", x.State+" "+ticket("T5").Status+" "+x.Answer[:7], "done answered after 2")
	do("ana", "desk", "desk.ticket.open", "desk.ticket", "T6", map[string]string{"subject": "auto fail"})
	think(6)
	x, _ = platform.Get[automation.FlowInstance](tn.automation(automation.ID, false), "desk.handle:T6")
	expect("fault", x.State+" "+x.Tokens[0].Step, "waiting manual")

	// Memory: ana's change proposed a memory, which she keeps;
	// bo may not forget it. The agent remembers a fact about ana itself, and
	// both go into the prompt of her next run, not into bo's.
	memory := func(id string) platform.Memory {
		m, _ := platform.Get[platform.Memory](tn.automation(platform.AgentApp, false), id)
		return m
	}
	expect("proposed", memory("R1:p1").State+" "+memory("R1:p1").For+" "+memory("R1:p1").Fact, `proposed ana ana changed a draft for "Answer ticket T1: wifi" to {"reply":"Hello, it works again"}.`)
	// A memory bo may not read is not there for him to act on:
	// the same answer a read gives, which does not say whose memories exist.
	expect("bo forgets", do("bo", platform.AgentApp, platform.MemoryType+".forget", platform.MemoryType, "R1:p1", map[string]any{}), "ERROR_CODE_POLICY_DENIED")
	expect("ana keeps", do("ana", platform.AgentApp, platform.MemoryType+".keep", platform.MemoryType, "R1:p1", map[string]any{}), "ok")
	start("ana", "R7", "remember this", "")
	think(3)
	expect("remembered", memory("R7:m1").State+" "+memory("R7:m1").For+" "+fmt.Sprint(memory("R7:m1").Expires.Sub(now) > 80*24*time.Hour), "active ana true")
	prompt := func(who string) string {
		return tn.agents.AgentEvaluationRuntime().Prompt(tn.automation(platform.AgentApp, false), tn.agents.AgentEvaluationRuntime().Definition("desk.triage"), platform.AgentRunRecord{Agent: "desk.triage", Goal: "reply to ana", OnBehalf: who}, "m", now).Messages[0].Content
	}
	expect("ana's prompt", fmt.Sprint(strings.Contains(prompt("ana"), "- ana signs replies"), strings.Contains(prompt("ana"), "it works again")), "true true")
	expect("bo's prompt", fmt.Sprint(strings.Contains(prompt("bo"), "What you remember")), "false")
	mine, _ := tn.Read(member("ana"), "memories")
	expect("ana's memories", fmt.Sprint(len(mine.([]platform.Memory))), "3") // R6's rejection proposed one too

	// A candidate model re-runs the runs people answered, dry: R1's changed
	// draft is not what it drafts again, R6's rejected one is. Nothing is done.
	expect("evaluate", do("ana", platform.AgentApp, platform.SchemaEvalStart, platform.EvaluationType, "E1", map[string]string{"agent": "desk.triage", "model": "lm/scripted"}), "ok")
	expect("only administrators", do("bo", platform.AgentApp, platform.SchemaEvalStart, platform.EvaluationType, "E2", map[string]string{"agent": "desk.triage", "model": "lm/scripted"}), "ERROR_CODE_POLICY_DENIED")
	before := ticket("T4").Revision
	tn.Evaluate(now)
	ev, _ := platform.Get[platform.Evaluation](tn.automation(platform.AgentApp, false), "E1")
	var verdicts []string
	for _, x := range ev.Cases {
		verdicts = append(verdicts, x.Run+" "+x.Signal+" "+x.Verdict)
	}
	expect("report", ev.State+" "+strings.Join(verdicts, ", ")+fmt.Sprint(" ", ev.Score, " ", ticket("T4").Revision == before), "done R6 rejected repeats, R1 changed differs 0 true")
	expect("reference", ev.Cases[1].Reference+" / "+ev.Cases[1].Candidate, `desk.ticket.answer T1 {"reply":"Hello, it works again"};  / desk.ticket.answer T1 {"reply":"Hello"}; `)

	// The declared cases, three runs each, dry: one passes every time, one never.
	expect("suite", do("ana", platform.AgentApp, platform.SchemaEvalStart, platform.EvaluationType, "E3", map[string]any{"agent": "desk.triage", "model": "lm/scripted", "suite": true}), "ok")
	t1 := ticket("T1").Revision
	tn.Evaluate(now)
	suite, _ := platform.Get[platform.Evaluation](tn.automation(platform.AgentApp, false), "E3")
	var cases []string
	for _, x := range suite.Cases {
		cases = append(cases, fmt.Sprintf("%s %s %d/%d", x.Case, x.Verdict, x.Passes, x.Runs))
	}
	expect("suite report", suite.State+" "+strings.Join(cases, ", ")+fmt.Sprint(" ", suite.Score, " ", ticket("T1").Revision == t1), "done answers wifi passes 3/3, says goodbye fails 0/3 0.5 true")
	expect("why it fails", suite.Cases[1].Reference, "run 1: it did not say goodbye; run 2: it did not say goodbye; run 3: it did not say goodbye")

	// A flow that compensates undoes what its agent did: a signal on the run.
	do("ana", "desk", "desk.ticket.open", "desk.ticket", "T8", map[string]string{"subject": "auto undo"})
	think(6)
	runs, _, _ := platform.Find[platform.AgentRunRecord](tn.automation(platform.AgentApp, false), platform.Query{Domain: json.RawMessage(`[["flow","=","desk.handle:T8"]]`)})
	expect("undone", runs[0].Signals[0].Kind+" "+runs[0].Signals[0].Detail, "undone the flow asked to undo")

	// Usage is metered as the agent's.
	expect("metered", fmt.Sprint(tn.ai.Spent("agent:desk.triage", now) > 0), "true")

	// The overview: each agent, declared or outside, with what
	// it did and what people made of it.
	overview := func(member string) platform.AgentOverview {
		for _, o := range tn.agents.AgentOverview() {
			if o.Member == member {
				return o
			}
		}
		return platform.AgentOverview{}
	}
	triage := overview("agent:desk.triage")
	expect("overview", fmt.Sprint(triage.Runs > 5, " ", triage.Calls > 0, " ", triage.Judged["changed"], " ", triage.Judged["rejected"], " ", triage.Actions > 0, " ", overview("bot").Outside), "true true 1 1 true true")
	expect("outside agents act", do("bot", "desk", "desk.ticket.open", "desk.ticket", "T20", map[string]string{"subject": "from outside"}), "ok")
	expect("counted", fmt.Sprint(overview("bot").Actions), "1")

	// The off switch (D4): only an agent administrator turns it; a suspended
	// agent's running run stops at its next step, a new one is refused, and an
	// outside agent's decisions and model calls are refused; resumed, it works again.
	expect("start before", start("ana", "R20", "Answer ticket T2: wifi", "desk.ticket/T2"), "ok")
	expect("not bo", do("bo", platform.AgentApp, platform.SchemaSuspend, platform.SwitchType, "agent:desk.triage", map[string]string{}), "ERROR_CODE_POLICY_DENIED")
	expect("suspend", do("ana", platform.AgentApp, platform.SchemaSuspend, platform.SwitchType, "agent:desk.triage", map[string]string{"reason": "misbehaves"}), "ok")
	think(1)
	expect("stopped", run("R20").State+" "+run("R20").Stopped, "stopped suspended by ana")
	expect("no new run", start("ana", "R21", "Answer ticket T2", "desk.ticket/T2"), "ERROR_CODE_POLICY_DENIED")
	expect("suspend bot", do("ana", platform.AgentApp, platform.SchemaSuspend, platform.SwitchType, "bot", map[string]string{}), "ok")
	expect("bot refused", do("bot", "desk", "desk.ticket.open", "desk.ticket", "T21", map[string]string{"subject": "again"}), "ERROR_CODE_POLICY_DENIED")
	_, refused, _ := tn.Chat(member("bot"), platform.ChatRequest{Model: "lm/scripted", Messages: []platform.Message{{Role: "user", Content: "hi"}}}, now)
	expect("bot's model call", refused.Message, "The agent bot is suspended")
	expect("overview says so", fmt.Sprint(overview("agent:desk.triage").Suspended, overview("bot").Suspended), "true true")
	expect("resume", do("ana", platform.AgentApp, platform.SchemaResume, platform.SwitchType, "agent:desk.triage", map[string]string{}), "ok")
	expect("works again", start("ana", "R22", "Answer ticket T2", "desk.ticket/T2"), "ok")
	CheckReplay(t, tn, journal, build)
}
