package organization

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"platformserver/internal/enterprise/uaf"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

// The tenant's enterprise model: elements and relationships
// typed by UAF stereotypes, in valid time, plus the views drawn over them.
// What  called units, structures, edges and memberships are the
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
	Performs   = "IsCapableToPerform"         // the older pair for "has a capability"; UAF's own spelling is Exhibits
	Exhibits   = "Exhibits"                   // organisation/resource exhibits a Capability (UAF 1.3)
	System     = "System"                     // a system the organisation runs (ERP, MES, WMS, PLM, CRM)
	Owns       = "ResponsibleFor"             // organisation answers for resource/location/project
	Typed      = "typedBy"                    // instance → its type element (platform, not UAF)
)

// Kind is a way organisations relate ('s Structure): legal,
// management, finance, site, project, governance, community, custom.
type Kind struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Matrix bool   `json:"matrix,omitempty"`
	Owner  string `json:"owner,omitempty"`
}

// Element is one thing in the enterprise, typed by a UAF stereotype.
type Element struct {
	ID         string            `json:"id"`
	Stereotype string            `json:"stereotype"`
	Name       string            `json:"name"`
	ShortName  string            `json:"shortName,omitempty"`
	Kind       string            `json:"kind,omitempty"` // open vocabulary within the stereotype: factory, line, team; plant, bin; machine
	Legal      bool              `json:"legal,omitempty"`
	External   bool              `json:"external,omitempty"`
	From       platform.Date     `json:"from,omitempty"`
	Until      platform.Date     `json:"until,omitempty"`
	Closed     string            `json:"closed,omitempty"`
	Calendar   string            `json:"calendar,omitempty"`
	Owner      string            `json:"owner,omitempty"`     // "tenant:<id>" when federated in from another tenant (read-only here)
	Published  bool              `json:"published,omitempty"` // shared with the tenants this one federates with
	Properties map[string]any    `json:"properties,omitempty"`
	Periods    []platform.Period `json:"periods,omitempty"`
	Deleted    bool              `json:"deleted,omitempty"`
}

// Relationship joins two things; Source and Target are element ids, or for a
// Membership the source may be a party "member:<id>".
type Relationship struct {
	ID         string            `json:"id"`
	Stereotype string            `json:"stereotype"`
	Kind       string            `json:"kind,omitempty"` // for a Placement: the Kind.ID it sits in
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Role       string            `json:"role,omitempty"`     // for a Membership: employee, chair, volunteer …
	Relation   string            `json:"relation,omitempty"` // for a Placement: part of, owned by, reports to …
	Share      float64           `json:"share,omitempty"`
	Primary    bool              `json:"primary,omitempty"`
	From       platform.Date     `json:"from,omitempty"`
	Until      platform.Date     `json:"until,omitempty"`
	Periods    []platform.Period `json:"periods,omitempty"`
}

type TenantLink struct {
	ID           string `json:"id"`
	Organization string `json:"organization"`
	Tenant       string `json:"tenant"`
	Root         string `json:"root"`
	Connection   string `json:"connection"`
	UpdatedAt    string `json:"updatedAt,omitempty"`
	Content      string `json:"content,omitempty"`
}

// View is a drawing over the model: which elements, where, from which viewpoint.
type View struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Viewpoint string                `json:"viewpoint"` // "organization", "data", "function", "output", "control"
	Kind      string                `json:"kind,omitempty"`
	Elements  []string              `json:"elements"`
	Layout    map[string][2]float64 `json:"layout,omitempty"`
	Context   []string              `json:"context,omitempty"`
	Pins      []Pin                 `json:"pins,omitempty"`
	AsOf      platform.Date         `json:"asOf,omitempty"`
}

// Pin is a record drawn on a view beside the element it names:
// the picture then holds the modules that work with the model, not only the
// model. The ref is "record:<entity type>/<id>"; the anchor is the element it
// points at. A pin is a mark on a drawing, not a fact about the model: nothing
// else reads it, and a record it names may be deleted without touching the view.
type Pin struct {
	Ref    string     `json:"ref"`
	Label  string     `json:"label,omitempty"` // how the record was named when it was pinned
	Anchor string     `json:"anchor"`
	At     [2]float64 `json:"at"`
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
	TenantLinks   []TenantLink        `json:"tenantLinks,omitempty"`
}

