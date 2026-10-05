package platformserver

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLegacyFlowAgentContextPreservesExactAcceptedPredecessor(t *testing.T) {
	current := `{"type":"mes.order","record":{"id":"SO-1"},"flows":[],"tasks":[]}`
	historical := `{"type":"mes.order","record":{"id":"SO-1"},"flows":[{"id":"F1","flow":"mes.confirm","state":"running","trace":[]}],"tasks":[]}`
	seen, _ := json.Marshal(current)
	oldSeen, _ := json.Marshal(historical)
	run := AgentRunRecord{Flow: "F1", Seen: current, State: "done"}
	run.ID = "R1"
	history := []RecordChange{{Change: "chg-1", Schema: "flow.instance.start", Fields: []FieldChange{{Field: "seen", After: seen}}}}
	prior := &row{value: reflect.ValueOf(run), history: history}
	oldRun := run
	oldRun.Seen = historical
	oldHistory := copyHistory(history)
	oldHistory[0].Fields[0].After = oldSeen
	wanted := &row{value: reflect.ValueOf(oldRun), history: oldHistory}
	original, _ := acceptedRowOf(RunType, run.ID, wanted)
	before, _ := canonicalDigest(original)
	image := acceptedBatchRow{App: AgentApp, Before: before, acceptedRow: original}
	image.History = append(copyHistory(oldHistory), RecordChange{Change: "chg-2", Schema: "flow.instance.step"})
	if restored := legacyAgentContextPredecessor(prior, image); restored == nil || restored.value.Interface().(AgentRunRecord).Seen != historical {
		t.Fatal("the original sealed context was not restored")
	}
	if restored := legacyAgentContextPredecessor(prior, image); !restored.value.CanAddr() {
		t.Fatal("the recovered row cannot be decoded by recordOf")
	}
	if prior.value.Interface().(AgentRunRecord).Seen != current || string(prior.history[0].Fields[0].After) != string(seen) {
		t.Fatal("validation mutated the original predecessor")
	}
	bad := image
	bad.Before = "wrong"
	if legacyAgentContextPredecessor(prior, bad) != nil {
		t.Fatal("an unmatched sealed predecessor was accepted")
	}
	changed := run
	changed.State = "running"
	if legacyAgentContextPredecessor(&row{value: reflect.ValueOf(changed), history: history}, image) != nil {
		t.Fatal("a business state difference was accepted")
	}
	foreign := image
	foreign.History = copyHistory(image.History)
	foreign.History[0].Fields[0].After = json.RawMessage(`"{\"type\":\"mes.order\",\"record\":{\"id\":\"SO-1\"},\"flows\":[{\"id\":\"another-flow\"}],\"tasks\":[]}"`)
	if legacyAgentContextPredecessor(prior, foreign) != nil {
		t.Fatal("foreign flow context was accepted")
	}
}

func TestRecoveredContextDoesNotGrantANewModelRead(t *testing.T) {
	tn := stockTenant(t, NewAgents("t-1"))
	definition := &agentDef{app: "stock"}
	tn.agents.defs["stock.guide"] = definition
	run := AgentRunRecord{Agent: "stock.guide", Ref: "foreign.record/I1", Seen: "private historical context"}
	request := tn.agents.prompt(tn.automation(AgentApp, false), definition, run, "test", time.Now())
	for _, message := range request.Messages {
		if strings.Contains(message.Content, run.Seen) {
			t.Fatal("a historical context bypassed the current reader")
		}
	}
}
