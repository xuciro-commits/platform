package host

import (
	"platformserver/kernel/records"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
	"platformserver/services/objects"
)

func (t *Tenant) legacyRelations() func() []platform.RelationLink {
	if source, ok := t.linker.(platform.LegacyRelationSource); ok {
		return source.LegacyRelations
	}
	return nil
}

func (t *Tenant) ownedRelationWriter(store *records.Store) objects.OwnedRelations {
	return objects.OwnedRelations{Tenant: t.ID, Records: store, Binding: t.recordSetBinding, Legacy: t.legacyRelations(),
		ReadableAt: func(m platform.Member, ref string, at time.Time) bool { return t.readableIn(store, m, ref, at) }}
}

func (d *stagedDecision) ownedRelationWriter() objects.OwnedRelations {
	writer := d.tenant.ownedRelationWriter(d.Records)
	writer.Readable, writer.Put = d.Readable, d.Put
	writer.Fail = func(err *kernel.Error) *kernel.Error {
		if d.Failure == nil {
			d.Failure = err
		}
		return err
	}
	return writer
}

func (t *Tenant) relationMigrationPlan(store *records.Store, c platform.Caller, binding platform.AssetBinding, at time.Time) (platform.RelationMigrationPlan, *kernel.Error) {
	return t.ownedRelationWriter(store).Plan(c, binding, at)
}

func (d *stagedDecision) MigrateOwnedRelations(c platform.Caller, receipt *pb.ChangeRecord, binding platform.AssetBinding, at time.Time) *kernel.Error {
	return d.ownedRelationWriter().Migrate(c, receipt, binding, at)
}

func (d *stagedDecision) PutOwnedRelation(c platform.Caller, receipt *pb.ChangeRecord, request platform.TxnLink, remove bool, at time.Time) *kernel.Error {
	return d.ownedRelationWriter().Write(c, receipt, request, remove, at, nil)
}

func (t *Tenant) ownedRelations(store *records.Store, c platform.Caller, entity string, at time.Time) []platform.RelationLink {
	return t.objectOwner(store).OwnedRelations(store, c, entity, at, func(ref string) bool { return t.readableIn(store, c.Member, ref, at) })
}

func (r runtime) OwnedRelations(c platform.Caller, entity string) []platform.RelationLink {
	return r.t.ownedRelations(r.t.Projection, c, entity, time.Now())
}
func (d *stagedDecision) OwnedRelations(c platform.Caller, entity string) []platform.RelationLink {
	return d.tenant.ownedRelations(d.Records, c, entity, d.At)
}
func (r runtime) LegacyRelationActive(c platform.Caller, link platform.RelationLink) bool {
	return r.t.Projection.LegacyRelationActive(link)
}
func (d *stagedDecision) LegacyRelationActive(c platform.Caller, link platform.RelationLink) bool {
	return d.Records.LegacyRelationActive(link)
}
func (r runtime) LegacyRelationWritable(c platform.Caller, from, to string) bool {
	return r.t.Projection.LegacyRelationWritable(from, to)
}
func (d *stagedDecision) LegacyRelationWritable(c platform.Caller, from, to string) bool {
	return d.Records.LegacyRelationWritable(from, to)
}
