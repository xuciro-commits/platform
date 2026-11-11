package agents

import (
	"encoding/json"
	evaluation "platformserver/services/ai/evaluation"
	"strings"
	"sync"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

type Turns struct {
	Agents      platform.AgentRuntime
	Stopped     func() bool
	Quarantined func() bool
	DueTurns    func(time.Time) []platform.AgentTurn
	Allowed     func(platform.AgentRunRecord) (bool, *kernel.Error)
	Running     func(run string) bool
	Call        func(platform.AIProvider, platform.AIModel, platform.Member, platform.ChatRequest, time.Time) (platform.ChatAnswer, *platform.AIError)
	Meter       func(platform.Member, platform.AIUsage) *kernel.Error
	Step        func(platform.AgentResultStep, time.Time) bool
	Wake        func()
}

func RunUsage(usage platform.AIUsage, run platform.AgentRunRecord) platform.AIUsage {
	usage.Applications = append([]platform.AssetBinding(nil), run.Applications...)
	usage.Run = run.ID
	if owner, name, valid := strings.Cut(run.Agent, "."); valid {
		ref := platform.AssetRef{App: owner, Kind: platform.AssetAgent, Name: name}
		usage.Resource = &ref
	}
	return usage
}
func (r Turns) Due(now time.Time) []func() {
	if r.Agents == nil || r.Stopped() {
		return nil
	}
	var out []func()
	turns := r.DueTurns(now)
	for _, x := range turns {
		if !r.Agents.ClaimAgentTurn(x.Run.ID) {
			continue
		}
		out = append(out, func() {
			defer r.Agents.IdleAgentTurn(x.Run.ID)
			if !r.Stopped() {
				r.take(x, now)
			}
		})
	}
	return out
}

func (r Turns) take(x platform.AgentTurn, now time.Time) {
	{
		if x.Stop != "" {
			r.Step(platform.AgentResultStep{Run: x.Run.ID, Stop: x.Stop}, now)
			return
		}
		if x.Run.OnBehalf != "" {
			allowed, refusal := r.Allowed(x.Run)
			if refusal != nil || !allowed {
				r.Step(platform.AgentResultStep{Run: x.Run.ID, Stop: "the person no longer has access to this agent's app"}, now)
				return
			}
		}
		if len(x.Run.Pending) > 0 {
			r.continueCalls(x.Run, now)
			return
		}
		answer, failure := r.Call(x.Provider, x.Model, r.Agents.AgentPrincipal(x.Run.Agent), x.Request, now)
		answer.Usage = RunUsage(answer.Usage, x.Run)
		if r.Quarantined() {
			return
		}
		if !r.Running(x.Run.ID) {
			if answer.Usage.Model != "" {
				r.Meter(r.Agents.AgentPrincipal(x.Run.Agent), answer.Usage)
			}
			return
		}
		body := platform.AgentResultStep{Run: x.Run.ID, Content: answer.Content, Protocol: answer.Protocol, Usage: answer.Usage}
		if failure != nil {
			if failure.Quota {
				body.Stop = failure.Detail
			} else {
				body.Failure = failure.Detail
			}
		} else if len(answer.ToolCalls) > 0 {
			var err error
			body.Calls, err = evaluation.CompleteToolCalls(answer.ToolCalls, x.Run.ID, x.Run.StepsUsed+1)
			if err != nil {
				body.Failure = err.Error()
			} else if body.Calls[0].Name == "knowledge" {
				body.Observation = r.Agents.ObserveAgentKnowledge(x.Run, body.Calls[0].Arguments, now)
			}
		}
		r.Step(body, now)
	}
}

func (r Turns) continueCalls(run platform.AgentRunRecord, now time.Time) {
	defer r.Wake()
	calls := run.Pending[:1]
	if calls[0].Name == "knowledge" {
		for len(calls) < len(run.Pending) && run.Pending[len(calls)].Name == "knowledge" {
			calls = run.Pending[:len(calls)+1]
		}
	}
	observations := make([]json.RawMessage, len(calls))
	if calls[0].Name == "knowledge" {
		jobs := make(chan int, len(calls))
		for i := range calls {
			jobs <- i
		}
		close(jobs)
		var workers sync.WaitGroup
		for range min(4, len(calls)) {
			workers.Go(func() {
				for i := range jobs {
					observations[i] = r.Agents.ObserveAgentKnowledge(run, calls[i].Arguments, now)
				}
			})
		}
		workers.Wait()
	}
	for i, call := range calls {
		if !r.Step(platform.AgentResultStep{Run: run.ID, CallID: call.ID, Tool: call.Name, Arguments: call.Arguments,
			Observation: observations[i]}, now) {
			break
		}
	}
}
