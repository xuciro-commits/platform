package data

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type DatasetMerge struct {
	Unguarded    func(object, marking string) []string
	DeclaredRows func(object, kind, source string, input []map[string]any, at time.Time) ([]SourceRow, *platform.DataSourceBinding, error)
	PrepareRows  func(object, producer string, rows []SourceRow) *kernel.Error
	Refused      func(key string) bool
	Answered     func(key string) bool
	Submit       func(binding *platform.DataSourceBinding, row SourceRow, key string) *kernel.Error
	Absent       func(binding *platform.DataSourceBinding, rows []SourceRow, at time.Time) ([]SourceRow, *kernel.Error)
}

func (m DatasetMerge) Project(object, dataset string, ds Dataset, data []map[string]any, version int, marking string, readErr error, now time.Time) DatasetProjection {
	result := DatasetProjection{Object: object, Version: ds.Version, At: now, Rows: len(data), MergeVersion: 1, Marking: marking, Compartments: slices.Clone(ds.Compartments)}
	paused := func(reason string) DatasetProjection {
		result.Paused = reason
		return result
	}
	if readErr != nil {
		return paused(readErr.Error())
	}
	result.Version = version
	if open := m.Unguarded(object, marking); len(open) > 0 {
		return paused(fmt.Sprintf("The dataset is %s and the fields %v name no readers", marking, open))
	}
	at := now
	if ds.Last != nil && ds.Last.Version == version {
		at = ds.Last.At
	}
	rows, binding, err := m.DeclaredRows(object, "dataset", dataset, data, at)
	if err != nil || binding == nil {
		return paused(fmt.Sprintf("The data source binding is unavailable: %v", err))
	}
	if refusal := m.PrepareRows(object, "dataset:"+ds.Name, rows); refusal != nil {
		return paused(refusal.Message)
	}
	problem := func(row SourceRow, message string) {
		result.Refused++
		if len(result.Problems) < 5 {
			result.Problems = append(result.Problems, row.ID+": "+message)
		}
	}
	key := func(row SourceRow) string {
		return fmt.Sprintf("dataset-merge-v1:%s:%s@%d:%s", object, dataset, version, row.Key)
	}
	for _, row := range rows {
		result.Objects = append(result.Objects, row.ID)
		if row.Error != "" {
			problem(row, row.Error)
			continue
		}
		if m.Refused(key(row)) {
			problem(row, "refused")
			continue
		}
		if !m.Answered(key(row)) {
			if err := m.Submit(binding, row, key(row)); err != nil {
				if !m.Refused(key(row)) {
					return paused(fmt.Sprintf("Paused at %s: %s", row.ID, err.Message))
				}
				problem(row, err.Message)
				continue
			}
		}
		result.Written++
	}
	if result.Refused == 0 {
		removed, refusal := m.Absent(binding, rows, at)
		if refusal != nil {
			return paused(refusal.Message)
		}
		for _, row := range removed {
			if refusal := m.Submit(binding, row, key(row)); refusal != nil {
				problem(row, refusal.Message)
			} else {
				result.Deleted++
			}
		}
	}
	return result
}

type DatasetPreparation struct {
	Dataset  Dataset
	Previous DatasetProjection
	Known    bool
	Data     []map[string]any
	Version  int
	Marking  string
	Member   platform.Member
	Err      error
}

func (o *Owner) PrepareProjection(c platform.Caller, authority, builder, object, dataset string) DatasetPreparation {
	ds, ok := platform.Get[Dataset](c, dataset)
	previous, known := ds.Projection(object)
	if !ok || ds.Version == 0 || known && previous.Version == ds.Version && previous.Paused == "" && previous.MergeVersion == 1 {
		return DatasetPreparation{}
	}
	data, version, err := DatasetRows(c, dataset, ds.Version)
	marking := ds.Marking
	if fixed, refused := o.ExecutionDataset(ds); refused == nil {
		marking = fixed.Marking
	}
	member := c.Member
	member.Roles = map[string]string{authority: builder}
	return DatasetPreparation{Dataset: ds, Previous: previous, Known: known, Data: data, Version: version, Marking: marking, Member: member, Err: err}
}

func ProjectionSubmission(tenant, authority string, member platform.Member, dataset string, previous DatasetProjection, known bool, result DatasetProjection) *pb.Submission {
	if known && previous.MergeVersion == result.MergeVersion && previous.Deleted == result.Deleted && slices.Equal(previous.Objects, result.Objects) && previous.Version == result.Version && previous.Paused == result.Paused && previous.Written == result.Written && previous.Refused == result.Refused && slices.Equal(previous.Problems, result.Problems) {
		return nil
	}
	payload, _ := json.Marshal(map[string]any{"projection": result})
	digest := fnv.New64a()
	_, _ = digest.Write(payload)
	key := fmt.Sprintf("projection:%s:%s@%d:%x", result.Object, dataset, result.Version, digest.Sum64())
	return &pb.Submission{TenantId: tenant, PrincipalId: member.ID, Authority: authority, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: SchemaDatasetProjected, Version: 1}, Payload: payload}
}
