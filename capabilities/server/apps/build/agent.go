package build

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

const AgentType = "build.agent"
const SchemaAgentPublish = AgentType + ".publish"

// Agent is an AI agent a builder declares over the ontology (ADR-0077): what
// it is for, its instructions, the published actions and queries it may use,
// its budget, who takes over when it stops, and the cases it must pass. The
// host runs it as any code-declared agent (ADR-0021): every step journaled,
// irreversible effects drafted for a person to confirm, evaluations archived
// against its version.
type Agent struct {
	platform.Record
	Name         string   `json:"name" field:"required,search" help:"Lower-case letters and digits; the agent is build.<name>"`
	Title        string   `json:"title" field:"required,search"`
	Description  string   `json:"description" field:"required" type:"longtext" help:"What it does for others; people and other agents see this"`
	Instructions string   `json:"instructions" field:"required" type:"longtext" help:"Its private instructions: the job, the order of work, when to ask"`
	Tools        []string `json:"tools" field:"aside" help:"Actions of published objects (build.<object>.<action>) and queries (query:<name>) it may use"`
	Steps        int      `json:"steps,omitempty" title:"Model turns per run" help:"Default 10"`
	Tokens       int      `json:"tokens,omitempty" title:"Tokens per run" help:"Default 40000"`
	Actions      int      `json:"actions,omitempty" title:"Actions per run" help:"Default 3"`
	Cost         float64  `json:"cost,omitempty" title:"USD per run" help:"0: no cap"`
	// Checkpoints are the tools whose use always waits for a person, even when
	// the agent could act: the irreversible ones (post, ship, pay).
	Checkpoints []string    `json:"checkpoints,omitempty" field:"aside" help:"Tools that always wait for a person to confirm"`
	HandoffRole string      `json:"handoffRole,omitempty" title:"Hands over to" help:"The builder-app role that takes the goal when a run stops; empty: the builder"`
	Cases       []AgentCase `json:"cases,omitempty" type:"json" title:"Evaluation cases"`
	State       string      `json:"state" field:"readonly" choices:"draft,published"`
	Version     int         `json:"version,omitempty" field:"readonly"`
	Published   string      `json:"published,omitempty" field:"readonly" type:"longtext" title:"What is installed"`
}

// AgentCase is one test of the agent (ADR-0029 D6), as data: a goal about a
// record, and what a dry run must come to.
type AgentCase struct {
	Name   string   `json:"name"`
	Goal   string   `json:"goal"`
	Ref    string   `json:"ref,omitempty" help:"The record it is about, <type>/<id>"`
	Expect string   `json:"expect,omitempty" help:"Text the result must contain"`
	Asks   bool     `json:"asks,omitempty" help:"It must ask a person"`
	Takes  []string `json:"takes,omitempty" help:"Actions it must take"`
	Avoids []string `json:"avoids,omitempty" help:"Actions it must not take"`
}

func (b *Build) agentEntity() platform.Entity {
	return platform.Entity{Type: AgentType, Title: "Agent", Plural: "Agents", Model: Agent{}, Display: "title",
		Description: "An AI agent over the ontology: bounded tools, a budget, checkpoints and cases; publishing installs it.",
		Scope:       platform.Scope{Default: platform.ScopeNone, Levels: map[string]string{Builder: platform.ScopeTenant}},
		Standard:    platform.Standard{Create: true, Edit: true, Archive: true, Roles: []string{Builder}, Capability: "functions"},
		Validate: func(c platform.Caller, record any) *kernel.Error {
			if err := b.checkAgent(*record.(*Agent)); err != nil {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "{why}", err.Error())
			}
			return nil
		},
		Lifecycle: &platform.Lifecycle{Field: "state", Initial: "draft", States: []platform.State{{Name: "draft", Title: "Draft", Tone: "warning"}, {Name: "published", Title: "Published", Tone: "success"}},
			Transitions: []platform.Transition{{Name: "publish", Title: "Publish", Description: "Install this agent as its next version; a changed definition stops existing runs.", From: []string{"draft", "published"}, To: []string{"published"}, Roles: []string{Builder}, Capability: "functions", Payload: []platform.Field{}, Do: b.publishAgent}}}}
}

