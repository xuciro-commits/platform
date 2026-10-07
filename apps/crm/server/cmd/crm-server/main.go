// Command crm-server runs the CRM alone on a development host (docs/Apps.md
// step 6; ADR-0025 D3): no lodging provider, so stays cannot be booked. With
// -web the host serves the workspace's build; the development tokens "sales"
// and "manager" sign in.
package main

import (
	"flag"
	"log"

	"crm"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/work"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8499")
	flag.Parse()
	seat := func(token, id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{token}, Member: platform.Member{ID: id, Roles: roles}}
	}
	development := []platformserver.Seat{
		seat("manager", "manager-1", map[string]string{crm.ID: string(crm.Manager), platformserver.PlatformApp: platformserver.Admin,
			ai.ID: ai.Admin, flow.ID: flow.Admin, platformserver.AgentApp: platformserver.AgentAdmin}),
		seat("sales", "sales-1", map[string]string{crm.ID: string(crm.Sales)}),
	}
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		seats := deployment.SeatsFor(id)
		return platformserver.NewTenant(id, platformserver.NewConsole(id, seats...), ai.New(id), work.New(id),
			flow.New(id), platformserver.NewAgents(id), crm.New(id))
	}
	if err := deployment.Run(platformserver.TenantSpec{ID: "dev", Seats: development}); err != nil {
		log.Fatal(err)
	}
}
