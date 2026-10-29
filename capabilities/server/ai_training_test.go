package platformserver

import (
	agentowner "platformserver/engine/agents"
	automation "platformserver/engine/automation"
)

import (
	"encoding/json"
	"platformserver/apps/ai"
	"platformserver/apps/work"
	"platformserver/platform"
	"strings"
	"testing"
	"time"
)

func TestAITrainingDerivesFromConfirmedProposalAndRechecksQualification(t *testing.T) {
	const id = "training-proposal"
	at := time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)
	tenant := composeTenant(t, id, []platform.Seat{seatOf("ana", "ai:admin", "desk:clerk")}, ai.New(id), work.New(id), automation.New(id), agentowner.New(id), newDesk(id))
	decide(t, tenant, "ana", "desk", "desk.ticket.open", "desk.ticket", "T1", map[string]any{"subject": "Printer unavailable"}, at)
	decide(t, tenant, "ana", ai.ID, "ai.training-policy.set", "ai.training-policy", "reviewed", map[string]any{"enabled": true, "fields": []string{"reply"}, "trainingAllowed": true, "providerAllowed": true, "retainDays": 30}, at)
	proposed := decide(t, tenant, "ana", platform.AgentApp, platform.SchemaToolPropose, platform.RunType, "P1", map[string]any{"owner": "desk", "action": "desk.ticket.answer", "target": "T1", "payload": map[string]any{"reply": "Restart the printer"}}, at.Add(time.Second))
	before, _ := platform.Get[Ticket](tenant.automation("desk", false), "T1")
	if before.Reply != "" {
		t.Fatal("saving the proposed action wrote the business record")
	}
	decide(t, tenant, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "P1", map[string]any{}, at.Add(2*time.Second))
	// A queued event must retain its own image even when confirmation has
	// already replaced the current run's proposal with its accepted outcome.
	evidence := (hostView{t: tenant}).TrainingEvidence(platform.Event{App: platform.AgentApp, Record: proposed, Changed: []string{platform.RunType + "/P1"}})
	if len(evidence.Candidates) != 1 || evidence.Candidates[0].Outcome != "proposed" || len(evidence.Candidates[0].Chosen) != 0 {
		t.Fatalf("a queued proposal event read the later confirmation: %+v", evidence)
	}
	tenant.Work(at.Add(3 * time.Second))
	report, refusal := tenant.Read(memberOf(t, tenant, "ana"), "ai-training")
	if refusal != nil {
		t.Fatal(refusal)
	}
	data := report.(platform.AITrainingReport)
	var candidate platform.AITrainingCandidate
	for _, item := range data.Candidates {
		if item.Outcome == "accepted" {
			candidate = item
		}
	}
	// Derivation is keyed by the accepted source: working the queue again adds nothing.
	tenant.Work(at.Add(3*time.Second + time.Millisecond))
	again, _ := tenant.Read(memberOf(t, tenant, "ana"), "ai-training")
	if len(again.(platform.AITrainingReport).Candidates) != len(data.Candidates) {
		t.Fatalf("a repeated event derived another candidate: %d then %d", len(data.Candidates), len(again.(platform.AITrainingReport).Candidates))
	}
	if candidate.ID == "" || candidate.Approved || len(candidate.Problems) == 0 {
		t.Fatalf("a business acceptance was incorrectly considered quality proof: %+v", data)
	}
	owner := tenant.ai.(platform.AITrainingOwner)
	caller := platform.NewCaller(runtime{tenant}, memberOf(t, tenant, "ana"), ai.ID, false, false)
	exported, refusal := owner.TrainingExport(caller, "sft", at.Add(4*time.Second))
	if refusal != nil || exported.JSONL != "" {
		t.Fatal("unreviewed candidate was exported", exported, refusal)
	}
	decide(t, tenant, "ana", ai.ID, "ai.training-candidate.review", "ai.training-candidate", candidate.ID, map[string]any{"approved": true, "qualityEvidence": "Reviewed against equipment troubleshooting guide"}, at.Add(4*time.Second))
	exported, refusal = owner.TrainingExport(caller, "sft", at.Add(5*time.Second))
	if refusal != nil || !strings.Contains(exported.JSONL, "Restart the printer") || strings.Contains(exported.JSONL, "Printer unavailable") || strings.Contains(exported.JSONL, "desk.ticket/T1") {
		t.Fatal("export lost qualification, field filtering or source pseudonyms", exported, refusal)
	}
	var row struct {
		Version  int                        `json:"version"`
		Format   string                     `json:"format"`
		Accepted map[string]json.RawMessage `json:"accepted"`
	}
	if json.Unmarshal([]byte(exported.JSONL), &row) != nil || row.Version != 1 || row.Format != "sft" || len(row.Accepted) != 1 {
		t.Fatal("export is not the fixed SFT JSONL format", exported.JSONL)
	}
	// An export rechecks the reader's current access to the sources: without it
	// nothing leaves, and the sample is eligible again once access returns.
	rewriteMember(t, testConsoleOf(tenant), "ana", func(member *platform.Member) { delete(member.Roles, "desk") })
	if withheld, refusal := owner.TrainingExport(caller, "sft", at.Add(6*time.Second)); refusal != nil || withheld.JSONL != "" {
		t.Fatal("a revoked source grant still exported its sample", withheld, refusal)
	}
	rewriteMember(t, testConsoleOf(tenant), "ana", func(member *platform.Member) { member.Roles["desk"] = "clerk" })
	if again, refusal := owner.TrainingExport(caller, "sft", at.Add(6*time.Second)); refusal != nil || again.JSONL == "" {
		t.Fatal("restored access did not make the sample eligible again", again, refusal)
	}
	decide(t, tenant, "ana", ai.ID, "ai.training-candidate.revoke", "ai.training-candidate", candidate.ID, map[string]any{}, at.Add(6*time.Second))
	exported, refusal = owner.TrainingExport(caller, "sft", at.Add(7*time.Second))
	if refusal != nil || exported.JSONL != "" {
		t.Fatal("revoked training use remained exportable", exported, refusal)
	}
}

// agentFixtureApp changes only the declaration supplied by the original owner.
type agentFixtureApp struct {
	platform.App
	definition platform.Agent
}

func (a agentFixtureApp) Manifest() platform.Manifest {
	m := a.App.Manifest()
	m.Agents = []platform.Agent{a.definition}
	return m
}
