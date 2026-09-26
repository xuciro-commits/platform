package platform

import (
	"slices"
	"time"
)

// The organisation's vocabulary (ADR-0012): units of any kind, in any number of
// structures, with memberships of members or of other units, all with valid
// time. Apps seed it and ask for a member's units; the org app owns the chart.

// Date is a calendar day "YYYY-MM-DD"; "" means unbounded.
type Date = string

type Unit struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Kind     string `json:"kind"` // open vocabulary: group, subsidiary, factory, line, committee, partner …
	Legal    bool   `json:"legal,omitempty"`
	External bool   `json:"external,omitempty"`
	From     Date   `json:"from,omitempty"`
	Until    Date   `json:"until,omitempty"`
	Closed   string `json:"closed,omitempty"` // why it ended: dissolved, merged into <unit> …
	// Calendar is the unit's working calendar (ADR-0028 D7); empty: the one
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
	Calendars   []Calendar   `json:"calendars,omitempty"`
	Structures  []Structure  `json:"structures"`
	Units       []Unit       `json:"units"`
	Edges       []Edge       `json:"edges"`
	Memberships []Membership `json:"memberships"`
}

// Units are the units c's member belongs to in structure on the input's day
// (now), with all units below them: the scope a rule reads from a named
// structure (ADR-0012). A replay passes the recorded time, so it scopes alike.
func (c Caller) Units(structure string, now time.Time) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Units(c, structure, now)
}
