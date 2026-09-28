// Command erpadapter-server runs the ERP adapter alone on a development host
// (docs/Apps.md step 6; ADR-0025 D3): the ERP's planned orders delivered by
// its service account, confirmed by a planner, sent to whatever endpoint an
// administrator binds. With -web the host serves the workspace's build; the
// development tokens "planner" (also the administrator) and "erp" sign in.
package main

import (
	"flag"
	"log"

	"erpadapter"
	"platformserver"
	"platformserver/platform"
)

func main() {
	deployment := platformserver.Flags("127.0.0.1:8499")
	flag.Parse()
	seat := func(token, id string, roles map[string]string) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{token}, Member: platform.Member{ID: id, Roles: roles}}
	}
	seats := deployment.Seats([]platformserver.Seat{
		seat("planner", "planner-1", map[string]string{erpadapter.ID: erpadapter.Planner, platformserver.PlatformApp: platformserver.Admin}),
		seat("erp", "erp", map[string]string{erpadapter.ID: erpadapter.Connector}),
	})
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		t, err := platformserver.NewTenant(id, platformserver.NewConsole(id, seats...), erpadapter.New(id))
		if err == nil {
			err = t.Connect(erpadapter.Poll("erp"))
		}
		return t, err
	}
	t, err := deployment.Rebuild("dev")
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
