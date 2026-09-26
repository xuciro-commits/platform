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
		seat("manager", "manager-1", map[string]string{crm.ID: string(crm.Manager), platformserver.PlatformApp: platformserver.Admin,
			platformserver.AIApp: platformserver.AIAdmin, platformserver.FlowApp: platformserver.FlowAdmin, platformserver.AgentApp: platformserver.AgentAdmin}),
		seat("sales", "sales-1", map[string]string{crm.ID: string(crm.Sales)}),
	})
	t, err := platformserver.NewTenant("dev", platformserver.NewConsole("dev", seats...), platformserver.NewAI("dev"), work.New("dev"),
		platformserver.NewFlows("dev"), platformserver.NewAgents("dev"), crm.New("dev"))
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
