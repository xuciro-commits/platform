package platform

import (
	"slices"
	"time"

	"platformkernel/kernel"
)

// The organisation's vocabulary (ADR-0022): units of any kind, in any number of
// structures, with memberships of members or of other units, all with valid
// time. Apps seed it and ask for a member's units; the org app owns the chart.

// Date is a calendar day "YYYY-MM-DD"; "" means unbounded.
type Date = string

// Period is an active interval; Until is exclusive. Missing periods retain the
// original From/Until interval in older organisation projections.
type Period struct {
	From  Date `json:"from,omitempty"`
	Until Date `json:"until,omitempty"`
}

type Unit struct {
	Periods  []Period `json:"periods,omitempty"`
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Kind     string   `json:"kind"` // open vocabulary: group, subsidiary, factory, line, committee, partner …
	Legal    bool     `json:"legal,omitempty"`
	External bool     `json:"external,omitempty"`
	From     Date     `json:"from,omitempty"`
	Until    Date     `json:"until,omitempty"`
	Closed   string   `json:"closed,omitempty"` // why it ended: dissolved, merged into <unit> …
	// Calendar is the unit's working calendar (ADR-0038); empty: the one
	// found above it in any structure, else the tenant's first.
	Calendar string `json:"calendar,omitempty"`
}

// Calendar is when a unit works: its working weekdays and its holidays.
type Calendar struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Workdays []int  `json:"workdays,omitempty"` // 1 Monday … 7 Sunday; empty: Monday to Friday
	Holidays []Date `json:"holidays,omitempty"`
}

// Works reports whether day is a working day of the calendar.
func (c Calendar) Works(day time.Time) bool {
	wd := int(day.Weekday())
	if wd == 0 {
		wd = 7
	}
	workdays := c.Workdays
	if len(workdays) == 0 {
		workdays = []int{1, 2, 3, 4, 5}
	}
	if !slices.Contains(workdays, wd) {
		return false
	}
	return !slices.Contains(c.Holidays, day.Format(time.DateOnly))
}

// After is from moved n working days on, at the same time of day: the due
// time of work that may take n working days.
func (c Calendar) After(from time.Time, n int) time.Time {
	at := from
	for n > 0 {
		at = at.AddDate(0, 0, 1)
		if c.Works(at) {
			n--
		}
	}
	return at
}

type Structure struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Kind   string `json:"kind"`             // legal, management, finance, site, project, governance, community, custom
	Matrix bool   `json:"matrix,omitempty"` // a unit may have several parents at once
}

type Edge struct {
	Periods   []Period `json:"periods,omitempty"`
	Structure string   `json:"structure"`
	Unit      string   `json:"unit"`
	Parent    string   `json:"parent"`
	Relation  string   `json:"relation,omitempty"` // part of, owned by, reports to, located at …
	Share     float64  `json:"share,omitempty"`    // ownership share in a legal structure
	From      Date     `json:"from,omitempty"`
	Until     Date     `json:"until,omitempty"`
}

type Membership struct {
	Periods []Period `json:"periods,omitempty"`
	Party   string   `json:"party"` // "member:<id>" or "unit:<id>"
	Unit    string   `json:"unit"`
	Role    string   `json:"role"` // open vocabulary: employee, volunteer, maintainer, chair, delegate …
	Primary bool     `json:"primary,omitempty"`
	From    Date     `json:"from,omitempty"`
	Until   Date     `json:"until,omitempty"`
}

// OrgSeed is an organisation's starting shape (an industry package's, or a deployment's).
type OrgSeed struct {
	Calendars   []Calendar   `json:"calendars,omitempty"`
	Structures  []Structure  `json:"structures"`
	Units       []Unit       `json:"units"`
	Edges       []Edge       `json:"edges"`
	Memberships []Membership `json:"memberships"`
}

// Units are the units c's member belongs to in structure on the input's day
// (now), with all units below them: the scope a rule reads from a named
// structure (ADR-0022). A replay passes the recorded time, so it scopes alike.
func (c Caller) Units(structure string, now time.Time) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Units(c, structure, now)
}