func (m *Model) ElementOf(id string) *Element {
	if i := slices.IndexFunc(m.Elements, func(e Element) bool { return e.ID == id }); i >= 0 {
		return &m.Elements[i]
	}
	return nil
}

func (m *Model) ViewOf(id string) *View {
	if i := slices.IndexFunc(m.Views, func(v View) bool { return v.ID == id }); i >= 0 {
		return &m.Views[i]
	}
	return nil
}

// RefType and RefID split a pinned record reference, "record:<type>/<id>".
func RefType(ref string) string {
	typ, _, _ := strings.Cut(strings.TrimPrefix(ref, "record:"), "/")
	return typ
}

func RefID(ref string) string {
	_, id, _ := strings.Cut(strings.TrimPrefix(ref, "record:"), "/")
	return id
}

func (m *Model) KindOf(id string) *Kind {
	if i := slices.IndexFunc(m.Kinds, func(k Kind) bool { return k.ID == id }); i >= 0 {
		return &m.Kinds[i]
	}
	return nil
}

func (m *Model) RelationshipOf(id string) *Relationship {
	if i := slices.IndexFunc(m.Relationships, func(r Relationship) bool { return r.ID == id }); i >= 0 {
		return &m.Relationships[i]
	}
	return nil
}

// live says the element exists on day: open, or not yet closed.
func (m Model) LiveOn(id string, day platform.Date) bool {
	e := m.ElementOf(id)
	return e != nil && e.ActiveOn(day)
}

// below reports whether unit sits (transitively) under ancestor in kind on day.
func (m Model) BelowIn(kind, ancestor, unit string, day platform.Date) bool {
	if !m.LiveOn(unit, day) || !m.LiveOn(ancestor, day) {
		return false
	}
	queue, seen := []string{unit}, map[string]bool{}
	for len(queue) > 0 {
		unit = queue[0]
		queue = queue[1:]
		if seen[unit] {
			continue
		}
		seen[unit] = true
		for _, r := range m.Relationships {
			if r.Stereotype == Placement && r.Kind == kind && r.Source == unit && r.ActiveOn(day) && m.LiveOn(r.Target, day) {
				if r.Target == ancestor {
					return true
				}
				queue = append(queue, r.Target)
			}
		}
	}
	return false
}

// units are the organisations party belongs to on day, directly or through a
// member organisation, and every one below them in kind ("" for none below).
func (m Model) UnitsIn(party, kind string, day platform.Date) []string {
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
			if r.Stereotype == Membership && r.Source == p && r.ActiveOn(day) && m.LiveOn(r.Target, day) && add(r.Target) {
				parties = append(parties, r.Target)
			}
		}
	}
	return m.Descendants(out, kind, day)
}

// descendants expands only active elements and placements of the named structure.
func (m Model) Descendants(units []string, kind string, day platform.Date) []string {
	var out []string
	add := func(id string) {
		if m.LiveOn(id, day) && !slices.Contains(out, id) {
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
			if r.Stereotype == Placement && r.Kind == kind && r.Target == out[i] && r.ActiveOn(day) {
				add(r.Source)
			}
		}
	}
	return out
}

