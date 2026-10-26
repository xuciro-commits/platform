package platformserver

import "platformserver/internal/construction"

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"platformkernel/kernel"
	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

func TestPostgresBusinessRefusalEscalationReadsSavedFacts(t *testing.T) {
	dsn := os.Getenv("PLATFORM_TEST_DATABASE")
	if dsn == "" {
		t.Skip("PLATFORM_TEST_DATABASE not set")
	}
	ctx := context.Background()
	j, err := OpenJournal(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	tn := businessRefusalEscalationTenant(t)
	defer forgetJournal(t, j, tn.ID)
	if _, err = j.Entries(ctx, tn.ID, 0); err != nil {
		t.Fatal(err)
	}
	tn.attachJournal(ctx, j)
	businessRefusalOwnerSetup(t, tn)
	setBusinessRefusalEscalation(t, tn, "flag", &platform.RefusalEscalation{Count: 2, WindowSeconds: 60, ReviewerRole: "manager"})
	member := memberOf(t, tn, "operator")
	for i := range 2 {
		if _, refusal := tn.Submit(member, txnRequest(t, tn, "operator", "flag"), txnAt.Add(time.Duration(i)*time.Second)); refusal != nil {
			t.Fatal(refusal)
		}
	}
	if err = tn.migrateRuntimeState(ctx, j); err != nil {
		t.Fatal(err)
	}
	var originalTask string
	check := func(tenant *Tenant) {
		t.Helper()
		tasks, refusal := recordRead(tenant, ctx, func() ([]work.WorkTask, *kernel.Error) {
			facts := storedValues(&tenant.committed, "answer", tenant.committed.answers, "business")
			if len(facts) != 2 {
				t.Fatalf("business selection returned ordinary replies: %d", len(facts))
			}
			rows, _, refusal := platform.Find[work.WorkTask](tenant.automation(work.ID, false), platform.Query{})
			return rows, refusal
		})
		if refusal != nil || len(tasks) != 1 || !slices.Equal(tasks[0].Candidates, []string{"manager"}) {
			t.Fatal("saved refusal facts lost their original Work task", tasks, refusal)
		}
		if originalTask != "" && tasks[0].ID != originalTask {
			t.Fatal("checkpoint changed escalation identity")
		}
		originalTask = tasks[0].ID
	}
	runJob(t, tn, work.ID, "refusals", txnAt.Add(3*time.Second))
	check(tn)
	if _, err = tn.checkpointRuntime(ctx, j, "refusal-history"); err != nil {
		t.Fatal(err)
	}
	restored := businessRefusalEscalationTenant(t)
	d := Deployment{SnapshotEvery: 1}
	if err = d.restoreTenants(ctx, j, "refusal-history", []*Tenant{restored}, map[string]int64{}, map[string]bool{}); err != nil || restored.quarantined() {
		t.Fatal(err, restored.fault.Load())
	}
	runJob(t, restored, work.ID, "refusals", txnAt.Add(4*time.Second))
	check(restored)
}

func businessRefusalEscalationTenant(t *testing.T) *Tenant {
	t.Helper()
	return composeTenant(t, "business-txn", []Seat{seatOf("builder", "build:builder"), seatOf("operator", "build:operator"), seatOf("limited", "build:limited"), seatOf("manager", "build:manager")}, work.New("business-txn"), build.New("business-txn"))
}

func setBusinessRefusalEscalation(t *testing.T, tn *Tenant, level string, policy *platform.RefusalEscalation) {
	t.Helper()
	view, err := tn.RecordOf(memberOf(t, tn, "builder"), construction.ObjectType, "REQUEST", txnAt)
	if err != nil {
		t.Fatal(err)
	}
	var object construction.Object
	if err := json.Unmarshal(platform.Raw(view.Record), &object); err != nil {
		t.Fatal(err)
	}
	for i := range object.Actions[0].Transaction.Steps {
		for j := range object.Actions[0].Transaction.Steps[i].Refusals {
			declaration := &object.Actions[0].Transaction.Steps[i].Refusals[j]
			if declaration.Level == level {
				declaration.Escalation = policy
			}
		}
	}
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".edit", construction.ObjectType, "REQUEST", map[string]any{"actions": object.Actions}, txnAt)
	decide(t, tn, "builder", construction.ID, construction.ObjectType+".publish", construction.ObjectType, "REQUEST", map[string]any{}, txnAt)
}
func TestBusinessRefusalEscalationCannotBeForged(t *testing.T) {
	tn := businessRefusalEscalationTenant(t)
	businessRefusalOwnerSetup(t, tn)
	if got := refuse(t, tn, "operator", work.ID, work.TaskType+".refusal", work.TaskType, "forged", map[string]any{}, txnAt); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("member forged a refusal escalation: %s", got)
	}
}
