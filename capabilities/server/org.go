package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

// Organization is the platform's organisation capability (ADR-0012): units of
// any kind, in any number of structures (legal, management, finance, site,
// project, governance, community …), with memberships of members or of other
// units, all with valid time. Apps ask for a member's units in a named structure
// (Caller.Units); they never read the chart. Changes are the org app's decisions.
const (
	OrgApp          = "org"
	UnitType        = "org.unit"
	StructureType   = "org.structure"
	SchemaUnitAdd   = "org.unit.add"
	SchemaUnitClose = "org.unit.close"
	SchemaStructure = "org.structure.add"
	SchemaPlace     = "org.unit.place"   // put a unit under a parent in a structure
	SchemaUnplace   = "org.unit.unplace" // end that edge
	SchemaJoin      = "org.membership.add"
	SchemaLeave     = "org.membership.end"
	OrgAdmin        = "admin"
)

// Date is a calendar day "YYYY-MM-DD"; "" means unbounded.
type Date = string

func activeOn(from, until Date, day Date) bool { return from <= day && (until == "" || day < until) }

type Unit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // open vocabulary: group, subsidiary, factory, line, committee, partner …
	Legal    bool   `json:"legal,omitempty"`
	External bool   `json:"external,omitempty"`
	From     Date   `json:"from,omitempty"`
	Until    Date   `json:"until,omitempty"`
	Closed   string `json:"closed,omitempty"` // why it ended: dissolved, merged into <unit> …
}

