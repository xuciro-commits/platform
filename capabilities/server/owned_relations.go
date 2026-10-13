package platformserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

func ownedRelationID(binding, parent, child string) string {
	hash := sha256.Sum256(platform.Raw([]string{binding, parent, child}))
	return "edge-" + hex.EncodeToString(hash[:])
}

func (d *stagedDecision) PutOwnedRelation(c platform.Caller, receipt *pb.ChangeRecord, request platform.TxnLink, remove bool, at time.Time) *kernel.Error {
	return d.putOwnedRelation(c, receipt, request, remove, at, nil)
}

func (d *stagedDecision) putOwnedRelation(c platform.Caller, receipt *pb.ChangeRecord, request platform.TxnLink, remove bool, at time.Time, legacy []platform.RelationLink) *kernel.Error {
	fail := func(message string) *kernel.Error {
		err := platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, message)
		if d.failure == nil {
			d.failure = err
		}
		return err
	}
	if c.Replaying || !c.Staging() || receipt.GetChangeId() == "" || receipt.GetSubmission().GetAuthority() != c.App || receipt.GetSubmission().GetPrincipalId() != c.ID || receipt.GetSubmission().GetTenantId() != d.tenant.ID || request.Binding.Ref.App != c.App || request.Parent.ID == "" || request.Child.ID == "" || request.Parent.Via != "" || request.Child.Via != "" {
		return fail("Owned relationship is outside its private authority")
	}
	definition, ok := d.tenant.recordSetBinding(request.Binding)
	if !ok || definition.LinkType == nil || definition.LinkType.Storage != "edge" {
		return fail("Owned relationship needs a retained edge declaration")
	}
	link := definition.LinkType
	if len(legacy) == 0 && !remove {
		if source, ok := d.tenant.linker.(platform.LegacyRelationSource); ok {
			for _, prior := range source.LegacyRelations() {
				from, _, _ := strings.Cut(prior.From, "/")
				to, _, _ := strings.Cut(prior.To, "/")
				if (from == link.Parent.Name && to == link.Child.Name || to == link.Parent.Name && from == link.Child.Name) && d.tenant.legacyRelationActive(d.records, prior) {
					return fail("Historical relationships require explicit migration first")
				}
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
	if !d.Readable(c, parent) || !d.Readable(c, child) {
		return fail("Owned relationship endpoints are unavailable")
	}
	id := ownedRelationID(request.Binding.Ref.String(), parent, child)
	entity := platform.RelationEntity(c.App)
	et := d.records.types[entity.Type]
	if et == nil || et.info.App != c.App {
		return fail("Relationship owner has not declared its relation records")
	}
	prior := et.rows[id]
	if remove && (prior == nil || recordOf(prior.value).Archived) {
		return fail("Owned relationship does not exist")
	}
	if !remove && prior != nil && !recordOf(prior.value).Archived {
		return fail("Owned relationship already exists")
	}
	return d.Put(c, receipt, platform.RelationValue(entity, id, request.Binding.Ref.Name, request.Binding.SourceVersion, parent, child, remove, legacy...))
}

func (t *Tenant) ownedRelationTargets(store *recordStore, c platform.Caller, link platform.LinkType, direction, id string, at time.Time) ([]string, bool, *kernel.Error) {
	object, target := link.Parent.Name, link.Child.Name
	if direction == "reverse" {
		object, target = target, object
	}
	if _, err := t.readRecordFrom(store, c, object, id, at); err != nil {
		return nil, false, err
	}
	store.mu.Lock()
	et := store.types[platform.OwnedRelationType(link.Child.App)]
	refs := []string{}
	if et != nil {
		for _, row := range et.rows {
			if recordOf(row.value).Archived {
				continue
			}
			var value struct {
				Binding, Parent, Child string
				Migration              bool
			}
			_ = json.Unmarshal(platform.Raw(row.value.Interface()), &value)
			if value.Migration || value.Binding != link.Name {
				continue
			}
			from, to := value.Parent, value.Child
			if direction == "reverse" {
				from, to = to, from
			}
			if from == object+"/"+id {
				refs = append(refs, to)
			}
		}
	}
	store.mu.Unlock()
	ids, partial := []string{}, false
	for _, ref := range refs {
		typ, id, ok := strings.Cut(ref, "/")
		if !ok || typ != target {
			return nil, false, platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, "Owned relationship has an invalid endpoint")
		}
		if _, err := t.readRecordFrom(store, c, typ, id, at); err != nil {
			partial = true
			continue
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return slices.Compact(ids), partial, nil
}

type ownedRelationViolation struct {
	id, parent, child, reason string
}

// Count the final membership first so conflicts do not depend on map order.
// Publication and the transaction gate use the same cardinality semantics.
func (s *recordStore) ownedRelationViolations(link platform.LinkType) ([]ownedRelationViolation, error) {
	et := s.types[platform.OwnedRelationType(link.Child.App)]
	if et == nil || !ownedRelationSchema(et.info) {
		return nil, fmt.Errorf("owned relationship storage is unavailable")
	}
	type edge struct{ id, parent, child string }
	var edges []edge
	parents, children := map[string]int{}, map[string]int{}
	for id, row := range et.rows {
		if recordOf(row.value).Archived {
			continue
		}
		var value struct {
			Binding, Parent, Child string
			Migration              bool
		}
		if err := json.Unmarshal(platform.Raw(row.value.Interface()), &value); err != nil {
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
	var violations []ownedRelationViolation
	for _, value := range edges {
		invalid := ""
		if link.Cardinality != "many-to-many" && children[value.child] > 1 || link.Cardinality == "one-to-one" && parents[value.parent] > 1 {
			invalid = "cardinality"
		}
		parentType, parentID, parentOK := strings.Cut(value.parent, "/")
		childType, childID, childOK := strings.Cut(value.child, "/")
		parent, child := s.types[parentType], s.types[childType]
		if !parentOK || !childOK || parentType != link.Parent.Name || childType != link.Child.Name || parent == nil || child == nil || parent.rows[parentID] == nil || child.rows[childID] == nil {
			invalid = "endpoint"
		} else if link.DeletePolicy == "restrict-active" && recordOf(parent.rows[parentID].value).Archived && !recordOf(child.rows[childID].value).Archived {
			invalid = "archive"
		}
		if invalid != "" {
			violations = append(violations, ownedRelationViolation{value.id, value.parent, value.child, invalid})
		}
	}
	if link.Required {
		child := s.types[link.Child.Name]
		if child == nil {
			return nil, fmt.Errorf("required relationship child is unavailable")
		}
		ids := []string{}
		for id, row := range child.rows {
			if !recordOf(row.value).Archived && children[child.info.Type+"/"+id] == 0 {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		for _, id := range ids {
			violations = append(violations, ownedRelationViolation{child: child.info.Type + "/" + id, reason: "required"})
		}
	}
	return violations, nil
}

func (d *stagedDecision) checkOwnedRelationConstraints() *kernel.Error {
	for _, definition := range d.tenant.definitions {
		link := definition.LinkType
		if link == nil || link.Storage != "edge" {
			continue
		}
		violations, err := d.records.ownedRelationViolations(*link)
		if err != nil {
			return platform.Refuse(pb.ErrorCode_ERROR_CODE_CONFLICT, err.Error())
		}
		for _, violation := range violations {
			ref := platform.OwnedRelationType(link.Child.App) + "/" + violation.id
			if violation.reason == "required" {
				ref = violation.child
			}
			write, changed := d.constraintWrites[ref]
			if !changed && violation.reason == "required" {
				for edgeRef, candidate := range d.constraintWrites {
					if !strings.HasPrefix(edgeRef, platform.OwnedRelationType(link.Child.App)+"/") {
						continue
					}
					id := strings.TrimPrefix(edgeRef, platform.OwnedRelationType(link.Child.App)+"/")
					et := d.records.types[platform.OwnedRelationType(link.Child.App)]
					for _, row := range []*row{candidate.before, et.rows[id]} {
						image := relationImage(row)
						if image.Binding == link.Name && image.Child == violation.child {
							write, changed = candidate, true
							break
						}
					}
					if changed {
						break
					}
				}
			}
			if !changed {
				for _, endpoint := range []string{violation.parent, violation.child} {
					if prior, ok := d.constraintWrites[endpoint]; ok {
						write, changed = prior, true
						break
					}
				}
			}
			if changed {
				rule := platform.Constraint{Name: "relation." + link.Name, Kind: "cardinality", Fields: []string{"archived"}, Message: "Relationship cardinality or archive policy failed.", Translations: map[string]string{"zh-CN": "关系基数或归档策略不允许这次变更。"}}
				return write.caller.RefuseConstraint(rule, ref, nil, violation.reason)
			}
		}
	}
	return nil
}

func ownedRelationSchema(info platform.EntityInfo) bool {
	return strings.HasSuffix(info.Type, ".__relation") && info.App != "" && info.Go == reflect.TypeOf(platform.RelationEntity(info.App).Model)
}
