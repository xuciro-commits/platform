package organization

import (
	"fmt"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/internal/enterprise/uaf"
	"platformserver/platform"
	"slices"
	"sort"
	"strings"
)

// The relationship contracts of the Enterprise Core Profile.
//
// UAF 1.3 states, for most of its relationship stereotypes, which ends are
// allowed: "Value for the client metaproperty must be stereotyped
// «ActualPerson»…". The metamodel reader takes those rules out of the XMI
// (uaf.Stereotype.Client/Supplier); this file turns them into the contracts the
// host checks, states the profile's own rules for the constructs the standard
// leaves open, and names — with the standard's own words — every place the
// profile goes beyond it. Nothing else decides what may be joined: element
// add, relationship add, a federated sync and a slice import all ask Allowed,
// so a relationship the standard forbids cannot enter the model the back way.
//
// Three groups:
//
//   - the standard's own rule, checked as written (FillsPost, MapsToGoal,
//     OwnsProcess, MilestoneDependency …): a pair the rule does not name is
//     refused, and the refusal quotes the rule;
//   - the standard's rule extended by the profile (ResponsibleFor: an
//     organisation here also answers for its posts, places, systems and goals;
//     IsCapableToPerform: UAF's conditional pairs relate performers to
//     activities, not to capabilities, so the profile says so and offers
//     «Exhibits», the standard's spelling of the same idea);
//   - a construct the standard leaves open: «ActualResourceRelationship» is
//     UAF's general resource association (its own rules constrain the
//     informationSource and realizes properties, not the ends),
//     «ActualOrganizationRole» is a slot rather than a relationship, and
//     «typedBy» is the platform's own instance-to-type link.
//
// Pair is one end combination, by stereotype name; "*" is any element.
type Pair struct {
	Source string `json:"source"`
	Target string `json:"target"`
}

// Contract is one relationship stereotype as this host checks it.
type Contract struct {
	Stereotype string   `json:"stereotype"`
	Client     []string `json:"client,omitempty"`    // ends from the standard's rule
	Supplier   []string `json:"supplier,omitempty"`  //
	Basis      []string `json:"basis,omitempty"`     // the rule texts those ends come from
	Extension  []Pair   `json:"extension,omitempty"` // ends this profile adds
	Platform   bool     `json:"platform,omitempty"`  // the standard states no plain rule
	Note       string   `json:"note,omitempty"`      // what the profile decides, in words
}

// Contracts are the profile's relationship contracts, by stereotype name.
func Contracts(mm *uaf.Metamodel) []Contract {
	byName := map[string]Contract{}
	for _, n := range mm.Names() {
		st := mm.Stereotypes[n]
		if !mm.Relationship(n) || len(st.Client) == 0 && len(st.Supplier) == 0 {
			continue
		}
		byName[n] = Contract{Stereotype: n, Client: st.Client, Supplier: st.Supplier, Basis: endRules(st)}
	}
	for _, c := range platformContracts() {
		if from, ok := byName[c.Stereotype]; ok { // keep the standard's ends and the text they come from
			c.Client, c.Supplier, c.Basis = from.Client, from.Supplier, from.Basis
		} else if st := mm.Stereotypes[c.Stereotype]; st != nil {
			c.Client, c.Supplier, c.Basis = st.Client, st.Supplier, endRules(st)
		}
		byName[c.Stereotype] = c
	}
	out := make([]Contract, 0, len(byName))
	for _, c := range byName {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Stereotype < out[j].Stereotype })
	return out
}

// endRules are the constraint texts that state which end accepts what.
func endRules(st *uaf.Stereotype) []string {
	var out []string
	for _, c := range st.Constraints {
		lower := strings.ToLower(c)
		if strings.Contains(lower, "client metaproperty") || strings.Contains(lower, "supplier metaproperty") {
			out = append(out, c)
		}
	}
	return out
}

// platformContracts are the profile's own rules and its extensions of the
// standard's. Each says in Note why, so a reader can tell a rule of UAF from a
// decision of this platform.
func platformContracts() []Contract {
	return []Contract{
		{
			Stereotype: Placement, Platform: true, Note: "UAF's general resource association. UAF constrains its informationSource and realizes properties, not the ends, so the profile states them: an organisation under an organisation, a place inside a place, a resource inside a resource, a resource at a place.",
			Extension: []Pair{{Organization, Organization}, {Location, Location}, {Resource, Resource}, {Resource, Location}},
		},
		{
			Stereotype: Membership, Platform: true, Note: "A slot, not a relationship: a party in an organisation. The party may also be an account of this tenant (member:<id>) — accounts are not elements. UAF's owningInstance rule («ActualOrganization») names the organisation end.",
			Extension: []Pair{{Organization, Organization}, {Person, Organization}},
		},
		{
			Stereotype: Typed, Platform: true, Note: "The platform's instance-to-type link, not UAF.",
			Extension: []Pair{{"*", "*"}},
		},
		{
			Stereotype: Performs, Platform: true, Note: "UAF's own pairs are conditional and relate performers to the activities, functions and services they perform — never to a Capability. The profile's meaning (an organisation or resource has a capability) is «Exhibits», which the profile offers beside this; this pair stays accepted so models made before  keep working.",
			Extension: []Pair{{"*", Capability}},
		},
		{
			Stereotype: Owns, Note: "An organisation answers for what it holds: its posts, the places it runs, its systems and resources, and the goals it is set. UAF's supplier names projects, responsibilities and milestones; the profile adds these four.",
			Extension: []Pair{
				{Organization, Post}, {Organization, Location}, {Organization, Resource}, {Organization, System}, {Organization, Goal},
			},
		},
	}
}

