// Package hospitality is a hotel company's software as a solution (ADR-0011,
// ADR-0025): the platform's directory, relations, AI, work (approvals, tasks),
// flows and agents, a lodging provider (the PMS), the CRM, HCM and CSM,
// composed without a bridge. The CRM consumes the lodging protocol; any provider serves it.
package hospitality

import (
	"crm"
	"csm"
	"hcm"
	"lodging"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/files"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/org"
	"platformserver/apps/relations"
	"platformserver/apps/work"
	"platformserver/platform"
	"pms"
)

// NewTenant composes the software for one tenant with two lodging providers:
// the hotel, bound first, and serviced apartments (the protocol's reference
// provider); an administrator chooses between them in Settings (#99).
func NewTenant(id string, rooms map[string]pms.RoomType, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return Compose(id, []platform.App{pms.New(id, rooms), lodging.NewMemory(id)}, seats...)
}

// Compose puts any lodging providers under the CRM; the first is bound until an administrator chooses another.
func Compose(id string, providers []platform.App, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	chart := DemoOrganization()
	chart.Memberships = append(chart.Memberships, platformserver.Memberships(seats)...)
	apps := append([]platform.App{platformserver.NewConsole(id, seats...), org.New(id, chart), relations.New(id), ai.New(id), work.New(id), flow.New(id), platformserver.NewAgents(id), knowledge.New(id), files.New(id)}, providers...)
	return platformserver.NewTenant(id, append(apps, crm.New(id), hcm.New(id), csm.New(id))...)
}

// DemoOrganization is a hospitality group as ADR-0012 sees it: the same units in
// a legal, a management and a governance structure, a temporary project and
// committee, and an external partner that sits on the committee.
func DemoOrganization() platform.OrgSeed {
	unit := func(id, name, kind string) platform.Unit {
		return platform.Unit{ID: id, Name: name, Kind: kind}
	}
	edge := func(structure, u, parent, relation string) platform.Edge {
		return platform.Edge{Structure: structure, Unit: u, Parent: parent, Relation: relation}
	}
	group, company := unit("group", "Harbour Hospitality Group", "group"), unit("hotel-a-co", "Hotel A Ltd.", "subsidiary")
	group.Legal, company.Legal = true, true
	frontOffice := unit("front-office", "Front office", "department")
	frontOffice.Calendar = "front-office"
	offsite := unit("offsite-2026", "Autumn offsite programme", "project")
	offsite.Until = "2027-01-01"
	committee := unit("guest-committee", "Guest experience committee", "committee")
	committee.Until = "2027-07-01"
	acme := unit("acme", "Acme Corp", "partner")
	acme.Legal, acme.External = true, true
	return platform.OrgSeed{
		// The office works Monday to Friday, closed for National Day week; the
		// hotel's front office works every day (ADR-0028 D7).
		Calendars: []platform.Calendar{
			{ID: "office", Name: "Office", Holidays: []platform.Date{"2026-10-01", "2026-10-02", "2026-10-05", "2026-10-06", "2026-10-07"}},
			{ID: "front-office", Name: "Front office", Workdays: []int{1, 2, 3, 4, 5, 6, 7}}},
		Structures: []platform.Structure{{ID: "legal", Name: "Legal entities", Kind: "legal"},
			{ID: "management", Name: "Management", Kind: "management"}, {ID: "governance", Name: "Committees", Kind: "governance"},
			{ID: "projects", Name: "Projects", Kind: "project"}},
		Units: []platform.Unit{group, company, unit("hospitality", "Hospitality business group", "business group"),
			unit("hotel-a", "Hotel A", "property"), frontOffice, unit("sales-team", "Sales", "team"),
			offsite, committee, acme},
		Edges: []platform.Edge{
			{Structure: "legal", Unit: "hotel-a-co", Parent: "group", Relation: "owned by", Share: 1},
			edge("management", "hospitality", "group", "part of"), edge("management", "hotel-a", "hospitality", "reports to"),
			edge("management", "front-office", "hotel-a", "part of"), edge("management", "sales-team", "hotel-a", "part of"),
			edge("governance", "guest-committee", "group", "part of"), edge("projects", "offsite-2026", "sales-team", "run by")},
		Memberships: []platform.Membership{{Party: "unit:acme", Unit: "guest-committee", Role: "observer"}},
	}
}
