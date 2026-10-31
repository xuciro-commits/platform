package platformserver

import (
	"fmt"
	"log"
	"time"

	"platformserver/platform"
	"platformserver/studio/build"
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
	if !ok || ds.Version == 0 || t.projectedVersion(object, dataset) == ds.Version {
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
	if err != nil {
		log.Printf("dataset projection of %s unavailable: %v", object, err)
		return
	}
	if open := t.unguarded(object, marking); len(open) > 0 {
		log.Printf("dataset projection of %s held: %s is %s and fields %v name no readers", object, ds.Name, marking, open)
		return
	}
	at := now
	if ds.Last != nil && ds.Last.Version == version {
		at = ds.Last.At
	}
	rows, binding, err := t.declaredRows(object, "dataset", dataset, data, at)
	if err != nil || binding == nil {
		log.Printf("dataset projection of %s refused: %v", object, err)
		return
	}
	if _, refusal := t.prepareRows(member, object, "dataset:"+ds.Name, rows, now); refusal != nil {
		log.Printf("dataset projection rows unavailable: %v", refusal)
		return
	}
	refused := 0
	for _, row := range rows {
		if row.Error != "" {
			refused++
			continue
		}
		key := fmt.Sprintf("dataset:%s:%s@%d:%s", object, dataset, version, row.Key)
		if t.integrationAnswered(app, key) {
			continue
		}
		if err := t.submitIntegrationRow(member, app, object, binding, row, key, now); err != nil {
			if !t.integrationRefused(app, key) {
				log.Printf("dataset projection of %s paused at %s: %s", object, row.ID, err.Message)
				return
			}
			refused++
		}
	}
	if refused > 0 {
		log.Printf("dataset projection of %s from %s v%d refused %d of %d rows", object, ds.Name, version, refused, len(rows))
	}
	t.projectionMu.Lock()
	if t.projected == nil {
		t.projected = map[string]int{}
	}
	t.projected[object+"\n"+dataset] = version
	t.projectionMu.Unlock()
}

func (t *Tenant) projectedVersion(object, dataset string) int {
	t.projectionMu.Lock()
	defer t.projectionMu.Unlock()
	return t.projected[object+"\n"+dataset]
}

func (t *Tenant) integrationAnswered(app, key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, refused := t.committed.Refusal(app + "/" + key)
	return refused || len(t.committed.Answer(app+"/"+key)) > 0
}
