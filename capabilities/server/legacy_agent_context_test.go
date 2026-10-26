package platformserver

import (
	"encoding/json"
	"platformserver/internal/acceptance"
	resultformat "platformserver/internal/acceptance/result"
	"platformserver/internal/records"
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
	history := []records.RecordChange{{Change: "chg-1", Schema: "flow.instance.start", Fields: []records.FieldChange{{Field: "seen", After: seen}}}}
	prior := &records.Row{Value: reflect.ValueOf(run), History: history}
	oldRun := run
	oldRun.Seen = historical
	oldHistory := records.CopyHistory(history)
	oldHistory[0].Fields[0].After = oldSeen
	wanted := &records.Row{Value: reflect.ValueOf(oldRun), History: oldHistory}
	original, _ := records.FullImage(RunType, run.ID, wanted)
	before, _ := acceptance.Digest(original)
	image := resultformat.BatchRow{App: AgentApp, Before: before, Image: original}
	image.History = append(records.CopyHistory(oldHistory), records.RecordChange{Change: "chg-2", Schema: "flow.instance.step"})
	if restored := legacyAgentContextPredecessor(prior, image); restored == nil || restored.Value.Interface().(AgentRunRecord).Seen != historical {
		t.Fatal("the original sealed context was not restored")
	}
	if restored := legacyAgentContextPredecessor(prior, image); !restored.Value.CanAddr() {
		t.Fatal("the recovered row cannot be decoded by recordOf")
	}
	if prior.Value.Interface().(AgentRunRecord).Seen != current || string(prior.History[0].Fields[0].After) != string(seen) {
		t.Fatal("validation mutated the original predecessor")
	}
	bad := image
	bad.Before = "wrong"
	if legacyAgentContextPredecessor(prior, bad) != nil {
		t.Fatal("an unmatched sealed predecessor was accepted")
	}
	changed := run
	changed.State = "running"
	if legacyAgentContextPredecessor(&records.Row{Value: reflect.ValueOf(changed), History: history}, image) != nil {
		t.Fatal("a business state difference was accepted")
	}
	foreign := image
	foreign.History = records.CopyHistory(image.History)
	foreign.History[0].Fields[0].After = json.RawMessage(`"{\"type\":\"mes.order\",\"record\":{\"id\":\"SO-1\"},\"flows\":[{\"id\":\"another-flow\"}],\"tasks\":[]}"`)
	if legacyAgentContextPredecessor(prior, foreign) != nil {
		t.Fatal("foreign flow context was accepted")
	}
}

func TestRecoveredContextDoesNotGrantANewModelRead(t *testing.T) {
	tn := stockTenant(t, NewAgents("t-1"))
	definition := &agentDef{app: "stock"}
	tn.agents.(*Agents).defs["stock.guide"] = definition
	run := AgentRunRecord{Agent: "stock.guide", Ref: "foreign.record/I1", Seen: "private historical context"}
	request := tn.agents.(*Agents).prompt(tn.automation(AgentApp, false), definition, run, "test", time.Now())
	for _, message := range request.Messages {
		if strings.Contains(message.Content, run.Seen) {
			t.Fatal("a historical context bypassed the current reader")
		}
	}
}

func TestLegacyAgentBindingRecoversOnlyExactCommittedStartupMetadata(t *testing.T) {
	run := AgentRunRecord{State: "running"}
	run.ID = "run"
	history := []records.RecordChange{{Change: "start", Schema: SchemaRunStart, Fields: []records.FieldChange{{Field: "state", After: json.RawMessage(`"running"`)}}}}
	prior := &records.Row{Value: reflect.ValueOf(run), History: history}
	saved := run
	saved.Release = "release:original"
	saved.DefinitionVersion = "agent.sha256.original"
	expectedHistory := records.CopyHistory(history)
	expectedHistory[0].Fields = append([]records.FieldChange{{Field: "release", After: json.RawMessage(`"release:original"`)}, {Field: "definitionVersion", After: json.RawMessage(`"agent.sha256.original"`)}}, expectedHistory[0].Fields...)
	expected := &records.Row{Value: reflect.ValueOf(saved), History: expectedHistory}
	body, _ := records.FullImage(RunType, run.ID, expected)
	digest, _ := acceptance.Digest(body)
	image := resultformat.BatchRow{App: AgentApp, Before: digest, Image: body}
	image.History = append(records.CopyHistory(expectedHistory), records.RecordChange{Change: "step", Schema: SchemaRunStep})
	if recovered := legacyAgentContextPredecessor(prior, image); recovered == nil || recovered.Value.Interface().(AgentRunRecord).DefinitionVersion != saved.DefinitionVersion {
		t.Fatal("exact startup metadata was not restored")
	}
	image.Before = "different"
	if legacyAgentContextPredecessor(prior, image) != nil {
		t.Fatal("nonmatching predecessor accepted")
	}
	if prior.Value.Interface().(AgentRunRecord).Release != "" {
		t.Fatal("failed probe mutated live data")
	}
}