// Allowed reports whether the profile accepts this relationship between the two
// stereotypes; when it does not, the second answer is the sentence to show. It
// names the ends the relationship does have, and cites the standard, so a
// modeller learns the model instead of reading a refusal. The full rule text
// stays in the contract's Basis, for a surface with room to show it.
func Allowed(contracts []Contract, mm *uaf.Metamodel, stereotype, source, target string) (bool, string) {
	var c Contract
	found := false
	for _, x := range contracts {
		if x.Stereotype == stereotype {
			c, found = x, true
			break
		}
	}
	if !found {
		return false, fmt.Sprintf("«%s» is not a relationship this profile serves", stereotype)
	}
	for _, p := range c.Extension {
		if anyEnd(mm, []string{p.Source}, source) && anyEnd(mm, []string{p.Target}, target) {
			return true, ""
		}
	}
	if len(c.Client) > 0 || len(c.Supplier) > 0 {
		if anyEnd(mm, c.Client, source) && anyEnd(mm, c.Supplier, target) {
			return true, ""
		}
		ends := fmt.Sprintf("«%s» joins %s (client) to %s (supplier) in UAF %s", stereotype, list(c.Client), list(c.Supplier), mm.Version)
		if ext := pairs(c.Extension); ext != "" {
			ends += ", and " + ext + " in this profile"
		}
		return false, fmt.Sprintf("%s — not %s to %s", ends, source, target)
	}
	if ext := pairs(c.Extension); ext != "" {
		return false, fmt.Sprintf("«%s» joins %s in this profile — not %s to %s", stereotype, ext, source, target)
	}
	return false, fmt.Sprintf("«%s» relates no elements of this profile", stereotype)
}

// list joins end names for a refusal text.
func list(ends []string) string {
	if len(ends) == 0 {
		return "any element"
	}
	return strings.Join(ends, ", ")
}

// pairs joins a contract's own end pairs for a refusal text.
func pairs(ps []Pair) string {
	var out []string
	for _, p := range ps {
		out = append(out, p.Source+" → "+p.Target)
	}
	return strings.Join(out, ", ")
}

// anyEnd reports whether a stereotype is one of the named ends or a
// specialisation of one; "*" is any element and an empty list constrains
// nothing.
func anyEnd(mm *uaf.Metamodel, ends []string, stereotype string) bool {
	if len(ends) == 0 {
		return true
	}
	for _, name := range ends {
		if name == "*" || mm.Is(stereotype, name) {
			return true
		}
	}
	return false
}

// EndsFor are the pairs a contract admits between two stereotypes, for a caller
// that shows a modeller what it may draw.
func EndsFor(c Contract) []Pair {
	if len(c.Client) > 0 || len(c.Supplier) > 0 {
		var out []Pair
		for _, s := range c.Client {
			for _, t := range c.Supplier {
				out = append(out, Pair{Source: s, Target: t})
			}
		}
		return append(out, c.Extension...)
	}
	return c.Extension
}

