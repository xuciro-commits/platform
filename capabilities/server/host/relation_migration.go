package host

import (
	"platformserver/kernel/records"
	"reflect"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func (t *Tenant) relationMigrationPlan(store *records.Store, c platform.Caller, binding platform.AssetBinding, at time.Time) (platform.RelationMigrationPlan, *kernel.Error) {
	definition, ok := t.recordSetBinding(binding)
	if !ok || definition.LinkType == nil || definition.LinkType.Storage != "edge" || binding.Ref.App != c.App {
		return platform.RelationMigrationPlan{Binding: binding, Links: []platform.RelationLink{}}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Migration needs the owner's retained edge declaration")
	}
	var legacy func() []platform.RelationLink
	if source, ok := t.linker.(platform.LegacyRelationSource); ok {
		legacy = source.LegacyRelations
	}
	return store.RelationMigrationPlan(c, binding, definition.LinkType, legacy, func(ref string) bool { return t.readableIn(store, c.Member, ref, at) })
}

func (d *stagedDecision) MigrateOwnedRelations(c platform.Caller, receipt *pb.ChangeRecord, binding platform.AssetBinding, at time.Time) *kernel.Error {
	fail := func(err *kernel.Error) *kernel.Error {
		if d.Failure == nil {
			d.Failure = err
		}
		return err
	}
	if !c.Staging() || c.Replaying || receipt.GetChangeId() == "" || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || receipt.GetSubmission().GetTenantId() != d.tenant.ID {
		return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration is outside its private authority"))
	}
	plan, err := d.tenant.relationMigrationPlan(d.Records, c, binding, at)
	if err != nil {
		return fail(err)
	}
	if plan.Partial {
		return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Migration contains withheld relationships / 迁移含不可见关系"))
	}
	if plan.Migrated {
		return nil
	}
	definition, _ := d.tenant.recordSetBinding(binding)
	for _, link := range records.MigrationLinks(binding, definition.LinkType, plan) {
		if err := d.Records.MigrationOverlaps(c.App, link); err != nil {
			return fail(err)
		}
		if err := d.putOwnedRelation(c, receipt, link.Request, false, at, []platform.RelationLink{link.Legacy}); err != nil {
			return fail(err)
		}
	}
	return d.Put(c, receipt, records.MigrationMarker(c.App, binding, plan.Links))
}

func (t *Tenant) ownedRelations(store *records.Store, c platform.Caller, entity string, at time.Time) []platform.RelationLink {
	store.Lock()
	types := []*records.Type{}
	for _, et := range store.Types() {
		if !records.IsRelationSchema(et.Info()) {
			continue
		}
		types = append(types, et)
	}
	store.Unlock()
	out := []platform.RelationLink{}
	for _, et := range types {
		filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "migration"), platform.ExprLiteralValue("boolean", false)}}
		if entity != "" {
			filter = platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{filter, {Op: "or", Kind: "boolean", Args: []platform.Expr{
				{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "parent"), platform.ExprLiteralValue("text", entity)}},
				{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "child"), platform.ExprLiteralValue("text", entity)}},
			}}}}
		}
		set := platform.ObjectSet{Object: et.Info().Type, Filter: &filter}
		if err := t.objectOwner(store).VisitRawSet(store, c, set, at, func(value reflect.Value) {
			image := records.ReadRelation(&records.Row{Value: value})
			if !t.readableIn(store, c.Member, image.Parent, at) || !t.readableIn(store, c.Member, image.Child, at) {
				return
			}
			by, at := image.Created.By, image.Created.At
			if len(image.Legacy) > 0 {
				by, at = image.Legacy[0].By, image.Legacy[0].At
			}
			out = append(out, platform.RelationLink{From: image.Parent, To: image.Child, By: by, At: at})
		}); err != nil {
			continue
		}
	}
	records.SortRelationLinks(out)
	return out
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