// ElementInfo is what an app learns about an element of the enterprise model
// (ADR-0022 D3) without owning it: enough to type a reference and label it.
type ElementInfo struct {
	ID         string `json:"id"`
	Stereotype string `json:"stereotype"`
	Name       string `json:"name"`
	Kind       string `json:"kind,omitempty"`
	Owner      string `json:"owner,omitempty"` // "tenant:<id>" when federated in
}

// Enterprise is the caller's window on the tenant's enterprise model
// (ADR-0022 D3): the one place apps ask who sits where, what a site holds and
// what an organisation can do. Day is the input's day (now), so replays agree.
type Enterprise struct{ c Caller }

func (c Caller) Enterprise() Enterprise { return Enterprise{c} }

// EnterpriseUsageRuntime checks references through the host's original stores.
// It supplies no new read or write grant to the calling application.
type EnterpriseUsageRuntime interface {
	CheckEnterpriseUsage(Caller, string, string, bool, time.Time) *kernel.Error
	CheckEnterpriseTenantLink(Caller, string, uint32, time.Time) *kernel.Error
	CheckEnterpriseTenantConnection(Caller, string, time.Time) *kernel.Error
}

func (e Enterprise) CheckUsage(id, until string, deleting bool, now time.Time) *kernel.Error {
	if e.c.Replaying {
		return nil
	}
	if runtime, ok := e.c.rt.(EnterpriseUsageRuntime); ok {
		return runtime.CheckEnterpriseUsage(e.c, id, until, deleting, now)
	}
	return nil
}

func (e Enterprise) CheckTenantLink(id string, revision uint32, now time.Time) *kernel.Error {
	if e.c.Replaying {
		return nil
	}
	if runtime, ok := e.c.rt.(EnterpriseUsageRuntime); ok {
		return runtime.CheckEnterpriseTenantLink(e.c, id, revision, now)
	}
	return nil
}

func (e Enterprise) CheckTenantConnection(id string, now time.Time) *kernel.Error {
	if e.c.Replaying {
		return nil
	}
	if runtime, ok := e.c.rt.(EnterpriseUsageRuntime); ok {
		return runtime.CheckEnterpriseTenantConnection(e.c, id, now)
	}
	return nil
}

// Units are the organisations the caller belongs to in kind ("" for all),
// with those below them.
func (e Enterprise) Units(kind string, now time.Time) []string { return e.c.Units(kind, now) }

// Element is the element id, when it is live on now's day.
func (e Enterprise) Element(id string, now time.Time) (ElementInfo, bool) {
	if e.c.rt == nil {
		return ElementInfo{}, false
	}
	return e.c.rt.Element(e.c, id, now)
}

// Related are the elements joined to element by stereotype on now's day.
func (e Enterprise) Related(element, stereotype string, outgoing bool, now time.Time) []string {
	if e.c.rt == nil {
		return nil
	}
	return e.c.rt.Related(e.c, element, stereotype, outgoing, now)
}

// Capable are the capabilities an organisation or resource has: «Exhibits»
// (UAF 1.3's pair, ADR-0022 D2) and the older «IsCapableToPerform» models kept.
func (e Enterprise) Capable(element string, now time.Time) []string {
	out := e.Related(element, "Exhibits", true, now)
	for _, cap := range e.Related(element, "IsCapableToPerform", true, now) {
		if !slices.Contains(out, cap) {
			out = append(out, cap)
		}
	}
	return out
}

// Located is where an element sits («ActualResourceRelationship» to a location or organisation), nearest first.
func (e Enterprise) Located(element string, now time.Time) []string {
	return e.Related(element, "ActualResourceRelationship", true, now)
}

// Of are the elements the organisation owns («OwnsProcess»: posts, resources, goals, projects).
func (e Enterprise) Of(organisation string, now time.Time) []string {
	return e.Related(organisation, "OwnsProcess", true, now)
}