func (b *Build) checkAgent(a Agent) error {
	if !named(a.Name) {
		return fmt.Errorf("The agent name %s must be lower-case letters and digits", a.Name)
	}
	if strings.TrimSpace(a.Instructions) == "" || strings.TrimSpace(a.Description) == "" {
		return fmt.Errorf("The agent %s needs a description and instructions", a.Name)
	}
	if len(a.Tools) == 0 {
		return fmt.Errorf("The agent %s needs at least one action or query", a.Name)
	}
	for _, tool := range a.Tools {
		if query, ok := strings.CutPrefix(tool, "query:"); ok {
			if _, found := b.queries[query]; !found {
				return fmt.Errorf("The agent %s uses query %s, which is not published", a.Name, query)
			}
			continue
		}
		if _, ok := b.ledger.Catalog.Action(tool); !ok || !strings.HasPrefix(tool, ID+".") || b.installedActionOf(tool) == "" {
			return fmt.Errorf("The agent %s uses %s, which is not an action of a published object", a.Name, tool)
		}
	}
	for _, cp := range a.Checkpoints {
		if !slices.Contains(a.Tools, cp) {
			return fmt.Errorf("The checkpoint %s is not one of the agent's tools", cp)
		}
	}
	if a.HandoffRole != "" && !slices.Contains(b.Manifest().AllRoles(), a.HandoffRole) {
		return fmt.Errorf("The role %s is not a role of this builder", a.HandoffRole)
	}
	for i, c := range a.Cases {
		if strings.TrimSpace(c.Goal) == "" {
			return fmt.Errorf("Case %d needs a goal", i+1)
		}
		for _, x := range append(slices.Clone(c.Takes), c.Avoids...) {
			if !slices.Contains(a.Tools, x) {
				return fmt.Errorf("Case %q names %s, not one of the agent's tools", c.Name, x)
			}
		}
	}
	for name, other := range b.agents {
		if name == a.Name && other.ID != a.ID {
			return fmt.Errorf("Agent %s is already declared", a.Name)
		}
	}
	if old, ok := wasPublished[Agent](a.Published); ok && old.Name != a.Name {
		return fmt.Errorf("A published agent keeps its name")
	}
	return nil
}

// installedActionOf says which installed object type an action schema belongs to.
func (b *Build) installedActionOf(schema string) string {
	for typ := range b.installed {
		if strings.HasPrefix(schema, typ+".") {
			return typ
		}
	}
	return ""
}

func (b *Build) publishAgent(c platform.Caller, record any, _ json.RawMessage, _ time.Time) *kernel.Error {
	a, ok := record.(*Agent)
	if !ok {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	if err := b.checkAgent(*a); err != nil {
		return &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, Message: err.Error()}
	}
	a.Version++
	a.State = "published"
	a.Published = published(*a)
	if !c.Staging() {
		b.agents[a.Name] = *a
	}
	return nil
}

func (b *Build) agentInventory() ([]Agent, error) {
	return readDefinitionInventory[Agent](b.host.Automation(platform.Caller{}, ID))
}

// AgentStamp changes whenever the set of installed agents or any of their
// versions does; the host re-declares them when it sees a new stamp.
func (b *Build) AgentStamp() string {
	names := make([]string, 0, len(b.agents))
	for name, a := range b.agents {
		names = append(names, fmt.Sprintf("%s@%d", name, a.Version))
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}

// agentDeclarations are the installed agents as the host runs them.
func (b *Build) agentDeclarations() []platform.Agent {
	names := make([]string, 0, len(b.agents))
	for name := range b.agents {
		names = append(names, name)
	}
	slices.Sort(names)
	out := make([]platform.Agent, 0, len(names))
	for _, name := range names {
		out = append(out, b.agents[name].definition())
	}
	return out
}

func (a Agent) definition() platform.Agent {
	checkpoints := slices.Clone(a.Checkpoints)
	role := a.HandoffRole
	d := platform.Agent{Name: a.Name, Title: a.Title, Description: a.Description, Instructions: a.Instructions, Tools: slices.Clone(a.Tools),
		Budget: platform.Budget{Steps: a.Steps, Tokens: a.Tokens, Actions: a.Actions, Cost: a.Cost},
		Guard: func(c platform.Caller, r platform.AgentRun, action, target string, payload json.RawMessage) *kernel.Error {
			if slices.Contains(checkpoints, action) && r.OnBehalf == "" {
				return platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "This step waits for a person: ask, or finish with what you propose.")
			}
			return nil
		},
		To: func(platform.Caller, platform.AgentRun) []platform.Recipient {
			if role == "" {
				return []platform.Recipient{{AppRole: Builder}}
			}
			return []platform.Recipient{{AppRole: role}}
		}}
	for _, c := range a.Cases {
		c := c
		d.Cases = append(d.Cases, platform.Case{Name: c.Name, Goal: c.Goal, Ref: c.Ref, Check: func(run platform.CaseRun) string {
			if c.Expect != "" && !strings.Contains(strings.ToLower(run.Result), strings.ToLower(c.Expect)) {
				return fmt.Sprintf("the result does not say %q", c.Expect)
			}
			if c.Asks && !run.Asked {
				return "it did not ask a person"
			}
			taken := func(action string) bool {
				return slices.ContainsFunc(run.Actions, func(x string) bool { return strings.HasPrefix(x, action+" ") || x == action })
			}
			for _, x := range c.Takes {
				if !taken(x) {
					return "it did not take " + x
				}
			}
			for _, x := range c.Avoids {
				if taken(x) {
					return "it took " + x
				}
			}
			return ""
		}})
	}
	return d
}

func (b *Build) AgentVersion(name string) int { return b.agents[name].Version }
