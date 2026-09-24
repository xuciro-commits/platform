// Package sales is the sales software as a solution (ADR-0011): the platform's
// directory and relations, a lodging provider and the CRM, composed without a
// bridge. The CRM consumes the lodging protocol; any provider serves it.
package sales

import (
	"crm"
	"hotel"
	"lodging"
	"platformserver"
	"platformserver/platform"
)

// NewTenant composes the software for one tenant with two lodging providers:
// the hotel, bound first, and serviced apartments (the protocol's reference
// provider); an administrator chooses between them in Settings (#99).
func NewTenant(id string, rooms map[string]hotel.RoomType, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	return Compose(id, []platform.App{hotel.NewHotel(id, rooms), lodging.NewMemory(id)}, seats...)
}

// Compose puts any lodging providers under the CRM; the first is bound until an administrator chooses another.
func Compose(id string, providers []platform.App, seats ...platformserver.Seat) (*platformserver.Tenant, error) {
	org := DemoOrganization()
	org.Memberships = append(org.Memberships, platformserver.Memberships(seats)...)
	apps := append([]platform.App{platformserver.NewConsole(id, seats...), platformserver.NewOrganization(id, org), platformserver.NewRelations(id)}, providers...)
	return platformserver.NewTenant(id, append(apps, crm.New(id))...)
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
	offsite := unit("offsite-2026", "Autumn offsite programme", "project")
	offsite.Until = "2027-01-01"
	committee := unit("guest-committee", "Guest experience committee", "committee")
	committee.Until = "2027-07-01"
	acme := unit("acme", "Acme Corp", "partner")
	acme.Legal, acme.External = true, true
	return platform.OrgSeed{
		Structures: []platform.Structure{{ID: "legal", Name: "Legal entities", Kind: "legal"},
			{ID: "management", Name: "Management", Kind: "management"}, {ID: "governance", Name: "Committees", Kind: "governance"},
			{ID: "projects", Name: "Projects", Kind: "project"}},
		Units: []platform.Unit{group, company, unit("hospitality", "Hospitality business group", "business group"),
			unit("hotel-a", "Hotel A", "property"), unit("front-office", "Front office", "department"), unit("sales-team", "Sales", "team"),
			offsite, committee, acme},
		Edges: []platform.Edge{
			{Structure: "legal", Unit: "hotel-a-co", Parent: "group", Relation: "owned by", Share: 1},
			edge("management", "hospitality", "group", "part of"), edge("management", "hotel-a", "hospitality", "reports to"),
			edge("management", "front-office", "hotel-a", "part of"), edge("management", "sales-team", "hotel-a", "part of"),
			edge("governance", "guest-committee", "group", "part of"), edge("projects", "offsite-2026", "sales-team", "run by")},
		Memberships: []platform.Membership{{Party: "unit:acme", Unit: "guest-committee", Role: "observer"}},
	}
}
