package host

import (
	"context"
	dataowner "platformserver/services/data"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) ReadDispatches(at time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) || t.App(build.ID) == nil {
		return
	}
	dataowner.DispatchPoll{Schedule: &t.schedules.Dispatches,
		Pending: func() ([]platform.DispatchRun, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) ([]platform.DispatchRun, *kernel.Error) {
				return platform.PendingDispatches(t.automation(build.ID, false))
			})
		},
		Plan: func(id string) (dataowner.DispatchReadPlan, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (dataowner.DispatchReadPlan, *kernel.Error) {
				run, exists := platform.Get[platform.DispatchRun](t.automation(build.ID, false), id)
				plan := dataowner.DispatchReadPlan{Run: run, Valid: exists && !slices.Contains([]string{"completed", "cancelled", "compensated"}, run.State)}
				if plan.Valid {
					plan.Valid = t.readTransport().CheckDispatchRead(run, at) == nil
					if plan.Valid {
						plan.Stamp, plan.Valid = t.statusReadStamp(run.Revision, run.Member, run.Source, run.Connection, run.Definition.Readback.Record != "")
					}
				}
				return plan, nil
			})
		},
		Read: func(run platform.DispatchRun) platform.DispatchReading { return t.readDispatchStatus(run, at) },
		Answer: func(plan dataowner.DispatchReadPlan, reading platform.DispatchReading) {
			run := plan.Run
			_, _ = decideOnImage(t, context.Background(), "dispatch:answer", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
				current, found := platform.Get[platform.DispatchRun](t.automation(build.ID, false), run.ID)
				currentStamp, valid := t.statusReadStamp(current.Revision, current.Member, current.Source, current.Connection, current.Definition.Readback.Record != "")
				if !found || current.Revision != run.Revision || !valid || currentStamp != plan.Stamp || t.readTransport().CheckDispatchRead(current, at) != nil {
					return nil, nil
				}
				caller := t.automation(build.ID, false).Member
				submission := dataowner.DispatchAnswerSubmission(t.ID, build.ID, caller, run, current, reading, at)
				if submission == nil {
					return nil, nil
				}
				return t.accept(t.App(build.ID), caller, submission, at, acceptOptions{automation: true})
			}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
				return t.finishPreparedSubmission(platform.Member{ID: "app:" + build.ID, Tenant: t.ID}, candidate, record, refusal)
			})
		}}.Run(at)
}

func (t *Tenant) readDispatchStatus(run platform.DispatchRun, at time.Time) platform.DispatchReading {
	return dataowner.DispatchReadback{Client: t.dataClient(), Observations: t.observationSources(), Records: func(id, typ string, query platform.Query, at time.Time) ([]any, *kernel.Error) {
		member, _ := t.Member(id)
		page, refusal := t.Records(context.Background(), member, typ, query, at)
		return page.Records, refusal
	}}.Read(run, at)
}
