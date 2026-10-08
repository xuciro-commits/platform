package enterprise

import (
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/apps/enterprise/uaf"
	"platformserver/platform"
	"strings"
)

// decideRelationship validates before exposing any change to the model.
func decideRelationship(m *Model, mm *uaf.Metamodel, id string, p payload) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := func(msg string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, msg, args...)
	}
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	st := mm.Stereotypes[p.Stereotype]
	switch {
	case st == nil || !mm.Relationship(p.Stereotype) && p.Stereotype != Typed && p.Stereotype != Membership: // a role is a slot in UAF, not a UML relationship; it still joins two elements here
		return nil, invalid("{stereotype} is not a UAF relationship stereotype", p.Stereotype)
	case m.relationship(id) != nil:
		return nil, conflict
	case p.Source == "" || p.Target == "":
		return nil, invalid("a relationship needs a source and a target")
	case p.Until != "" && p.Until <= p.From:
		return nil, invalid("the relationship would end before it starts")
	}
	target := m.element(p.Target)
	if target == nil {
		return nil, notFound
	}
	source := m.element(p.Source)
	if source == nil && !(p.Stereotype == Membership && strings.HasPrefix(p.Source, "member:")) {
		return nil, notFound
	}
	if source != nil && p.Source == p.Target {
		return nil, invalid("an element cannot relate to itself")
	}
	// The ends the standard names, and the profile's own rules, in one
	// place (ADR-0085 D2): a pair the contract refuses never enters the
	// model, and the refusal names the rule it breaks.
	sourceEnd := source
	if sourceEnd == nil && strings.HasPrefix(p.Source, "member:") {
		sourceEnd = &Element{Stereotype: Person} // an account stands where a person stands
	}
	if sourceEnd != nil {
		if ok, why := Allowed(Contracts(mm), mm, p.Stereotype, sourceEnd.Stereotype, target.Stereotype); !ok {
			return nil, invalid(why)
		}
	}
	var ends func()
	switch p.Stereotype {
	case Placement:
		k := m.kind(p.Kind)
		if k == nil {
			return nil, invalid("a placement needs a relationship kind")
		}
		if m.below(p.Kind, p.Source, p.Target, p.From) {
			return nil, invalid("{target} already sits under {source}; an organisation cannot sit under itself", target.Name, source.Name)
		}
		ends = func() { // a tree keeps one current parent
			for i, r := range m.Relationships {
				if r.Stereotype == Placement && r.Kind == p.Kind && r.Source == p.Source && (!k.Matrix || r.Target == p.Target) && activeOn(r.From, r.Until, p.From) {
					m.Relationships[i].Until = p.From
				}
			}
		}
	case Membership:
		if p.Role == "" {
			return nil, invalid("a membership needs a role")
		}
	}
	return func(*pb.ChangeRecord) {
		if ends != nil {
			ends()
		}
		m.Relationships = append(m.Relationships, Relationship{ID: id, Stereotype: p.Stereotype, Kind: p.Kind, Source: p.Source, Target: p.Target, Role: p.Role, Relation: p.Relation, Share: p.Share, Primary: p.Primary, From: p.From, Until: p.Until})
	}, nil

}
