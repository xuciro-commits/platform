package host

import (
	"context"
	"log"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	"time"

	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) ProjectDatasets(now time.Time) {
	if t.businessStopped() || !t.OwnerActive(build.ID) {
		return
	}
	type projection struct{ object, app, dataset string }
	wanted, refusal := recordRead(t, context.Background(), func(t *Tenant) ([]projection, *kernel.Error) {
		var wanted []projection
		for _, et := range t.Projection.SortedTypes() {
			info := et.Info()
			for _, binding := range info.DataSources {
				if binding.Kind == "dataset" {
					wanted = append(wanted, projection{info.Type, info.App, binding.Source})
				}
			}
		}
		return wanted, nil
	})
	if refusal != nil {
		return
	}
	for _, p := range wanted {
		t.projectDataset(p.object, p.app, p.dataset, now)
	}
}

func (t *Tenant) projectDataset(object, app, dataset string, now time.Time) {
	prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (dataowner.DatasetPreparation, *kernel.Error) {
		return t.DataOwner().PrepareProjection(t.automation(build.ID, false), build.ID, build.Builder, object, dataset), nil
	})
	if refusal != nil || prepared.Dataset.ID == "" {
		return
	}
	member := prepared.Member
	result := dataowner.DatasetMerge{Unguarded: t.unguarded,
		DeclaredRows: t.declaredRows,
		PrepareRows: func(object, producer string, rows []dataowner.SourceRow) *kernel.Error {
			_, refusal := t.prepareRows(member, object, producer, rows, now)
			return refusal
		},
		Refused:  func(key string) bool { return t.integrationRefused(app, key) },
		Answered: func(key string) bool { return t.integrationAnswered(app, key) },
		Submit: func(binding *platform.DataSourceBinding, row dataowner.SourceRow, key string) *kernel.Error {
			return t.submitIntegrationRow(member, app, object, binding, row, key, now)
		},
		Absent: func(binding *platform.DataSourceBinding, rows []dataowner.SourceRow, at time.Time) ([]dataowner.SourceRow, *kernel.Error) {
			return t.absentSourceRows(object, binding, rows, at)
		}}.Project(object, dataset, prepared.Dataset, prepared.Data, prepared.Version, prepared.Marking, prepared.Err, now)
	if sub := dataowner.ProjectionSubmission(t.ID, build.ID, member, dataset, prepared.Previous, prepared.Known, result); sub != nil {
		if _, err := t.Submit(member, sub, now); err != nil {
			log.Printf("dataset projection result of %s not kept: %s", result.Object, err.Message)
		}
	}
}

func (t *Tenant) integrationAnswered(app, key string) bool {
	value, _ := recordRead(t, context.Background(), func(t *Tenant) (bool, *kernel.Error) { return t.integrationAnsweredRead(app, key), nil })
	return value
}

func (t *Tenant) integrationAnsweredRead(app, key string) bool {
	owner := t.App(app)
	return owner != nil && t.priorOf(owner, key).Known()
}
