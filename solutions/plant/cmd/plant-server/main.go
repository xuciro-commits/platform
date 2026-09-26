// Command plant-server runs the plant solution — the MES and the ERP meeting
// through production.orders/1 (ADR-0024 7c) — for one demo plant. Without flags
// it keeps state in memory, seeds the ERP's books and products, and accepts the
// development tokens: supervisor (also the ERP's controller), accountant,
// operator-l1, operator-l2, quality-1 and quality-2.
package main

import (
	"flag"
	"log"
	"time"

	"erp"
	"mes"
	"plant"
	"platformserver"
)

const tenant = "plant-sz"

func main() {
	deployment := platformserver.Flags("127.0.0.1:8491")
	flag.Parse()
	seat := func(role mes.Role) map[string]string { return map[string]string{"mes": string(role)} }
	supervisor := seat(mes.Supervisor)
	for app, role := range map[string]string{erp.ID: erp.Controller, platformserver.PlatformApp: platformserver.Admin, platformserver.OrgApp: platformserver.OrgAdmin,
		platformserver.AIApp: platformserver.AIAdmin, platformserver.FlowApp: platformserver.FlowAdmin, platformserver.AgentApp: platformserver.AgentAdmin,
		platformserver.KnowledgeApp: platformserver.KnowledgeEditor, platformserver.WorkApp: platformserver.WorkAdmin} {
		supervisor[app] = role
	}
	seats := deployment.Seats([]platformserver.Seat{
		plant.Seat("supervisor", "sup-1", supervisor, "plant-sz"),
		plant.Seat("accountant", "acc-1", map[string]string{erp.ID: erp.Accountant}),
		plant.Seat("operator-l1", "op-l1", seat(mes.Operator), "L1"),
		plant.Seat("operator-l2", "op-l2", seat(mes.Operator), "L2"),
		plant.Seat("quality-1", "qa-1", seat(mes.Quality)),
		plant.Seat("quality-2", "qa-2", seat(mes.Quality)),
	})
	t, err := plant.NewTenant(tenant, seats...)
	if err == nil && deployment.Database == "" {
		err = plant.Seed(t, "sup-1", time.Now())
	}
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
