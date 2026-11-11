package host

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"platformserver/services/actions"

	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) PullSources(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	sources, refusal := coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]dataowner.Source, *kernel.Error) {
		found, _, err := platform.Find[dataowner.Source](t.automation(build.ID, false), platform.Query{Domain: json.RawMessage(`[["state","=","published"]]`), Sort: []string{"id"}, Limit: 200})
		if err != nil {
			return nil, err
		}
		owner := t.DataOwner()
		var fixed []dataowner.Source
		for _, source := range found {
			source, err := owner.ExecutionSource(source)
			if err != nil {
				log.Printf("source definition refused: %v", err)
				continue
			}
			fixed = append(fixed, source)
		}
		return fixed, nil
	})
	if refusal != nil {
		log.Printf("source preparation unavailable: %v", refusal)
		return
	}
	for _, source := range sources {
		if source.Due(now) {
			t.integration().PullSource(source, now)
		}
	}
	t.ReconcileConfirmations(now)
	t.ReadDispatches(now)
	t.EvaluateObservations(now)
}

func (t *Tenant) integrationPending(typ, id string, execution *dataowner.IntegrationExecution) bool {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) { return t.integrationPendingRead(typ, id, execution), nil })
	return value
}

func (t *Tenant) integrationPendingRead(typ, id string, execution *dataowner.IntegrationExecution) bool {
	c := t.automation(build.ID, false)
	var current *dataowner.IntegrationExecution
	switch typ {
	case dataowner.SourceType:
		source, ok := platform.Get[dataowner.Source](c, id)
		if !ok || source.Archived || source.State != "published" || source.Last == nil {
			return false
		}
		current = source.Last.Execution
	case dataowner.PipelineType:
		pipeline, ok := platform.Get[dataowner.Pipeline](c, id)
		if !ok || pipeline.Archived || pipeline.State != "published" || pipeline.Last == nil {
			return false
		}
		current = pipeline.Last.Execution
	}
	return current != nil && current.Pending && current.ID == execution.ID && current.Snapshot == execution.Snapshot && current.Member == execution.Member && slices.Equal(current.Scopes, execution.Scopes)
}

func (t *Tenant) sourceResult(member platform.Member, s dataowner.Source, pull dataowner.SourcePull, phase string, revision *uint32, now time.Time) *kernel.Error {
	payload := platform.Raw(map[string]any{"pull": pull})
	key := "pulled:" + s.ID + ":" + now.UTC().Format(time.RFC3339Nano)
	if pull.Execution != nil {
		key = "source:" + pull.Execution.ID + ":" + phase
	}
	_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key, ExpectedRevision: revision,
		Target: &pb.EntityRef{Type: dataowner.SourceType, Id: s.ID}, Schema: &pb.SchemaRef{Name: dataowner.SchemaSourcePulled, Version: 1}, Payload: payload}, now)
	if phase == "finished" {
		status, detail := "ok", pull.Error
		if pull.Error != "" || err != nil {
			status = "failed"
			if detail == "" && err != nil {
				detail = err.Error()
			}
		}
		t.observeOwnerRun(now, "data", "source:"+build.ID+":"+s.ID+":"+now.UTC().Format(time.RFC3339Nano), build.ID,
			"source/"+s.Name, status, "Source "+s.Title, detail)
	}
	return err
}

func (t *Tenant) declaredRows(object, kind, source string, input []map[string]any, now time.Time) ([]dataowner.SourceRow, *platform.DataSourceBinding, error) {
	type answer struct {
		rows    []dataowner.SourceRow
		binding *platform.DataSourceBinding
	}
	value, err := readWithError(t, context.Background(), func(t *Tenant) (answer, error) {
		rows, binding, err := t.declaredRowsRead(object, kind, source, input, now)
		return answer{rows: rows, binding: binding}, err
	})
	return value.rows, value.binding, err
}

