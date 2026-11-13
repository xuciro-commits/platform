package host

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"platformkernel/kernel"
	dataowner "platformserver/services/data"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
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
	type preparation struct {
		dataset  dataowner.Dataset
		previous dataowner.DatasetProjection
		known    bool
		data     []map[string]any
		version  int
		marking  string
		member   platform.Member
		err      error
	}
	prepared, refusal := recordRead(t, context.Background(), func(t *Tenant) (preparation, *kernel.Error) {
		c := t.automation(build.ID, false)
		ds, ok := platform.Get[dataowner.Dataset](c, dataset)
		previous, known := ds.Projection(object)
		if !ok || ds.Version == 0 || known && previous.Version == ds.Version && previous.Paused == "" && previous.MergeVersion == 1 {
			return preparation{}, nil
		}
		data, version, err := dataowner.DatasetRows(c, dataset, ds.Version)
		marking := ds.Marking
		if fixed, refused := t.DataOwner().ExecutionDataset(ds); refused == nil {
			marking = fixed.Marking
		}
		member := c.Member
		member.Roles = map[string]string{build.ID: build.Builder}
		return preparation{dataset: ds, previous: previous, known: known, data: data, version: version, marking: marking, member: member, err: err}, nil
	})
	if refusal != nil || prepared.dataset.ID == "" {
		return
	}
	member := prepared.member
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
		}}.Project(object, dataset, prepared.dataset, prepared.data, prepared.version, prepared.marking, prepared.err, now)
	t.recordProjection(member, dataset, prepared.previous, prepared.known, result, now)
}

func (t *Tenant) recordProjection(member platform.Member, dataset string, previous dataowner.DatasetProjection, known bool, result dataowner.DatasetProjection, now time.Time) {
	if known && previous.MergeVersion == result.MergeVersion && previous.Deleted == result.Deleted && slices.Equal(previous.Objects, result.Objects) && previous.Version == result.Version && previous.Paused == result.Paused && previous.Written == result.Written && previous.Refused == result.Refused && slices.Equal(previous.Problems, result.Problems) {
		return
	}
	payload, _ := json.Marshal(map[string]any{"projection": result})
	digest := fnv.New64a()
	_, _ = digest.Write(payload)
	key := fmt.Sprintf("projection:%s:%s@%d:%x", result.Object, dataset, result.Version, digest.Sum64())
	if _, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: dataowner.DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: dataowner.SchemaDatasetProjected, Version: 1}, Payload: payload}, now); err != nil {
		log.Printf("dataset projection result of %s not kept: %s", result.Object, err.Message)
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
