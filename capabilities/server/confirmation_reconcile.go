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
)

// Reconciliation uses the original bounded source readers on their outside
// lane, then revalidates the captured progress and the applicant's access.
func (t *Tenant) ReconcileConfirmations(at time.Time) {
	if t.quarantined() || t.app(build.ID) == nil {
		return
	}
	t.mu.Lock()
	runs, _, _ := platform.Find[platform.ConfirmationRun](t.automation(build.ID, false), platform.Query{Limit: 500, Sort: []string{"id"}})
	t.mu.Unlock()
	for _, run := range runs {
		if run.State == "confirmed" || run.State == "compensated" {
			continue
		}
		for i, p := range run.Parties {
			policy := run.Definition.Parties[i].Reconcile
			if policy == nil || (p.State != "pending" && p.State != "unknown") || p.ReconcileDue.After(at) {
				continue
			}
			member, known := t.Member(run.Member)
			if !known || !member.Active() {
				continue
			}
			if !t.Readable(member, run.Subject, at) {
				continue
			}
			var source build.Source
			var connection build.Connection
			if json.Unmarshal(p.Source, &source) != nil || json.Unmarshal(p.Connection, &connection) != nil {
				continue
			}
			sourceMember, sourceKnown := t.Member(source.Puller)
			if !sourceKnown || !sourceMember.Active() || !t.Readable(sourceMember, build.SourceType+"/"+policy.Source, at) {
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
			}
			if matches == 0 && problem == nil {
				diff.Error = "The requested source record is absent"
			}
			if len(diff.Observed) > 4096 {
				diff.Observed = nil
				diff.Matched = false
				diff.Error = "Reconciliation evidence exceeds its scalar budget"
			}
			t.mu.Lock()
			current, ok := platform.Get[platform.ConfirmationRun](t.automation(build.ID, false), run.ID)
			valid := ok && current.Revision == run.Revision && current.Parties[i].Intent == p.Intent
			t.mu.Unlock()
			member, known = t.Member(run.Member)
			if !valid || !known || !t.Readable(member, run.Subject, at) {
				continue
			}
			sourceMember, sourceKnown = t.Member(source.Puller)
			if !sourceKnown || !sourceMember.Active() || !t.Readable(sourceMember, build.SourceType+"/"+policy.Source, at) {
				continue
			}
			outcome := "unknown"
			if diff.Matched {
				outcome = "accepted"
			}
			payload := map[string]any{"party": p.Name, "outcome": outcome, "reason": "Source reconciliation", "difference": diff, "answer": diff.Observed}
			caller := t.automation(build.ID, false).Member
			_, refusal := t.Submit(caller, &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID, IdempotencyKey: "reconcile:" + run.ID + ":" + p.Name + ":" + at.UTC().Format(time.RFC3339Nano), Target: &pb.EntityRef{Type: platform.ConfirmationType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.ConfirmationAnswerSchema, Version: 1}, Payload: platform.Raw(payload)}, at)
			if refusal != nil {
				return
			}
		}
	}
}

var _ host.ConfirmationHost = hostView{}
