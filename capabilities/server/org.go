package platformserver

import (
	"encoding/json"
	"slices"
	"strings"
	"sync"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
	"platformserver/platform"
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

func activeOn(from, until platform.Date, day platform.Date) bool {
	return from <= day && (until == "" || day < until)
}

type Organization struct {
	mu     sync.Mutex
	chart  platform.OrgSeed
	ledger *platform.Ledger
}

func NewOrganization(tenant string, seed platform.OrgSeed) *Organization {
	admin := []string{OrgAdmin}
	f := func(name, typ, description string, required bool) platform.Field {
		return platform.Field{Name: name, Type: typ, Required: required, Description: description}
	}
	from, until := f("from", "date", "Valid from (YYYY-MM-DD; empty: today)", false), f("until", "date", "Valid until, exclusive (empty: open)", false)
	catalog := platform.NewCatalog(
		platform.Action{Schema: SchemaUnitAdd, Target: UnitType, Capability: "units", Title: "Add unit", Roles: admin,
			Description: "Add a unit of any kind: a company, factory, line, team, project, committee, or an external organisation.",
			Payload: []platform.Field{f("name", "string", "Name", true), f("kind", "string", "Kind, e.g. subsidiary, factory, committee, partner", true),
				f("legal", "boolean", "A legal entity", false), f("external", "boolean", "Outside the tenant's own organisation", false), from, until}},
		platform.Action{Schema: SchemaUnitClose, Target: UnitType, Capability: "units", Title: "Close unit", Roles: admin,
			Description: "End a unit (dissolved, merged into another); its history stays.",
			Payload:     []platform.Field{f("until", "date", "Last day + 1", true), f("reason", "string", "e.g. merged into <unit>", false)}},
		platform.Action{Schema: SchemaStructure, Target: StructureType, Capability: "structures", Title: "Add structure", Roles: admin,
			Description: "Add a way units relate: legal, management, finance, site, project, governance, community or custom.",
			Payload:     []platform.Field{f("name", "string", "Name", true), f("kind", "string", "Kind", true), f("matrix", "boolean", "Units may have several parents", false)}},
		platform.Action{Schema: SchemaPlace, Target: UnitType, Capability: "structures", Title: "Place unit", Roles: admin,
			Description: "Put the unit under a parent in a structure; in a tree this ends its previous parent there.",
			Payload: []platform.Field{f("structure", "string", "Structure ID", true), f("parent", "string", "Parent unit ID", true),
				f("relation", "string", "e.g. part of, owned by, reports to", false), f("share", "number", "Ownership share", false), from}},
		platform.Action{Schema: SchemaUnplace, Target: UnitType, Capability: "structures", Title: "Remove from structure", Roles: admin,
			Description: "End the unit's edge to a parent in a structure.",
			Payload:     []platform.Field{f("structure", "string", "Structure ID", true), f("parent", "string", "Parent unit ID", true), until}},
		platform.Action{Schema: SchemaJoin, Target: UnitType, Capability: "memberships", Title: "Add membership", Roles: admin,
			Description: "Make a member (member:<id>) or another unit (unit:<id>) a member of this unit, with a role and a period.",
			Payload: []platform.Field{f("party", "string", "member:<id> or unit:<id>", true), f("role", "string", "e.g. employee, chair, volunteer", true),
				f("primary", "boolean", "The party's primary unit", false), from, until}},
		platform.Action{Schema: SchemaLeave, Target: UnitType, Capability: "memberships", Title: "End membership", Roles: admin,
			Description: "End a party's membership of this unit in a role.",
			Payload:     []platform.Field{f("party", "string", "member:<id> or unit:<id>", true), f("role", "string", "Role", true), until}},
	)
	var chart platform.OrgSeed // a copy: decisions change it, the seed stays as given
	raw, _ := json.Marshal(seed)
	json.Unmarshal(raw, &chart)
	return &Organization{chart: chart, ledger: platform.NewLedger(tenant, OrgApp, catalog, UnitType, StructureType)}
}

// Snapshot and Restore: the chart as decisions left it (ADR-0019 D6).
func (o *Organization) Snapshot() (json.RawMessage, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ledger.SnapshotWith(o.chart)
}

func (o *Organization) Restore(raw json.RawMessage) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.chart = platform.OrgSeed{}
	return o.ledger.RestoreWith(raw, &o.chart)
}

func (o *Organization) Manifest() platform.Manifest {
	return platform.Manifest{ID: OrgApp, Title: "Organisation", Version: "1", Actions: o.ledger.Catalog, Reads: []string{"organization"}}
}

func (o *Organization) Declarations() []*pb.AuthorityDeclaration { return o.ledger.Declarations() }

func (o *Organization) Input(platform.Caller, string, []byte, time.Time) (any, *kernel.Error) {
	return nil, &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_UNKNOWN_SCHEMA}
}

