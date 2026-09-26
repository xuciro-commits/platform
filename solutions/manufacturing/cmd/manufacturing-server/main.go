// Command manufacturing-server runs the plant solution — the MES and its books meeting
// through production.orders/1 (ADR-0024) — for one demo plant. The books are
// the ERP app, seeded with its chart, the month open and the plant's products
// when the journal is empty; with -erp external they are the adapter to an ERP
// outside, whose planned orders the service account "erp" delivers. Without
// -oidc-issuer it accepts the development tokens: supervisor (also the books'
// controller), accountant, operator-l1, operator-l2, quality-1, quality-2,
// gateway-l1, assistant-l1 (an AI agent) and erp.
package main

import (
	"flag"
	"log"
	"time"

	"erp"
	"erpadapter"
	"manufacturing"
	"mes"
	"platformserver"
	"platformserver/apps/ai"
	"platformserver/apps/flow"
	"platformserver/apps/knowledge"
	"platformserver/apps/org"
	"platformserver/apps/work"
	"platformserver/platform"
)

const tenant = "plant-sz"

func main() {
	deployment := platformserver.Flags("127.0.0.1:8491")
	books := flag.String("erp", "app", "the plant's books: app (the ERP app) or external (the adapter to an ERP outside)")
	flag.Parse()
	seat := func(role mes.Role) map[string]string { return map[string]string{"mes": string(role)} }
	supervisor := seat(mes.Supervisor)
	for app, role := range map[string]string{erp.ID: erp.Controller, erpadapter.ID: erpadapter.Planner, platformserver.PlatformApp: platformserver.Admin,
		org.ID: org.Admin, ai.ID: ai.Admin, flow.ID: flow.Admin,
		platformserver.AgentApp: platformserver.AgentAdmin, knowledge.ID: knowledge.Editor, work.ID: work.Admin} {
		supervisor[app] = role
	}
	assistant := manufacturing.Seat("assistant-l1", "agent-l1", seat(mes.Assistant), "L1")
	assistant.Agent = true // what it causes that cannot be recalled waits for a person (ADR-0014 D6)
	seats := deployment.Seats([]platformserver.Seat{
		manufacturing.Seat("supervisor", "sup-1", supervisor, "plant-sz"),
		manufacturing.Seat("accountant", "acc-1", map[string]string{erp.ID: erp.Accountant}),
		manufacturing.Seat("operator-l1", "op-l1", seat(mes.Operator), "L1"),
		manufacturing.Seat("operator-l2", "op-l2", seat(mes.Operator), "L2"),
		manufacturing.Seat("quality-1", "qa-1", seat(mes.Quality)),
		manufacturing.Seat("quality-2", "qa-2", seat(mes.Quality)),
		manufacturing.Seat("gateway-l1", "gateway-l1", seat(mes.Gateway)),
		assistant,
		manufacturing.Seat("erp", "erp", map[string]string{erpadapter.ID: erpadapter.Connector}),
	})
	var app platform.App = erp.New(tenant)
	switch *books {
	case "app":
		deployment.Seed = func(t *platformserver.Tenant, now time.Time) error { return manufacturing.Seed(t, "sup-1", now) }
	case "external":
		app = erpadapter.New(tenant)
	default:
		log.Fatalf("-erp: %q is neither app nor external", *books)
	}
	t, err := manufacturing.NewTenant(tenant, app, seats...)
	if err == nil && *books == "external" {
		err = t.Connect(erpadapter.Poll("erp"))
	}
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
