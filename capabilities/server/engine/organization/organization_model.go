package organization

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
	"time"
)

const (
	Organization = "ActualOrganization"
	Person       = "ActualPerson"
	Post         = "ActualPost"
	Location     = "ActualLocation"
	Resource     = "ActualResource"
	Project      = "ActualProject"
	Capability   = "Capability"
	Goal         = "EnterpriseGoal"
	Placement    = "ActualResourceRelationship"
	Membership   = "ActualOrganizationRole"
	FillsPost    = "FillsPost"
	Performs     = "IsCapableToPerform"
	Exhibits     = "Exhibits"
	System       = "System"
	Owns         = "ResponsibleFor"
	Typed        = "typedBy"
)

type Kind struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`
	Matrix bool   `json:"matrix,omitempty"`
	Owner  string `json:"owner,omitempty"`
}

type Element struct {
	ID         string            `json:"id"`
	Stereotype string            `json:"stereotype"`
	Name       string            `json:"name"`
	ShortName  string            `json:"shortName,omitempty"`
	Kind       string            `json:"kind,omitempty"`
	Legal      bool              `json:"legal,omitempty"`
	External   bool              `json:"external,omitempty"`
	From       platform.Date     `json:"from,omitempty"`
	Until      platform.Date     `json:"until,omitempty"`
	Closed     string            `json:"closed,omitempty"`
	Calendar   string            `json:"calendar,omitempty"`
	Owner      string            `json:"owner,omitempty"`
	Published  bool              `json:"published,omitempty"`
	Properties map[string]any    `json:"properties,omitempty"`
	Periods    []platform.Period `json:"periods,omitempty"`
	Deleted    bool              `json:"deleted,omitempty"`
}

type Relationship struct {
	ID         string            `json:"id"`
	Stereotype string            `json:"stereotype"`
	Kind       string            `json:"kind,omitempty"`
	Source     string            `json:"source"`
	Target     string            `json:"target"`
	Role       string            `json:"role,omitempty"`
	Relation   string            `json:"relation,omitempty"`
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

type View struct {
	ID        string                `json:"id"`
	Name      string                `json:"name"`
	Viewpoint string                `json:"viewpoint"`
	Kind      string                `json:"kind,omitempty"`
	Elements  []string              `json:"elements"`
	Layout    map[string][2]float64 `json:"layout,omitempty"`
	Context   []string              `json:"context,omitempty"`
	Pins      []Pin                 `json:"pins,omitempty"`
	AsOf      platform.Date         `json:"asOf,omitempty"`
}

type Pin struct {
	Ref    string     `json:"ref"`
	Label  string     `json:"label,omitempty"`
	Anchor string     `json:"anchor"`
	At     [2]float64 `json:"at"`
}

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

func (m Model) LiveOn(id string, day platform.Date) bool {
	e := m.ElementOf(id)
	return e != nil && e.ActiveOn(day)
}

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

type Slice struct {
	Tenant        string         `json:"tenant"`
	UAF           string         `json:"uaf"`
	Elements      []Element      `json:"elements"`
	Relationships []Relationship `json:"relationships"`
	Kinds         []Kind         `json:"kinds,omitempty"`
}

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

type SyncResult struct {
	Added, Changed, Closed, Reopened, Related, Ended int
}

func (m *Model) Sync(source string, elements []Element, relationships []Relationship, today platform.Date) SyncResult {
	return m.sync(source, elements, relationships, today, false)
}

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

func (m Model) ElementPropertyUses(id string, day platform.Date) []string {
	out := []string{}
	for _, element := range m.Elements {
		if element.ID == id || element.Deleted && day != "" {
			continue
		}
		if day != "" && !slices.ContainsFunc(element.PeriodsFromLegacy(), func(period platform.Period) bool { return period.Until == "" || period.Until > day }) {
			continue
		}
		for _, property := range historicalReferences[element.Stereotype] {
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
	return Slice{}, fmt.Errorf("Full enterprise imports are retired; use the retained original binary for historical input recovery")
}

func closeSourceElement(e *Element, day platform.Date, reason string) {
	e.Until, e.Closed = day, reason
	if len(e.Periods) > 0 {
		e.Periods[len(e.Periods)-1].Until = day
	}
}

type Date = platform.Date
type Period = platform.Period

type DirectoryModel struct {
	Version      string                           `json:"version"`
	URI          string                           `json:"uri"`
	Domains      []string                         `json:"domains"`
	Stereotypes  map[string]*DirectoryType        `json:"stereotypes"`
	Enumerations map[string]*DirectoryEnumeration `json:"enumerations"`
}
type DirectoryType struct {
	Name        string              `json:"name"`
	ID          string              `json:"id"`
	Domain      string              `json:"domain"`
	Abstract    bool                `json:"abstract,omitempty"`
	Generals    []string            `json:"generals,omitempty"`
	Bases       []string            `json:"bases,omitempty"`
	Properties  []DirectoryProperty `json:"properties,omitempty"`
	Client      []string            `json:"client,omitempty"`
	Supplier    []string            `json:"supplier,omitempty"`
	Constraints []string            `json:"constraints,omitempty"`
}
type DirectoryProperty struct {
	Name string `json:"name"`
	Type string `json:"type"`
	Many bool   `json:"many,omitempty"`
}
type DirectoryEnumeration struct {
	Name     string   `json:"name"`
	Literals []string `json:"literals"`
}

func directoryElement(name string) bool {
	return name == Organization || name == Post || name == Person
}
func directoryRelationship(name string) bool {
	return name == Placement || name == Membership || name == FillsPost || name == Owns
}
func directory() *DirectoryModel {
	types := map[string]*DirectoryType{}
	for _, name := range []string{Organization, Post, Person, Placement, Membership, FillsPost, Owns} {
		types[name] = &DirectoryType{Name: name, ID: name, Domain: "Organisation"}
	}
	return &DirectoryModel{Version: "directory-1", URI: "platform:directory:1", Domains: []string{"Organisation"}, Stereotypes: types, Enumerations: map[string]*DirectoryEnumeration{}}
}
func (m *DirectoryModel) Is(name, parent string) bool   { return name == parent }
func (m *DirectoryModel) Relationship(name string) bool { return directoryRelationship(name) }

var historicalReferences = map[string][]DirectoryProperty{
	"Achiever":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Achieves":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Activity":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualCondition":                {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualEffect":                   {{Name: "enablingCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualEnduringTask":             {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualEnterprisePhase":          {{Name: "statementTask", Type: "ActualEnduringTask", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualEnvironment":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualLocation":                 {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMeasurement":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMeasurementSet":           {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMission":                  {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualMissionPhase":             {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualMissionScenario":          {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualMissionVignette":          {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualOrganization":             {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualOrganizationRole":         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualOrganizationalResource":   {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualOutcome":                  {{Name: "enablingCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualPerson":                   {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualPost":                     {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualProject":                  {{Name: "ownedMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualProjectMilestone":         {{Name: "actualResource", Type: "ActualResource", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "versionReleased", Type: "VersionedElement", Many: true}, {Name: "versionWithdrawn", Type: "VersionedElement", Many: true}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualProjectMilestoneRole":     {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualProjectRole":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualPropertySet":              {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResource":                 {{Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResourceRelationship":     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResourceRole":             {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualResponsibility":           {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualResponsibleResource":      {{Name: "publishedStandards", Type: "Standard", Many: true}, {Name: "ratifiedStandards", Type: "Standard", Many: true}, {Name: "createdStandards", Type: "Standard", Many: true}, {Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"ActualRisk":                     {{Name: "actualRiskOwner", Type: "ActualResponsibleResource", Many: false}, {Name: "affectedActualResource", Type: "ActualResource", Many: false}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualService":                  {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualState":                    {{Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ActualStrategicPhase":           {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"AffectableElement":              {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Affects":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"AffectsInContext":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Alias":                          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArbitraryConnector":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArchitecturalDescription":       {{Name: "view", Type: "View", Many: true}, {Name: "viewpoint", Type: "Viewpoint", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArchitecturalReference":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Architecture":                   {{Name: "describedBy", Type: "ArchitecturalDescription", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ArchitectureMetadata":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Asset":                          {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"AssetRole":                      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Capability":                     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"CapabilityConfiguration":        {{Name: "doctrine", Type: "StandardOperationalActivity", Many: true}, {Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"CapabilityRole":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"CapableElement":                 {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Challenge":                      {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Command":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ComparesTo":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Competence":                     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"CompetenceForRole":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"CompetenceToConduct":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ConceptItem":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ConceptRole":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Concern":                        {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Condition":                      {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ConflictsWith":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Control":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Creates":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Defines":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Definition":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"DesignationKind":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Desirer":                        {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Desires":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Doctrine":                       {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Driver":                         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Effect":                         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Enables":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnhancedSecurityControl":        {{Name: "mitigatingActualResource", Type: "ActualResource", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Enhances":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnterpriseGoal":                 {{Name: "benefit", Type: "ValueItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnterpriseObjective":            {{Name: "benefit", Type: "ValueItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EnterpriseVision":               {{Name: "statement", Type: "VisionStatement", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Environment":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"EnvironmentProperty":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"EvokedBy":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Exchange":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Exhibits":                       {{Name: "environmentalConditions", Type: "Environment", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FieldedCapability":              {{Name: "milestone", Type: "ActualProjectMilestone", Many: true}, {Name: "actualCondition", Type: "ActualCondition", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FillsPost":                      {{Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Forecast":                       {{Name: "forecastPeriod", Type: "ActualStrategicPhase", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Function":                       {{Name: "affectedResource", Type: "ResourceExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionAction":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionControlFlow":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionEdge":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"FunctionObjectFlow":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"GeoPoliticalExtentType":         {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}},
	"GovernedBy":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"HighLevelOperationalConcept":    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ISO8601DateTime":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ImpactedBy":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Implements":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Information":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"InformationModel":               {{Name: "conformsTo", Type: "Standard", Many: true}},
	"IsCapableToPerform":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"KnownResource":                  {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}},
	"Location":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"LocationHolder":                 {{Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MapsToCapability":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MapsToGoal":                     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MeasurableElement":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Measurement":                    {{Name: "environmentalContext", Type: "ActualCondition", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MeasurementSet":                 {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Metadata":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MilestoneDependency":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Mission":                        {{Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionEngineeringThread":       {{Name: "affectedResource", Type: "ResourceExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionScenario":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionTask":                    {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionTaskAction":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionThread":                  {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MissionVignette":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Mitigates":                      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MotivatedBy":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"MotivationalElement":            {{Name: "conformsTo", Type: "Standard", Many: true}},
	"NaturalResource":                {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"OperationalActivity":            {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalActivityAction":      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalActivityEdge":        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalAgent":               {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalArchitecture":        {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"OperationalAsset":               {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalConnector":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalConstraint":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalControlFlow":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalExchange":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalExchangeItem":        {{Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalInformation":         {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}},
	"OperationalInformationRole":     {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalInterface":           {{Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalMessage":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalMethod":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalMitigation":          {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"OperationalObjectFlow":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalParameter":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalPerformer":           {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"OperationalPort":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalRole":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"OperationalSignal":              {{Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalSignalProperty":      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OperationalStateDescription":    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Opportunity":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"OpposableElement":               {{Name: "designation", Type: "DesignationKind", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Opposes":                        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Organization":                   {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"OrganizationInPhase":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OrganizationalResource":         {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"OwnsProcess":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OwnsRisk":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OwnsRiskInContext":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"OwnsValue":                      {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"PerformsInContext":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Person":                         {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"PhaseableElement":               {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Phases":                         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"PhysicalResource":               {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"Post":                           {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"PresentedBy":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProblemDomain":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"Project":                        {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"ProjectActivity":                {{Name: "affectedResource", Type: "ResourceExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectActivityAction":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectMilestone":               {{Name: "resource", Type: "ResourcePerformer", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectMilestoneRole":           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectRole":                    {{Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ProjectSequence":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectStatus":                  {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ProjectTheme":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"PropertySet":                    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Protects":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtectsInContext":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Protocol":                       {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtocolImplementation":         {{Name: "implements", Type: "Protocol", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtocolLayer":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProtocolStack":                  {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProvidedServiceLevel":           {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ProvidesCompetence":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"RequiredServiceLevel":           {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"RequiresCompetence":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Resource":                       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceArchitecture":           {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"ResourceArtifact":               {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourceAsset":                  {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceConnector":              {{Name: "boundaryCondition", Type: "Environment", Many: true}, {Name: "implements", Type: "Protocol", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}},
	"ResourceConstraint":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceExchange":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceExchangeItem":           {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceInformation":            {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"ResourceInformationRole":        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceInterface":              {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceMessage":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceMethod":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceMitigation":             {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"ResourceParameter":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourcePerformer":              {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourcePort":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "implements", Type: "Protocol", Many: true}},
	"ResourceRole":                   {{Name: "physicalLocation", Type: "ActualLocation", Many: true}, {Name: "requiredEnvironment", Type: "Condition", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourceService":                {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"ResourceServiceInterface":       {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceSignal":                 {{Name: "function", Type: "Function", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceSignalProperty":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ResourceStateDescription":       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Responsibility":                 {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "stakeholderConcern", Type: "Concern", Many: true}},
	"ResponsibleFor":                 {{Name: "startDate", Type: "ISO8601DateTime", Many: false}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Risk":                           {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Rule":                           {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SameAs":                         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityConstraint":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityControl":                {{Name: "mitigatingActualResource", Type: "ActualResource", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityControlFamily":          {{Name: "mitigatingActualResource", Type: "ActualResource", Many: true}, {Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityEnclave":                {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"SecurityProcess":                {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityProcessAction":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SecurityRisk":                   {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Sequence":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Service":                        {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"ServiceArchitecture":            {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"ServiceConnector":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceContract":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceControlFlow":             {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceExchange":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceExchangeItem":            {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceFunction":                {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceFunctionAction":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceFunctionEdge":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceInterface":               {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceMessage":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceMethod":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceObjectFlow":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceParameter":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServicePolicy":                  {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServicePort":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceRole":                    {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceSignal":                  {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceSignalProperty":          {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"ServiceStateDescription":        {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Software":                       {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"Stakeholder":                    {{Name: "stakeholderConcern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Standard":                       {{Name: "ratifiedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "mandatedDate", Type: "ISO8601DateTime", Many: false}, {Name: "retiredDate", Type: "ISO8601DateTime", Many: false}, {Name: "publishedBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "createdBy", Type: "ActualOrganizationalResource", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StandardOperationalActivity":    {{Name: "affectedResource", Type: "OperationalExchangeItem", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StatusIndicators":               {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicAsset":                 {{Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicConstraint":            {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicExchange":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicExchangeItem":          {{Name: "conformsTo", Type: "Standard", Many: true}},
	"StrategicInformation":           {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"StrategicPhase":                 {{Name: "conformsTo", Type: "Standard", Many: true}},
	"StructuralPart":                 {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfForecast":              {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfOperationalConstraint": {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfResourceConstraint":    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfSecurityConstraint":    {{Name: "conformsTo", Type: "Standard", Many: true}},
	"SubjectOfStrategicConstraint":   {{Name: "conformsTo", Type: "Standard", Many: true}},
	"Supports":                       {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"System":                         {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}, {Name: "describedBy", Type: "ArchitecturalDescription", Many: true}},
	"Technology":                     {{Name: "milestone", Type: "ProjectMilestone", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "function", Type: "Function", Many: true}, {Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "activity", Type: "OperationalActivity", Many: true}, {Name: "designation", Type: "DesignationKind", Many: false}},
	"TemporalPart":                   {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"UAFElement":                     {{Name: "conformsTo", Type: "Standard", Many: true}},
	"ValueItem":                      {{Name: "appliesFor", Type: "MeasurableElement", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "categoryCategorizesAsset", Type: "MeasurementSet", Many: false}},
	"ValueStream":                    {{Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
	"VersionOfConfiguration":         {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"VersionSuccession":              {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"VersionedElement":               {{Name: "versionReleasedAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "versionWithdrawnAtMilestone", Type: "ActualProjectMilestone", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"View":                           {{Name: "viewpoint", Type: "Viewpoint", Many: false}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"Viewpoint":                      {{Name: "stakeholder", Type: "Stakeholder", Many: true}, {Name: "concern", Type: "Concern", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"VisionStatement":                {{Name: "measurementSet", Type: "MeasurementSet", Many: true}, {Name: "actualMeasurementSet", Type: "ActualMeasurementSet", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}},
	"WholeLifeConfiguration":         {{Name: "conformsTo", Type: "Standard", Many: true}},
	"WholeLifeEnterprise":            {{Name: "statementTask", Type: "ActualEnduringTask", Many: true}, {Name: "conformsTo", Type: "Standard", Many: true}, {Name: "endDate", Type: "ISO8601DateTime", Many: false}, {Name: "startDate", Type: "ISO8601DateTime", Many: false}},
}
