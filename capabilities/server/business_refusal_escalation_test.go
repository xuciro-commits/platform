package platformserver

import "platformserver/internal/construction"

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"platformserver/apps/build"
	"platformserver/apps/work"
	"platformserver/platform"
)

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

func TestBusinessRefusalEscalationUsesDistinctDurableDecisions(t *testing.T) {
	for _, level := range []string{"block", "flag", "route"} {
		t.Run(level, func(t *testing.T) {
			tn := businessRefusalEscalationTenant(t)
			var entries []Entry
			tn.AcceptResult = func(e Entry, _, _ string) ([]byte, error) { entries = append(entries, e); return e.Body, nil }
			businessRefusalOwnerSetup(t, tn)
			setBusinessRefusalEscalation(t, tn, level, &platform.RefusalEscalation{Count: 2, WindowSeconds: 60, ReviewerRole: "manager"})
			member := memberOf(t, tn, "operator")
			first := txnRequest(t, tn, "operator", level)
			before := txnState(tn)
			_, firstError := tn.Submit(member, first, txnAt)
			if (firstError != nil) != (level == "block") {
				t.Fatalf("unexpected first result: %v", firstError)
			}
			position := len(entries)
			_, repeatedError := tn.Submit(member, first, txnAt.Add(time.Second))
			if (repeatedError != nil) != (level == "block") || len(entries) != position {
				t.Fatal("a retry contributed another escalation input")
			}
			runJob(t, tn, work.ID, "refusals", txnAt.Add(time.Second))
			if tn.records.types[work.TaskType].rows.len() != 0 {
				t.Fatal("the first refused intent opened a repeated-refusal task")
			}
			if level == "block" {
				txnUnchanged(t, tn, before)
			}
			// The original frozen escalation survives a later published rule change.
			second := txnRequest(t, tn, "operator", level)
			_, refusal := tn.Submit(member, second, txnAt.Add(2*time.Second))
			if (refusal != nil) != (level == "block") {
				t.Fatalf("unexpected second result: %v", refusal)
			}
			setBusinessRefusalEscalation(t, tn, level, nil)
			runJob(t, tn, work.ID, "refusals", txnAt.Add(3*time.Second))
			tasks, _, err := platform.Find[work.WorkTask](tn.automation(work.ID, false), platform.Query{})
			if err != nil || len(tasks) != 1 || tasks[0].Ref != "build.request/operator" || tasks[0].State != "open" || !slices.Equal(tasks[0].Candidates, []string{"manager"}) {
				t.Fatalf("ordinary review task missing: %+v %v", tasks, err)
			}
			if notices := tn.notificationsFor("manager"); len(notices) != 1 || notices[0].Ref != tasks[0].Ref {
				t.Fatalf("the original business role was not notified: %+v", notices)
			}
			decide(t, tn, "manager", work.ID, work.TaskType+".complete", work.TaskType, tasks[0].ID, map[string]any{}, txnAt.Add(4*time.Second))
			runJob(t, tn, work.ID, "refusals", txnAt.Add(5*time.Second))
			if tn.records.types[work.TaskType].rows.len() != 1 || len(tn.notificationsFor("manager")) != 1 {
				t.Fatal("completed escalation was reopened or notified twice")
			}
			CheckReplay(t, tn, entries, func() *Tenant { return businessRefusalEscalationTenant(t) })
		})
	}
}

func TestBusinessRefusalEscalationCannotBeForged(t *testing.T) {
	tn := businessRefusalEscalationTenant(t)
	businessRefusalOwnerSetup(t, tn)
	if got := refuse(t, tn, "operator", work.ID, work.TaskType+".refusal", work.TaskType, "forged", map[string]any{}, txnAt); !strings.Contains(got, "POLICY_DENIED") {
		t.Fatalf("member forged a refusal escalation: %s", got)
	}
}
