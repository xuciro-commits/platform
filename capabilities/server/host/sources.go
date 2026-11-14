package host

import (
	"context"
	"net/http"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/actions"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) PullSources(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	t.integration().PullSources(now)
	t.ReconcileConfirmations(now)
	t.ReadDispatches(now)
	t.EvaluateObservations(now)
}

func (t *Tenant) integrationPending(typ, id string, execution *dataowner.IntegrationExecution) bool {
	return readOnly(t, func(t *Tenant) bool {
		return dataowner.PendingExecution(t.automation(build.ID, false), typ, id, execution)
	})
}

func readOnly[T any](t *Tenant, read func(*Tenant) T) T {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (T, *kernel.Error) { return read(t), nil })
	return value
}

func (t *Tenant) integrationRefused(authority, key string) bool {
	return readOnly(t, func(t *Tenant) bool {
		_, ok := t.Committed.Refusal(authority + "/" + key)
		return ok
	})
}

func (t *Tenant) connectionMarkings(connection string) platform.MarkingSet {
	return t.DataOwner().ConnectionMarkings(t.automation(build.ID, false), connection)
}

func (t *Tenant) readSource(source dataowner.Source) ([]map[string]any, error) {
	return t.dataClient().ReadSource(source, func(id string) (dataowner.Connection, error) {
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
	caller := func() platform.Caller { return t.automation(build.ID, false) }
	return dataowner.Integration{
		Tenant: t.ID, Authority: build.ID, Rules: t.services.Rules, Files: t.files, Member: t.Member,
		Automation: func() platform.Member { return caller().Member },
		Sources: func() ([]dataowner.Source, *kernel.Error) {
			return coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]dataowner.Source, *kernel.Error) {
				return t.DataOwner().ExecutionSources(caller())
			})
		},
		Quarantined: t.quarantined, Pending: t.integrationPending,
		Target: func(object string) (string, bool) {
			info, found := records.LockedInfo(t.Projection, object)
			return info.App, found
		},
		ReadSource: t.readSource,
		ConnectionMarkings: func(connection string) (platform.MarkingSet, *kernel.Error) {
			return readOnly(t, func(t *Tenant) platform.MarkingSet { return t.connectionMarkings(connection) }), nil
		},
		Source: t.sourcePreparation(), DeclaredRows: t.declaredRows, PrepareRows: t.prepareRows, AbsentRows: t.absentSourceRows,
		Refused: t.integrationRefused, Submit: t.Submit, SubmitBackground: t.submitBackground,
		SubmitSource: func(member platform.Member, submission *pb.Submission, binding *platform.DataSourceBinding, row dataowner.SourceRow, now time.Time) *kernel.Error {
			_, err := t.submit(member, submission, now, submitOptions{source: &dataowner.SourceApplication{Binding: binding, Row: row, Deleted: row.Deleted}})
			return err
		},
		ObserveRun: t.observeOwnerRun, PipelineInputs: t.pipelineInputs,
		Unguarded: t.unguarded, OutputVersion: t.datasetOutput,
		PipelinePlan: func() (dataowner.PipelinePlan, *kernel.Error) {
			filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "state"), platform.ExprLiteralValue("text", "published")}}
			return readOnly(t, func(t *Tenant) dataowner.PipelinePlan {
				return t.DataOwner().PlanPipelines(caller(), platform.Query{Filter: &filter, Sort: []string{"id"}, Limit: 200})
			}), nil
		},
		CurrentPipeline: func(id string) (dataowner.Pipeline, *kernel.Error) {
			return coordinatedRecordRead(t, context.Background(), func(t *Tenant) (dataowner.Pipeline, *kernel.Error) {
				pipeline, _ := platform.Get[dataowner.Pipeline](caller(), id)
				return pipeline, nil
			})
		},
		ScheduleExists: func(id string) bool {
			exists, _ := coordinatedRecordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
				_, ok := platform.Get[platform.Schedule](caller(), id)
				return ok, nil
			})
			return exists
		},
		PipelineProject: func(id string) string {
			return readOnly(t, func(t *Tenant) string { return t.resourceProject(dataowner.PipelineType, id) })
		},
		ExecutionSchedules: func() ([]platform.Schedule, *kernel.Error) {
			return coordinatedRecordRead(t, context.Background(), func(t *Tenant) ([]platform.Schedule, *kernel.Error) {
				return t.DataOwner().ExecutionSchedules(caller())
			})
		},
		Composite: func(member platform.Member, key string, edits []platform.CompositeEdit, now time.Time) error {
			if _, refusal := t.Composite(member, key, edits, now); refusal != nil {
				return refusal
			}
			return nil
		},
	}
}
