package platformserver

import (
	agentowner "platformserver/engine/agents"
	automation "platformserver/engine/automation"
)

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
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

// The confirmed proposal, its handed-over task and the training candidate with
// its reviewed qualification are accepted facts: recovered from the real
// journal, from any checkpoint plus the rest, they equal the live tenant and
// the export rechecks the same eligibility.
func TestJournalAITrainingRecoversEligibilityAndHandoff(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE is not set")
	}
	ctx := context.Background()
	journal, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	id := fmt.Sprintf("ai-training-%d", time.Now().UnixNano())
	defer journal.Pool().Exec(ctx, `delete from journal where tenant=$1`, id)
	if _, err := journal.Entries(ctx, id, 0); err != nil {
		t.Fatal(err)
	}
	build := func() *Tenant {
		return composeTenant(t, id, []platform.Seat{seatOf("ana", "ai:admin", "desk:clerk")}, ai.New(id), work.New(id), automation.New(id), agentowner.New(id), newDesk(id))
	}
	live := build()
	live.Record = func(e Entry) {
		if err := journal.Append(ctx, id, e); err != nil {
			t.Fatal(err)
		}
	}
	live.AcceptResult = func(e Entry, key, hash string) ([]byte, error) { return journal.AppendAccepted(ctx, id, e, key, hash) }
	at := time.Date(2026, 10, 28, 14, 0, 0, 0, time.UTC)
	decide(t, live, "ana", "desk", "desk.ticket.open", "desk.ticket", "T1", map[string]any{"subject": "Printer unavailable"}, at)
	decide(t, live, "ana", ai.ID, "ai.training-policy.set", "ai.training-policy", "reviewed", map[string]any{"enabled": true, "fields": []string{"reply"}, "trainingAllowed": true, "providerAllowed": true, "retainDays": 30}, at)
	decide(t, live, "ana", platform.AgentApp, platform.SchemaToolPropose, platform.RunType, "P1", map[string]any{"owner": "desk", "action": "desk.ticket.answer", "target": "T1", "payload": map[string]any{"reply": "Restart the printer"}}, at.Add(time.Second))
	decide(t, live, "ana", platform.AgentApp, platform.SchemaRunConfirm, platform.RunType, "P1", map[string]any{}, at.Add(2*time.Second))
	live.Work(at.Add(3 * time.Second))
	report, refusal := live.Read(memberOf(t, live, "ana"), "ai-training")
	if refusal != nil {
		t.Fatal(refusal)
	}
	var candidate string
	for _, item := range report.(platform.AITrainingReport).Candidates {
		if item.Outcome == "accepted" {
			candidate = item.ID
		}
	}
	if candidate == "" {
		t.Fatalf("no accepted candidate derived: %+v", report)
	}
	decide(t, live, "ana", ai.ID, "ai.training-candidate.review", "ai.training-candidate", candidate, map[string]any{"approved": true, "qualityEvidence": "Reviewed against equipment troubleshooting guide"}, at.Add(4*time.Second))
	entries, err := journal.Entries(ctx, id, 0)
	if err != nil {
		t.Fatal(err)
	}
	CheckReplay(t, live, entries, build)
	restored := build()
	if err := restored.Replay(entries); err != nil {
		t.Fatal(err)
	}
	caller := platform.NewCaller(runtime{restored}, memberOf(t, restored, "ana"), ai.ID, false, false)
	exported, refusal := restored.ai.(platform.AITrainingOwner).TrainingExport(caller, "sft", at.Add(5*time.Second))
	if refusal != nil || !strings.Contains(exported.JSONL, "Restart the printer") {
		t.Fatal("recovered qualification no longer exports the reviewed sample", exported, refusal)
	}
}
