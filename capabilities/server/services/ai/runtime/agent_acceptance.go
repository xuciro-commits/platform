package agents

import (
	"encoding/json"
	"fmt"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/acceptedformat"
	"platformserver/platform"
)

type AgentAcceptance struct {
	Tenant      string
	Agent       platform.AgentRuntime
	Draft       platform.Runtime
	RunAgent    func(run string) string
	UsageBefore func() (string, error)
	Answer      func(time.Time) *kernel.Error
	Observers   func() *kernel.Error
	Failure     func() *kernel.Error
	Events      func() []platform.Event
	Changes     func(last platform.Event, at time.Time) ([]byte, error)
	Prepared    func(body, principal []byte, key, hash string)
}

func (a AgentAcceptance) Accept(b platform.AgentResultStep, at time.Time) (err error) {
	var name string
	if b.Evaluation != nil {
		name = b.Evaluation.Agent
	} else {
		name = a.RunAgent(b.Run)
	}
	member := a.Agent.AgentPrincipal(name)
	if b.Evaluation != nil && b.Evaluation.Candidate != "" {
		member = platform.Member{ID: "app:" + platform.AgentApp, Tenant: a.Tenant}
	}
	c := platform.NewCaller(a.Draft, member, platform.AgentApp, false, true)
	r := acceptedformat.RuntimeAgent{Version: 1, Kind: "agent-result", Tenant: a.Tenant, App: platform.AgentApp, Member: member.ID, At: at.UTC(), Step: b}
	if b.Evaluation == nil && b.Usage.Model != "" {
		if r.UsageBefore, err = a.UsageBefore(); err != nil {
			return err
		}
	}
	refusal := a.Agent.ApplyAgentStepCaller(c, b, at)
	if refusal == nil {
		refusal = a.Failure()
	}
	if refusal == nil {
		refusal = a.Answer(at)
	}
	if refusal == nil {
		refusal = a.Observers()
	}
	events := a.Events()
	if refusal != nil || len(events) == 0 {
		return fmt.Errorf("agent answer has no complete decision: %v", refusal)
	}
	last := events[len(events)-1]
	if r.Changes, err = a.Changes(last, at); err != nil {
		return err
	}
	r.Key = "agent:" + last.Record.GetSubmission().GetIdempotencyKey()
	r.RequestHash, err = acceptedformat.AgentRequestHash(r)
	if err == nil {
		r.Digest, err = acceptedformat.DigestAgent(r)
	}
	var raw []byte
	if err == nil {
		raw, err = json.Marshal(r)
	}
	if err == nil {
		_, err = acceptedformat.DecodeAgent(raw)
	}
	if err != nil {
		return err
	}
	principal, _ := json.Marshal(member)
	a.Prepared(raw, principal, r.Key, r.RequestHash)
	return nil
}
