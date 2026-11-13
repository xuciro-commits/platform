package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ConfirmationReadPlan struct {
	Run        platform.ConfirmationRun
	Stamp      string
	Source     Source
	Connection Connection
	Valid      bool
}

type ConfirmationReconcile struct {
	Schedule *StatusReads
	Pending  func() ([]platform.ConfirmationRun, *kernel.Error)
	Plan     func(id string, index int) (ConfirmationReadPlan, *kernel.Error)
	Read     func(Source, *Connection) ([]map[string]any, error)
	Answer   func(plan ConfirmationReadPlan, index int, diff platform.ConfirmationDifference, outcome string)
}

func ConfirmationOpen(run platform.ConfirmationRun) bool {
	return slices.Contains([]string{"pending", "partial", "expired"}, run.State) && run.Completion == ""
}

func confirmationReconciling(r platform.ConfirmationRun, i int) bool {
	p := r.Parties[i]
	return (p.State == "pending" || p.State == "unknown") && i < len(r.Definition.Parties) && r.Definition.Parties[i].Reconcile != nil
}

func (c ConfirmationReconcile) Run(at time.Time) {
	runs, err := c.Pending()
	if err != nil {
		return
	}
	active := map[string]bool{}
	for _, run := range runs {
		if ConfirmationOpen(run) {
			for i, party := range run.Parties {
				if confirmationReconciling(run, i) {
					active[run.ID+":"+party.Name] = true
				}
			}
		}
	}
	c.Schedule.Retain(active)
	due := func(r platform.ConfirmationRun) time.Time {
		var earliest time.Time
		found := false
		for i, p := range r.Parties {
			if confirmationReconciling(r, i) {
				next := c.Schedule.Next(r.ID+":"+p.Name, p.ReconcileDue)
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
		if !ConfirmationOpen(saved) {
			continue
		}
		for i := range saved.Parties {
			slot := saved.ID + ":" + saved.Parties[i].Name
			plan, refusal := c.Plan(saved.ID, i)
			if refusal != nil || !plan.Valid {
				continue
			}
			run := plan.Run
			if reads >= StatusReadLimit {
				return
			}
			if !c.Schedule.Start(slot, run.Parties[i].ReconcileDue, at, &reads) {
				continue
			}
			policy := run.Definition.Parties[i].Reconcile
			diff := platform.ConfirmationDifference{At: at}
			var rows []map[string]any
			var problem error
			if (platform.MarkingSet{Level: plan.Connection.Marking, Compartments: plan.Connection.Compartments}).Guarded() {
				problem = fmt.Errorf("Reconciliation requires an unclassified source projection")
			} else {
				rows, problem = c.Read(plan.Source, &plan.Connection)
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
				diff.Matched = equalRaw(diff.Observed, policy.Expected)
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
			c.Schedule.Finished(slot, at.Add(time.Duration(policy.EverySeconds)*time.Second))
			c.Answer(plan, i, diff, outcome)
		}
	}
}

func equalRaw(a, b []byte) bool {
	var left, right any
	return json.Unmarshal(a, &left) == nil && json.Unmarshal(b, &right) == nil && reflect.DeepEqual(left, right)
}

func ConfirmationAnswerSubmission(tenant, authority string, caller platform.Member, run, current platform.ConfirmationRun, i int, diff platform.ConfirmationDifference, outcome string, at time.Time) *pb.Submission {
	party := current.Parties[i]
	state := "unknown"
	if diff.Matched {
		state = "confirmed"
	}
	if prior := party.Differences; party.State == state && len(prior) > 0 {
		last := prior[len(prior)-1]
		if last.Matched == diff.Matched && last.Error == diff.Error && bytes.Equal(last.Observed, diff.Observed) {
			return nil
		}
	}
	p := run.Parties[i]
	payload := map[string]any{"party": p.Name, "revision": run.Revision, "outcome": outcome, "reason": "Source reconciliation", "difference": diff, "answer": diff.Observed}
	return &pb.Submission{TenantId: tenant, PrincipalId: caller.ID, Authority: authority, IdempotencyKey: "reconcile:" + run.ID + ":" + p.Name + ":" + at.UTC().Format(time.RFC3339Nano), Target: &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.ConfirmationAnswerSchema, Version: 1}, Payload: platform.Raw(payload)}
}