type Structure struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`             // legal, management, finance, site, project, governance, community, custom
	Matrix bool   `json:"matrix,omitempty"` // a unit may have several parents at once
}

type Edge struct {
	Structure string  `json:"structure"`
	Unit      string  `json:"unit"`
	Parent    string  `json:"parent"`
	Relation  string  `json:"relation,omitempty"` // part of, owned by, reports to, located at …
	Share     float64 `json:"share,omitempty"`    // ownership share in a legal structure
	From      Date    `json:"from,omitempty"`
	Until     Date    `json:"until,omitempty"`
}

type Membership struct {
	Party   string `json:"party"` // "member:<id>" or "unit:<id>"
	Unit    string `json:"unit"`
	Role    string `json:"role"` // open vocabulary: employee, volunteer, maintainer, chair, delegate …
	Primary bool   `json:"primary,omitempty"`
	From    Date   `json:"from,omitempty"`
	Until   Date   `json:"until,omitempty"`
}

// OrgSeed is an organisation's starting shape (an industry package's, or a deployment's).
type OrgSeed struct {
	Structures  []Structure  `json:"structures"`
	Units       []Unit       `json:"units"`
	Edges       []Edge       `json:"edges"`
	Memberships []Membership `json:"memberships"`
}

type Organization struct {
	mu     sync.Mutex
	chart  OrgSeed
	ledger *Ledger
}

func NewOrganization(tenant string, seed OrgSeed) *Organization {
	admin := []string{OrgAdmin}
	f := func(name, typ, description string, required bool) Field {
		return Field{Name: name, Type: typ, Required: required, Description: description}
	}
	from, until := f("from", "date", "Valid from (YYYY-MM-DD; empty: today)", false), f("until", "date", "Valid until, exclusive (empty: open)", false)
	catalog := NewCatalog(
		Action{Schema: SchemaUnitAdd, Target: UnitType, Capability: "units", Title: "Add unit", Roles: admin,
			Description: "Add a unit of any kind: a company, factory, line, team, project, committee, or an external organisation.",
			Payload: []Field{f("name", "string", "Name", true), f("kind", "string", "Kind, e.g. subsidiary, factory, committee, partner", true),
				f("legal", "boolean", "A legal entity", false), f("external", "boolean", "Outside the tenant's own organisation", false), from, until}},
		Action{Schema: SchemaUnitClose, Target: UnitType, Capability: "units", Title: "Close unit", Roles: admin,
			Description: "End a unit (dissolved, merged into another); its history stays.",
			Payload:     []Field{f("until", "date", "Last day + 1", true), f("reason", "string", "e.g. merged into <unit>", false)}},
		Action{Schema: SchemaStructure, Target: StructureType, Capability: "structures", Title: "Add structure", Roles: admin,
			Description: "Add a way units relate: legal, management, finance, site, project, governance, community or custom.",
			Payload:     []Field{f("name", "string", "Name", true), f("kind", "string", "Kind", true), f("matrix", "boolean", "Units may have several parents", false)}},
		Action{Schema: SchemaPlace, Target: UnitType, Capability: "structures", Title: "Place unit", Roles: admin,
			Description: "Put the unit under a parent in a structure; in a tree this ends its previous parent there.",
			Payload: []Field{f("structure", "string", "Structure ID", true), f("parent", "string", "Parent unit ID", true),
				f("relation", "string", "e.g. part of, owned by, reports to", false), f("share", "number", "Ownership share", false), from}},
		Action{Schema: SchemaUnplace, Target: UnitType, Capability: "structures", Title: "Remove from structure", Roles: admin,
			Description: "End the unit's edge to a parent in a structure.",
			Payload:     []Field{f("structure", "string", "Structure ID", true), f("parent", "string", "Parent unit ID", true), until}},
		Action{Schema: SchemaJoin, Target: UnitType, Capability: "memberships", Title: "Add membership", Roles: admin,
			Description: "Make a member (member:<id>) or another unit (unit:<id>) a member of this unit, with a role and a period.",
			Payload: []Field{f("party", "string", "member:<id> or unit:<id>", true), f("role", "string", "e.g. employee, chair, volunteer", true),
				f("primary", "boolean", "The party's primary unit", false), from, until}},
		Action{Schema: SchemaLeave, Target: UnitType, Capability: "memberships", Title: "End membership", Roles: admin,
			Description: "End a party's membership of this unit in a role.",
			Payload:     []Field{f("party", "string", "member:<id> or unit:<id>", true), f("role", "string", "Role", true), until}},
	)
	var chart OrgSeed // a copy: decisions change it, the seed stays as given
	raw, _ := json.Marshal(seed)
	json.Unmarshal(raw, &chart)
	return &Organization{chart: chart, ledger: NewLedger(tenant, OrgApp, catalog, UnitType, StructureType)}
}

func (o *Organization) Manifest() Manifest {
	return Manifest{ID: OrgApp, Version: "1", Actions: o.ledger.Catalog, Reads: []string{"organization"}}
}

func (o *Organization) Declarations() []*pb.AuthorityDeclaration { return o.ledger.Declarations() }

func (o *Organization) Input(Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (o *Organization) unit(id string) *Unit {
	i := slices.IndexFunc(o.chart.Units, func(u Unit) bool { return u.ID == id })
	if i < 0 {
		return nil
	}
	return &o.chart.Units[i]
}

func (o *Organization) structure(id string) *Structure {
	i := slices.IndexFunc(o.chart.Structures, func(s Structure) bool { return s.ID == id })
	if i < 0 {
		return nil
	}
	return &o.chart.Structures[i]
}

func (o *Organization) Submit(c Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		var p struct {
			Name, Kind, Reason, Structure, Parent, Relation, Party, Role string
			Legal, External, Matrix, Primary                            bool
			Share                                                       float64
			From, Until                                                 Date
		}
		if json.Unmarshal(s.GetPayload(), &p) != nil {
			return nil, invalid
		}
		if p.From == "" {
			p.From = now.UTC().Format(time.DateOnly)
		}
		id := s.GetTarget().GetId()
		switch s.GetSchema().GetName() {
		case SchemaUnitAdd:
			if p.Name == "" || p.Kind == "" || p.Until != "" && p.Until <= p.From {
				return nil, invalid
			}
			if o.unit(id) != nil {
				return nil, conflict
			}
			return func(*pb.ChangeRecord) {
				o.chart.Units = append(o.chart.Units, Unit{ID: id, Name: p.Name, Kind: p.Kind, Legal: p.Legal, External: p.External, From: p.From, Until: p.Until})
			}, nil
		case SchemaStructure:
			if p.Name == "" || p.Kind == "" {
				return nil, invalid
			}
			if o.structure(id) != nil {
				return nil, conflict
			}
			return func(*pb.ChangeRecord) {
				o.chart.Structures = append(o.chart.Structures, Structure{ID: id, Name: p.Name, Kind: p.Kind, Matrix: p.Matrix})
			}, nil
		}
		u := o.unit(id)
		if u == nil {
			return nil, notFound
		}
		switch s.GetSchema().GetName() {
		case SchemaUnitClose:
			if p.Until == "" || p.Until <= u.From {
				return nil, invalid
			}
			return func(*pb.ChangeRecord) { u.Until, u.Closed = p.Until, p.Reason }, nil
		case SchemaPlace:
			st, parent := o.structure(p.Structure), o.unit(p.Parent)
			if st == nil || parent == nil {
				return nil, notFound
			}
			if p.Parent == id || o.below(p.Structure, id, p.Parent, p.From) {
				return nil, invalid // a unit cannot sit under itself
			}
			return func(*pb.ChangeRecord) {
				for i, e := range o.chart.Edges { // a tree keeps one current parent
					if e.Structure == p.Structure && e.Unit == id && (!st.Matrix || e.Parent == p.Parent) && activeOn(e.From, e.Until, p.From) {
						o.chart.Edges[i].Until = p.From
					}
				}
				o.chart.Edges = append(o.chart.Edges, Edge{Structure: p.Structure, Unit: id, Parent: p.Parent, Relation: p.Relation, Share: p.Share, From: p.From})
			}, nil
		case SchemaUnplace, SchemaLeave:
			day := p.Until
			if day == "" {
				day = now.UTC().Format(time.DateOnly)
			}
			if s.GetSchema().GetName() == SchemaUnplace {
				i := slices.IndexFunc(o.chart.Edges, func(e Edge) bool {
					return e.Structure == p.Structure && e.Unit == id && e.Parent == p.Parent && activeOn(e.From, e.Until, day)
				})
				if i < 0 {
					return nil, notFound
				}
				return func(*pb.ChangeRecord) { o.chart.Edges[i].Until = day }, nil
			}
			i := slices.IndexFunc(o.chart.Memberships, func(m Membership) bool {
				return m.Party == p.Party && m.Unit == id && m.Role == p.Role && activeOn(m.From, m.Until, day)
			})
			if i < 0 {
				return nil, notFound
			}
			return func(*pb.ChangeRecord) { o.chart.Memberships[i].Until = day }, nil
		}
		// SchemaJoin
		if !strings.HasPrefix(p.Party, "member:") && !strings.HasPrefix(p.Party, "unit:") || p.Role == "" {
			return nil, invalid
		}
		if strings.HasPrefix(p.Party, "unit:") && o.unit(strings.TrimPrefix(p.Party, "unit:")) == nil {
			return nil, notFound
		}
		return func(*pb.ChangeRecord) {
			o.chart.Memberships = append(o.chart.Memberships, Membership{Party: p.Party, Unit: id, Role: p.Role, Primary: p.Primary, From: p.From, Until: p.Until})
		}, nil
	})
}

// below reports whether unit sits (transitively) under ancestor in structure on day.
func (o *Organization) below(structure, ancestor, unit string, day Date) bool {
	for seen := map[string]bool{}; unit != "" && !seen[unit]; {
		seen[unit] = true
		i := slices.IndexFunc(o.chart.Edges, func(e Edge) bool { return e.Structure == structure && e.Unit == unit && activeOn(e.From, e.Until, day) })
		if i < 0 {
			return false
		}
		unit = o.chart.Edges[i].Parent
		if unit == ancestor {
			return true
		}
	}
	return false
}

// units are the units party belongs to on day, directly or as a member of a
// member unit, and every unit below them in structure; closed units count for
// nothing after they close.
func (o *Organization) units(party, structure string, day Date) []string {
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
		for _, m := range o.chart.Memberships {
			if u := o.unit(m.Unit); m.Party == p && activeOn(m.From, m.Until, day) && u != nil && activeOn(u.From, u.Until, day) && add(m.Unit) {
				parties = append(parties, "unit:"+m.Unit)
			}
		}
	}
	for i := 0; i < len(out); i++ { // descend: out grows while it is walked
		for _, e := range o.chart.Edges {
			if u := o.unit(e.Unit); e.Structure == structure && e.Parent == out[i] && activeOn(e.From, e.Until, day) && u != nil && activeOn(u.From, u.Until, day) {
				add(e.Unit)
			}
		}
	}
	return out
}

// Units are the units c's member belongs to in structure today, with all units
// below them: the scope a rule reads from a named structure (ADR-0012).
func (c Caller) Units(structure string) []string {
	if c.tenant == nil || c.tenant.org == nil {
		return nil
	}
	o := c.tenant.org
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.units("member:"+c.ID, structure, time.Now().UTC().Format(time.DateOnly))
}

// Read "organization": the whole chart, for members holding a role in the org app.
func (o *Organization) Read(Caller, string) (any, *kernel.Error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	raw, _ := json.Marshal(o.chart)
	out := OrgSeed{Structures: []Structure{}, Units: []Unit{}, Edges: []Edge{}, Memberships: []Membership{}}
	json.Unmarshal(raw, &out) // a deep copy, with empty lists rather than null
	return out, nil
}
