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
	build "platformserver/internal/host/buildapi"
	"platformserver/platform"
)

// ReadDispatches uses the source loop's outside lane. Only the small status
// evidence becomes an input; external reads never execute during recovery.
func (t *Tenant) ReadDispatches(at time.Time) {
	if t.quarantined() || t.app(build.ID) == nil {
		return
	}
	t.mu.Lock()
	runs, refusal := platform.PendingDispatches(t.automation(build.ID, false))
	if refusal != nil {
		t.mu.Unlock()
		return
	}
	active := make(map[string]bool, len(runs))
	for _, run := range runs {
		active[run.Intent] = true
	}
	for intent, attempt := range t.dispatchReads {
		if !active[intent] && !attempt.Busy {
			delete(t.dispatchReads, intent)
		}
	}
	due := func(run platform.DispatchRun) time.Time {
		if cached := t.dispatchReads[run.Intent].Due; cached.After(run.ReadDue) {
			return cached
		}
		return run.ReadDue
	}
	slices.SortStableFunc(runs, func(a, b platform.DispatchRun) int { return due(a).Compare(due(b)) })
	t.mu.Unlock()
	reads := 0
	for _, saved := range runs {
		if saved.ReadDue.After(at) {
			continue
		}
		t.mu.Lock()
		run, exists := platform.Get[platform.DispatchRun](t.automation(build.ID, false), saved.ID)
		attempt := t.dispatchReads[run.Intent]
		valid := exists && !slices.Contains([]string{"completed", "cancelled", "compensated"}, run.State) && !run.ReadDue.After(at) && !attempt.Busy && !attempt.Due.After(at)
		var stamp string
		if valid {
			valid = (hostView{t: t}).CheckDispatchRead(run, at) == nil
			if valid {
				stamp, valid = t.dispatchReadStamp(run)
			}
		}
		if !valid || reads >= 64 {
			t.mu.Unlock()
			if reads >= 64 {
				return
			}
			continue
		}
		if t.dispatchReads == nil {
			t.dispatchReads = map[string]statusReadAttempt{}
		}
		t.dispatchReads[run.Intent] = statusReadAttempt{Busy: true}
		t.mu.Unlock()
		reads++
		reading := t.readDispatchStatus(run, at)
		t.mu.Lock()
		t.dispatchReads[run.Intent] = statusReadAttempt{Due: at.Add(time.Duration(run.Definition.Readback.EverySeconds) * time.Second)}
		current, found := platform.Get[platform.DispatchRun](t.automation(build.ID, false), run.ID)
		currentStamp, valid := t.dispatchReadStamp(current)
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
		caller := t.automation(build.ID, false).Member
		submission := &pb.Submission{TenantId: t.ID, PrincipalId: caller.ID, Authority: build.ID,
			IdempotencyKey: fmt.Sprintf("dispatch-read:%s:%d:%s", run.ID, run.Revision, at.UTC().Format(time.RFC3339Nano)),
			Target:         &pb.EntityRef{Type: platform.DispatchType, Id: run.ID}, Schema: &pb.SchemaRef{Name: platform.DispatchAnswerSchema, Version: 1},
			Payload: platform.Raw(map[string]any{"revision": run.Revision, "intent": run.Intent, "outcome": reading.State, "reason": reading.Reason, "reading": reading, "answer": reading.Observed})}
		_, _ = t.submitAccepted(t.app(build.ID), caller, submission, at, true)
		t.mu.Unlock()
	}
}

// The stamp is disposable. Credentials only contribute to this in-memory
// comparison and never enter the run or the accepted read result.
func (t *Tenant) dispatchReadStamp(run platform.DispatchRun) (string, bool) {
	member, found := t.Member(run.Member)
	if !found {
		return "", false
	}
	var connection build.Connection
	_ = json.Unmarshal(run.Connection, &connection)
	var credential []byte
	if connection.Secret != "" {
		credential, _ = t.secret(connection.Secret)
	}
	var source build.Source
	_ = json.Unmarshal(run.Source, &source)
	publisher, _ := t.Member(source.Puller)
	marking := ""
	if connection.ID != "" {
		marking = t.connectionMarking(connection.ID)
	}
	var generation uint64
	if run.Definition.Readback.Record != "" {
		t.records.mu.Lock()
		generation = t.records.generation
		t.records.mu.Unlock()
	}
	stamp, err := canonicalDigest([]any{run.Revision, member, publisher, marking, credential, generation})
	return stamp, err == nil
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
		var source build.Source
		var connection build.Connection
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
		var connection build.Connection
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
			credential, found := t.secret(connection.Secret)
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
		result, err := t.fetchObservation(context.Background(), plan, ObservationRead{Record: subject, Since: since, Until: at, Mode: "summary"})
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
			key, found := build.ValueLookupPath(row, policy.KeyField)
			if !found || build.ValueScalar(key) != commandKey {
				continue
			}
			matches++
			value, found := build.ValueLookupPath(row, policy.ResultField)
			if !found {
				problem = fmt.Errorf("The dispatch status field is absent")
				continue
			}
			reading.Value, reading.Observed = build.ValueScalar(value), platform.Raw(value)
			if policy.ReasonField != "" {
				if reason, found := build.ValueLookupPath(row, policy.ReasonField); found {
					reading.Reason = build.ValueScalar(reason)
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