func (o *Organization) unit(id string) *platform.Unit {
	i := slices.IndexFunc(o.chart.Units, func(u platform.Unit) bool { return u.ID == id })
	if i < 0 {
		return nil
	}
	return &o.chart.Units[i]
}

func (o *Organization) structure(id string) *platform.Structure {
	i := slices.IndexFunc(o.chart.Structures, func(s platform.Structure) bool { return s.ID == id })
	if i < 0 {
		return nil
	}
	return &o.chart.Structures[i]
}

func (o *Organization) Submit(c platform.Caller, s *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.ledger.Receive(c, s, now, nil, func() (func(*pb.ChangeRecord), *kernel.Error) {
		invalid := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_INVALID_ARGUMENT}
		notFound := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_NOT_FOUND}
		conflict := &kernel.Error{Code: pb.ErrorCode_ERROR_CODE_CONFLICT}
		var p struct {
			Name, Kind, Reason, Structure, Parent, Relation, Party, Role string
			Legal, External, Matrix, Primary                             bool
			Share                                                        float64
			From, Until                                                  platform.Date
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
				o.chart.Units = append(o.chart.Units, platform.Unit{ID: id, Name: p.Name, Kind: p.Kind, Legal: p.Legal, External: p.External, From: p.From, Until: p.Until})
			}, nil
		case SchemaStructure:
			if p.Name == "" || p.Kind == "" {
				return nil, invalid
			}
			if o.structure(id) != nil {
				return nil, conflict
			}
			return func(*pb.ChangeRecord) {
				o.chart.Structures = append(o.chart.Structures, platform.Structure{ID: id, Name: p.Name, Kind: p.Kind, Matrix: p.Matrix})
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
				o.chart.Edges = append(o.chart.Edges, platform.Edge{Structure: p.Structure, Unit: id, Parent: p.Parent, Relation: p.Relation, Share: p.Share, From: p.From})
			}, nil
		case SchemaUnplace, SchemaLeave:
			day := p.Until
			if day == "" {
				day = now.UTC().Format(time.DateOnly)
			}
			if s.GetSchema().GetName() == SchemaUnplace {
				i := slices.IndexFunc(o.chart.Edges, func(e platform.Edge) bool {
					return e.Structure == p.Structure && e.Unit == id && e.Parent == p.Parent && activeOn(e.From, e.Until, day)
				})
				if i < 0 {
					return nil, notFound
				}
				return func(*pb.ChangeRecord) { o.chart.Edges[i].Until = day }, nil
			}
			i := slices.IndexFunc(o.chart.Memberships, func(m platform.Membership) bool {
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
			o.chart.Memberships = append(o.chart.Memberships, platform.Membership{Party: p.Party, Unit: id, Role: p.Role, Primary: p.Primary, From: p.From, Until: p.Until})
		}, nil
	})
}

// below reports whether unit sits (transitively) under ancestor in structure on day.
func (o *Organization) below(structure, ancestor, unit string, day platform.Date) bool {
	for seen := map[string]bool{}; unit != "" && !seen[unit]; {
		seen[unit] = true
		i := slices.IndexFunc(o.chart.Edges, func(e platform.Edge) bool {
			return e.Structure == structure && e.Unit == unit && activeOn(e.From, e.Until, day)
		})
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
func (o *Organization) units(party, structure string, day platform.Date) []string {
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
	if structure == "" { // the units themselves, none below (a record scope of level unit)
		return out
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

// holders are the members holding a membership (with role, when given) in unit
// or in a unit above it in structure on day: who answers for the unit.
func (o *Organization) holders(structure, unit, role string, day platform.Date) []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	var out []string
	for _, m := range o.chart.Memberships {
		id, person := strings.CutPrefix(m.Party, "member:")
		u := o.unit(m.Unit)
		if !person || role != "" && m.Role != role || !activeOn(m.From, m.Until, day) || u == nil || !activeOn(u.From, u.Until, day) || slices.Contains(out, id) {
			continue
		}
		if m.Unit == unit || o.below(structure, m.Unit, unit, day) {
			out = append(out, id)
		}
	}
	return out
}

// unitsOf are c's member's units in structure on now's day (Caller.Units).
func (t *Tenant) unitsOf(c platform.Caller, structure string, now time.Time) []string {
	if t.org == nil {
		return nil
	}
	o := t.org
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.units("member:"+c.ID, structure, now.UTC().Format(time.DateOnly))
}

// Read "organization": the whole chart, for members holding a role in the org app.
func (o *Organization) Read(platform.Caller, string) (any, *kernel.Error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	raw, _ := json.Marshal(o.chart)
	out := platform.OrgSeed{Structures: []platform.Structure{}, Units: []platform.Unit{}, Edges: []platform.Edge{}, Memberships: []platform.Membership{}}
	json.Unmarshal(raw, &out) // a deep copy, with empty lists rather than null
	return out, nil
}
