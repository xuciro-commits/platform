package platformserver

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"platformserver/apps/ai"
	"platformserver/platform"
)

type checkpointDesk struct {
	*desk
	version string
}

func (d *checkpointDesk) Manifest() platform.Manifest {
	m := d.desk.Manifest()
	m.Version, m.Flows = d.version, nil
	m.Agents = []platform.Agent{{Name: "review", Title: "Review", Instructions: "Answer the goal.", Budget: platform.Budget{Steps: 1, Tokens: 10000}, Cases: []platform.Case{
		{Name: "first", Goal: "first"}, {Name: "second", Goal: "second"},
	}}}
	return m
}

// A failed checkpoint leaves only the last accepted item durable. A snapshot
// restart keeps that item, retries the unaccepted one, and retains the original
// requester and pinned configuration. Replaying the completed report calls no AI.
func TestEvaluationCheckpointRestart(t *testing.T) {
	var calls atomic.Int32
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"content":"answer %d"}}],"usage":{"prompt_tokens":8,"completion_tokens":2,"cost":0.01}}`, calls.Add(1))
	}))
	t.Cleanup(model.Close)
	compose := func(version string) *Tenant {
		tn := composeTenant(t, "eval-checkpoint", []Seat{
			seatOf("ana", "platform:admin", "agent:admin", "ai:admin", "desk:clerk"), seatOf("viewer", "desk:viewer"),
		}, ai.New("eval-checkpoint"), NewAgents("eval-checkpoint"), &checkpointDesk{desk: newDesk("eval-checkpoint"), version: version})
		tn.AIClient = func(*http.Request) (*http.Response, error) { t.Fatal("recovery called a model"); return nil, nil }
		return tn
	}
	at := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	live := compose("1")
	live.AIClient = nil
	var entries []Entry
	live.Record = func(e Entry) { entries = append(entries, e) }
	live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
		if e.App == AgentApp {
			var saved acceptedAgent
			if json.Unmarshal(e.Body, &saved) == nil && saved.Step.Evaluation != nil {
				ev := saved.Step.Evaluation
				if ev.State == "queued" && len(ev.Cases) > 0 && ev.Cases[0].Runs == 2 {
					return nil, errors.New("checkpoint append failed")
				}
			}
		}
		entries = append(entries, e)
		return e.Body, nil
	}
	decide(t, live, "ana", ai.ID, ai.SchemaProviderAdd, ai.ProviderType, "local", map[string]any{"kind": "local", "baseUrl": model.URL + "/v1"}, at)
	decide(t, live, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/model", map[string]any{"access": "users", "defaults": platform.ModelOptions{MaxTokens: 64}}, at)
	request := map[string]any{"agent": "desk.review", "model": "local/model", "suite": true}
	if got := refuse(t, live, "viewer", AgentApp, SchemaEvalStart, EvaluationType, "denied", request, at); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("non-administrator queued an evaluation: %s", got)
	}
	decide(t, live, "ana", AgentApp, SchemaEvalStart, EvaluationType, "E1", request, at)
	live.Evaluate(at)
	partial, _ := platform.Get[Evaluation](live.automation(AgentApp, false), "E1")
	if calls.Load() != 2 || partial.State != "queued" || len(partial.Cases) != 1 || partial.Cases[0].Runs != 1 || partial.Cases[0].Candidate != "answer 1" || partial.Tokens != 10 || partial.Cost != 0.01 {
		t.Fatalf("failed checkpoint changed accepted progress: calls=%d evaluation=%+v", calls.Load(), partial)
	}
	image, _, err := live.Snapshot(func() int64 { return int64(len(entries)) })
	if err != nil {
		t.Fatal(err)
	}
	prefix := append([]Entry(nil), entries...)

	t.Run("same process retries accepted item", func(t *testing.T) {
		live.Evaluate(at.Add(time.Second))
		if calls.Load() != 2 {
			t.Fatal("append retry called the model again")
		}
		live.AcceptResult = func(e Entry, _, _ string) ([]byte, error) {
			entries = append(entries, e)
			return e.Body, nil
		}
		live.Evaluate(at.Add(2 * time.Second))
		report, _ := platform.Get[Evaluation](live.automation(AgentApp, false), "E1")
		tokens, cost := 0, 0.0
		for _, usage := range live.ai.Usage() {
			tokens += usage.Input + usage.Output
			cost += usage.Cost
		}
		if calls.Load() != 6 || report.State != "done" || report.Tokens != 60 || tokens != 60 || report.Cost < 0.0599 || report.Cost > 0.0601 || cost < 0.0599 || cost > 0.0601 {
			t.Fatalf("same-process retry repeated model spend: calls=%d tokens=%d cost=%v report=%+v", calls.Load(), tokens, cost, report)
		}
		CheckReplay(t, live, entries, func() *Tenant { return compose("1") })
	})

	for _, scenario := range []string{"resume", "model changed", "definition changed"} {
		t.Run(scenario, func(t *testing.T) {
			version := "1"
			if scenario == "definition changed" {
				version = "2"
			}
			restarted := compose(version)
			if err := restarted.Restore(image); err != nil {
				t.Fatal(err)
			}
			restarted.AIClient = nil
			history := append([]Entry(nil), prefix...)
			restarted.Record = func(e Entry) { history = append(history, e) }
			if scenario == "model changed" {
				decide(t, restarted, "ana", ai.ID, ai.SchemaModelEnable, ai.ModelType, "local/model", map[string]any{"access": "users", "dailyTokens": 1000}, at)
			}
			before := calls.Load()
			restarted.Evaluate(at)
			report, _ := platform.Get[Evaluation](restarted.automation(AgentApp, false), "E1")
			if report.State != "done" || report.Created.By != "ana" || report.DefinitionDigest != partial.DefinitionDigest || report.ModelConfig != partial.ModelConfig || report.Cases[0].Candidate != "answer 1" {
				t.Fatalf("restart lost accepted result, requester or version: %+v", report)
			}
			if scenario != "resume" {
				if calls.Load() != before || report.Fails != 1 || report.Score != 0 || len(report.Cases) != 2 {
					t.Fatalf("changed configuration resumed the old evaluation: calls=%d report=%+v", calls.Load()-before, report)
				}
				return
			}
			if calls.Load()-before != 5 || report.Score != 1 || report.Agrees != 2 || report.Tokens != 60 || report.Cost < 0.0599 || report.Cost > 0.0601 {
				t.Fatalf("completed item was rerun or totals changed: calls=%d report=%+v", calls.Load()-before, report)
			}
			for _, item := range report.Cases {
				if item.Runs != suiteRuns || item.Passes != suiteRuns {
					t.Fatalf("unfinished report: %+v", report)
				}
			}
			for _, usage := range restarted.ai.Usage() {
				if usage.Member != "ana" {
					t.Fatalf("evaluation escaped its requester: %+v", usage)
				}
			}
			CheckReplay(t, restarted, history, func() *Tenant { return compose("1") })
		})
	}
}
