package platformserver

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/host"
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
	"slices"
)

// Reconciliation uses the original bounded source readers on their outside
// lane, then revalidates the captured progress and the applicant's access.
func (t *Tenant) ReconcileConfirmations(at time.Time) {
	if t.quarantined() || t.app(build.ID) == nil {
		return
	}
	t.mu.Lock()
	runs, err := platform.PendingConfirmations(t.automation(build.ID, false))
	t.mu.Unlock()
	if err != nil {
		return
	}
	// Poll the oldest due evidence first, including runs beyond the first page.
	due := func(r platform.ConfirmationRun) time.Time {
		var earliest time.Time
		found := false
		for _, p := range r.Parties {
			if p.State == "pending" || p.State == "unknown" {
				if !found || p.ReconcileDue.Before(earliest) {
					earliest, found = p.ReconcileDue, true
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
			t.mu.Lock()
			run, exists := platform.Get[platform.ConfirmationRun](t.automation(build.ID, false), saved.ID)
			valid := exists && i < len(run.Parties) && i < len(run.Definition.Parties) && run.Completion == "" && slices.Contains([]string{"pending", "partial", "expired"}, run.State)
			if valid {
				valid = (hostView{t: t}).CheckConfirmationRead(run, i, at) == nil
			}
			t.mu.Unlock()
			if !valid {
				continue
			}
			p := run.Parties[i]
			policy := run.Definition.Parties[i].Reconcile
			if policy == nil || (p.State != "pending" && p.State != "unknown") || p.ReconcileDue.After(at) {
				continue
			}
			if reads >= 64 {
				return
			}
			reads++
			var source build.Source
			var connection build.Connection
			if json.Unmarshal(p.Source, &source) != nil || json.Unmarshal(p.Connection, &connection) != nil {
				continue
			}
			diff := platform.ConfirmationDifference{At: at}
			var rows []map[string]any
			var problem error
			if build.Guarded(connection.Marking) {
				problem = fmt.Errorf("Reconciliation requires an unclassified source projection")
			} else {
				rows, problem = t.readSourceWithConnection(source, &connection)
			}
			_, subject, _ := strings.Cut(run.Subject, "/")
			matches := 0
			for _, row := range rows {
				key, exists := build.ValueLookupPath(row, policy.KeyField)
				if !exists || fmt.Sprint(key) != subject {
					continue
				}
				matches++
				value, exists := build.ValueLookupPath(row, policy.ResultField)
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
			payload := map[string]any{"party": p.Name, "revision": run.Revision, "outcome": outcome, "reason": "Source reconciliation", "difference": diff, "answer": diff.Observed}
			caller := t.automation(build.ID, false).Member
			submission := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID, IdempotencyKey: "reconcile:" + run.ID + ":" + p.Name + ":" + at.UTC().Format(time.RFC3339Nano), Target: &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.ConfirmationAnswerSchema, Version: 1}, Payload: platform.Raw(payload)}
			t.mu.Lock()
			_, refusal := t.submitAccepted(t.app(build.ID), caller, submission, at, true)
			t.mu.Unlock()
			if refusal != nil {
				continue // A stale read or revoked access is not an accepted difference.
			}
		}
	}
}

var _ host.ConfirmationHost = hostView{}
