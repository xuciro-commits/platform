package platformserver

import (
	"encoding/json"
	"fmt"
	"time"

	"platformserver/platform"
)

// An agent answer includes the tool decision, resulting run/evaluation record
// and measured spend in one accepted result; recovery never invokes the tool.
type acceptedAgent struct {
	Version     int             `json:"version"`
	Kind        string          `json:"kind"`
	Tenant      string          `json:"tenant"`
	App         string          `json:"app"`
	Member      string          `json:"member"`
	Key         string          `json:"key"`
	At          time.Time       `json:"at"`
	RequestHash string          `json:"requestHash"`
	Digest      string          `json:"digest"`
	Step        stepBody        `json:"step"`
	UsageBefore string          `json:"usageBefore,omitempty"`
	Changes     json.RawMessage `json:"changes"`
}

func digestAgent(r acceptedAgent) (string, error) { r.Digest = ""; return canonicalDigest(r) }
func agentHash(r acceptedAgent) (string, error) {
	return canonicalDigest([]any{r.Tenant, r.Member, r.Step, r.UsageBefore})
}
func decodeAcceptedAgent(raw []byte) (acceptedAgent, error) {
	var r acceptedAgent
	if len(raw) == 0 || len(raw) > maxAcceptedResultBytes {
		return r, fmt.Errorf("agent result is empty or too large")
	}
	if err := decodeAcceptedJSON(raw, &r, "agent result"); err != nil {
		return r, err
	}
	hash, err := agentHash(r)
	digest, digestErr := digestAgent(r)
	batch, receipt, batchErr := decodeAcceptedBatch(r.Changes)
	if err != nil || digestErr != nil || batchErr != nil || r.Version != 1 || r.Kind != "agent-result" || r.Tenant == "" || r.App != AgentApp || r.Member == "" || r.At.IsZero() || r.Step.Run == "" || r.RequestHash != hash || r.Digest != digest ||
		batch.Tenant != r.Tenant || batch.App != AgentApp || !batch.At.Equal(r.At) || receipt.GetSubmission().GetPrincipalId() != r.Member || r.Key != "agent:"+receipt.GetSubmission().GetIdempotencyKey() {
		return r, fmt.Errorf("agent result has inconsistent identity or changes")
	}
	if r.Step.Evaluation == nil && r.Step.Usage.Model != "" {
		if !validUsage(r.Step.Usage) || r.UsageBefore == "" || r.Step.Usage.Member != r.Member {
			return r, fmt.Errorf("agent result has invalid usage")
		}
	} else if r.UsageBefore != "" {
		return r, fmt.Errorf("agent result has unexpected usage")
	}
	return r, nil
}

func (t *Tenant) acceptAgentStep(b stepBody, at time.Time) {
	defer func() {
		if p := recover(); p != nil {
			t.quarantine(fmt.Errorf("agent answer cannot stage: %v", p))
		}
	}()
	draft := t.newStagedDecision()
	draft.at = at
	agent := draft.stateApp(t.agents).(AgentRuntime)
	// Authorization and source fences have been rechecked by agentStep while
	// holding the tenant lock, after the outside model call has completed.
	run, _ := platform.Get[AgentRunRecord](t.automation(AgentApp, false), b.Run)
	if b.Evaluation != nil {
		run.Agent = b.Evaluation.Agent
	}
	member := agent.AgentPrincipal(run.Agent)
	if b.Evaluation != nil && b.Evaluation.Candidate != "" {
		// A candidate's agent need not be installed as a principal yet. Only
		// its original owner records the dry report; it executes no AI tool.
		member = platform.Member{ID: "app:" + AgentApp, Tenant: t.ID}
	}
	c := platform.NewCaller(draft, member, AgentApp, false, true)
	r := acceptedAgent{Version: 1, Kind: "agent-result", Tenant: t.ID, App: AgentApp, Member: member.ID, At: at.UTC(), Step: b}
	var err error
	if b.Evaluation == nil && b.Usage.Model != "" {
		r.UsageBefore, err = t.usagePredecessor()
		if err != nil {
			t.quarantine(err)
			return
		}
	}
	refusal := agent.ApplyAgentStepCaller(c, b, at)
	if refusal == nil {
		refusal = draft.failure
	}
	if refusal == nil {
		refusal = draft.answerRequests(at)
	}
	if refusal == nil {
		refusal = draft.stageObservers()
	}
	if refusal != nil || len(draft.events) == 0 {
		t.quarantine(fmt.Errorf("agent answer has no complete decision: %v", refusal))
		return
	}
	last := draft.events[len(draft.events)-1]
	r.Changes, err = draft.batchResult(last.App, last.Record.GetSubmission(), last.Record, at)
	if err != nil {
		t.quarantine(err)
		return
	}
	r.Key = "agent:" + last.Record.GetSubmission().GetIdempotencyKey()
	r.RequestHash, err = agentHash(r)
	if err == nil {
		r.Digest, err = digestAgent(r)
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(r)
	}
	if err == nil {
		_, err = decodeAcceptedAgent(raw)
	}
	if err != nil {
		t.quarantine(err)
		return
	}
	principal, _ := json.Marshal(member)
	committed, err := t.acceptResult(Entry{App: AgentApp, Kind: "accepted-result", Principal: principal, Body: raw, At: at, Versions: draft.flowVersions()}, r.Key, r.RequestHash)
	if err != nil {
		return
	}
	saved, err := decodeAcceptedAgent(committed)
	if err != nil || saved.Key != r.Key || saved.RequestHash != r.RequestHash {
		t.quarantine(fmt.Errorf("committed agent answer differs: %v", err))
		return
	}
	if _, err = t.applyAcceptedAgent(committed); err != nil {
		t.quarantine(err)
		return
	}
	t.acceptedResultApplied()
	t.enqueue(saved.At)
	t.changedOwner(AgentApp)
}

func (t *Tenant) applyAcceptedAgent(raw []byte) (acceptedAgent, error) {
	r, err := decodeAcceptedAgent(raw)
	if err != nil {
		return r, err
	}
	if r.Tenant != t.ID || t.agents == nil {
		return r, fmt.Errorf("agent result belongs to another tenant")
	}
	if r.UsageBefore != "" {
		before, err := t.usagePredecessor()
		if err != nil || before != r.UsageBefore {
			return r, fmt.Errorf("agent usage predecessor differs: %v", err)
		}
	}
	batch, _, _ := decodeAcceptedBatch(r.Changes)
	if applied, err := t.applyAcceptedBatch(t.agents.AcceptedLedger(), r.Changes); err != nil || !applied {
		return r, fmt.Errorf("agent result application: %v (applied=%t)", err, applied)
	}
	if r.UsageBefore != "" {
		t.ai.Meter(r.Step.Usage)
	}
	t.publishAcceptedBatch(batch)
	return r, nil
}
