package enterprise

import (
	"encoding/json"
	"slices"
	"strings"

	"platformserver/platform"
)

// The tenant's enterprise model (ADR-0067 D3): elements and relationships
// typed by UAF stereotypes, in valid time, plus the views drawn over them.
// What ADR-0012 called units, structures, edges and memberships are the
// Personnel-domain corner of it: «ActualOrganization» elements, relationship
// kinds, «ActualResourceRelationship» and «ActualOrganizationRole».

const (
	// Stereotypes the platform itself relies on (all UAF 1.3).
	Organization = "ActualOrganization"
	Person       = "ActualPerson"
	Post         = "ActualPost"
	Location     = "ActualLocation"
	Resource     = "ActualResource"
	Project      = "ActualProject"
	Capability   = "Capability"
	Goal         = "EnterpriseGoal"
	// Relationship stereotypes.
	Placement  = "ActualResourceRelationship" // unit under parent, in a kind
	Membership = "ActualOrganizationRole"     // party (member:<id> or element id) in an organisation, with a role
	FillsPost  = "FillsPost"                  // person holds post
	Performs   = "IsCapableToPerform"         // organisation/resource has capability
	Owns       = "ResponsibleFor"             // organisation answers for resource/location/project
	Typed      = "typedBy"                    // instance → its type element (platform, not UAF)
)

type Date = platform.Date

// Kind is a way organisations relate (ADR-0012's Structure): legal,
// management, finance, site, project, governance, community, custom.
type Kind struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Matrix bool   `json:"matrix,omitempty"`
}

// Element is one thing in the enterprise, typed by a UAF stereotype.
type Element struct {
	ID         string         `json:"id"`
	Stereotype string         `json:"stereotype"`
	Name       string         `json:"name"`
	ShortName  string         `json:"shortName,omitempty"`
	Kind       string         `json:"kind,omitempty"` // open vocabulary within the stereotype: factory, line, team; plant, bin; machine
	Legal      bool           `json:"legal,omitempty"`
	External   bool           `json:"external,omitempty"`
	From       Date           `json:"from,omitempty"`
	Until      Date           `json:"until,omitempty"`
	Closed     string         `json:"closed,omitempty"`
	Calendar   string         `json:"calendar,omitempty"`
	Owner      string         `json:"owner,omitempty"`     // "tenant:<id>" when federated in from another tenant (read-only here)
	Published  bool           `json:"published,omitempty"` // shared with the tenants this one federates with (ADR-0067 D5)
	Properties map[string]any `json:"properties,omitempty"`
}

// Relationship joins two things; Source and Target are element ids, or for a
// Membership the source may be a party "member:<id>".
type Relationship struct {
	ID         string  `json:"id"`
	Stereotype string  `json:"stereotype"`
	Kind       string  `json:"kind,omitempty"` // for a Placement: the Kind.ID it sits in
	Source     string  `json:"source"`
	Target     string  `json:"target"`
	Role       string  `json:"role,omitempty"`     // for a Membership: employee, chair, volunteer …
	Relation   string  `json:"relation,omitempty"` // for a Placement: part of, owned by, reports to …
	Share      float64 `json:"share,omitempty"`
	Primary    bool    `json:"primary,omitempty"`
	From       Date    `json:"from,omitempty"`
	Until      Date    `json:"until,omitempty"`
}

// View is a drawing over the model: which elements, where, in which UAF grid cell.
type View struct {
	ID       string                `json:"id"`
	Name     string                `json:"name"`
	Grid     string                `json:"grid"` // "Pr-Sr", "St-Tx", "Rs-Sr", "Pj-Rm" …
	Kind     string                `json:"kind,omitempty"`
	Elements []string              `json:"elements"`
	Layout   map[string][2]float64 `json:"layout,omitempty"`
	AsOf     Date                  `json:"asOf,omitempty"`
}

