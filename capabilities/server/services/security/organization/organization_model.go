package organization

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"platformserver/platform"
	"slices"
	"strings"
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
