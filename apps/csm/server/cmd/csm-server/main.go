// Command csm-server runs CSM on a development host: tickets, service levels,
// and the original helpdesk workflows.
package main

import (
	"flag"
	"log"

	"csm"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/files"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
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
		seat("lead", "lead-1", map[string]string{csm.ID: csm.Lead, platformserver.PlatformApp: platformserver.Admin, ai.ID: ai.Admin,
			work.ID: work.Admin, flow.ID: flow.Admin, platformserver.AgentApp: platformserver.AgentAdmin,
			knowledge.ID: knowledge.Editor}),
		seat("desk", "desk-1", map[string]string{csm.ID: csm.Desk}),
	}
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		seats := deployment.SeatsFor(id)
		return platformserver.NewTenant(id, platformserver.NewConsole(id, seats...), ai.New(id), work.New(id),
			flow.New(id), platformserver.NewAgents(id), knowledge.New(id), files.New(id), csm.New(id))
	}
	if err := deployment.Run(platformserver.TenantSpec{ID: "dev", Seats: development}); err != nil {
		log.Fatal(err)
	}
}
