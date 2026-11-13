package records

import (
	"reflect"
	"slices"
	"strings"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func AppendRelationNeighbors(out []string, links []platform.RelationLink, entity string) []string {
	for _, link := range links {
		if link.From == entity && !slices.Contains(out, link.To) {
			out = append(out, link.To)
		} else if link.To == entity && !slices.Contains(out, link.From) {
			out = append(out, link.From)
		}
	}
	return out
}

func migrationID(binding platform.AssetRef) string {
	return RelationID(binding.String(), "migration", "migration")
}

func SortRelationLinks(links []platform.RelationLink) {
	slices.SortFunc(links, func(a, b platform.RelationLink) int {
		if order := strings.Compare(a.From, b.From); order != 0 {
			return order
		}
		return strings.Compare(a.To, b.To)
	})
}

func (s *Store) LegacyRelationActive(link platform.RelationLink) bool {
	s.Lock()
	defer s.Unlock()
	for _, et := range s.Types() {
		if !IsRelationSchema(et.Info()) {
			continue
		}
		for _, row := range et.Rows().All() {
			image := ReadRelation(row)
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

func (s *Store) LegacyRelationWritable(from, to string) bool {
	fromType, _, fromOK := strings.Cut(from, "/")
	toType, _, toOK := strings.Cut(to, "/")
	s.Lock()
	defer s.Unlock()
	a, b := s.Type(fromType), s.Type(toType)
	return !fromOK || !toOK || a == nil || b == nil || a.Info().App != b.Info().App
}

func (s *Store) RelationMigrationPlan(c platform.Caller, binding platform.AssetBinding, link *platform.LinkType, legacy func() []platform.RelationLink, readable func(ref string) bool) (platform.RelationMigrationPlan, *kernel.Error) {
	plan := platform.RelationMigrationPlan{Binding: binding, Links: []platform.RelationLink{}}
	if !slices.ContainsFunc(link.Roles, func(role string) bool { return slices.Contains(c.RolesHere(), role) }) {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The caller cannot migrate this relationship")
	}
	var et *Type
	func() {
		s.Lock()
		defer s.Unlock()
		et = s.Type(platform.OwnedRelationType(c.App))
		if et != nil {
			plan.Migrated = ReadRelation(et.Rows().Get(migrationID(binding.Ref))).Migration
		}
	}()
	if et == nil {
		return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Relationship owner has not declared its relation records")
	}
	if plan.Migrated || legacy == nil {
		return plan, nil
	}
	for _, candidate := range legacy() {
		if !s.LegacyRelationActive(candidate) {
			continue
		}
		from, _, fromOK := strings.Cut(candidate.From, "/")
		to, _, toOK := strings.Cut(candidate.To, "/")
		if !fromOK || !toOK || !(from == link.Parent.Name && to == link.Child.Name || to == link.Parent.Name && from == link.Child.Name) {
			continue
		}
		var visible bool
		func() {
			s.Lock()
			defer s.Unlock()
			visible = readable(candidate.From) && readable(candidate.To)
		}()
		if !visible {
			plan.Partial = true
			continue
		}
		plan.Links = append(plan.Links, candidate)
		if len(plan.Links) > platform.TxnMaxIterations {
			return plan, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration has {count} links; limit is {limit}", len(plan.Links), platform.TxnMaxIterations)
		}
	}
	SortRelationLinks(plan.Links)
	return plan, nil
}

type MigratedLink struct {
	Request platform.TxnLink
	Legacy  platform.RelationLink
}

func MigrationLinks(binding platform.AssetBinding, link *platform.LinkType, plan platform.RelationMigrationPlan) []MigratedLink {
	out := make([]MigratedLink, 0, len(plan.Links))
	for _, legacy := range plan.Links {
		parent, child := legacy.From, legacy.To
		if parentType, _, _ := strings.Cut(parent, "/"); parentType != link.Parent.Name {
			parent, child = child, parent
		}
		parentType, parentID, _ := strings.Cut(parent, "/")
		childType, childID, _ := strings.Cut(child, "/")
		out = append(out, MigratedLink{Request: platform.TxnLink{Binding: binding, Parent: platform.TxnRef{Type: parentType, ID: parentID}, Child: platform.TxnRef{Type: childType, ID: childID}}, Legacy: legacy})
	}
	return out
}

func (s *Store) MigrationOverlaps(app string, m MigratedLink) *kernel.Error {
	parent, child := m.Request.Parent.Type+"/"+m.Request.Parent.ID, m.Request.Child.Type+"/"+m.Request.Child.ID
	prior := s.Type(platform.RelationEntity(app).Type).Rows().Get(RelationID(m.Request.Binding.Ref.String(), parent, child))
	if prior != nil && !Header(prior.Value).Archived {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Migration overlaps an existing owned relationship")
	}
	return nil
}

func MigrationMarker(app string, binding platform.AssetBinding, links []platform.RelationLink) any {
	value := reflect.ValueOf(platform.RelationValue(platform.RelationEntity(app), migrationID(binding.Ref), binding.Ref.Name, binding.SourceVersion, "", "", false, links...))
	marker := reflect.New(value.Type()).Elem()
	marker.Set(value)
	marker.FieldByName("Migration").SetBool(true)
	return marker.Interface()
}
