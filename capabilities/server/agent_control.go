package platformserver

import (
	"maps"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

// The agents overview and the off switch (ADR-0029 D4, D5): every agent the
// apps declare and every outside principal marked as one, what each did and
// what people made of it; an administrator suspends one by a decision, which
// stops its runs at their next step, refuses new ones, and refuses an outside
// agent's decisions and model calls. The switch is a record, so it replays.

const (
	SwitchType    = "agent.switch"
	SchemaSuspend = "agent.suspend"
	SchemaResume  = "agent.resume"
)

// Switch is an agent's off switch, by its member ID ("agent:<app>.<name>", or
// an outside agent's member).
type Switch struct {
	platform.Record
	Suspended bool   `json:"suspended" field:"readonly"`
	By        string `json:"by,omitempty" field:"readonly" title:"Changed by"`
	Reason    string `json:"reason,omitempty" field:"readonly" type:"longtext"`
}

func switchActions() []platform.Action {
	admin := []string{AgentAdmin}
	return []platform.Action{
		{Schema: SchemaSuspend, Target: SwitchType, New: true, Capability: "switch", Title: "Suspend agent", Roles: admin,
			Description: "Stop an agent (target its member ID, such as agent:csm.triage): its runs stop at their next step, new ones are refused, and an outside agent's decisions and model calls are refused.",
			Payload:     []platform.Field{{Name: "reason", Type: "string", Description: "Why, for the audit and whom its runs were for"}}},
		{Schema: SchemaResume, Target: SwitchType, Capability: "switch", Title: "Resume agent", Roles: admin,
			Description: "Let a suspended agent work again.", Payload: []platform.Field{}},
	}
}

// switchDecision suspends or resumes an agent.
func (a *Agents) switchDecision(c platform.Caller, s *pb.Submission, p struct{ Reason string }) (func(*pb.ChangeRecord), *kernel.Error) {
	id := s.GetTarget().GetId()
	if id == "" {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
	}
	app := a.t.automated(c, AgentApp)
	sw, _ := platform.Get[Switch](app, id)
	suspend := s.GetSchema().GetName() == SchemaSuspend
	if sw.Suspended == suspend {
		return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	}
	sw.ID, sw.Suspended, sw.By, sw.Reason = id, suspend, c.ID, strings.TrimSpace(p.Reason)
	return func(r *pb.ChangeRecord) { app.Put(r, sw) }, nil
}

// suspended says whether member is an agent an administrator switched off.
func (t *Tenant) suspended(member string) bool {
	v, ok := t.Held(SwitchType + "/" + member)
	return ok && v.(Switch).Suspended
}

// AgentOverview is one agent as its administrators weigh it (D5): what it did,
// what it cost, and what people made of it — measured, not estimated.
type AgentOverview struct {
	Member    string         `json:"member"` // agent:<app>.<name>, or an outside agent's member
	Title     string         `json:"title"`
	Outside   bool           `json:"outside,omitempty"`
	Suspended bool           `json:"suspended"`
	Runs      int            `json:"runs"`
	Live      int            `json:"live"`    // running or waiting for a person
	Actions   int            `json:"actions"` // what it did: actions its runs took, an outside agent's decisions
	Calls     int            `json:"calls"`   // model calls in the usage kept
	Tokens    int            `json:"tokens"`
	Cost      float64        `json:"cost"`
	Judged    map[string]int `json:"judged"` // people's signals by kind: confirmed, changed, rejected, discarded, …
}

// AgentsOverview is every agent, declared and outside, for the agent app's administrators.
func (t *Tenant) AgentsOverview() []AgentOverview {
	c := t.automation(AgentApp, false)
	rows := map[string]*AgentOverview{}
	t.agents.each(func(id string, d *agentDef) {
		rows["agent:"+id] = &AgentOverview{Member: "agent:" + id, Title: d.Title, Judged: map[string]int{}}
	})
	if console, ok := t.app(PlatformApp).(*Console); ok {
		for _, x := range console.Identities() {
			if m, ok := console.Member(x.Token); ok && m.Agent && rows[m.ID] == nil {
				rows[m.ID] = &AgentOverview{Member: m.ID, Title: m.ID, Outside: true, Judged: map[string]int{}}
			}
		}
	}
	runs, _, _ := platform.Find[AgentRunRecord](c, platform.Query{})
	for _, r := range runs {
		row := rows["agent:"+r.Agent]
		if row == nil {
			continue
		}
		row.Runs++
		row.Actions += r.ActionsUsed
		if r.State == "running" || r.State == "waiting" {
			row.Live++
		}
		for _, s := range r.Signals {
			row.Judged[s.Kind]++
		}
	}
	for _, e := range t.Audit() {
		if row := rows[e.Member]; row != nil && row.Outside {
			row.Actions++
		}
	}
	if t.ai != nil {
		for _, u := range t.ai.Usage() {
			if row := rows[u.Member]; row != nil {
				row.Calls, row.Tokens, row.Cost = row.Calls+1, row.Tokens+u.Input+u.Output, row.Cost+u.Cost
			}
		}
	}
	out := []AgentOverview{}
	for _, id := range slices.Sorted(maps.Keys(rows)) {
		row := rows[id]
		row.Suspended = t.suspended(id)
		out = append(out, *row)
	}
	return out
}