// Model is the whole of it.
type Model struct {
	UAF           string              `json:"uaf"`
	Scale         string              `json:"scale,omitempty"`
	Kinds         []Kind              `json:"kinds"`
	Elements      []Element           `json:"elements"`
	Relationships []Relationship      `json:"relationships"`
	Views         []View              `json:"views"`
	Calendars     []platform.Calendar `json:"calendars,omitempty"`
}

func activeOn(from, until, day Date) bool { return from <= day && (until == "" || day < until) }

func (m *Model) element(id string) *Element {
	if i := slices.IndexFunc(m.Elements, func(e Element) bool { return e.ID == id }); i >= 0 {
		return &m.Elements[i]
	}
	return nil
}

func (m *Model) kind(id string) *Kind {
	if i := slices.IndexFunc(m.Kinds, func(k Kind) bool { return k.ID == id }); i >= 0 {
		return &m.Kinds[i]
	}
	return nil
}

func (m *Model) relationship(id string) *Relationship {
	if i := slices.IndexFunc(m.Relationships, func(r Relationship) bool { return r.ID == id }); i >= 0 {
		return &m.Relationships[i]
	}
	return nil
}

// live says the element exists on day: open, or not yet closed.
func (m Model) live(id string, day Date) bool {
	e := m.element(id)
	return e != nil && activeOn(e.From, e.Until, day)
}

// below reports whether unit sits (transitively) under ancestor in kind on day.
func (m Model) below(kind, ancestor, unit string, day Date) bool {
	for seen := map[string]bool{}; unit != "" && !seen[unit]; {
		seen[unit] = true
		i := slices.IndexFunc(m.Relationships, func(r Relationship) bool {
			return r.Stereotype == Placement && r.Kind == kind && r.Source == unit && activeOn(r.From, r.Until, day)
		})
		if i < 0 {
			return false
		}
		unit = m.Relationships[i].Target
		if unit == ancestor {
			return true
		}
	}
	return false
}

// units are the organisations party belongs to on day, directly or through a
// member organisation, and every one below them in kind ("" for none below).
func (m Model) units(party, kind string, day Date) []string {
	var out []string
	add := func(u string) bool {
		if slices.Contains(out, u) {
			return false
		}
		out = append(out, u)
		return true
	}
	parties := []string{party}
	for len(parties) > 0 {
		p := parties[0]
		parties = parties[1:]
		for _, r := range m.Relationships {
			if r.Stereotype == Membership && r.Source == p && activeOn(r.From, r.Until, day) && m.live(r.Target, day) && add(r.Target) {
				parties = append(parties, r.Target)
			}
		}
	}
	return m.descendants(out, kind, day)
}

