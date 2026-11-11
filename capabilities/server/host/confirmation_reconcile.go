package host

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/services/resources/authoring"

	"platformserver/platform"
)

func (t *Tenant) ReconcileConfirmations(at time.Time) {
	if t.quarantined() || t.App(build.ID) == nil {
		return
	}
	runs, err := recordRead(t, context.Background(), func(t *Tenant) ([]platform.ConfirmationRun, *kernel.Error) {
		return platform.PendingConfirmations(t.automation(build.ID, false))
	})
	if err != nil {
		return
	}
	active := map[string]bool{}
	for _, run := range runs {
		if slices.Contains([]string{"pending", "partial", "expired"}, run.State) && run.Completion == "" {
			for i, party := range run.Parties {
				if (party.State == "pending" || party.State == "unknown") && i < len(run.Definition.Parties) && run.Definition.Parties[i].Reconcile != nil {
					active[run.ID+":"+party.Name] = true
				}
			}
		}
	}
	t.schedules.Confirmations.Retain(active)
	due := func(r platform.ConfirmationRun) time.Time {
		var earliest time.Time
		found := false
		for i, p := range r.Parties {
			if (p.State == "pending" || p.State == "unknown") && i < len(r.Definition.Parties) && r.Definition.Parties[i].Reconcile != nil {
				next := t.schedules.Confirmations.Next(r.ID+":"+p.Name, p.ReconcileDue)
				if !found || next.Before(earliest) {
					earliest, found = next, true
				}
			}
		}
		return earliest
	}
	slices.SortStableFunc(runs, func(a, b platform.ConfirmationRun) int { return due(a).Compare(due(b)) })
	reads := 0
	for _, saved := range runs {
		if !slices.Contains([]string{"pending", "partial", "expired"}, saved.State) || saved.Completion != "" {
			continue
		}
		for i := range saved.Parties {
			slot := saved.ID + ":" + saved.Parties[i].Name
			type plan struct {
				run        platform.ConfirmationRun
				stamp      string
				source     dataowner.Source
				connection dataowner.Connection
				valid      bool
			}
			prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (plan, *kernel.Error) {

				run, exists := platform.Get[platform.ConfirmationRun](t.automation(build.ID, false), saved.ID)
				valid := exists && i < len(run.Parties) && i < len(run.Definition.Parties) && run.Completion == "" && slices.Contains([]string{"pending", "partial", "expired"}, run.State)
				if valid {
					valid = (hostView{t: t}).CheckConfirmationRead(run, i, at) == nil
				}
				var stamp string
				var source dataowner.Source
				var connection dataowner.Connection
				if valid {
					party, policy := run.Parties[i], run.Definition.Parties[i].Reconcile
					valid = policy != nil && (party.State == "pending" || party.State == "unknown")
					valid = valid && json.Unmarshal(party.Source, &source) == nil && json.Unmarshal(party.Connection, &connection) == nil
					if valid {
						stamp, valid = t.statusReadStamp(run.Revision, run.Member, party.Source, party.Connection, false)
					}
				}
				return plan{run: run, stamp: stamp, source: source, connection: connection, valid: valid}, nil
			})
			run, stamp, source, connection, valid := prepared.run, prepared.stamp, prepared.source, prepared.connection, prepared.valid
			if refusal != nil || !valid {
				continue
			}
			if reads >= dataowner.StatusReadLimit {
				return
			}
			if !t.schedules.Confirmations.Start(slot, run.Parties[i].ReconcileDue, at, &reads) {
				continue
			}
			p := run.Parties[i]
			policy := run.Definition.Parties[i].Reconcile
			diff := platform.ConfirmationDifference{At: at}
			var rows []map[string]any
			var problem error
			if (platform.MarkingSet{Level: connection.Marking, Compartments: connection.Compartments}).Guarded() {
				problem = fmt.Errorf("Reconciliation requires an unclassified source projection")
			} else {
				rows, problem = t.dataClient().Read(source, &connection)
			}
			_, subject, _ := strings.Cut(run.Subject, "/")
			if policy.KeyInput != "" {
				var inputs map[string]any
				if json.Unmarshal(run.Input, &inputs) != nil {
					problem = fmt.Errorf("Reconciliation input is unavailable")
				}
				value, ok := inputs[policy.KeyInput].(string)
				if !ok || value == "" {
					problem = fmt.Errorf("Reconciliation key input is unavailable")
				}
				subject = value
			}
			matches := 0
			for _, row := range rows {
				key, exists := platform.IntegrationLookupPath(row, policy.KeyField)
				if !exists || fmt.Sprint(key) != subject {
					continue
				}
				matches++
				value, exists := platform.IntegrationLookupPath(row, policy.ResultField)
				if !exists {
					continue
				}
				diff.Observed = platform.Raw(value)
				diff.Matched = evalEqualRaw(diff.Observed, policy.Expected)
			}
			if matches > 1 {
				diff.Matched = false
				problem = fmt.Errorf("Reconciliation source key is ambiguous")
			}
			if problem != nil {
				diff.Error = problem.Error()
				if len(diff.Error) > 4096 {
					diff.Error = diff.Error[:4096]
				}
			}
			if matches == 0 && problem == nil {
				diff.Error = "The requested source record is absent"
			}
			if len(diff.Observed) > 4096 {
				diff.Observed = nil
				diff.Matched = false
				diff.Error = "Reconciliation evidence exceeds its scalar budget"
			}
			outcome := "unknown"
			if diff.Matched {
				outcome = "accepted"
			}
			t.schedules.Confirmations.Finished(slot, at.Add(time.Duration(policy.EverySeconds)*time.Second))
			_, _ = decideOnImage(t, context.Background(), "confirmation:answer", func(t *Tenant) (*pb.ChangeRecord, *kernel.Error) {
				current, exists := platform.Get[platform.ConfirmationRun](t.automation(build.ID, false), run.ID)
				valid := exists && current.Revision == run.Revision && i < len(current.Parties) && current.Completion == "" && slices.Contains([]string{"pending", "partial", "expired"}, current.State)
				if valid {
					valid = (current.Parties[i].State == "pending" || current.Parties[i].State == "unknown") && (hostView{t: t}).CheckConfirmationRead(current, i, at) == nil
					if valid {
						currentStamp, stable := t.statusReadStamp(current.Revision, current.Member, current.Parties[i].Source, current.Parties[i].Connection, false)
						valid = stable && currentStamp == stamp
					}
				}
				if !valid {
					return nil, nil
				}
				party := current.Parties[i]
				state := "unknown"
				if diff.Matched {
					state = "confirmed"
				}
				if prior := party.Differences; party.State == state && len(prior) > 0 {
					last := prior[len(prior)-1]
					if last.Matched == diff.Matched && last.Error == diff.Error && bytes.Equal(last.Observed, diff.Observed) {
						return nil, nil
					}
				}
				payload := map[string]any{"party": p.Name, "revision": run.Revision, "outcome": outcome, "reason": "Source reconciliation", "difference": diff, "answer": diff.Observed}
				caller := t.automation(build.ID, false).Member
				submission := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID, IdempotencyKey: "reconcile:" + run.ID + ":" + p.Name + ":" + at.UTC().Format(time.RFC3339Nano), Target: &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.ConfirmationAnswerSchema, Version: 1}, Payload: platform.Raw(payload)}
				return t.accept(t.App(build.ID), caller, submission, at, acceptOptions{automation: true})
			}, func(t *Tenant, candidate preparedAccepted, record *pb.ChangeRecord, refusal *kernel.Error) (*pb.ChangeRecord, *kernel.Error) {
				return t.finishPreparedSubmission(platform.Member{ID: "app:" + build.ID, Tenant: t.ID}, candidate, record, refusal)
			})
		}
	}
}

var _ platform.ConfirmationHost = hostView{}
