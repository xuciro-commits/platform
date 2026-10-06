// Command hcm-server runs HCM alone on a development host (docs/Apps.md step
// 6; ADR-0025 D3) with a small organisation: an employee, their manager and
// the company's head, who approve leave. With -web the host serves the
// workspace's build; the development tokens "employee", "manager", "head" and
// "hr" sign in.
package main

import (
	"flag"
	"log"

	"hcm"
	"platformserver"
	"platformserver/apps/enterprise"
	"platformserver/apps/work"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8499")
	flag.Parse()
	seat := func(token, id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{token}, Member: platform.Member{ID: id, Roles: roles}}
	}
	seats := deployment.Seats([]platformserver.Seat{
		seat("employee", "employee-1", map[string]string{hcm.ID: hcm.Employee}),
		seat("manager", "manager-1", map[string]string{hcm.ID: hcm.Employee}),
		seat("head", "head-1", map[string]string{hcm.ID: hcm.Employee}),
		seat("hr", "hr-1", map[string]string{hcm.ID: hcm.HR, platformserver.PlatformApp: platformserver.Admin, enterprise.ID: enterprise.Admin,
			work.ID: work.Admin}),
	})
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		organization := enterprise.New(id, platform.OrgSeed{Structures: []platform.Structure{{ID: hcm.Structure, Name: "Management", Kind: "management"}},
			Units: []platform.Unit{{ID: "company", Name: "Company", Kind: "company"}, {ID: "team", Name: "Team", Kind: "department"}},
			Edges: []platform.Edge{{Structure: hcm.Structure, Unit: "team", Parent: "company"}},
			Memberships: []platform.Membership{{Party: "member:employee-1", Unit: "team", Role: "employee"}, {Party: "member:manager-1", Unit: "team", Role: "manager"},
				{Party: "member:head-1", Unit: "company", Role: "head"}}})
		return platformserver.NewTenant(id, platformserver.NewConsole(id, seats...), organization, work.New(id), hcm.New(id))
	}
	t, err := deployment.Rebuild("dev")
	if err == nil {
		err = deployment.Serve(t)
	}
	if err != nil {
		log.Fatal(err)
	}
}