func (t *Tenant) declaredRowsRead(object, kind, source string, input []map[string]any, now time.Time) ([]dataowner.SourceRow, *platform.DataSourceBinding, error) {
	t.Projection.Lock()
	et := t.Projection.Type(object)
	var binding *platform.DataSourceBinding
	if et != nil {
		for _, candidate := range et.Info().DataSources {
			if candidate.Kind == kind && candidate.Source == source {
				copy := candidate
				binding = &copy
				break
			}
		}
	}
	t.Projection.Unlock()
	if binding == nil {
		return nil, nil, nil
	}
	if len(input) > dataowner.ValueSourceRows {
		return nil, binding, fmt.Errorf("the answer holds %d rows; a pull takes at most %d", len(input), dataowner.ValueSourceRows)
	}
	rows := make([]dataowner.SourceRow, 0, len(input))
	for _, values := range input {
		id := platform.IntegrationScalar(values[binding.Key])
		at := now.UTC()
		row := dataowner.SourceRow{ID: id, Origin: id, At: &at}
		if id != "" {
			if resolved, failure := t.claims.Identity.Resolve(&pb.EntityRef{Type: object, Id: id}); failure == nil && len(resolved) == 1 && resolved[0].Type == object {
				row.ID = resolved[0].ID
			}
		}
		if id == "" {
			row.Error = "row has no id"
		}
		if raw := platform.IntegrationScalar(values[binding.SourceTime]); binding.SourceTime != "" && raw != "" {
			at, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				at, err = time.Parse(time.DateOnly, raw)
			}
			if err != nil {
				row.Error = "row has an invalid source time"
			} else {
				parsed := at.UTC()
				row.At = &parsed
			}
		}
		payload := map[string]any{}
		for _, column := range binding.Columns {
			if value, exists := values[column.Column]; exists {
				payload[column.Attribute] = value
			}
		}
		if len(payload) == 0 {
			row.Error = "row has no mapped attributes"
		}
		row.Payload, _ = json.Marshal(payload)
		rows = append(rows, row)
	}
	return rows, binding, nil
}

func (t *Tenant) prepareRows(member platform.Member, object, producer string, rows []dataowner.SourceRow, now time.Time) (int, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (int, *kernel.Error) {
		merged := 0
		if resolver := t.resolver(member, object, producer, now); resolver != nil {
			for i := range rows {
				resolver.Resolve(&rows[i])
			}
			merged = resolver.Merged
		}
		t.Projection.Lock()
		defer t.Projection.Unlock()
		et := t.Projection.Type(object)
		seen := map[string]bool{}
		for i := range rows {
			row := &rows[i]
			row.Key = fmt.Sprintf("row:%d", i)
			row.Verb = ".create"
			if seen[row.ID] || et != nil && et.Rows().Get(row.ID) != nil {
				row.Verb = ".edit"
			}
			if row.Error == "" {
				seen[row.ID] = true
			}
		}
		return merged, nil
	})
}

func (t *Tenant) integrationRefused(authority, key string) bool {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) { return t.integrationRefusedRead(authority, key), nil })
	return value
}

func (t *Tenant) integrationRefusedRead(authority, key string) bool {
	_, ok := t.Committed.Refusal(authority + "/" + key)
	return ok
}

func (t *Tenant) applyRows(member platform.Member, plan dataowner.IntegrationPreparation, execution *dataowner.IntegrationExecution, now time.Time, pull *dataowner.SourcePull) bool {
	t.Projection.Lock()
	et := t.Projection.Type(plan.Object)
	t.Projection.Unlock()
	for _, row := range plan.Rows {
		if t.quarantined() || !t.integrationPending(plan.Type, plan.ID, execution) {
			return false
		}
		if row.Deleted && pull.Failed > 0 {
			continue
		}
		if row.Error != "" {
			pull.Fail(row.ID, row.Error)
			continue
		}
		if et == nil {
			pull.Fail(row.ID, "the target object is no longer installed")
			continue
		}
		key := "integration:" + execution.ID + ":" + row.Key
		if err := t.submitIntegrationRow(member, et.Info().App, plan.Object, plan.Binding, row, key, now); err != nil {
			if !t.integrationRefused(et.Info().App, key) {
				return false
			}
			pull.Fail(row.ID, err.Code.String()+": "+err.Message)
		} else if row.Deleted {
			pull.Deleted++
		} else {
			pull.Applied++
		}
	}
	return true
}

func (t *Tenant) submitIntegrationRow(member platform.Member, app, object string, binding *platform.DataSourceBinding, row dataowner.SourceRow, key string, now time.Time) *kernel.Error {
	sub := &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: app, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: object, Id: row.ID}, Schema: &pb.SchemaRef{Name: object + row.Verb, Version: 1}, Payload: row.Payload}
	if binding != nil {
		_, err := t.submit(member, sub, now, submitOptions{source: &sourceSubmission{binding: binding, row: row, deleted: row.Deleted}})
		return err
	}
	_, err := t.Submit(member, sub, now)
	return err
}

func (t *Tenant) connectionMarkings(connection string) platform.MarkingSet {
	if connection == "" {
		return platform.MarkingSet{}
	}
	conn, _ := platform.Get[dataowner.Connection](t.automation(build.ID, false), connection)
	fixed, err := t.DataOwner().ExecutionConnection(conn)
	if err != nil {
		return platform.MarkingSet{Level: "restricted"}
	}
	return platform.MarkingSet{Level: fixed.Marking, Compartments: fixed.Compartments}
}