// descendants expands only active elements and placements of the named structure.
func (m Model) descendants(units []string, kind string, day Date) []string {
	var out []string
	add := func(id string) {
		if m.live(id, day) && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	for _, id := range units {
		add(id)
	}
	if kind == "" {
		return out
	}
	for i := 0; i < len(out); i++ {
		for _, r := range m.Relationships {
			if r.Stereotype == Placement && r.Kind == kind && r.Target == out[i] && activeOn(r.From, r.Until, day) {
				add(r.Source)
			}
		}
	}
	return out
}

// Of are the elements one relationship away from id: targets of stereotype
// going out (out=true) or sources coming in, live on day.
func (m Model) Of(id, stereotype string, out bool, day Date) []string {
	var found []string
	for _, r := range m.Relationships {
		if r.Stereotype != stereotype || !activeOn(r.From, r.Until, day) {
			continue
		}
		if out && r.Source == id && !slices.Contains(found, r.Target) {
			found = append(found, r.Target)
		} else if !out && r.Target == id && !slices.Contains(found, r.Source) {
			found = append(found, r.Source)
		}
	}
	return found
}

// --- The ADR-0012 projection, kept as the seed format and the "organization" read. ---

// FromOrgSeed lifts units, structures, edges and memberships into the model.
func FromOrgSeed(seed platform.OrgSeed) Model {
	m := Model{UAF: "1.3", Kinds: []Kind{}, Elements: []Element{}, Relationships: []Relationship{}, Views: []View{}, Calendars: slices.Clone(seed.Calendars)}
	for _, s := range seed.Structures {
		m.Kinds = append(m.Kinds, Kind{ID: s.ID, Name: s.Name, Kind: s.Kind, Matrix: s.Matrix})
	}
	for _, u := range seed.Units {
		m.Elements = append(m.Elements, Element{ID: u.ID, Stereotype: Organization, Name: u.Name, Kind: u.Kind, Legal: u.Legal, External: u.External, From: u.From, Until: u.Until, Closed: u.Closed, Calendar: u.Calendar})
	}
	for i, e := range seed.Edges {
		m.Relationships = append(m.Relationships, Relationship{ID: relID("pl", i), Stereotype: Placement, Kind: e.Structure, Source: e.Unit, Target: e.Parent, Relation: e.Relation, Share: e.Share, From: e.From, Until: e.Until})
	}
	for i, ms := range seed.Memberships {
		source := ms.Party
		if id, ok := cutPrefix(ms.Party, "unit:"); ok {
			source = id
		}
		m.Relationships = append(m.Relationships, Relationship{ID: relID("mb", i), Stereotype: Membership, Source: source, Target: ms.Unit, Role: ms.Role, Primary: ms.Primary, From: ms.From, Until: ms.Until})
	}
	return m
}

// OrgSeed projects the Personnel corner back into ADR-0012's shape.
func (m Model) OrgSeed() platform.OrgSeed {
	out := platform.OrgSeed{Calendars: slices.Clone(m.Calendars), Structures: []platform.Structure{}, Units: []platform.Unit{}, Edges: []platform.Edge{}, Memberships: []platform.Membership{}}
	for _, k := range m.Kinds {
		out.Structures = append(out.Structures, platform.Structure{ID: k.ID, Name: k.Name, Kind: k.Kind, Matrix: k.Matrix})
	}
	for _, e := range m.Elements {
		if e.Stereotype == Organization {
			out.Units = append(out.Units, platform.Unit{ID: e.ID, Name: e.Name, Kind: e.Kind, Legal: e.Legal, External: e.External, From: e.From, Until: e.Until, Closed: e.Closed, Calendar: e.Calendar})
		}
	}
	for _, r := range m.Relationships {
		switch r.Stereotype {
		case Placement:
			out.Edges = append(out.Edges, platform.Edge{Structure: r.Kind, Unit: r.Source, Parent: r.Target, Relation: r.Relation, Share: r.Share, From: r.From, Until: r.Until})
		case Membership:
			party := r.Source
			if m.element(party) != nil {
				party = "unit:" + party
			}
			out.Memberships = append(out.Memberships, platform.Membership{Party: party, Unit: r.Target, Role: r.Role, Primary: r.Primary, From: r.From, Until: r.Until})
		}
	}
	return out
}

func relID(prefix string, i int) string { return prefix + "-" + itoa(i) }

func itoa(i int) string {
	raw, _ := json.Marshal(i)
	return string(raw)
}

func cutPrefix(s, p string) (string, bool) {
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return s, false
}

// Slice is what one tenant publishes to another (ADR-0067 D5): the elements
// it marked published, with the relationships among them. The receiving
// tenant imports it read-only, owned by "tenant:<id>", and may relate its own
// elements to them (a subsidiary under the group, a shared capability).
type Slice struct {
	Tenant        string         `json:"tenant"`
	UAF           string         `json:"uaf"`
	Elements      []Element      `json:"elements"`
	Relationships []Relationship `json:"relationships"`
}

// Published is the slice this model offers.
func (m Model) Published(tenant string) Slice {
	out := Slice{Tenant: tenant, UAF: m.UAF, Elements: []Element{}, Relationships: []Relationship{}}
	in := map[string]bool{}
	for _, e := range m.Elements {
		if e.Published && e.Owner == "" {
			e.Owner, e.Published = "tenant:"+tenant, false
			out.Elements = append(out.Elements, e)
			in[e.ID] = true
		}
	}
	for _, r := range m.Relationships {
		if in[r.Source] && in[r.Target] {
			r.ID = "fed:" + tenant + ":" + r.ID
			out.Relationships = append(out.Relationships, r)
		}
	}
	return out
}

// Import replaces what this model mirrors from a tenant with the slice:
// elements and relationships owned there; the tenant's own relationships to
// mirrored elements are kept while their ends still exist.
func (m *Model) Import(s Slice) {
	owner := "tenant:" + s.Tenant
	kept := m.Elements[:0]
	for _, e := range m.Elements {
		if e.Owner != owner {
			kept = append(kept, e)
		}
	}
	m.Elements = append(kept, s.Elements...)
	ids := map[string]bool{}
	for _, e := range m.Elements {
		ids[e.ID] = true
	}
	prefix := "fed:" + s.Tenant + ":"
	rels := m.Relationships[:0]
	for _, r := range m.Relationships {
		if !strings.HasPrefix(r.ID, prefix) && ids[r.Source] && (ids[r.Target] || strings.HasPrefix(r.Source, "member:")) {
			rels = append(rels, r)
		}
	}
	m.Relationships = append(rels, s.Relationships...)
}

// Sync (ADR-0073) lands what an external system holds about the enterprise -
// SAP HR/OM organisational units, AD departments, MES equipment - as a slice
// this model owns on the system's behalf (Owner "source:<name>"). Unlike
// Import it diffs instead of replacing: a new id is added from today, a known
// id is updated in place, an id the system no longer sends is closed today
// (its history stays), and a closed id that returns reopens. Relationships
// come and go the same way, by their ids.
type SyncResult struct {
	Added, Changed, Closed, Reopened, Related, Ended int
}

func (m *Model) Sync(source string, elements []Element, relationships []Relationship, today Date) SyncResult {
	owner, res := "source:"+source, SyncResult{}
	seen := map[string]bool{}
	for _, in := range elements {
		seen[in.ID] = true
		if cur := m.element(in.ID); cur != nil {
			changed := cur.Name != in.Name || cur.ShortName != in.ShortName || cur.Kind != in.Kind || cur.Legal != in.Legal || cur.External != in.External || !sameProperties(cur.Properties, in.Properties)
			if cur.Until != "" && cur.Closed == syncClosed(source) {
				cur.Until, cur.Closed = "", ""
				res.Reopened++
			} else if changed {
				res.Changed++
			}
			cur.Name, cur.ShortName, cur.Kind, cur.Legal, cur.External, cur.Properties = in.Name, in.ShortName, in.Kind, in.Legal, in.External, in.Properties
			if in.Until != "" {
				cur.Until = in.Until
			}
			continue
		}
		in.Owner, in.Published = owner, false
		if in.From == "" {
			in.From = today
		}
		m.Elements = append(m.Elements, in)
		res.Added++
	}
	for i := range m.Elements {
		e := &m.Elements[i]
		if e.Owner == owner && !seen[e.ID] && e.Until == "" {
			e.Until, e.Closed = today, syncClosed(source)
			res.Closed++
		}
	}
	prefix := "sync:" + source + ":"
	seenRel := map[string]bool{}
	for _, in := range relationships {
		if !strings.HasPrefix(in.ID, prefix) {
			in.ID = prefix + in.ID
		}
		seenRel[in.ID] = true
		if cur := m.relationship(in.ID); cur != nil {
			if cur.Until != "" && in.Until == "" {
				cur.Until = ""
				res.Reopened++
			}
			cur.Kind, cur.Role, cur.Relation, cur.Share, cur.Primary = in.Kind, in.Role, in.Relation, in.Share, in.Primary
			if in.Until != "" {
				cur.Until = in.Until
			}
			continue
		}
		if in.From == "" {
			in.From = today
		}
		m.Relationships = append(m.Relationships, in)
		res.Related++
	}
	for i := range m.Relationships {
		r := &m.Relationships[i]
		if strings.HasPrefix(r.ID, prefix) && !seenRel[r.ID] && r.Until == "" {
			r.Until = today
			res.Ended++
		}
	}
	return res
}

func syncClosed(source string) string { return "no longer in " + source }

func sameProperties(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}
