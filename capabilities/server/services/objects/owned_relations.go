package objects

import (
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/kernel/records"
	"platformserver/platform"
)

type OwnedRelations struct {
	Tenant     string
	Records    *records.Store
	Binding    func(platform.AssetBinding) (*platform.Definition, bool)
	Legacy     func() []platform.RelationLink
	Readable   func(platform.Caller, string) bool
	ReadableAt func(m platform.Member, ref string, at time.Time) bool
	Put        func(platform.Caller, *pb.ChangeRecord, any) *kernel.Error
	Fail       func(*kernel.Error) *kernel.Error
}

func (o OwnedRelations) authority(c platform.Caller, receipt *pb.ChangeRecord) bool {
	return c.Staging() && !c.Replaying && receipt.GetChangeId() != "" && receipt.GetSubmission().GetAuthority() == c.App && receipt.GetSubmission().GetPrincipalId() == c.ID && receipt.GetSubmission().GetTenantId() == o.Tenant
}

func (o OwnedRelations) Plan(c platform.Caller, binding platform.AssetBinding, at time.Time) (platform.RelationMigrationPlan, *kernel.Error) {
	definition, ok := o.Binding(binding)
	if !ok || definition.LinkType == nil || definition.LinkType.Storage != "edge" || binding.Ref.App != c.App {
		return platform.RelationMigrationPlan{Binding: binding, Links: []platform.RelationLink{}}, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Migration needs the owner's retained edge declaration")
	}
	return o.Records.RelationMigrationPlan(c, binding, definition.LinkType, o.Legacy, func(ref string) bool { return o.ReadableAt(c.Member, ref, at) })
}

func (o OwnedRelations) Migrate(c platform.Caller, receipt *pb.ChangeRecord, binding platform.AssetBinding, at time.Time) *kernel.Error {
	if !o.authority(c, receipt) {
		return o.Fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration is outside its private authority"))
	}
	plan, err := o.Plan(c, binding, at)
	if err != nil {
		return o.Fail(err)
	}
	if plan.Partial {
		return o.Fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "Migration contains withheld relationships / 迁移含不可见关系"))
	}
	if plan.Migrated {
		return nil
	}
	definition, _ := o.Binding(binding)
	for _, link := range records.MigrationLinks(binding, definition.LinkType, plan) {
		if err := o.Records.MigrationOverlaps(c.App, link); err != nil {
			return o.Fail(err)
		}
		if err := o.Write(c, receipt, link.Request, false, at, []platform.RelationLink{link.Legacy}); err != nil {
			return o.Fail(err)
		}
	}
	return o.Put(c, receipt, records.MigrationMarker(c.App, binding, plan.Links))
}

func (o OwnedRelations) Write(c platform.Caller, receipt *pb.ChangeRecord, request platform.TxnLink, remove bool, at time.Time, legacy []platform.RelationLink) *kernel.Error {
	fail := func(message string) *kernel.Error {
		return o.Fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message))
	}
	if !o.authority(c, receipt) || request.Binding.Ref.App != c.App || request.Parent.ID == "" || request.Child.ID == "" || request.Parent.Via != "" || request.Child.Via != "" {
		return fail("Owned relationship is outside its private authority")
	}
	definition, ok := o.Binding(request.Binding)
	if !ok || definition.LinkType == nil || definition.LinkType.Storage != "edge" {
		return fail("Owned relationship needs a retained edge declaration")
	}
	link := definition.LinkType
	if len(legacy) == 0 && !remove && o.Legacy != nil {
		for _, prior := range o.Legacy() {
			from, _, _ := strings.Cut(prior.From, "/")
			to, _, _ := strings.Cut(prior.To, "/")
			if (from == link.Parent.Name && to == link.Child.Name || to == link.Parent.Name && from == link.Child.Name) && o.Records.LegacyRelationActive(prior) {
				return fail("Historical relationships require explicit migration first")
			}
		}
	}
	if request.Parent.Type != link.Parent.Name || request.Child.Type != link.Child.Name || link.Parent.App != c.App || link.Child.App != c.App {
		return fail("Owned relationship endpoint types differ")
	}
	if !slices.ContainsFunc(link.Roles, func(role string) bool { return slices.Contains(c.RolesHere(), role) }) {
		return fail("The caller cannot write this owned relationship")
	}
	parent, child := request.Parent.Type+"/"+request.Parent.ID, request.Child.Type+"/"+request.Child.ID
	if !o.Readable(c, parent) || !o.Readable(c, child) {
		return fail("Owned relationship endpoints are unavailable")
	}
	id := records.RelationID(request.Binding.Ref.String(), parent, child)
	entity := platform.RelationEntity(c.App)
	et := o.Records.Type(entity.Type)
	if et == nil || et.Info().App != c.App {
		return fail("Relationship owner has not declared its relation records")
	}
	prior := et.Rows().Get(id)
	if remove && (prior == nil || records.Header(prior.Value).Archived) {
		return fail("Owned relationship does not exist")
	}
	if !remove && prior != nil && !records.Header(prior.Value).Archived {
		return fail("Owned relationship already exists")
	}
	return o.Put(c, receipt, platform.RelationValue(entity, id, request.Binding.Ref.Name, request.Binding.SourceVersion, parent, child, remove, legacy...))
}

func (o *Owner) OwnedRelations(store *records.Store, c platform.Caller, entity string, at time.Time, readable func(ref string) bool) []platform.RelationLink {
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
		if err := o.VisitRawSet(store, c, set, at, func(value reflect.Value) {
			image := records.ReadRelation(&records.Row{Value: value})
			if !readable(image.Parent) || !readable(image.Child) {
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

func (o *Owner) ownedRelationTargets(store *records.Store, c platform.Caller, link platform.LinkType, direction, id string, at time.Time) ([]string, bool, *kernel.Error) {
	object, target := link.Parent.Name, link.Child.Name
	if direction == "reverse" {
		object, target = target, object
	}
	if _, err := o.ReadRecordFrom(store, c, object, id, at); err != nil {
		return nil, false, err
	}
	refs := records.OwnedRelationRefs(store, link, direction, object+"/"+id)
	ids, partial := []string{}, false
	for _, ref := range refs {
		typ, id, ok := strings.Cut(ref, "/")
		if !ok || typ != target {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Owned relationship has an invalid endpoint")
		}
		if _, err := o.ReadRecordFrom(store, c, typ, id, at); err != nil {
			partial = true
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return slices.Compact(ids), partial, nil
}
