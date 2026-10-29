package agents

import (
	"cmp"
	"encoding/json"
	"fmt"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"

	"platformserver/internal/agents"
	"platformserver/platform"
	"slices"
	"strings"

	"time"
)

// sourcesOf are the records a run has read, cited or acted on so far: what
// anything derived from it is checked against when it is read again.

func visibleAgentInstructions(c platform.Caller, d *platform.CompiledAgent) string {
	if d.App == "build" && !c.Holds(platform.AgentApp, platform.AgentAdmin) && !c.Holds("build", "builder") {
		return ""
	}
	return d.Instructions
}

// look searches knowledge for a run, as whom it reads, outside the tenant's lock.
func (a *Agents) ObserveAgentKnowledge(run platform.AgentRunRecord, args json.RawMessage, now time.Time) json.RawMessage {
	var p struct{ Query string }
	json.Unmarshal(args, &p)
	app := ""
	if d := a.def(run.Agent); d != nil {
		app = d.App
	}
	return a.host.AgentKnowledge(a.reader(run), app, p.Query, 4, now)
}

// prompt gathers observations through the run's current reader. Conversation
// shaping never receives a reader or any host callback.
func (a *Agents) prompt(c platform.Caller, d *platform.CompiledAgent, run platform.AgentRunRecord, model string, now time.Time) platform.ChatRequest {
	input := agents.Prompt{Goal: run.Goal, OnBehalf: run.OnBehalf, Ref: run.Ref,
		Instructions: d.Instructions, Meaning: a.host.AgentMeaning(d.App),
		Glossary: a.host.AgentGlossary(d.App), Language: a.host.AgentLanguage(run.Language)}
	// Historical Seen bytes remain immutable recovery evidence. New model calls
	// use today's reader, including after a person's grants changed.
	if typ, id, ok := strings.Cut(run.Ref, "/"); ok {
		if view, err := a.host.AgentContext(a.readsAs(run), typ, id, now); err == nil {
			input.Context = string(view.Body)
			input.HasContext = true
		}
	}
	for _, memory := range a.memories(c, run, now) {
		input.Memories = append(input.Memories, memory.Fact)
	}
	for _, step := range run.Steps {
		input.History = append(input.History, agents.HistoryStep{Calls: step.Calls, Content: step.Content,
			Tool: step.Tool, CallID: step.CallID, Outcome: step.Outcome, Rationale: step.Rationale, Arguments: step.Arguments})
	}
	input.Tools = make(map[string]platform.Tool, len(d.Toolset))
	for name, tool := range d.Toolset {
		input.Tools[name] = tool.Tool
	}
	request := input.Request(model)
	step := 0
	for i := range request.Messages {
		if request.Messages[i].Role == "assistant" {
			for step < len(run.Steps) && len(run.Steps[step].Calls) == 0 {
				step++
			}
			if raw := run.Protocol[step]; len(raw) > 0 {
				request.Messages[i].Protocol = raw
			}
			step++
		}
	}
	return request
}

// reader is the person a run works for, or nil for an app's automation. It
// answers about authority: a removed person must not silently become
// automation, and an app's automation drafts nothing.
func (a *Agents) reader(run platform.AgentRunRecord) *platform.Member {
	if run.OnBehalf != "" {
		if m, ok := a.host.Member(run.OnBehalf); ok {
			m.Scopes = slices.Clone(run.Scopes)
			return &m
		}
		return &platform.Member{ID: run.OnBehalf} // no tenant or grants: derived reads refuse it
	}
	return nil
}

// readsAs is who a run's reads happen as: the person it runs for, or its own
// app when no person started it (a flow's agent). An app's automation reads
// its own app's records only — never the host's view of every app, which the
// reads used to fall back to.
func (a *Agents) readsAs(run platform.AgentRunRecord) *platform.Member {
	if m := a.reader(run); m != nil {
		return m
	}
	app := ""
	if d := a.def(run.Agent); d != nil {
		app = d.App
	}
	if app == "" {
		return &platform.Member{ID: "app:" + platform.AgentApp, Tenant: a.tenant} // no grants: nothing is readable
	}
	return a.host.AgentAppReader(app)
}

