package platformserver

import (
	"encoding/json"
	"platformserver/kernel/records"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

type ownedRelationImage struct {
	platform.Record
	Binding, Version, Parent, Child string
	Migration                       bool
	Legacy                          []platform.RelationLink
}

func appendOwnedRelationNeighbors(out []string, links []platform.RelationLink, entity string) []string {
	for _, link := range links {
		if link.From == entity && !slices.Contains(out, link.To) {
			out = append(out, link.To)
		} else if link.To == entity && !slices.Contains(out, link.From) {
			out = append(out, link.From)
		}
	}
	return out
}

func relationImage(row *records.Row) ownedRelationImage {
	var image ownedRelationImage
	if row != nil {
		_ = json.Unmarshal(platform.Raw(row.Value.Interface()), &image)
	}
	return image
}

func relationMigrationID(binding platform.AssetRef) string {
	return ownedRelationID(binding.String(), "migration", "migration")
}

func (t *Tenant) relationMigrationPlan(store *records.Store, c platform.Caller, binding platform.AssetBinding, at time.Time) (platform.RelationMigrationPlan, *kernel.Error) {
	plan := platform.RelationMigrationPlan{Binding: binding, Links: []platform.RelationLink{}}
	definition, ok := t.recordSetBinding(binding)
	if !ok || definition.LinkType == nil || definition.LinkType.Storage != "edge" || binding.Ref.App != c.App {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, "Migration needs the owner's retained edge declaration")
	}
	link := definition.LinkType
	if !slices.ContainsFunc(link.Roles, func(role string) bool { return slices.Contains(c.RolesHere(), role) }) {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The caller cannot migrate this relationship")
	}
	var et *records.Type
	func() {
		store.Lock()
		defer store.Unlock()
		et = store.Type(platform.OwnedRelationType(c.App))
		if et != nil {
			plan.Migrated = relationImage(et.Rows().Get(relationMigrationID(binding.Ref))).Migration
		}
	}()
	if et == nil {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Relationship owner has not declared its relation records")
	}
	if plan.Migrated {
		return plan, nil
	}
	source, ok := t.linker.(platform.LegacyRelationSource)
	if !ok {
		return plan, nil
	}
	for _, legacy := range source.LegacyRelations() {
		if !t.legacyRelationActive(store, legacy) {
			continue
		}
		from, _, fromOK := strings.Cut(legacy.From, "/")
		to, _, toOK := strings.Cut(legacy.To, "/")
		if !fromOK || !toOK {
			continue
		}
		if !(from == link.Parent.Name && to == link.Child.Name || to == link.Parent.Name && from == link.Child.Name) {
			continue
		}
		var readable bool
		func() {
			store.Lock()
			defer store.Unlock()
			readable = t.readableIn(store, c.Member, legacy.From, at) && t.readableIn(store, c.Member, legacy.To, at)
		}()
		if !readable {
			plan.Partial = true
			continue
		}
		plan.Links = append(plan.Links, legacy)
		if len(plan.Links) > platform.TxnMaxIterations {
			return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration has {count} links; limit is {limit}", len(plan.Links), platform.TxnMaxIterations)
		}
	}
	slices.SortFunc(plan.Links, func(a, b platform.RelationLink) int {
		if order := strings.Compare(a.From, b.From); order != 0 {
			return order
		}
		return strings.Compare(a.To, b.To)
	})
	return plan, nil
}

