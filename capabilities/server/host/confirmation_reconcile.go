package host

import (
	"context"
	"encoding/json"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (t *Tenant) ReconcileConfirmations(at time.Time) {
	if t.quarantined() || t.App(build.ID) == nil {
		return
	}
	dataowner.ConfirmationReconcile{Schedule: &t.schedules.Confirmations,
		Pending: func() ([]platform.ConfirmationRun, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) ([]platform.ConfirmationRun, *kernel.Error) {
				return platform.PendingConfirmations(t.automation(build.ID, false))
			})
		},
		Plan: func(id string, i int) (dataowner.ConfirmationReadPlan, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (dataowner.ConfirmationReadPlan, *kernel.Error) {
				run, exists := platform.Get[platform.ConfirmationRun](t.automation(build.ID, false), id)
				plan := dataowner.ConfirmationReadPlan{Run: run}
				plan.Valid = exists && i < len(run.Parties) && i < len(run.Definition.Parties) && dataowner.ConfirmationOpen(run)
				if plan.Valid {
					plan.Valid = t.readTransport().CheckConfirmationRead(run, i, at) == nil
				}
				if plan.Valid {
					party, policy := run.Parties[i], run.Definition.Parties[i].Reconcile
					plan.Valid = policy != nil && (party.State == "pending" || party.State == "unknown")
					plan.Valid = plan.Valid && json.Unmarshal(party.Source, &plan.Source) == nil && json.Unmarshal(party.Connection, &plan.Connection) == nil
					if plan.Valid {
						plan.Stamp, plan.Valid = t.statusReadStamp(run.Revision, run.Member, party.Source, party.Connection, false)
					}
				}
				return plan, nil
			})
		},
		Read: func(source dataowner.Source, connection *dataowner.Connection) ([]map[string]any, error) {
			return t.dataClient().Read(source, connection)
		},
		Answer: func(plan dataowner.ConfirmationReadPlan, i int, diff platform.ConfirmationDifference, outcome string) {
			run := plan.Run
			_, _ = t.decideAutomated("confirmation:answer", build.ID, func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
				current, exists := platform.Get[platform.ConfirmationRun](t.automation(build.ID, false), run.ID)
				valid := exists && current.Revision == run.Revision && i < len(current.Parties) && dataowner.ConfirmationOpen(current)
				if valid {
					valid = (current.Parties[i].State == "pending" || current.Parties[i].State == "unknown") && t.readTransport().CheckConfirmationRead(current, i, at) == nil
					if valid {
						currentStamp, stable := t.statusReadStamp(current.Revision, current.Member, current.Parties[i].Source, current.Parties[i].Connection, false)
						valid = stable && currentStamp == plan.Stamp
					}
				}
				if !valid {
					return nil, nil
				}
				caller := t.automation(build.ID, false).Member
				submission := dataowner.ConfirmationAnswerSubmission(t.ID, build.ID, caller, run, current, i, diff, outcome, at)
				if submission == nil {
					return nil, nil
				}
				return t.accept(t.App(build.ID), caller, submission, at, acceptOptions{automation: true})
			})
		}}.Run(at)
}
