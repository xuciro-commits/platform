package platformserver

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// Agent memory (ADR-0022 D5): short facts an agent keeps across runs, as
// records people see, keep and forget. An agent writes one with its remember
// tool; people's corrections propose one, which counts only once a person
// keeps it. Memories expire unless kept; the relevant ones go into the prompt.
const (
	MemoryType   = "agent.memory"
	memoryDays   = 90 // what an agent remembers, unless kept
	proposalDays = 14 // a proposal nobody kept
	memoriesUsed = 8  // in a prompt
)

// Memory is one fact an agent keeps.
type Memory struct {
	platform.Record
	Agent   string    `json:"agent" field:"readonly,search"`
	Fact    string    `json:"fact" field:"readonly,search" type:"longtext"`
	For     string    `json:"for,omitempty" field:"readonly" title:"About"` // the person it is about; none: every run of the agent
	Run     string    `json:"run,omitempty" field:"readonly" title:"From run"`
	State   string    `json:"state" field:"readonly" choices:"proposed,active,forgotten"`
	Expires time.Time `json:"expires,omitzero" field:"readonly"` // none: kept
}

func memoryEntity() platform.Entity {
	people := []string{AgentAdmin, platform.AnyMember}
	return platform.Entity{Type: MemoryType, Title: "Agent memory", Model: Memory{}, Display: "fact",
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "proposed",
			States: []platform.State{{Name: "proposed", Title: "Proposed", Tone: "warning"}, {Name: "active", Title: "Remembered", Tone: "success"},
				{Name: "forgotten", Title: "Forgotten", Tone: "neutral"}},
			Transitions: []platform.Transition{
				{Name: "keep", Title: "Keep", From: []string{"proposed", "active"}, To: []string{"active"}, Roles: people, Capability: "memory",
					Description: "Keep a memory for good: an agent administrator any, a person those about them.", Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
						m := record.(*Memory)
						if err := mayRemember(c, *m); err != nil {
							return err
						}
						m.Expires = time.Time{}
						return nil
					}},
				{Name: "forget", Title: "Forget", From: []string{"proposed", "active"}, To: []string{"forgotten"}, Roles: people, Capability: "memory",
					Description: "Forget a memory: an agent administrator any, a person those about them.", Do: func(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
						return mayRemember(c, *record.(*Memory))
					}},
			}}}
}

func mayRemember(c platform.Caller, m Memory) *kernel.Error {
	if c.Replaying || c.Roles[AgentApp] == AgentAdmin || m.For != "" && m.For == c.ID {
		return nil
	}
	return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_POLICY_DENIED}
}

// remember keeps what the agent chose to remember, about the person it runs for or for every run.
func (a *Agents) remember(c platform.Caller, run *AgentRunRecord, fact string, aboutPerson bool, now time.Time) (string, func(*pb.ChangeRecord)) {
	fact = strings.TrimSpace(fact)
	if fact == "" {
		return "refused: nothing to remember", nil
	}
	m := Memory{Record: platform.Record{ID: fmt.Sprintf("%s:m%d", run.ID, len(run.Steps)+1)}, Agent: run.Agent, Fact: clip(fact, 500), Run: run.ID,
		State: "active", Expires: now.AddDate(0, 0, memoryDays)}
	if aboutPerson {
		m.For = run.OnBehalf
	}
	return "remembered" + map[bool]string{true: " about " + m.For, false: ""}[m.For != ""], func(r *pb.ChangeRecord) {
		a.t.automation(AgentApp, c.Replaying).Put(r, m)
	}
}

// propose turns a correction into a memory for a person to keep (D5).
func (a *Agents) propose(c platform.Caller, r *pb.ChangeRecord, run AgentRunRecord, s Signal, now time.Time) {
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
	m := Memory{Record: platform.Record{ID: fmt.Sprintf("%s:p%d", run.ID, len(run.Signals))}, Agent: run.Agent, Fact: fact, For: about, Run: run.ID,
		State: "proposed", Expires: now.AddDate(0, 0, proposalDays)}
	a.t.automation(AgentApp, c.Replaying).Put(r, m)
}

// memories are what an agent remembers for a run: active, unexpired, about
// everyone or the person it runs for, the most relevant to the goal first.
func (a *Agents) memories(c platform.Caller, run AgentRunRecord, now time.Time) []Memory {
	domain, _ := json.Marshal([]any{[]any{"agent", "=", run.Agent}, []any{"state", "=", "active"}})
	all, _, _ := platform.Find[Memory](c, platform.Query{Domain: domain, Sort: []string{"-created"}, Limit: 500})
	goal := map[string]bool{}
	for _, w := range words(run.Goal) {
		goal[w] = true
	}
	overlap := func(m Memory) int {
		n := 0
		for _, w := range words(m.Fact) {
			if goal[w] {
				n++
			}
		}
		return n
	}
	all = slices.DeleteFunc(all, func(m Memory) bool {
		return m.For != "" && m.For != run.OnBehalf || !m.Expires.IsZero() && m.Expires.Before(now)
	})
	slices.SortStableFunc(all, func(x, y Memory) int { return overlap(y) - overlap(x) })
	return all[:min(len(all), memoriesUsed)]
}
