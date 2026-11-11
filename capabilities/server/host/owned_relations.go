package host

import (
	"encoding/json"
	"platformserver/kernel/records"
	"slices"
	"strings"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
)

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
	id := records.RelationID(request.Binding.Ref.String(), parent, child)
	entity := platform.RelationEntity(c.App)
	et := d.records.Type(entity.Type)
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
	return d.Put(c, receipt, platform.RelationValue(entity, id, request.Binding.Ref.Name, request.Binding.SourceVersion, parent, child, remove, legacy...))
}

func (t *Tenant) ownedRelationTargets(store *records.Store, c platform.Caller, link platform.LinkType, direction, id string, at time.Time) ([]string, bool, *kernel.Error) {
	object, target := link.Parent.Name, link.Child.Name
	if direction == "reverse" {
		object, target = target, object
	}
	if _, err := t.readRecordFrom(store, c, object, id, at); err != nil {
		return nil, false, err
	}
	refs := []string{}
	func() {
		store.Lock()
		defer store.Unlock()
		et := store.Type(platform.OwnedRelationType(link.Child.App))
		if et != nil {
			for _, row := range et.Rows().All() {
				if records.Header(row.Value).Archived {
					continue
				}
				var value struct {
					Binding, Parent, Child string
					Migration              bool
				}
				_ = json.Unmarshal(platform.Raw(row.Value.Interface()), &value)
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
	}()
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
