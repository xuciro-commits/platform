package platformserver

import (
	"encoding/json"
	"errors"
	resultformat "platformserver/internal/acceptance/result"
	"strings"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestRuntimeUsageAcceptsBeforeMeterAndRecoversWithoutCall(t *testing.T) {
	const id = "runtime-meter"
	at := time.Date(2026, 10, 11, 10, 0, 0, 0, time.UTC)
	build := func() *Tenant { return composeTenant(t, id, []Seat{seatOf("ana")}, ai.New(id)) }
	live := build()
	m := memberOf(t, live, "ana")
	u := ai.Usage{Member: m.ID, Model: "fixture/model", Input: 7, Output: 3, At: at}
	live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	if err := live.meter(m, u); err == nil || len(live.ai.Usage()) != 0 {
		t.Fatal("usage escaped failed acceptance")
	}
	var entries []Entry
	live.Record = func(e Entry) {
		if len(live.ai.Usage()) != 0 {
			t.Fatal("usage visible before append")
		}
		entries = append(entries, e)
	}
	live.AcceptResult = nil
	if err := live.meter(m, u); err != nil || len(entries) != 1 || len(live.ai.Usage()) != 1 {
		t.Fatalf("Record mode did not accept usage: %v", err)
	}
	if entries[0].Kind != "accepted-result" || !strings.Contains(string(entries[0].Body), `"kind":"usage-result"`) {
		t.Fatal("new usage used legacy journal")
	}
	recovered := build()
	if err := recovered.Replay(entries); err != nil || len(recovered.ai.Usage()) != 1 {
		t.Fatalf("usage recovery: %v", err)
	}
	if _, err := recovered.applyAcceptedUsage(entries[0].Body); err == nil || len(recovered.ai.Usage()) != 1 {
		t.Fatal("duplicate usage bypassed predecessor")
	}
	memory := build()
	if err := memory.meter(memberOf(t, memory, "ana"), u); err != nil || len(memory.ai.Usage()) != 1 {
		t.Fatalf("memory did not use same acceptance: %v", err)
	}
}

func TestRuntimeAgentAcceptsStepAndMeterTogether(t *testing.T) {
	const id = "runtime-agent"
	at := time.Date(2026, 10, 11, 11, 0, 0, 0, time.UTC)
	build := func() *Tenant {
		return composeTenant(t, id, []Seat{seatOf("ana", "desk:clerk", "agent:admin")}, ai.New(id), work.New(id), flow.New(id), NewAgents(id), newDesk(id))
	}
	live := build()
	decide(t, live, "ana", AgentApp, SchemaRunStart, RunType, "R", map[string]string{"agent": "desk.triage", "goal": "Answer"}, at)
	image, _, err := live.Snapshot(func() int64 { return 0 })
	if err != nil {
		t.Fatal(err)
	}
	get := func(tn *Tenant) AgentRunRecord {
		r, _ := platform.Get[AgentRunRecord](tn.automation(AgentApp, false), "R")
		return r
	}
	initial := get(live)
	step := stepBody{Run: "R", Content: "Finished", Usage: ai.Usage{Member: "agent:desk.triage", Agent: true, Model: "fixture/model", Input: 3, Output: 2, At: at}}
	live.AcceptResult = func(Entry, string, string) ([]byte, error) { return nil, errors.New("append failed") }
	live.agentStep(step, at)
	if get(live).Revision != initial.Revision || len(live.ai.Usage()) != 0 {
		t.Fatal("agent or meter escaped failed append")
	}
	var entries []Entry
	live.AcceptResult = nil
	live.Record = func(e Entry) {
		if get(live).Revision != initial.Revision || len(live.ai.Usage()) != 0 {
			t.Fatal("agent answer exposed before append")
		}
		entries = append(entries, e)
	}
	live.agentStep(step, at)
	if live.quarantined() || len(entries) != 1 || get(live).State != "done" || len(live.ai.Usage()) != 1 {
		t.Fatalf("agent answer not committed together: fault=%v entries=%d run=%+v", live.fault.Load(), len(entries), get(live))
	}
	recovered := build()
	if err := recovered.Restore(image); err != nil {
		t.Fatal(err)
	}
	if err := recovered.Replay(entries); err != nil || get(recovered).State != "done" || len(recovered.ai.Usage()) != 1 {
		t.Fatalf("agent answer recovery: %v", err)
	}
	var envelope struct{ Kind string }
	_ = json.Unmarshal(entries[0].Body, &envelope)
	if envelope.Kind != "agent-result" {
		t.Fatalf("agent used legacy journal %q", envelope.Kind)
	}
	live.agentStep(step, at)
	if len(entries) != 1 || len(live.ai.Usage()) != 1 {
		t.Fatal("late agent answer applied again")
	}
}

func TestRuntimeWorkAndEffectAcceptInRecordMode(t *testing.T) {
	const id = "runtime-work-effects"
	at := time.Date(2026, 10, 11, 12, 0, 0, 0, time.UTC)
	live := resultWorkTenant(t, id)
	var entries []Entry
	live.Record = func(e Entry) { entries = append(entries, e) }
	decide(t, live, "ana", "stock", "stock.item.create", "stock.item", "I", map[string]any{"name": "Bolt", "qty": 2, "line": "L1"}, at)
	task := live.work.queues["worker"][0]
	live.mu.Lock()
	outcome := live.attempt(task, at, false)
	live.mu.Unlock()
	if outcome != "ok" || len(entries) != 2 {
		t.Fatalf("work Record acceptance: %s %+v", outcome, live.fault.Load())
	}
	saved, err := decodeAcceptedWork(entries[1].Body)
	if err != nil || saved.Kind != "work-result" {
		t.Fatalf("work generated legacy row: %v", err)
	}
	live.outbound = append(live.outbound, &effect{Effect: platform.Effect{ID: "endpoint", Endpoint: "sink", State: "pending", At: at, Due: at}})
	live.settle("endpoint", platform.Outcome{Effect: "endpoint", Result: "delivered"}, at)
	if live.quarantined() || len(entries) != 3 {
		t.Fatalf("effect Record acceptance: %v", live.fault.Load())
	}
	if _, err := resultformat.DecodeEffect(entries[2].Body); err != nil {
		t.Fatal(err)
	}
}
