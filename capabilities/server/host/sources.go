package host

import (
	"context"
	"log"
	"net/http"
	"platformserver/services/actions"

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
		return t.DataOwner().ExecutionSources(t.automation(build.ID, false))
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
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
		return dataowner.PendingExecution(t.automation(build.ID, false), typ, id, execution), nil
	})
	return value
}

func (t *Tenant) sourceResult(member platform.Member, s dataowner.Source, pull dataowner.SourcePull, phase string, revision *uint32, now time.Time) *kernel.Error {
	_, err := t.Submit(member, dataowner.SourcePullSubmission(t.ID, build.ID, member, s, pull, phase, revision, now), now)
	if phase == "finished" {
		status, detail := dataowner.RunOutcome(pull.Error, err)
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
	var info platform.EntityInfo
	t.Projection.Lock()
	if et := t.Projection.Type(object); et != nil {
		info = et.Info()
	}
	t.Projection.Unlock()
	return dataowner.DeclaredSourceRows(info, kind, source, input, now, func(id string) string {
		if resolved, failure := t.claims.Identity.Resolve(&pb.EntityRef{Type: object, Id: id}); failure == nil && len(resolved) == 1 && resolved[0].Type == object {
			return resolved[0].ID
		}
		return id
	})
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
		dataowner.AssignVerbs(rows, func(id string) bool { return et != nil && et.Rows().Get(id) != nil })
		return merged, nil
	})
}

func (t *Tenant) integrationRefused(authority, key string) bool {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
		_, ok := t.Committed.Refusal(authority + "/" + key)
		return ok, nil
	})
	return value
}

func (t *Tenant) rowApplication() dataowner.RowApplication {
	return dataowner.RowApplication{Quarantined: t.quarantined, Pending: t.integrationPending, Refused: t.integrationRefused,
		Target: func(object string) (string, bool) {
			t.Projection.Lock()
			defer t.Projection.Unlock()
			if et := t.Projection.Type(object); et != nil {
				return et.Info().App, true
			}
			return "", false
		},
		Submit: t.submitIntegrationRow}
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
	return t.DataOwner().ConnectionMarkings(t.automation(build.ID, false), connection)
}

func (t *Tenant) resolver(member platform.Member, object, producer string, now time.Time) *dataowner.Resolver {
	return t.DataOwner().MatchResolver(t.automation(build.ID, false), object, producer, func(offset int) ([]any, int, *kernel.Error) {
		page, err := t.recordsRead(member, object, platform.Query{Limit: 500, Offset: offset, Sort: []string{"id"}}, now)
		return page.Records, page.Total, err
	})
}

func (t *Tenant) loadDatasetKey(member platform.Member, dataset, producer string, marking platform.MarkingSet, rows []map[string]any, key string, now time.Time, transaction string) *kernel.Error {
	_, err := t.Submit(member, dataowner.DatasetLoadSubmission(t.ID, build.ID, member, dataset, producer, marking, rows, key, transaction), now)
	return err
}

func (t *Tenant) readSource(s dataowner.Source) ([]map[string]any, error) {
	return t.dataClient().ReadSource(s, func(id string) (dataowner.Connection, error) {
		return readWithError(t, context.Background(), func(t *Tenant) (dataowner.Connection, error) {
			return t.DataOwner().ExecutionConnectionByID(t.automation(build.ID, false), id)
		})
	}, t.CredentialHeader)
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
		ApplyRows: t.rowApplication().Apply, SourceResult: t.sourceResult, PipelineResult: t.pipelineResult, PipelineInputs: t.pipelineInputs,
		Unguarded: t.unguarded, DatasetOutput: t.datasetOutput,
	}
}
