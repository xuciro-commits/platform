// Command csm-server runs customer service alone on a development host
// (docs/Apps.md step 6; ADR-0025 D3): tickets, their service levels and the
// triage agent, with no CRM beside it. With -web the host serves the
// workspace's build; the development tokens "desk" and "lead" sign in; the
// lead administers the platform, AI and agents.
package main

import (
	"flag"
	"log"

	"csm"
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
		seat("lead", "lead-1", map[string]string{csm.ID: csm.Lead, platformserver.PlatformApp: platformserver.Admin, platformserver.AIApp: platformserver.AIAdmin,
			work.ID: work.Admin, platformserver.FlowApp: platformserver.FlowAdmin, platformserver.AgentApp: platformserver.AgentAdmin,
			platformserver.KnowledgeApp: platformserver.KnowledgeEditor}),
		seat("desk", "desk-1", map[string]string{csm.ID: csm.Desk}),
	})
	t, err := platformserver.NewTenant("dev", platformserver.NewConsole("dev", seats...), platformserver.NewAI("dev"), work.New("dev"),
		platformserver.NewFlows("dev"), platformserver.NewAgents("dev"), platformserver.NewKnowledge("dev"), csm.New("dev"))
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
