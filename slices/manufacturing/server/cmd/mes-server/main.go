// Command mes-server runs the manufacturing reference slice for one demo plant.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"mes"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8490", "listen address")
	flag.Parse()
	const tenant = "plant-sz"
	plant := mes.NewPlant(tenant, mes.DemoMaster())
	for _, d := range mes.DemoConnectors(tenant) {
		plant.RegisterConnector(d)
	}
	server := &mes.Server{
		Plants: map[string]*mes.Plant{tenant: plant},
		Tokens: map[string]mes.Principal{
			"supervisor":  {ID: "sup-1", Tenant: tenant, Role: mes.Supervisor, Lines: []string{"L1", "L2"}},
			"operator-l1": {ID: "op-l1", Tenant: tenant, Role: mes.Operator, Lines: []string{"L1"}},
			"operator-l2": {ID: "op-l2", Tenant: tenant, Role: mes.Operator, Lines: []string{"L2"}},
			"quality-1":   {ID: "qa-1", Tenant: tenant, Role: mes.Quality},
			"quality-2":   {ID: "qa-2", Tenant: tenant, Role: mes.Quality},
			"gateway-l1":  {ID: "gateway-l1", Tenant: tenant, Role: mes.Gateway},
			"erp":         {ID: "erp", Tenant: tenant, Role: mes.ERP},
		},
		Now: time.Now,
	}
	log.Printf("mes-server on http://%s (tenant %s)", *addr, tenant)
	log.Fatal(http.ListenAndServe(*addr, server.Handler()))
}
