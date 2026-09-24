package platform

import "time"

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

// Units are the units c's member belongs to in structure on the input's day
// (now), with all units below them: the scope a rule reads from a named
// structure (ADR-0012). A replay passes the recorded time, so it scopes alike.
func (c Caller) Units(structure string, now time.Time) []string {
	if c.rt == nil {
		return nil
	}
	return c.rt.Units(c, structure, now)
}
