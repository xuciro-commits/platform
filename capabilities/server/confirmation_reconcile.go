package platformserver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/internal/host"
	"platformserver/platform"
)

// Scheduling is disposable; accepted differences remain the evidence owner.
type statusReadAttempt struct {
	Due  time.Time
	Busy bool
}

// Reconciliation uses the original bounded source readers on their outside
// lane, then revalidates the captured progress and the applicant's access.
func (t *Tenant) ReconcileConfirmations(at time.Time) {
	if t.quarantined() || t.app(construction.ID) == nil {
		return
	}
	t.mu.Lock()
	runs, err := platform.PendingConfirmations(t.automation(construction.ID, false))
	if err != nil {
		t.mu.Unlock()
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
	for slot, attempt := range t.confirmationReads {
		if !active[slot] && !attempt.Busy {
			delete(t.confirmationReads, slot)
		}
	}
	// Poll the oldest due evidence first, including runs beyond the first page.
	due := func(r platform.ConfirmationRun) time.Time {
		var earliest time.Time
		found := false
		for i, p := range r.Parties {
			if (p.State == "pending" || p.State == "unknown") && i < len(r.Definition.Parties) && r.Definition.Parties[i].Reconcile != nil {
				next := p.ReconcileDue
				if cached := t.confirmationReads[r.ID+":"+p.Name].Due; cached.After(next) {
					next = cached
				}
				if !found || next.Before(earliest) {
					earliest, found = next, true
				}
			}
		}
		return earliest
	}
	slices.SortStableFunc(runs, func(a, b platform.ConfirmationRun) int { return due(a).Compare(due(b)) })
	t.mu.Unlock()
	reads := 0
	for _, saved := range runs {
		if !slices.Contains([]string{"pending", "partial", "expired"}, saved.State) || saved.Completion != "" {
			continue
		}
		for i := range saved.Parties {
			slot := saved.ID + ":" + saved.Parties[i].Name
			t.mu.Lock()
			run, exists := platform.Get[platform.ConfirmationRun](t.automation(construction.ID, false), saved.ID)
			valid := exists && i < len(run.Parties) && i < len(run.Definition.Parties) && run.Completion == "" && slices.Contains([]string{"pending", "partial", "expired"}, run.State)
			if valid {
				valid = (hostView{t: t}).CheckConfirmationRead(run, i, at) == nil
			}
			var source construction.Source
			var connection construction.Connection
			if valid {
				party, policy := run.Parties[i], run.Definition.Parties[i].Reconcile
				attempt := t.confirmationReads[slot]
				valid = policy != nil && (party.State == "pending" || party.State == "unknown") && !party.ReconcileDue.After(at) && !attempt.Busy && !attempt.Due.After(at)
				valid = valid && json.Unmarshal(party.Source, &source) == nil && json.Unmarshal(party.Connection, &connection) == nil
			}
			if !valid {
				t.mu.Unlock()
				continue
			}
			if reads >= 64 {
				t.mu.Unlock()
				return
			}
			if t.confirmationReads == nil {
				t.confirmationReads = map[string]statusReadAttempt{}
			}
			t.confirmationReads[slot] = statusReadAttempt{Busy: true}
			t.mu.Unlock()
			reads++
			p := run.Parties[i]
			policy := run.Definition.Parties[i].Reconcile
			diff := platform.ConfirmationDifference{At: at}
			var rows []map[string]any
			var problem error
			if construction.Guarded(connection.Marking) {
				problem = fmt.Errorf("Reconciliation requires an unclassified source projection")
			} else {
				rows, problem = t.readSourceWithConnection(source, &connection)
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
				key, exists := construction.ValueLookupPath(row, policy.KeyField)
				if !exists || fmt.Sprint(key) != subject {
					continue
				}
				matches++
				value, exists := construction.ValueLookupPath(row, policy.ResultField)
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
			t.mu.Lock()
			t.confirmationReads[slot] = statusReadAttempt{Due: at.Add(time.Duration(policy.EverySeconds) * time.Second)}
			current, exists := platform.Get[platform.ConfirmationRun](t.automation(construction.ID, false), run.ID)
			valid = exists && current.Revision == run.Revision && i < len(current.Parties) && current.Completion == "" && slices.Contains([]string{"pending", "partial", "expired"}, current.State)
			if valid {
				valid = (current.Parties[i].State == "pending" || current.Parties[i].State == "unknown") && (hostView{t: t}).CheckConfirmationRead(current, i, at) == nil
			}
			if !valid {
				t.mu.Unlock()
				continue
			}
			party := current.Parties[i]
			state := "unknown"
			if diff.Matched {
				state = "confirmed"
			}
			if prior := party.Differences; party.State == state && len(prior) > 0 {
				last := prior[len(prior)-1]
				if last.Matched == diff.Matched && last.Error == diff.Error && bytes.Equal(last.Observed, diff.Observed) {
					t.mu.Unlock()
					continue // Keep the first evidence; polling time is not a new result.
				}
			}
			payload := map[string]any{"party": p.Name, "revision": run.Revision, "outcome": outcome, "reason": "Source reconciliation", "difference": diff, "answer": diff.Observed}
			caller := t.automation(construction.ID, false).Member
			submission := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: construction.ID, IdempotencyKey: "reconcile:" + run.ID + ":" + p.Name + ":" + at.UTC().Format(time.RFC3339Nano), Target: &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.ConfirmationAnswerSchema, Version: 1}, Payload: platform.Raw(payload)}
			_, refusal := t.submitAccepted(t.app(construction.ID), caller, submission, at, true)
			t.mu.Unlock()
			if refusal != nil {
				continue // A stale read or revoked access is not an accepted difference.
			}
		}
	}
}

var _ host.ConfirmationHost = hostView{}