// decideRelationship validates before exposing any change to the model.
func decideRelationship(m *Model, mm *uaf.Metamodel, id string, p payload, strict, sourceImport bool) (func(*pb.ChangeRecord), *kernel.Error) {
	invalid := func(msg string, args ...any) *kernel.Error {
		return platform.Refuse(pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, msg, args...)
	}
	notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
	conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
	st := mm.Stereotypes[p.Stereotype]
	switch {
	case st == nil || !mm.Relationship(p.Stereotype) && p.Stereotype != Typed && p.Stereotype != Membership: // a role is a slot in UAF, not a UML relationship; it still joins two elements here
		return nil, invalid("{stereotype} is not a UAF relationship stereotype", p.Stereotype)
	case m.RelationshipOf(id) != nil:
		return nil, conflict
	case p.Source == "" || p.Target == "":
		return nil, invalid("a relationship needs a source and a target")
	case p.Until != "" && p.Until <= p.From:
		return nil, invalid("the relationship would end before it starts")
	}
	if strict && sourceRelationship(id) {
		return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The relationship is maintained by its source")
	}
	target := m.ElementOf(p.Target)
	if target == nil {
		return nil, notFound
	}
	source := m.ElementOf(p.Source)
	if source == nil && !(p.Stereotype == Membership && strings.HasPrefix(p.Source, "member:")) {
		return nil, notFound
	}
	if source != nil && p.Source == p.Target {
		return nil, invalid("an element cannot relate to itself")
	}
	if strict && (!target.Covers(p.From, p.Until) || source != nil && !source.Covers(p.From, p.Until)) {
		return nil, invalid("The relationship must stay within both elements' effective periods")
	}
	if strict && source == nil && p.Source == "member:" {
		return nil, invalid("Choose a member for the relationship")
	}
	// The ends the standard names, and the profile's own rules, in one
	// place: a pair the contract refuses never enters the
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
		k := m.KindOf(p.Kind)
		if k == nil {
			return nil, invalid("a placement needs a relationship kind")
		}
		if m.BelowIn(p.Kind, p.Source, p.Target, p.From) {
			return nil, invalid("{target} already sits under {source}; an organisation cannot sit under itself", target.Name, source.Name)
		}
		if strict {
			days := []platform.Date{p.From}
			for _, r := range m.Relationships {
				if r.Stereotype != Placement || r.Kind != p.Kind {
					continue
				}
				for _, period := range r.PeriodsFromLegacy() {
					if r.Source == p.Source && (!k.Matrix || r.Target == p.Target) && sourceImport && overlap(p.From, p.Until, period.From, period.Until) {
						return nil, invalid("The placement overlaps a scheduled parent relationship")
					}
					if r.Source == p.Source && (!k.Matrix || r.Target == p.Target) && sourceRelationship(r.ID) && overlap(p.From, p.Until, period.From, period.Until) {
						return nil, platform.Refuse(pb.ErrorCode_ERROR_CODE_POLICY_DENIED, "The relationship is maintained by its source")
					}
					if period.From > p.From && (p.Until == "" || period.From < p.Until) {
						days = append(days, period.From)
					}
					if r.Source == p.Source && (!k.Matrix || r.Target == p.Target) && overlap(p.From, p.Until, period.From, period.Until) && period.From > p.From {
						return nil, invalid("The placement overlaps a scheduled parent relationship")
					}
				}
			}
			for _, day := range days {
				if m.BelowIn(p.Kind, p.Source, p.Target, day) {
					return nil, invalid("The placement would create a relationship cycle")
				}
			}
		}
		ends = func() { // a tree keeps one current parent
			for i, r := range m.Relationships {
				if r.Stereotype == Placement && r.Kind == p.Kind && r.Source == p.Source && (!k.Matrix || r.Target == p.Target) && r.ActiveOn(p.From) {
					if strict && sourceRelationship(r.ID) {
						continue
					}
					if strict {
						_ = m.Relationships[i].EndOn(p.From)
					} else {
						m.Relationships[i].Until = p.From
					}
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

func sourceRelationship(id string) bool {
	return strings.HasPrefix(id, "sync:") || strings.HasPrefix(id, "fed:")
}
func overlap(a, b, c, d platform.Date) bool { return (b == "" || c < b) && (d == "" || a < d) }
func closingUsage(m Model, id string, day platform.Date) string {
	if refs := m.ElementPropertyUses(id, day); len(refs) > 0 {
		return refs[0]
	}
	for _, r := range m.Relationships {
		if r.Source != id && r.Target != id {
			continue
		}
		for _, period := range r.PeriodsFromLegacy() {
			if period.Until == "" || period.Until > day {
				return r.ID
			}
		}
	}
	for _, link := range m.TenantLinks {
		if link.Organization == id {
			return link.ID
		}
	}
	for _, el := range m.Elements {
		if el.ID == id && el.Published {
			return "published element"
		}
	}
	return ""
}

func checkSourceRelationships(m Model, mm *uaf.Metamodel, prefix string, today platform.Date) *kernel.Error {
	for _, r := range m.Relationships {
		if !strings.HasPrefix(r.ID, prefix) {
			continue
		}
		for _, period := range r.PeriodsFromLegacy() {
			if period.Until != "" && period.Until <= today {
				continue
			}
			from := period.From
			if from < today {
				from = today
			}
			candidate := copyModel(m)
			candidate.Relationships = slices.DeleteFunc(candidate.Relationships, func(other Relationship) bool { return other.ID == r.ID })
			p := payload{Stereotype: r.Stereotype, Source: r.Source, Target: r.Target, Kind: r.Kind, Role: r.Role, Relation: r.Relation, Share: r.Share, Primary: r.Primary, From: from, Until: period.Until}
			// Source ids remain reserved; the shared validator checks their semantic shape.
			_, err := decideRelationship(&candidate, mm, "", p, true, true)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ValidateImportedModel lets preview use the same relationship checks as the accepted import.
func (e *Enterprise) ValidateImportedModel(model Model, sourceTenant string, today platform.Date) error {
	if refusal := checkSourceRelationships(model, uaf.Current(), "fed:"+sourceTenant+":", today); refusal != nil {
		return refusal
	}
	return nil
}
