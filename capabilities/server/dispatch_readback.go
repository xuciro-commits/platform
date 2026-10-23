package platformserver

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/internal/construction"
	"platformserver/platform"
)

// ReadDispatches uses the source loop's outside lane. Only the small status
// evidence becomes an input; external reads never execute during recovery.
func (t *Tenant) ReadDispatches(at time.Time) {
	if t.quarantined() || t.app(construction.ID) == nil {
		return
	}
	t.mu.Lock()
	runs, refusal := platform.PendingDispatches(t.automation(construction.ID, false))
	if refusal != nil {
		t.mu.Unlock()
		return
	}
	active := make(map[string]bool, len(runs))
	for _, run := range runs {
		active[run.Intent] = true
	}
	t.dispatchReads.retain(active)
	due := func(run platform.DispatchRun) time.Time {
		return t.dispatchReads.next(run.Intent, run.ReadDue)
	}
	slices.SortStableFunc(runs, func(a, b platform.DispatchRun) int { return due(a).Compare(due(b)) })
	t.mu.Unlock()
	reads := 0
	for _, saved := range runs {
		if saved.ReadDue.After(at) {
			continue
		}
		t.mu.Lock()
		run, exists := platform.Get[platform.DispatchRun](t.automation(construction.ID, false), saved.ID)
		valid := exists && !slices.Contains([]string{"completed", "cancelled", "compensated"}, run.State)
		var stamp string
		if valid {
			valid = (hostView{t: t}).CheckDispatchRead(run, at) == nil
			if valid {
				stamp, valid = t.statusReadStamp(run.Revision, run.Member, run.Source, run.Connection, run.Definition.Readback.Record != "")
			}
		}
		if !valid || reads >= statusReadLimit {
			t.mu.Unlock()
			if reads >= statusReadLimit {
				return
			}
			continue
		}
		if !t.dispatchReads.start(run.Intent, run.ReadDue, at, &reads) {
			t.mu.Unlock()
			continue
		}
		t.mu.Unlock()
		reading := t.readDispatchStatus(run, at)
		t.mu.Lock()
		t.dispatchReads.finished(run.Intent, at.Add(time.Duration(run.Definition.Readback.EverySeconds)*time.Second))
		current, found := platform.Get[platform.DispatchRun](t.automation(construction.ID, false), run.ID)
		currentStamp, valid := t.statusReadStamp(current.Revision, current.Member, current.Source, current.Connection, current.Definition.Readback.Record != "")
		if !found || current.Revision != run.Revision || !valid || currentStamp != stamp || (hostView{t: t}).CheckDispatchRead(current, at) != nil {
			t.mu.Unlock()
			continue
		}
		if n := len(current.Readback); n > 0 {
			last := current.Readback[n-1]
			state := reading.State
			if !at.Before(current.Deadline) && state != "unknown" && state != "failed" && state != "completed" {
				state = "stuck"
			}
			// A new command attempt must retain its own first status, even
			// when the controller returns the same value as the old attempt.
			started := current.StartedAt
			for _, attempt := range current.Attempts {
				if attempt.Intent == current.Intent {
					started = attempt.At
				}
			}
			if current.ReadDue.After(started) && !last.At.Before(started) && current.State == state && last.State == reading.State && last.Value == reading.Value && last.Reason == reading.Reason && bytes.Equal(last.Observed, reading.Observed) {
				t.mu.Unlock()
				continue // Polling time alone is not new business evidence.
			}
		}
		caller := t.automation(construction.ID, false).Member
		submission := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: construction.ID,
			IdempotencyKey: fmt.Sprintf("dispatch-read:%s:%d:%s", run.ID, run.Revision, at.UTC().Format(time.RFC3339Nano)),
			Target:         &pb.EntityRef{Type: platform.DispatchType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.DispatchAnswerSchema, Version: 1},
			Payload: platform.Raw(map[string]any{"revision": run.Revision, "intent": run.Intent, "outcome": reading.State, "reason": reading.Reason, "reading": reading, "answer": reading.Observed})}
		_, _ = t.submitAccepted(t.app(construction.ID), caller, submission, at, true)
		t.mu.Unlock()
	}
}

