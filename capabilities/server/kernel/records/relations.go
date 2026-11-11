package records

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"platformserver/platform"
)

type RelationImage struct {
	platform.Record
	Binding, Version, Parent, Child string
	Migration                       bool
	Legacy                          []platform.RelationLink
}

type RelationViolation struct {
	ID, Parent, Child, Reason string
}

func RelationID(binding, parent, child string) string {
	hash := sha256.Sum256(platform.Raw([]string{binding, parent, child}))
	return "edge-" + hex.EncodeToString(hash[:])
}

func ReadRelation(row *Row) RelationImage {
	var image RelationImage
	if row != nil {
		_ = json.Unmarshal(platform.Raw(row.Value.Interface()), &image)
	}
	return image
}

func IsRelationSchema(info platform.EntityInfo) bool {
	return strings.HasSuffix(info.Type, ".__relation") && info.App != "" && info.Go == reflect.TypeOf(platform.RelationEntity(info.App).Model)
}

func ConstraintValues(info platform.EntityInfo, value reflect.Value) map[string]any {
	values := map[string]any{"id": Header(value).ID, "archived": Header(value).Archived}
	for _, field := range info.Fields {
		values[field.Name] = value.FieldByIndex(field.Index).Interface()
	}
	return values
}

func RelationViolations(s *Store, link platform.LinkType) ([]RelationViolation, error) {
	et := s.Type(platform.OwnedRelationType(link.Child.App))
	if et == nil || !IsRelationSchema(et.Info()) {
		return nil, fmt.Errorf("owned relationship storage is unavailable")
	}
	type edge struct{ id, parent, child string }
	var edges []edge
	parents, children := map[string]int{}, map[string]int{}
	for id, row := range et.Rows().All() {
		if Header(row.Value).Archived {
			continue
		}
		var value RelationImage
		if err := json.Unmarshal(platform.Raw(row.Value.Interface()), &value); err != nil {
			return nil, err
		}
		if value.Migration || value.Binding != link.Name {
			continue
		}
		edges = append(edges, edge{id, value.Parent, value.Child})
		parents[value.Parent]++
		children[value.Child]++
	}
	slices.SortFunc(edges, func(a, b edge) int { return strings.Compare(a.id, b.id) })
	var violations []RelationViolation
	for _, value := range edges {
		invalid := ""
		if link.Cardinality != "many-to-many" && children[value.child] > 1 || link.Cardinality == "one-to-one" && parents[value.parent] > 1 {
			invalid = "cardinality"
		}
		parentType, parentID, parentOK := strings.Cut(value.parent, "/")
		childType, childID, childOK := strings.Cut(value.child, "/")
		parent, child := s.Type(parentType), s.Type(childType)
		if !parentOK || !childOK || parentType != link.Parent.Name || childType != link.Child.Name || parent == nil || child == nil || parent.Rows().Get(parentID) == nil || child.Rows().Get(childID) == nil {
			invalid = "endpoint"
		} else if link.DeletePolicy == "restrict-active" && Header(parent.Rows().Get(parentID).Value).Archived && !Header(child.Rows().Get(childID).Value).Archived {
			invalid = "archive"
		}
		if invalid != "" {
			violations = append(violations, RelationViolation{value.id, value.parent, value.child, invalid})
		}
	}
	if link.Required {
		child := s.Type(link.Child.Name)
		if child == nil {
			return nil, fmt.Errorf("required relationship child is unavailable")
		}
		ids := []string{}
		for id, row := range child.Rows().All() {
			if !Header(row.Value).Archived && children[child.Info().Type+"/"+id] == 0 {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			violations = append(violations, RelationViolation{Child: child.Info().Type + "/" + id, Reason: "required"})
		}
	}
	return violations, nil
}
