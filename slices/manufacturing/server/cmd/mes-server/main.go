// Command mes-server runs the manufacturing app ("mes") on the platform host for
// one demo plant. Without flags it keeps state in memory and accepts the demo
// tokens; with -database and -oidc-issuer it is the production path (ADR-0007).
package main

import (
	"flag"
	"log"
	"strings"

	"mes"
	"platformserver"
	"platformserver/platform"
)

const tenant = "plant-sz"

// demo is the directory without -directory; a development token is the subject.
var demo = []platformserver.Seat{
	seat("supervisor", "sup-1", mes.Supervisor, "plant-sz"),
	seat("operator-l1", "op-l1", mes.Operator, "L1"),
	seat("operator-l2", "op-l2", mes.Operator, "L2"),
	seat("quality-1", "qa-1", mes.Quality),
	seat("quality-2", "qa-2", mes.Quality),
	seat("gateway-l1", "gateway-l1", mes.Gateway),
	seat("erp", "erp", mes.ERP),
	agent(seat("assistant-l1", "agent-l1", mes.Assistant, "L1")),
}

// agent marks an AI agent: what it causes that cannot be recalled waits for a person (ADR-0014 D6).
func agent(s platformserver.Seat) platformserver.Seat { s.Agent = true; return s }

// seat signs in as subject and belongs to units of the site structure (ADR-0012).
func seat(subject, id string, role mes.Role, units ...string) platformserver.Seat {
	s := platformserver.Seat{Subjects: []string{subject}, Member: platform.Member{ID: id, Roles: map[string]string{"mes": string(role)}}}
	if subject == "supervisor" {
		s.Roles[platformserver.PlatformApp], s.Roles[platformserver.OrgApp], s.Roles[platformserver.AIApp] = platformserver.Admin, platformserver.OrgAdmin, platformserver.AIAdmin
		s.Roles[platformserver.FlowApp], s.Roles[platformserver.AgentApp] = platformserver.FlowAdmin, platformserver.AgentAdmin
	}
	for _, u := range units {
		s.Units = append(s.Units, platform.Membership{Unit: u, Role: string(role)})
	}
	return s
}

func main() {
	deployment := platformserver.Flags("127.0.0.1:8490")
	disable := flag.String("disable", "", "comma-separated capabilities to deactivate (ADR-0008)")
	flag.Parse()
	plant := mes.NewPlant(tenant, mes.DemoMaster())
	for _, c := range strings.FieldsFunc(*disable, func(r rune) bool { return r == ',' }) {
		if !plant.Disable(c) {
			log.Fatalf("-disable: no capability %q", c)
		}
	}
	seats := deployment.Seats(demo)
	t, err := platformserver.NewTenant(tenant, platformserver.NewConsole(tenant, seats...),
		platformserver.NewOrganization(tenant, mes.DemoOrganization(platformserver.Memberships(seats))), platformserver.NewAI(tenant), platformserver.NewWork(tenant), platformserver.NewFlows(tenant), platformserver.NewAgents(tenant), plant)
	if err == nil {
		err = t.Connect(mes.DemoConnectors(tenant)...)
	}
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
