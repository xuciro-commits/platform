package host

import (
	"bytes"
	"context"
	"fmt"
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
	runs, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]platform.DispatchRun, *kernel.Error) {
		return platform.PendingDispatches(t.automation(build.ID, false))
	})
	if refusal != nil {
		return
	}
	active := make(map[string]bool, len(runs))
	for _, run := range runs {
		active[run.Intent] = true
	}
	t.schedules.Dispatches.Retain(active)
	due := func(run platform.DispatchRun) time.Time {
		return t.schedules.Dispatches.Next(run.Intent, run.ReadDue)
	}
	slices.SortStableFunc(runs, func(a, b platform.DispatchRun) int { return due(a).Compare(due(b)) })
	reads := 0
	for _, saved := range runs {
		if saved.ReadDue.After(at) {
			continue
		}
		type plan struct {
			run   platform.DispatchRun
			stamp string
			valid bool
		}
		prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (plan, *kernel.Error) {

			run, exists := platform.Get[platform.DispatchRun](t.automation(build.ID, false), saved.ID)
			valid := exists && !slices.Contains([]string{"completed", "cancelled", "compensated"}, run.State)
			var stamp string
			if valid {
				valid = t.readTransport().CheckDispatchRead(run, at) == nil
				if valid {
					stamp, valid = t.statusReadStamp(run.Revision, run.Member, run.Source, run.Connection, run.Definition.Readback.Record != "")
				}
			}
			return plan{run: run, stamp: stamp, valid: valid}, nil
		})
		run, stamp, valid := prepared.run, prepared.stamp, prepared.valid
		if refusal != nil || !valid {
			continue
		}
		if reads >= dataowner.StatusReadLimit {
			return
		}
		if !t.schedules.Dispatches.Start(run.Intent, run.ReadDue, at, &reads) {
			continue
		}
		reading := t.readDispatchStatus(run, at)
		t.schedules.Dispatches.Finished(run.Intent, at.Add(time.Duration(run.Definition.Readback.EverySeconds)*time.Second))
		_, _ = decideOnImage(t, context.Background(), "dispatch:answer", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
			current, found := platform.Get[platform.DispatchRun](t.automation(build.ID, false), run.ID)
			currentStamp, valid := t.statusReadStamp(current.Revision, current.Member, current.Source, current.Connection, current.Definition.Readback.Record != "")
			if !found || current.Revision != run.Revision || !valid || currentStamp != stamp || t.readTransport().CheckDispatchRead(current, at) != nil {
				return nil, nil
			}
			if n := len(current.Readback); n > 0 {
				last := current.Readback[n-1]
				state := reading.State
				if !at.Before(current.Deadline) && state != "unknown" && state != "failed" && state != "completed" {
					state = "stuck"
				}
				started := current.StartedAt
				for _, attempt := range current.Attempts {
					if attempt.Intent == current.Intent {
						started = attempt.At
					}
				}
				if current.ReadDue.After(started) && !last.At.Before(started) && current.State == state && last.State == reading.State && last.Value == reading.Value && last.Reason == reading.Reason && bytes.Equal(last.Observed, reading.Observed) {
					return nil, nil
				}
			}
			caller := t.automation(build.ID, false).Member
			submission := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID,
				IdempotencyKey: fmt.Sprintf("dispatch-read:%s:%d:%s", run.ID, run.Revision, at.UTC().Format(time.RFC3339Nano)),
				Target:         &pb.EntityRef{Type: platform.DispatchType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.DispatchAnswerSchema, Version: 1},
				Payload: platform.Raw(map[string]any{"revision": run.Revision, "intent": run.Intent, "outcome": reading.State, "reason": reading.Reason, "reading": reading, "answer": reading.Observed})}
			return t.accept(t.App(build.ID), caller, submission, at, acceptOptions{automation: true})
		}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
			return t.finishPreparedSubmission(platform.Member{ID: "app:" + build.ID, Tenant: t.ID}, candidate, record, refusal)
		})
	}
}

func (t *Tenant) readDispatchStatus(run platform.DispatchRun, at time.Time) platform.DispatchReading {
	return dataowner.DispatchReadback{Client: t.dataClient(), Observations: t.observationSources(), Records: func(id, typ string, query platform.Query, at time.Time) ([]any, *kernel.Error) {
		member, _ := t.Member(id)
		page, refusal := t.Records(context.Background(), member, typ, query, at)
		return page.Records, refusal
	}}.Read(run, at)
}