func (t *Tenant) resolver(member platform.Member, object, producer string, now time.Time) *dataowner.Resolver {
	rules, _, _ := platform.Find[dataowner.Match](t.automation(build.ID, false), platform.Query{Limit: 500})
	owner := t.DataOwner()
	var fixed []dataowner.Match
	for _, rule := range rules {
		execution, err := owner.ExecutionMatching(rule)
		if err != nil {
			log.Printf("matching definition refused: %v", err)
			continue
		}
		fixed = append(fixed, execution)
	}
	rules = fixed
	i := slices.IndexFunc(rules, func(m dataowner.Match) bool { return m.Object == object && m.State == "published" })
	if i < 0 {
		return nil
	}
	var records []map[string]any
	for offset := 0; ; offset += 500 {
		page, err := t.recordsRead(member, object, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, now)
		if err != nil {
			break
		}
		for _, r := range page.Records {
			raw, _ := json.Marshal(r)
			var fields map[string]any
			if json.Unmarshal(raw, &fields) == nil {
				records = append(records, fields)
			}
		}
		if len(page.Records) < 500 || len(records) >= page.Total {
			break
		}
	}
	return dataowner.NewResolver(rules[i], producer, records)
}

func (t *Tenant) loadDatasetKey(member platform.Member, dataset, producer string, marking platform.MarkingSet, rows []map[string]any, key string, now time.Time, transaction string) *kernel.Error {
	if rows == nil {
		rows = []map[string]any{}
	}
	payload, _ := json.Marshal(map[string]any{"type": transaction, "rows": rows, "producer": producer, "marking": marking.Level, "compartments": marking.Compartments})
	_, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: dataowner.DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: dataowner.SchemaDatasetLoad, Version: 1}, Payload: payload}, now)
	return err
}

func (t *Tenant) readSource(s dataowner.Source) ([]map[string]any, error) {
	var conn *dataowner.Connection
	if s.Connection != "" {
		c, failure := readWithError(t, context.Background(), func(t *Tenant) (dataowner.Connection, error) {
			current, ok := platform.Get[dataowner.Connection](t.automation(build.ID, false), s.Connection)
			if !ok {
				return current, fmt.Errorf("the connection is not ready")
			}
			return t.DataOwner().ExecutionConnection(current)
		})
		if failure != nil || c.State != "ready" {
			return nil, fmt.Errorf("the connection is not ready")
		}
		conn = &c
		if c.Secret != "" {
			if value, ok := t.CredentialHeader(c.Secret, "connection/"+c.ID, c.Address); !ok || len(strings.TrimSpace(string(value))) == 0 {
				return nil, fmt.Errorf("the named connection secret is unavailable")
			}
		}
	}
	return t.dataClient().Read(s, conn)
}

func (t *Tenant) sender() func(*http.Request, bool) (*http.Response, error) {
	if t.Outbound == nil {
		return guarded
	}
	return t.Outbound
}

func (t *Tenant) transport() actions.Transport {
	return actions.Transport{Credentials: t, Send: t.sender()}
}

func (t *Tenant) dataClient() dataowner.Client {
	return dataowner.Client{Credentials: t, Send: t.sender(), Private: actions.ErrPrivate,
		A2A: func(id, url, secret string, allowPrivate bool, x platform.Effect) platform.Outcome {
			return t.transport().A2A(actions.Endpoint{ID: id, URL: url, Secret: secret, AllowPrivate: allowPrivate}, x, true)
		}}
}

func (t *Tenant) integration() dataowner.Integration {
	return dataowner.Integration{Tenant: t.ID, Rules: t.services.Rules, Files: t.files, Member: t.Member,
		Automation: func() platform.Member { return t.automation(build.ID, false).Member },
		Pending:    t.integrationPending, ReadSource: t.readSource,
		ConnectionMarkings: func(connection string) (platform.MarkingSet, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (platform.MarkingSet, *kernel.Error) { return t.connectionMarkings(connection), nil })
		},
		DeclaredRows: t.declaredRows, PrepareRows: t.prepareRows, AbsentRows: t.absentSourceRows, LoadDataset: t.loadDatasetKey,
		Refused:   func(key string) bool { return t.integrationRefused(build.ID, key) },
		ApplyRows: t.applyRows, SourceResult: t.sourceResult, PipelineResult: t.pipelineResult, PipelineInputs: t.pipelineInputs,
		Unguarded: t.unguarded, DatasetOutput: t.datasetOutput,
	}
}