// Of are the elements one relationship away from id: targets of stereotype
// going out (out=true) or sources coming in, live on day.
func (m Model) Of(id, stereotype string, out bool, day platform.Date) []string {
	var found []string
	for _, r := range m.Relationships {
		if r.Stereotype != stereotype || !r.ActiveOn(day) {
			continue
		}
		if !m.LiveOn(r.Target, day) || !strings.HasPrefix(r.Source, "member:") && !m.LiveOn(r.Source, day) {
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

// --- The  projection, kept as the seed format and the "organization" read. ---

// FromOrgSeed lifts units, structures, edges and memberships into the model.
func FromOrgSeed(seed platform.OrgSeed) Model {
	m := Model{UAF: "1.3", Kinds: []Kind{}, Elements: []Element{}, Relationships: []Relationship{}, Views: []View{}, Calendars: slices.Clone(seed.Calendars)}
	for _, s := range seed.Structures {
		m.Kinds = append(m.Kinds, Kind{ID: s.ID, Name: s.Name, Kind: s.Kind, Matrix: s.Matrix})
	}
	for _, u := range seed.Units {
		m.Elements = append(m.Elements, Element{ID: u.ID, Stereotype: Organization, Name: u.Name, Kind: u.Kind, Legal: u.Legal, External: u.External, From: u.From, Until: u.Until, Closed: u.Closed, Calendar: u.Calendar, Periods: slices.Clone(u.Periods)})
	}
	for i, e := range seed.Edges {
		m.Relationships = append(m.Relationships, Relationship{ID: RelID("pl", i), Stereotype: Placement, Kind: e.Structure, Source: e.Unit, Target: e.Parent, Relation: e.Relation, Share: e.Share, From: e.From, Until: e.Until, Periods: slices.Clone(e.Periods)})
	}
	for i, ms := range seed.Memberships {
		source := ms.Party
		if id, ok := CutPrefix(ms.Party, "unit:"); ok {
			source = id
		}
		m.Relationships = append(m.Relationships, Relationship{ID: RelID("mb", i), Stereotype: Membership, Source: source, Target: ms.Unit, Role: ms.Role, Primary: ms.Primary, From: ms.From, Until: ms.Until, Periods: slices.Clone(ms.Periods)})
	}
	return m
}

// OrgSeed projects the Personnel corner back into 's shape.
func (m Model) OrgSeed() platform.OrgSeed {
	out := platform.OrgSeed{Calendars: slices.Clone(m.Calendars), Structures: []platform.Structure{}, Units: []platform.Unit{}, Edges: []platform.Edge{}, Memberships: []platform.Membership{}}
	for _, k := range m.Kinds {
		out.Structures = append(out.Structures, platform.Structure{ID: k.ID, Name: k.Name, Kind: k.Kind, Matrix: k.Matrix})
	}
	for _, e := range m.Elements {
		if e.Stereotype == Organization && !e.Deleted {
			out.Units = append(out.Units, platform.Unit{ID: e.ID, Name: e.Name, Kind: e.Kind, Legal: e.Legal, External: e.External, From: e.From, Until: e.Until, Closed: e.Closed, Calendar: e.Calendar, Periods: slices.Clone(e.Periods)})
		}
	}
	for _, r := range m.Relationships {
		switch r.Stereotype {
		case Placement:
			out.Edges = append(out.Edges, platform.Edge{Structure: r.Kind, Unit: r.Source, Parent: r.Target, Relation: r.Relation, Share: r.Share, From: r.From, Until: r.Until, Periods: slices.Clone(r.Periods)})
		case Membership:
			party := r.Source
			if m.ElementOf(party) != nil {
				party = "unit:" + party
			}
			out.Memberships = append(out.Memberships, platform.Membership{Party: party, Unit: r.Target, Role: r.Role, Primary: r.Primary, From: r.From, Until: r.Until, Periods: slices.Clone(r.Periods)})
		}
	}
	return out
}

func RelID(prefix string, i int) string { return prefix + "-" + Itoa(i) }

func Itoa(i int) string {
	raw, _ := json.Marshal(i)
	return string(raw)
}

func CutPrefix(s, p string) (string, bool) {
	if len(s) >= len(p) && s[:len(p)] == p {
		return s[len(p):], true
	}
	return s, false
}

// Slice is what one tenant publishes to another: the elements
// it marked published, with the relationships among them. The receiving
// tenant imports it read-only, owned by "tenant:<id>", and may relate its own
// elements to them (a subsidiary under the group, a shared capability).
type Slice struct {
	Tenant        string         `json:"tenant"`
	UAF           string         `json:"uaf"`
	Elements      []Element      `json:"elements"`
	Relationships []Relationship `json:"relationships"`
	Kinds         []Kind         `json:"kinds,omitempty"`
}

// Published is the slice this model offers.
func (m Model) Published(tenant string) Slice {
	out := Slice{Tenant: tenant, UAF: m.UAF, Elements: []Element{}, Relationships: []Relationship{}}
	in := map[string]bool{}
	for _, e := range m.Elements {
		if e.Published && e.Owner == "" && !e.Deleted {
			e.Owner, e.Published = "tenant:"+tenant, false
			out.Elements = append(out.Elements, e)
			in[e.ID] = true
		}
	}
	for _, r := range m.Relationships {
		if in[r.Source] && in[r.Target] {
			r.ID = "fed:" + tenant + ":" + r.ID
			out.Relationships = append(out.Relationships, r)
			if r.Kind != "" && !slices.ContainsFunc(out.Kinds, func(k Kind) bool { return k.ID == r.Kind }) {
				if kind := m.KindOf(r.Kind); kind != nil {
					out.Kinds = append(out.Kinds, *kind)
				}
			}
		}
	}
	return out
}

// Import replaces what this model mirrors from a tenant with the slice:
// elements and relationships owned there; the tenant's own relationships to
// mirrored elements are kept while their ends still exist.
func (m *Model) Import(s Slice) { m.ImportOn(s, "") }

func (m *Model) ImportOn(s Slice, today platform.Date) {
	owner := "tenant:" + s.Tenant
	if today != "" {
		for _, kind := range s.Kinds {
			kind.Owner = owner
			if old := m.KindOf(kind.ID); old != nil {
				*old = kind
			} else {
				m.Kinds = append(m.Kinds, kind)
			}
		}
		seen := map[string]bool{}
		for _, in := range s.Elements {
			seen[in.ID] = true
			in.Owner, in.Published = owner, false
			if old := m.ElementOf(in.ID); old != nil {
				incomingUntil := in.Until
				if old.Until != "" && old.Closed == SyncClosed(owner) {
					_ = old.ReopenOn(today)
				}
				if len(old.Periods) > 0 {
					in.Periods = slices.Clone(old.Periods)
					in.From, in.Until = old.From, old.Until
					if incomingUntil != "" {
						in.Until = incomingUntil
						in.Periods[len(in.Periods)-1].Until = incomingUntil
					}
				}
				*old = in
			} else {
				m.Elements = append(m.Elements, in)
			}
		}
		for i := range m.Elements {
			el := &m.Elements[i]
			if el.Owner == owner && !seen[el.ID] && el.Until == "" {
				closeSourceElement(el, today, SyncClosed(owner))
			}
		}
		incoming := map[string]bool{}
		prefix := "fed:" + s.Tenant + ":"
		for _, in := range s.Relationships {
			if !strings.HasPrefix(in.ID, prefix) {
				in.ID = prefix + in.ID
			}
			incoming[in.ID] = true
			if old := m.RelationshipOf(in.ID); old != nil {
				if old.Until != "" {
					old.ReopenOn(today)
				}
				old.Kind, old.Role, old.Relation, old.Share, old.Primary = in.Kind, in.Role, in.Relation, in.Share, in.Primary
				if in.Until != "" {
					old.Until = in.Until
					if len(old.Periods) > 0 {
						old.Periods[len(old.Periods)-1].Until = in.Until
					}
				}
			} else {
				m.Relationships = append(m.Relationships, in)
			}
		}
		for i := range m.Relationships {
			r := &m.Relationships[i]
			if strings.HasPrefix(r.ID, prefix) && !incoming[r.ID] && r.Until == "" {
				_ = r.EndOn(today)
			}
		}
		return
	}
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

// Sync lands what an external system holds about the enterprise -
// SAP HR/OM organisational units, AD departments, MES equipment - as a slice
// this model owns on the system's behalf (Owner "source:<name>"). Unlike
// Import it diffs instead of replacing: a new id is added from today, a known
// id is updated in place, an id the system no longer sends is closed today
// (its history stays), and a closed id that returns reopens. Relationships
// come and go the same way, by their ids.
type SyncResult struct {
	Added, Changed, Closed, Reopened, Related, Ended int
}

func (m *Model) Sync(source string, elements []Element, relationships []Relationship, today platform.Date) SyncResult {
	return m.sync(source, elements, relationships, today, false)
}

// ReplaySync preserves already accepted single-window source semantics.
func (m *Model) ReplaySync(source string, elements []Element, relationships []Relationship, today platform.Date) SyncResult {
	return m.sync(source, elements, relationships, today, true)
}

func (m *Model) sync(source string, elements []Element, relationships []Relationship, today platform.Date, replay bool) SyncResult {
	owner, res := "source:"+source, SyncResult{}
	seen := map[string]bool{}
	for _, in := range elements {
		seen[in.ID] = true
		if cur := m.ElementOf(in.ID); cur != nil {
			changed := cur.Name != in.Name || cur.ShortName != in.ShortName || cur.Kind != in.Kind || cur.Legal != in.Legal || cur.External != in.External || !SameProperties(cur.Properties, in.Properties)
			if cur.Until != "" && cur.Closed == SyncClosed(source) {
				if replay {
					cur.Until, cur.Closed = "", ""
				} else {
					_ = cur.ReopenOn(today)
				}
				res.Reopened++
			} else if changed {
				res.Changed++
			}
			cur.Name, cur.ShortName, cur.Kind, cur.Legal, cur.External, cur.Properties = in.Name, in.ShortName, in.Kind, in.Legal, in.External, in.Properties
			if in.Until != "" {
				cur.Until = in.Until
				if len(cur.Periods) > 0 {
					cur.Periods[len(cur.Periods)-1].Until = in.Until
				}
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
			if replay {
				e.Until, e.Closed = today, SyncClosed(source)
			} else {
				closeSourceElement(e, today, SyncClosed(source))
			}
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
		if cur := m.RelationshipOf(in.ID); cur != nil {
			if cur.Until != "" && in.Until == "" {
				if replay {
					cur.Until = ""
				} else {
					cur.ReopenOn(today)
				}
				res.Reopened++
			}
			cur.Kind, cur.Role, cur.Relation, cur.Share, cur.Primary = in.Kind, in.Role, in.Relation, in.Share, in.Primary
			if in.Until != "" {
				cur.Until = in.Until
				if len(cur.Periods) > 0 {
					cur.Periods[len(cur.Periods)-1].Until = in.Until
				}
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
			if replay {
				r.Until = today
			} else {
				_ = r.EndOn(today)
			}
			res.Ended++
		}
	}
	return res
}

func SyncClosed(source string) string { return "no longer in " + source }

func SameProperties(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func periodsOrLegacy(periods []platform.Period, from, until platform.Date) []platform.Period {
	if len(periods) > 0 {
		return periods
	}
	return []platform.Period{{From: from, Until: until}}
}
func periodsActive(periods []platform.Period, from, until, day platform.Date) bool {
	return slices.ContainsFunc(periodsOrLegacy(periods, from, until), func(p platform.Period) bool { return platform.ActiveOn(p.From, p.Until, day) })
}
func (e Element) ActiveOn(day platform.Date) bool {
	return !e.Deleted && periodsActive(e.Periods, e.From, e.Until, day)
}
func (r Relationship) ActiveOn(day platform.Date) bool {
	return periodsActive(r.Periods, r.From, r.Until, day)
}
func (e Element) Covers(from, until platform.Date) bool {
	if e.Deleted {
		return false
	}
	return slices.ContainsFunc(periodsOrLegacy(e.Periods, e.From, e.Until), func(p platform.Period) bool {
		return (p.From == "" || from >= p.From) && (p.Until == "" || until != "" && until <= p.Until)
	})
}
func (r Relationship) PeriodsFromLegacy() []platform.Period {
	return periodsOrLegacy(r.Periods, r.From, r.Until)
}
func (e Element) PeriodsFromLegacy() []platform.Period {
	return periodsOrLegacy(e.Periods, e.From, e.Until)
}
func CheckPeriods(periods []platform.Period) error {
	for i, p := range periods {
		if p.From != "" {
			if _, err := time.Parse(time.DateOnly, p.From); err != nil {
				return fmt.Errorf("Invalid effective period")
			}
		}
		if p.Until != "" {
			if _, err := time.Parse(time.DateOnly, p.Until); err != nil {
				return fmt.Errorf("Invalid effective period")
			}
			if p.Until <= p.From {
				return fmt.Errorf("The effective period must end after it starts")
			}
		}
		if i > 0 && (periods[i-1].Until == "" || p.From < periods[i-1].Until) {
			return fmt.Errorf("Effective periods must not overlap")
		}
	}
	return nil
}
func (e *Element) CloseOn(day platform.Date, reason string) error {
	periods := slices.Clone(e.PeriodsFromLegacy())
	last := &periods[len(periods)-1]
	if day == "" || day <= last.From || last.Until != "" && day > last.Until {
		return fmt.Errorf("The closing date cannot extend the effective period")
	}
	last.Until = day
	if err := CheckPeriods(periods); err != nil {
		return err
	}
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods = periods
	}
	return nil
}
func (e *Element) ReopenOn(day platform.Date) error {
	periods := slices.Clone(e.PeriodsFromLegacy())
	last := periods[len(periods)-1]
	if e.Deleted || last.Until == "" || day == "" || day < last.Until {
		return fmt.Errorf("Reopening starts a new period after the previous one ends")
	}
	periods = append(periods, platform.Period{From: day})
	if err := CheckPeriods(periods); err != nil {
		return err
	}
	e.Periods = periods
	e.Until, e.Closed = "", ""
	return nil
}
func (e *Element) Reinstate() error {
	if e.Deleted || e.Until == "" {
		return fmt.Errorf("Only a closed element can be reinstated")
	}
	e.Until, e.Closed = "", ""
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = ""
	}
	return nil
}
func (r *Relationship) EndOn(day platform.Date) error {
	if _, err := time.Parse(time.DateOnly, day); err != nil {
		return fmt.Errorf("Invalid effective period")
	}
	periods := slices.Clone(r.PeriodsFromLegacy())
	last := &periods[len(periods)-1]
	if day < last.From || last.Until != "" && day > last.Until {
		return fmt.Errorf("Ending a relationship cannot extend its effective period")
	}
	last.Until = day
	r.Until = day
	if len(r.Periods) > 0 {
		r.Periods = periods
	}
	return nil
}
func (r *Relationship) ReopenOn(day platform.Date) {
	periods := slices.Clone(r.PeriodsFromLegacy())
	last := periods[len(periods)-1]
	if last.Until != "" && day >= last.Until {
		r.Periods = append(periods, platform.Period{From: day})
		r.Until = ""
	}
}
func (m *Model) TenantLinkOf(id string) *TenantLink {
	if i := slices.IndexFunc(m.TenantLinks, func(link TenantLink) bool { return link.ID == id }); i >= 0 {
		return &m.TenantLinks[i]
	}
	return nil
}
func fingerprint(value any) string {
	raw, _ := json.Marshal(value)
	return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
}
func (m Model) Fingerprint() string { return fingerprint(m) }
func (s Slice) Fingerprint() string { return fingerprint(s) }

// InternalUsage includes historical declarations; deleting must not erase a named identity.
func (m Model) InternalUsage(id string) string {
	for _, r := range m.Relationships {
		if r.Source == id || r.Target == id {
			return "relationship " + r.ID
		}
	}
	for _, el := range m.Elements {
		if el.ID == id && el.Published {
			return "published element"
		}
	}
	for _, v := range m.Views {
		if slices.Contains(v.Elements, id) || slices.Contains(v.Context, id) {
			return "view " + v.ID
		}
		if _, ok := v.Layout[id]; ok {
			return "view " + v.ID
		}
		for _, pin := range v.Pins {
			if pin.Anchor == id || pin.Ref == "record:enterprise.element/"+id {
				return "view " + v.ID
			}
		}
	}
	if refs := m.ElementPropertyUses(id, ""); len(refs) > 0 {
		return "element " + refs[0]
	}
	for _, link := range m.TenantLinks {
		if link.Organization == id || ImportedElementID(link.Tenant, link.Root) == id {
			return "tenant link " + link.ID
		}
	}
	return ""
}

// ElementPropertyUses follows the metamodel's element-typed tagged values, never free text.
// Empty day includes every retained period; otherwise only use on or after day counts.
func (m Model) ElementPropertyUses(id string, day platform.Date) []string {
	mm := uaf.Current()
	out := []string{}
	for _, element := range m.Elements {
		if element.ID == id || element.Deleted && day != "" {
			continue
		}
		if day != "" && !slices.ContainsFunc(element.PeriodsFromLegacy(), func(period platform.Period) bool { return period.Until == "" || period.Until > day }) {
			continue
		}
		for _, property := range mm.Properties(element.Stereotype) {
			if mm.Stereotypes[property.Type] == nil || mm.Relationship(property.Type) {
				continue
			}
			match := func(value any) bool {
				text, ok := value.(string)
				return ok && (text == id || text == "enterprise.element/"+id)
			}
			value := element.Properties[property.Name]
			found := match(value)
			if property.Many {
				if values, ok := value.([]any); ok {
					found = slices.ContainsFunc(values, match)
				}
				if values, ok := value.([]string); ok {
					found = slices.ContainsFunc(values, func(value string) bool { return match(value) })
				}
			}
			if found {
				out = append(out, element.ID)
				break
			}
		}
	}
	return out
}
func ImportedElementID(tenant, id string) string { return "tenant:" + tenant + ":" + id }

// PreviewImport is the same pure model update used by preview and accepted import.
func (m Model) PreviewImport(s Slice, linkID, content, base string, today platform.Date) (Model, error) {
	if base != m.Fingerprint() {
		return Model{}, fmt.Errorf("The enterprise model changed; preview the import again")
	}
	if content != s.Fingerprint() {
		return Model{}, fmt.Errorf("The shared slice differs from its preview")
	}
	raw, _ := json.Marshal(m)
	var next Model
	json.Unmarshal(raw, &next)
	link := next.TenantLinkOf(linkID)
	if link == nil || link.Tenant != s.Tenant {
		return Model{}, fmt.Errorf("The tenant link does not match the shared slice")
	}
	if s.UAF != "" && s.UAF != m.UAF {
		return Model{}, fmt.Errorf("The shared slice uses a different UAF version")
	}
	root := slices.IndexFunc(s.Elements, func(e Element) bool {
		return e.ID == link.Root && e.Stereotype == Organization && !e.Deleted && e.ActiveOn(today)
	})
	if root < 0 {
		return Model{}, fmt.Errorf("The tenant link root is not a shared active organisation")
	}
	s, err := next.NamespaceSlice(s)
	if err != nil {
		return Model{}, err
	}
	next.ImportOn(s, today)
	link = next.TenantLinkOf(linkID)
	link.Content, link.UpdatedAt = content, today
	return next, nil
}

func (m Model) NamespaceSlice(s Slice) (Slice, error) {
	raw, _ := json.Marshal(s)
	var cloned Slice
	json.Unmarshal(raw, &cloned)
	s = cloned
	mm := uaf.Current()
	kinds := map[string]string{}
	for i := range s.Kinds {
		kind := &s.Kinds[i]
		if kind.ID == "" || kind.Name == "" || kind.Kind == "" || kinds[kind.ID] != "" {
			return Slice{}, fmt.Errorf("The shared slice contains an invalid relationship kind")
		}
		id := ImportedElementID(s.Tenant, kind.ID)
		legacy := false
		if old := m.KindOf(kind.ID); old != nil && old.Owner == "" && slices.ContainsFunc(m.Relationships, func(r Relationship) bool { return strings.HasPrefix(r.ID, "fed:"+s.Tenant+":") && r.Kind == kind.ID }) {
			id = kind.ID
			legacy = true
		}
		if old := m.KindOf(id); old != nil && !legacy && old.Owner != "tenant:"+s.Tenant {
			return Slice{}, fmt.Errorf("The relationship kind identity is already reserved")
		}
		kinds[kind.ID] = id
		kind.ID, kind.Owner = id, "tenant:"+s.Tenant
	}
	ids := map[string]string{}
	for _, e := range s.Elements {
		if e.ID == "" || e.Name == "" || e.Deleted {
			return Slice{}, fmt.Errorf("The shared slice contains an invalid element")
		}
		if st := mm.Stereotypes[e.Stereotype]; st == nil || st.Abstract || mm.Relationship(e.Stereotype) {
			return Slice{}, fmt.Errorf("Choose a concrete enterprise element type")
		}
		props := mm.Properties(e.Stereotype)
		for name, value := range e.Properties {
			index := slices.IndexFunc(props, func(p uaf.Property) bool { return p.Name == name })
			if index < 0 {
				return Slice{}, fmt.Errorf("The shared element contains an unknown tagged value")
			}
			if enumeration := mm.Enumerations[props[index].Type]; enumeration != nil {
				if text, ok := value.(string); !ok || !slices.Contains(enumeration.Literals, text) {
					return Slice{}, fmt.Errorf("The shared element contains an invalid tagged value")
				}
			}
		}
		if err := CheckPeriods(e.PeriodsFromLegacy()); err != nil {
			return Slice{}, err
		}
		id := ImportedElementID(s.Tenant, e.ID)
		if previous := m.ElementOf(e.ID); previous != nil && previous.Owner == "tenant:"+s.Tenant {
			id = e.ID
		}
		if previous := m.ElementOf(id); previous != nil && (previous.Owner != "tenant:"+s.Tenant || previous.Deleted) {
			return Slice{}, fmt.Errorf("The shared element identity is already reserved")
		}
		if previous := m.ElementOf(id); previous != nil && previous.Stereotype != e.Stereotype {
			return Slice{}, fmt.Errorf("A source element keeps its identity and type")
		}
		if ids[e.ID] != "" {
			return Slice{}, fmt.Errorf("The shared slice repeats an element")
		}
		ids[e.ID] = id
	}
	for i := range s.Elements {
		s.Elements[i].ID = ids[s.Elements[i].ID]
		for _, property := range mm.Properties(s.Elements[i].Stereotype) {
			if mm.Stereotypes[property.Type] == nil || mm.Relationship(property.Type) {
				continue
			}
			translate := func(value any) any {
				if ref, ok := value.(string); ok {
					if mapped := ids[ref]; mapped != "" {
						return mapped
					}
					if mapped := ids[strings.TrimPrefix(ref, "enterprise.element/")]; strings.HasPrefix(ref, "enterprise.element/") && mapped != "" {
						return "enterprise.element/" + mapped
					}
				}
				return value
			}
			value := s.Elements[i].Properties[property.Name]
			if property.Many {
				if values, ok := value.([]any); ok {
					for j := range values {
						values[j] = translate(values[j])
					}
				}
			} else if value != nil {
				s.Elements[i].Properties[property.Name] = translate(value)
			}
		}
	}
	relIDs := map[string]bool{}
	for i := range s.Relationships {
		r := &s.Relationships[i]
		if ref := kinds[r.Kind]; ref != "" {
			r.Kind = ref
		}
		if r.ID == "" || relIDs[r.ID] {
			return Slice{}, fmt.Errorf("The shared slice repeats a relationship")
		}
		relIDs[r.ID] = true
		if err := CheckPeriods(r.PeriodsFromLegacy()); err != nil {
			return Slice{}, err
		}
		r.Source, r.Target = ids[r.Source], ids[r.Target]
		if r.Source == "" || r.Target == "" {
			return Slice{}, fmt.Errorf("The shared relationship has an unavailable endpoint")
		}
		if !strings.HasPrefix(r.ID, "fed:"+s.Tenant+":") {
			r.ID = "fed:" + s.Tenant + ":" + r.ID
		}
		if old := m.RelationshipOf(r.ID); old != nil && (old.Source != r.Source || old.Target != r.Target || old.Stereotype != r.Stereotype) {
			return Slice{}, fmt.Errorf("A source relationship keeps its endpoints; use a new relationship ID")
		}
	}
	return s, nil
}

func closeSourceElement(e *Element, day platform.Date, reason string) {
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = day
	}
}

type Date = platform.Date
type Period = platform.Period
