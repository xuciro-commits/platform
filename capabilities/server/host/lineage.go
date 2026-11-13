package host

import (
	"context"
	dataowner "platformserver/services/data"
	build "platformserver/services/resources/authoring"
	"time"

	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/data/lineage"
)

func lineageInputs(store *records.Store) lineage.Input {
	return lineage.Input{
		Connections: records.RowsOf[dataowner.Connection](store, dataowner.ConnectionType),
		Sources:     records.RowsOf[dataowner.Source](store, dataowner.SourceType),
		Datasets:    records.RowsOf[dataowner.Dataset](store, dataowner.DatasetType),
		Pipelines:   records.RowsOf[dataowner.Pipeline](store, dataowner.PipelineType),
		Objects:     records.RowsOf[build.Object](store, build.ObjectType),
		Writebacks:  records.RowsOf[dataowner.Writeback](store, dataowner.WritebackType),
	}
}

func (t *Tenant) RecordGeneration() uint64 { return t.Projection.Generation() }

func (t *Tenant) LineageState() (lineage.Input, []build.Resource, uint64) {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return lineageInputs(t.Projection), records.RowsOf[build.Resource](t.Projection, build.ResourceType), t.Projection.Generation()
}

func (t *Tenant) LineageResources() []build.Resource {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return records.RowsOf[build.Resource](t.Projection, build.ResourceType)
}

func (t *Tenant) ResourceReadable(m platform.Member, typ, id string) bool {
	allowed, _ := t.ResourcePermission(t.Projection, m, typ, id, "read", false)
	return allowed
}

func (t *Tenant) LineageCache() *lineage.Cache[lineage.Host] { return &t.lineageCache }

func (t *Tenant) BranchView(m platform.Member, id string, now time.Time) (lineage.Host, *kernel.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (lineage.Host, *kernel.Error) {
		world, refusal := t.branchWorldLocked(m, id, now)
		if refusal != nil {
			return nil, refusal
		}
		return world, nil
	})
}

func (t *Tenant) lineage(ctx context.Context, m platform.Member, request lineage.LineageRequest) (lineage.Lineage, *kernel.Error) {
	return recordRead(t, ctx, func(t *Tenant) (lineage.Lineage, *kernel.Error) {
		return lineage.NewReader(t).Read(m, request)
	})
}