func (t *Tenant) readDispatchStatus(run platform.DispatchRun, at time.Time) platform.DispatchReading {
	reading := platform.DispatchReading{At: at, State: "unknown"}
	policy := run.Definition.Readback
	commandKey := run.Key
	if run.Compensating {
		commandKey += ":compensate"
	}
	var rows []map[string]any
	var problem error
	switch {
	case policy.Record != "":
		member, _ := t.Member(run.Member)
		page, refusal := t.Records(member, policy.Record, platform.Query{Domain: platform.Raw([]any{[]any{policy.KeyField, "=", commandKey}}), Limit: 2, Sort: []string{"id"}}, at)
		if refusal != nil {
			problem = fmt.Errorf("The dispatch status record is unavailable to the applicant")
		} else {
			for _, value := range page.Records {
				var row map[string]any
				if json.Unmarshal(platform.Raw(value), &row) == nil {
					rows = append(rows, row)
				}
			}
		}
	case policy.Source != "":
		var source construction.Source
		var connection construction.Connection
		if json.Unmarshal(run.Source, &source) != nil || json.Unmarshal(run.Connection, &connection) != nil {
			problem = fmt.Errorf("The original dispatch status source is unavailable")
		} else if connection.Secret != "" && len(t.authorization(connection)) == 0 {
			problem = fmt.Errorf("The dispatch status credential is unavailable in this environment")
		} else {
			rows, problem = t.readSourceWithConnection(source, &connection, true)
			if connection.Protocol != nil {
				rows = slices.DeleteFunc(rows, func(row map[string]any) bool { return row["runID"] != run.ID || row["attemptID"] != run.Intent })
			}
		}
	case policy.Observation != "":
		var definition platform.ObservationDefinition
		var connection construction.Connection
		if json.Unmarshal(run.Observation, &definition) != nil || json.Unmarshal(run.Connection, &connection) != nil || run.Device == "" {
			problem = fmt.Errorf("The original dispatch observation is unavailable")
			break
		}
		plan := observationReadPlan{info: ObservationInfo{Ref: platform.AssetRef{App: definition.Object.App, Kind: platform.AssetObservation, Name: definition.Name}, Definition: definition}, connection: connection, device: run.Device, retained: true}
		for _, asset := range run.Assets {
			if asset.Ref == plan.info.Ref {
				plan.info.Version = asset.SourceVersion
				break
			}
		}
		if connection.Secret != "" {
			credential, found := t.credentialHeader(connection.Secret, "connection/"+connection.ID, connection.Address)
			if !found || strings.TrimSpace(string(credential)) == "" {
				problem = fmt.Errorf("The dispatch observation credential is unavailable in this environment")
				break
			}
			plan.header = strings.TrimSpace(string(credential))
		}
		_, subject, _ := strings.Cut(run.Subject, "/")
		started := run.StartedAt
		for _, attempt := range run.Attempts {
			if attempt.Intent == run.Intent && attempt.At.After(started) {
				started = attempt.At
			}
		}
		since := at.Add(-time.Duration(definition.WindowSeconds) * time.Second)
		if started.After(since) {
			since = started
		}
		if !since.Before(at) {
			problem = fmt.Errorf("No observation status exists after this command started")
			break
		}
		result, err := t.fetchObservation(context.Background(), plan, ObservationRead{Record: subject, Since: since, Until: at, Mode: "summary"}, at)
		if err != nil {
			problem = err
		} else if result.Window.Count == 0 {
			problem = fmt.Errorf("The dispatch observation window has no status")
		} else if !result.Window.LastAt.After(started) {
			problem = fmt.Errorf("The observation status predates this command attempt")
		} else {
			reading.Value, reading.Observed = result.Window.Last, platform.Raw(result.Window)
		}
	}
	if policy.Observation == "" && problem == nil {
		matches := 0
		for _, row := range rows {
			key, found := construction.ValueLookupPath(row, policy.KeyField)
			if !found || construction.ValueScalar(key) != commandKey {
				continue
			}
			matches++
			value, found := construction.ValueLookupPath(row, policy.ResultField)
			if !found {
				problem = fmt.Errorf("The dispatch status field is absent")
				continue
			}
			reading.Value, reading.Observed = construction.ValueScalar(value), platform.Raw(value)
			if policy.ReasonField != "" {
				if reason, found := construction.ValueLookupPath(row, policy.ReasonField); found {
					reading.Reason = construction.ValueScalar(reason)
				}
			}
		}
		if matches != 1 {
			problem = fmt.Errorf("The dispatch status key is absent or ambiguous")
		}
	}
	if len(reading.Observed) > 4096 {
		reading.Observed = nil
		problem = fmt.Errorf("The dispatch status exceeds its retained scalar budget")
	}
	if problem != nil {
		reading.Reason = problem.Error()
	} else if state := policy.States[reading.Value]; state != "" {
		reading.State = state
	} else {
		reading.Reason = "The returned dispatch status has no declared mapping"
	}
	if len(reading.Reason) > 4096 {
		reading.Reason = reading.Reason[:4096]
	}
	return reading
}
