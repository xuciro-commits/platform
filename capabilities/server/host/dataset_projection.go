package host

import (
	"context"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	dataowner "platformserver/services/data"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) ProjectDatasets(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	t.datasetMerge().ProjectAll(now)
}

func (t *Tenant) datasetMerge() dataowner.DatasetMerge {
	return dataowner.DatasetMerge{
		Tenant: t.ID, Authority: build.ID,
		Targets: func() ([]dataowner.DatasetTarget, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) ([]dataowner.DatasetTarget, *kernel.Error) {
				var targets []dataowner.DatasetTarget
				for _, entity := range t.Projection.SortedTypes() {
					info := entity.Info()
					for _, binding := range info.DataSources {
						if binding.Kind == "dataset" {
							targets = append(targets, dataowner.DatasetTarget{Object: info.Type, App: info.App, Dataset: binding.Source})
						}
					}
				}
				return targets, nil
			})
		},
		Prepare: func(object, dataset string, at time.Time) (dataowner.DatasetPreparation, *kernel.Error) {
			return recordRead(t, context.Background(), func(t *Tenant) (dataowner.DatasetPreparation, *kernel.Error) {
				return t.DataOwner().PrepareProjection(t.automation(build.ID, false), build.ID, build.Builder, object, dataset), nil
			})
		},
		SubmitResult: func(member platform.Member, dataset string, submission *pb.Submission, at time.Time) *kernel.Error {
			_, refusal := t.Submit(member, submission, at)
			return refusal
		},
		Unguarded:    t.unguarded,
		DeclaredRows: t.declaredRows,
		PrepareRows: func(member platform.Member, object, producer string, rows []dataowner.SourceRow, at time.Time) *kernel.Error {
			_, refusal := t.prepareRows(member, object, producer, rows, at)
			return refusal
		},
		Refused: t.integrationRefused, Answered: t.integrationAnswered,
		Submit: func(member platform.Member, app, object string, binding *platform.DataSourceBinding, row dataowner.SourceRow, key string, at time.Time) *kernel.Error {
			return t.integration().SubmitIntegrationRow(member, app, object, binding, row, key, at)
		},
		Absent: t.absentSourceRows,
	}
}

func (t *Tenant) integrationAnswered(authority, key string) bool {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) {
		owner := t.App(authority)
		return owner != nil && t.priorOf(owner, key).Known(), nil
	})
	return value
}