func memoryEntity() platform.Entity {
	people := []string{platform.AgentAdmin, platform.AnyMember}
	return platform.Entity{Type: platform.MemoryType, Title: "Agent memory", Model: platform.Memory{}, Display: "fact",
		// A fact was taken from what the run read.
		Derived:  []platform.Derivation{{From: "sources", Fields: []string{"fact", "sources"}}},
		Withheld: "withheld",
		Scope:    platform.Scope{Participants: func(record any) []string { return []string{record.(platform.Memory).For} }},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "proposed",
			States: []platform.State{{Name: "proposed", Title: "Proposed", Tone: "warning"}, {Name: "active", Title: "Remembered", Tone: "success"},
				{Name: "forgotten", Title: "Forgotten", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "keep", Title: "Keep", From: []string{"proposed", "active"}, To: []string{"active"}, Roles: people, Capability: "memory",
					Description: "Keep a memory for good: an agent administrator any, a person those about them.", Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
						m := record.(*platform.Memory)
						if err := mayRemember(c, *m); err != nil {
							return err
						}
						m.Expires = time.Time{}
						return nil
					}},
				{Name: "forget", Title: "Forget", From: []string{"proposed", "active"}, To: []string{"forgotten"}, Roles: people, Capability: "memory",
					Description: "Forget a memory: an agent administrator any, a person those about them.", Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
						return mayRemember(c, *record.(*platform.Memory))
					}},
			}}}
}

func mayRemember(c platform.Caller, m platform.Memory) *kernel.Error {
	if c.Replaying || c.Roles[platform.AgentApp] == platform.AgentAdmin || m.For != "" && m.For == c.ID {
		return nil
	}
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
}

// remember keeps what the agent chose to remember, about the person it runs for or for every run.
func (a *Agents) remember(c platform.Caller, run *platform.AgentRunRecord, fact string, aboutPerson bool, now time.Time) (string, func(*pb.ChangeRecord)) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return "refused: nothing to remember", nil
	}
	m := platform.Memory{Record: platform.Record{ID: fmt.Sprintf("%s:m%d", run.ID, len(run.Steps)+1)}, Agent: run.Agent, Fact: agents.Clip(fact, 500), Run: run.ID,
		Sources: run.SourceRefs(), State: "active", Expires: now.AddDate(0, 0, memoryDays)}
	if aboutPerson {
		m.For = run.OnBehalf
	}
	return "remembered" + map[bool]string{true: " about " + m.For, false: ""}[m.For != ""], func(r *pb.ChangeRecord) {
		a.host.Automation(c, platform.AgentApp).Put(r, m)
	}
}

// propose turns a correction into a memory for a person to keep (D5).
func (a *Agents) propose(c platform.Caller, r *pb.ChangeRecord, run platform.AgentRunRecord, s platform.Signal, now time.Time) {
	var fact, about string
	switch s.Kind {
	case "changed":
		fact, about = fmt.Sprintf("%s changed a draft for %q to %s.", s.By, run.Title, s.Value), run.OnBehalf
	case "rejected":
		fact, about = fmt.Sprintf("%s rejected a draft for %q: %s.", s.By, run.Title, cmp.Or(s.Detail, "no reason given")), run.OnBehalf
	case "corrected":
		fact = fmt.Sprintf("People corrected the proposal for %q: %s answered %q.", run.Title, s.By, s.Detail)
	case "discarded":
		fact = fmt.Sprintf("%s discarded %s the agent caused for %q.", s.By, s.Detail, run.Title)
	default:
		return
	}
	m := platform.Memory{Record: platform.Record{ID: fmt.Sprintf("%s:p%d", run.ID, len(run.Signals))}, Agent: run.Agent, Fact: fact, For: about, Run: run.ID, Sources: run.SourceRefs(),
		State: "proposed", Expires: now.AddDate(0, 0, proposalDays)}
	a.host.Automation(c, platform.AgentApp).Put(r, m)
}

// memories are what an agent remembers for a run: active, unexpired, about
// everyone or the person it runs for, the most relevant to the goal first.
func (a *Agents) memories(c platform.Caller, run platform.AgentRunRecord, now time.Time) []platform.Memory {
	domain, _ := json.Marshal([]any{[]any{"agent", "=", run.Agent}, []any{"state", "=", "active"}})
	all, _, _ := platform.Find[platform.Memory](c, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 500})
	goal := map[string]bool{}
	for _, w := range platform.TokenizeWords(run.Goal) {
		goal[w] = true
	}
	overlap := func(m platform.Memory) int {
		n := 0
		for _, w := range platform.TokenizeWords(m.Fact) {
			if goal[w] {
				n++
			}
		}
		return n
	}
	all = slices.DeleteFunc(all, func(m platform.Memory) bool {
		return m.For != "" && m.For != run.OnBehalf || !m.Expires.IsZero() && m.Expires.Before(now)
	})
	slices.SortStableFunc(all, func(x, y platform.Memory) int { return overlap(y) - overlap(x) })
	return all[:min(len(all), memoriesUsed)]
}
