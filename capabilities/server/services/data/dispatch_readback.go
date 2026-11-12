package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"platformkernel/kernel"
	"platformserver/platform"
)

type DispatchReadback struct {
	Client       Client
	Observations Observations
	Records      func(member, typ string, query platform.Query, at time.Time) ([]any, *kernel.Error)
}

func (d DispatchReadback) Read(run platform.DispatchRun, at time.Time) platform.DispatchReading {
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
		found, refusal := d.Records(run.Member, policy.Record, platform.Query{Domain: platform.Raw([]any{[]any{policy.KeyField, "=", commandKey}}), Limit: 2, Sort: []string{"id"}}, at)
		if refusal != nil {
			problem = fmt.Errorf("The dispatch status record is unavailable to the applicant")
		} else {
			for _, value := range found {
				var row map[string]any
				if json.Unmarshal(platform.Raw(value), &row) == nil {
					rows = append(rows, row)
				}
			}
		}
	case policy.Source != "":
		var source Source
		var connection Connection
		if json.Unmarshal(run.Source, &source) != nil || json.Unmarshal(run.Connection, &connection) != nil {
			problem = fmt.Errorf("The original dispatch status source is unavailable")
		} else if connection.Secret != "" && len(d.Client.Authorization(connection)) == 0 {
			problem = fmt.Errorf("The dispatch status credential is unavailable in this environment")
		} else {
			rows, problem = d.Client.Read(source, &connection)
		}
	case policy.Observation != "":
		var definition platform.ObservationDefinition
		var connection Connection
		if json.Unmarshal(run.Observation, &definition) != nil || json.Unmarshal(run.Connection, &connection) != nil || run.Device == "" {
			problem = fmt.Errorf("The original dispatch observation is unavailable")
			break
		}
		plan := ObservationPlan{Info: ObservationInfo{Ref: platform.AssetRef{App: definition.Object.App, Kind: platform.AssetObservation, Name: definition.Name}, Definition: definition}, Connection: connection, Device: run.Device}
		for _, asset := range run.Assets {
			if asset.Ref == plan.Info.Ref {
				plan.Info.Version = asset.SourceVersion
				break
			}
		}
		if connection.Secret != "" {
			credential, found := d.Client.Credentials.CredentialHeader(connection.Secret, "connection/"+connection.ID, connection.Address)
			if !found || strings.TrimSpace(string(credential)) == "" {
				problem = fmt.Errorf("The dispatch observation credential is unavailable in this environment")
				break
			}
			plan.Header = strings.TrimSpace(string(credential))
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
		result, err := d.Observations.Fetch(context.Background(), plan, ObservationRead{Record: subject, Since: since, Until: at, Mode: "summary"}, at)
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
			key, found := platform.IntegrationLookupPath(row, policy.KeyField)
			if !found || platform.IntegrationScalar(key) != commandKey {
				continue
			}
			matches++
			value, found := platform.IntegrationLookupPath(row, policy.ResultField)
			if !found {
				problem = fmt.Errorf("The dispatch status field is absent")
				continue
			}
			reading.Value, reading.Observed = platform.IntegrationScalar(value), platform.Raw(value)
			if policy.ReasonField != "" {
				if reason, found := platform.IntegrationLookupPath(row, policy.ReasonField); found {
					reading.Reason = platform.IntegrationScalar(reason)
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
