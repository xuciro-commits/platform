package host

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"slices"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/resources/authoring"
)

func (t *Tenant) ProjectDatasets(now time.Time) {
	if t.businessStopped() || !t.ownerActive(build.ID) {
		return
	}
	type projection struct{ object, app, dataset string }
	var wanted []projection
	for _, et := range t.records.SortedTypes() {
		info := et.Info()
		for _, binding := range info.DataSources {
			if binding.Kind == "dataset" {
				wanted = append(wanted, projection{info.Type, info.App, binding.Source})
			}
		}
	}
	for _, p := range wanted {
		t.projectDataset(p.object, p.app, p.dataset, now)
	}
}

func (t *Tenant) projectDataset(object, app, dataset string, now time.Time) {
	t.mu.Lock()
	c := t.automation(build.ID, false)
	ds, ok := platform.Get[build.Dataset](c, dataset)
	previous, known := ds.Projection(object)
	if !ok || ds.Version == 0 || known && previous.Version == ds.Version && previous.Paused == "" && previous.MergeVersion == 1 {
		t.mu.Unlock()
		return
	}
	data, version, err := build.DatasetRows(c, dataset, ds.Version)
	marking := ds.Marking
	if fixed, refused := t.app(build.ID).(build.IntegrationDefinitions).ExecutionDataset(ds); refused == nil {
		marking = fixed.Marking
	}
	member := c.Member
	member.Roles = map[string]string{build.ID: build.Builder}
	t.mu.Unlock()
	result := build.DatasetProjection{Object: object, Version: ds.Version, At: now, Rows: len(data), MergeVersion: 1}
	pause := func(reason string) {
		result.Paused = reason
		t.recordProjection(member, dataset, previous, known, result, now)
	}
	if err != nil {
		pause(err.Error())
		return
	}
	result.Version = version
	if open := t.unguarded(object, marking); len(open) > 0 {
		pause(fmt.Sprintf("The dataset is %s and the fields %v name no readers", marking, open))
		return
	}
	at := now
	if ds.Last != nil && ds.Last.Version == version {
		at = ds.Last.At
	}
	rows, binding, err := t.declaredRows(object, "dataset", dataset, data, at)
	if err != nil || binding == nil {
		pause(fmt.Sprintf("The data source binding is unavailable: %v", err))
		return
	}
	if _, refusal := t.prepareRows(member, object, "dataset:"+ds.Name, rows, now); refusal != nil {
		pause(refusal.Message)
		return
	}
	problem := func(row build.SourceRow, message string) {
		result.Refused++
		if len(result.Problems) < 5 {
			result.Problems = append(result.Problems, row.ID+": "+message)
		}
	}
	for _, row := range rows {
		result.Objects = append(result.Objects, row.ID)
		if row.Error != "" {
			problem(row, row.Error)
			continue
		}
		key := fmt.Sprintf("dataset-merge-v1:%s:%s@%d:%s", object, dataset, version, row.Key)
		if t.integrationRefused(app, key) {
			problem(row, "refused")
			continue
		}
		if !t.integrationAnswered(app, key) {
			if err := t.submitIntegrationRow(member, app, object, binding, row, key, now); err != nil {
				if !t.integrationRefused(app, key) {
					pause(fmt.Sprintf("Paused at %s: %s", row.ID, err.Message))
					return
				}
				problem(row, err.Message)
				continue
			}
		}
		result.Written++
	}
	if result.Refused == 0 {
		removed := slices.DeleteFunc(slices.Clone(previous.Objects), func(id string) bool { return slices.Contains(result.Objects, id) })
		if previous.MergeVersion == 0 {
			t.records.Lock()
			if et := t.records.Type(object); et != nil {
				for id, row := range et.Rows().All() {
					if records.Header(row.Value).Archived || slices.Contains(result.Objects, id) {
						continue
					}
					if slices.ContainsFunc(et.Info().Fields, func(field platform.FieldInfo) bool {
						return slices.ContainsFunc(t.claims.CurrentClaims(t.ID, &pb.EntityRef{Type: object, Id: id}, field.Name), func(claim *pb.FactRecord) bool {
							return claim.GetFact().GetProvenance().GetConnectorId() == "dataset:"+dataset
						})
					}) {
						removed = append(removed, id)
					}
				}
			}
			t.records.Unlock()
		}
		for _, id := range removed {
			key := fmt.Sprintf("dataset-merge-v1:%s:%s@%d:delete:%s", object, dataset, version, id)
			sub := &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: app, IdempotencyKey: key, Target: &pb.EntityRef{Type: object, Id: id}, Schema: &pb.SchemaRef{Name: object + ".edit", Version: 1}, Payload: json.RawMessage("{}")}
			if _, refusal := t.submitSource(member, sub, now, &sourceSubmission{binding: binding, row: build.SourceRow{ID: id, At: &at}, deleted: true}); refusal != nil {
				problem(build.SourceRow{ID: id}, refusal.Message)
			} else {
				result.Deleted++
			}
		}
	}
	t.recordProjection(member, dataset, previous, known, result, now)
}

func (t *Tenant) recordProjection(member platform.Member, dataset string, previous build.DatasetProjection, known bool, result build.DatasetProjection, now time.Time) {
	if known && previous.MergeVersion == result.MergeVersion && previous.Deleted == result.Deleted && slices.Equal(previous.Objects, result.Objects) && previous.Version == result.Version && previous.Paused == result.Paused && previous.Written == result.Written && previous.Refused == result.Refused && slices.Equal(previous.Problems, result.Problems) {
		return
	}
	payload, _ := json.Marshal(map[string]any{"projection": result})
	digest := fnv.New64a()
	_, _ = digest.Write(payload)
	key := fmt.Sprintf("projection:%s:%s@%d:%x", result.Object, dataset, result.Version, digest.Sum64())
	if _, err := t.Submit(member, &pb.Submission{TenantId: t.ID, PrincipalId: member.ID, Authority: build.ID, IdempotencyKey: key,
		Target: &pb.EntityRef{Type: build.DatasetType, Id: dataset}, Schema: &pb.SchemaRef{Name: build.SchemaDatasetProjected, Version: 1}, Payload: payload}, now); err != nil {
		log.Printf("dataset projection result of %s not kept: %s", result.Object, err.Message)
	}
}

func (t *Tenant) integrationAnswered(app, key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, refused := t.committed.Refusal(app + "/" + key)
	return refused || len(t.committed.Answer(app+"/"+key)) > 0
}
