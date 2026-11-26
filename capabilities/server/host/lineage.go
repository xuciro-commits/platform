package host

import (
	"context"
	"platformserver/kernel/protocol"
	"platformserver/kernel/records"
	"platformserver/platform"
	"platformserver/services/data/lineage"
	build "platformserver/services/resources/authoring"
	"reflect"
	"time"
)

func (t *Tenant) RecordGeneration() uint64 { return t.Projection.Generation() }

func (t *Tenant) LineageState() (lineage.Input, []protocol.Resource, uint64) {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return lineage.StoreInput(t.Projection), records.RowsOf[protocol.Resource](t.Projection, build.ResourceType), t.Projection.Generation()
}

func (t *Tenant) LineageResources() []protocol.Resource {
	t.Projection.Lock()
	defer t.Projection.Unlock()
	return records.RowsOf[protocol.Resource](t.Projection, build.ResourceType)
}

func (t *Tenant) ResourceReadable(m platform.Member, typ, id string) bool {
	allowed, _ := t.ResourcePermission(t.Projection, m, typ, id, "read", false)
	return allowed
}

func (t *Tenant) ProjectDelegated(m platform.Member) bool { return build.ProjectDelegated(m) }

func (t *Tenant) BranchRevision(m platform.Member, id string, now time.Time) (uint32, *platform.Error) {
	branch, refusal := t.ReadBranch(m, id, now)
	return branch.Revision, refusal
}

func (t *Tenant) LineageCache() *lineage.Cache[lineage.Host] { return &t.lineageCache }

func (t *Tenant) BranchView(m platform.Member, id string, now time.Time) (lineage.Host, *platform.Error) {
	return recordRead(t, context.Background(), func(t *Tenant) (lineage.Host, *platform.Error) {
		world, refusal := t.branchWorldLocked(m, id, now)
		if refusal != nil {
			return nil, refusal
		}
		return world, nil
	})
}

func (t *Tenant) lineage(ctx context.Context, m platform.Member, request lineage.LineageRequest) (lineage.Lineage, *platform.Error) {
	return recordRead(t, ctx, func(t *Tenant) (lineage.Lineage, *platform.Error) {
		return lineage.NewReader(t).Read(m, request)
	})
}

func (t *Tenant) markingsIn(store *records.Store, held bool) lineage.MarkingIndex {
	if !held {
		store.Lock()
		defer store.Unlock()
	}
	key, cacheable := lineage.MarkingKeyOf(store, t.DataOwner(), t.DefinitionEpoch.Load())
	return t.markings.Get(key, cacheable, store == t.Projection, func() lineage.MarkingIndex {
		return lineage.IndexMarkings(lineage.Build(lineage.NewReader(t).Guard(lineage.StoreInput(store)), lineage.Guard), store.Generation())
	})
}

func rowValue(row *records.Row) reflect.Value {
	if row == nil {
		return reflect.Value{}
	}
	return row.Value
}

func (t *Tenant) files() platform.FileStore {
	if t.Files != nil {
		return t.Files
	}
	return &t.memFiles
}