func (d *stagedDecision) MigrateOwnedRelations(c platform.Caller, receipt *pb.ChangeRecord, binding platform.AssetBinding, at time.Time) *kernel.Error {
	fail := func(err *kernel.Error) *kernel.Error {
		if d.failure == nil {
			d.failure = err
		}
		return err
	}
	if !c.Staging() || c.Replaying || receipt.GetChangeId() == "" || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || receipt.GetSubmission().GetTenantId() != d.tenant.ID {
		return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration is outside its private authority"))
	}
	plan, err := d.tenant.relationMigrationPlan(d.records, c, binding, at)
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
	link := definition.LinkType
	entity := platform.RelationEntity(c.App)
	for _, legacy := range plan.Links {
		parent, child := legacy.From, legacy.To
		parentType, _, _ := strings.Cut(parent, "/")
		if parentType != link.Parent.Name {
			parent, child = child, parent
		}
		parentType, parentID, _ := strings.Cut(parent, "/")
		childType, childID, _ := strings.Cut(child, "/")
		request := platform.TxnLink{Binding: binding, Parent: platform.TxnRef{Type: parentType, ID: parentID}, Child: platform.TxnRef{Type: childType, ID: childID}}
		id := ownedRelationID(binding.Ref.String(), parent, child)
		prior := d.records.Type(entity.Type).Rows().Get(id)
		if prior != nil && !records.Header(prior.Value).Archived {
			return fail(platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration overlaps an existing owned relationship"))
		}
		if err := d.putOwnedRelation(c, receipt, request, false, at, []platform.RelationLink{legacy}); err != nil {
			return fail(err)
		}
	}
	value := reflect.ValueOf(platform.RelationValue(entity, relationMigrationID(binding.Ref), binding.Ref.Name, binding.SourceVersion, "", "", false, plan.Links...))
	copy := reflect.New(value.Type()).Elem()
	copy.Set(value)
	copy.FieldByName("Migration").SetBool(true)
	return d.Put(c, receipt, copy.Interface())
}

func (t *Tenant) ownedRelations(store *records.Store, c platform.Caller, entity string, at time.Time) []platform.RelationLink {
	store.Lock()
	defer store.Unlock()
	out := []platform.RelationLink{}
	for _, et := range store.Types() {
		if !ownedRelationSchema(et.Info()) {
			continue
		}
		filter := platform.Expr{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("boolean", "migration"), platform.ExprLiteralValue("boolean", false)}}
		if entity != "" {
			filter = platform.Expr{Op: "and", Kind: "boolean", Args: []platform.Expr{filter, {Op: "or", Kind: "boolean", Args: []platform.Expr{
				{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "parent"), platform.ExprLiteralValue("text", entity)}},
				{Op: "=", Kind: "boolean", Args: []platform.Expr{platform.ExprReference("text", "child"), platform.ExprLiteralValue("text", entity)}},
			}}}}
		}
		set := platform.ObjectSet{Object: et.Info().Type, Filter: &filter}
		reader := &setRead{t: t, store: store, c: c, now: at, types: map[string]*typeRead{et.Info().Type: {entity: et, view: et}}}
		node, err := reader.prepare(set, false, nil)
		if err != nil {
			continue
		}
		compiled, err := reader.compile(node)
		if err != nil {
			continue
		}
		store.VisitPlan(et, compiled.plans[0], nil, func(value reflect.Value) {
			image := relationImage(&records.Row{Value: value})
			if !t.readableIn(store, c.Member, image.Parent, at) || !t.readableIn(store, c.Member, image.Child, at) {
				return
			}
			by, at := image.Created.By, image.Created.At
			if len(image.Legacy) > 0 {
				by, at = image.Legacy[0].By, image.Legacy[0].At
			}
			out = append(out, platform.RelationLink{From: image.Parent, To: image.Child, By: by, At: at})
		})
	}
	slices.SortFunc(out, func(a, b platform.RelationLink) int {
		if order := strings.Compare(a.From, b.From); order != 0 {
			return order
		}
		return strings.Compare(a.To, b.To)
	})
	return out
}

func (t *Tenant) legacyRelationActive(store *records.Store, link platform.RelationLink) bool {
	store.Lock()
	defer store.Unlock()
	for _, et := range store.Types() {
		if !ownedRelationSchema(et.Info()) {
			continue
		}
		for _, row := range et.Rows().All() {
			image := relationImage(row)
			if !image.Migration || image.Archived {
				continue
			}
			if slices.ContainsFunc(image.Legacy, func(prior platform.RelationLink) bool { return prior.From == link.From && prior.To == link.To }) {
				return false
			}
		}
	}
	return true
}

func (t *Tenant) legacyRelationWritable(store *records.Store, from, to string) bool {
	fromType, _, fromOK := strings.Cut(from, "/")
	toType, _, toOK := strings.Cut(to, "/")
	store.Lock()
	defer store.Unlock()
	a, b := store.Type(fromType), store.Type(toType)
	return !fromOK || !toOK || a == nil || b == nil || a.Info().App != b.Info().App
}

func (r runtime) OwnedRelations(c platform.Caller, entity string) []platform.RelationLink {
	return r.t.ownedRelations(r.t.records, c, entity, time.Now())
}
func (d *stagedDecision) OwnedRelations(c platform.Caller, entity string) []platform.RelationLink {
	return d.tenant.ownedRelations(d.records, c, entity, d.at)
}
func (r runtime) LegacyRelationActive(c platform.Caller, link platform.RelationLink) bool {
	return r.t.legacyRelationActive(r.t.records, link)
}
func (d *stagedDecision) LegacyRelationActive(c platform.Caller, link platform.RelationLink) bool {
	return d.tenant.legacyRelationActive(d.records, link)
}
func (r runtime) LegacyRelationWritable(c platform.Caller, from, to string) bool {
	return r.t.legacyRelationWritable(r.t.records, from, to)
}
func (d *stagedDecision) LegacyRelationWritable(c platform.Caller, from, to string) bool {
	return d.tenant.legacyRelationWritable(d.records, from, to)
}
