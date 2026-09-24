// Command mes-server runs the manufacturing reference slice for one demo plant.
// Without flags it keeps state in memory and accepts the demo tokens; with
// -database and -oidc-issuer it is the production path (docs/ADR/0007).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"

	"mes"
	"platformserver"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8490", "listen address")
	database := flag.String("database", "", "PostgreSQL URL of the journal (empty: memory only)")
	issuer := flag.String("oidc-issuer", "", "OpenID issuer whose access tokens are accepted (empty: demo tokens)")
	keys := flag.String("oidc-keys", "", "JWKS URL of the issuer, when the server reaches it on another address")
	directory := flag.String("directory", "", "JSON file: subject (user:<email>, client:<id>) → principal")
	disable := flag.String("disable", "", "comma-separated capabilities to deactivate (ADR-0008)")
	flag.Parse()
	const tenant = "plant-sz"
	plant := mes.NewPlant(tenant, mes.DemoMaster())
	for _, c := range strings.FieldsFunc(*disable, func(r rune) bool { return r == ',' }) {
		if !plant.Disable(c) {
			log.Fatalf("-disable: no capability %q", c)
		}
	}
	for _, d := range mes.DemoConnectors(tenant) {
		plant.RegisterConnector(d)
	}
	if *database != "" {
		ctx := context.Background()
		journal, err := platformserver.OpenJournal(ctx, *database)
		if err != nil {
			log.Fatalf("journal: %v", err)
		}
		entries, err := journal.Entries(ctx, tenant)
		if err != nil {
			log.Fatalf("journal: %v", err)
		}
		if err := plant.Replay(entries); err != nil {
			log.Fatalf("replay: %v", err)
		}
		log.Printf("replayed %d entries for %s", len(entries), tenant)
		// Fail-stop: an input the journal did not take is never answered; the
		// client's outbox resends it after the restart has replayed the rest.
		plant.Record = func(e platformserver.Entry) {
			if err := journal.Append(ctx, tenant, e); err != nil {
				log.Fatalf("journal append: %v", err)
			}
		}
	}
	authenticate := platformserver.Tokens(map[string]mes.Principal{
		"supervisor":  {ID: "sup-1", Tenant: tenant, Role: mes.Supervisor, Lines: []string{"L1", "L2"}},
		"operator-l1": {ID: "op-l1", Tenant: tenant, Role: mes.Operator, Lines: []string{"L1"}},
		"operator-l2": {ID: "op-l2", Tenant: tenant, Role: mes.Operator, Lines: []string{"L2"}},
		"quality-1":   {ID: "qa-1", Tenant: tenant, Role: mes.Quality},
		"quality-2":   {ID: "qa-2", Tenant: tenant, Role: mes.Quality},
		"gateway-l1":  {ID: "gateway-l1", Tenant: tenant, Role: mes.Gateway},
		"erp":         {ID: "erp", Tenant: tenant, Role: mes.ERP},
	})
	if *issuer != "" {
		raw, err := os.ReadFile(*directory)
		var people map[string]mes.Principal
		if err == nil {
			err = json.Unmarshal(raw, &people)
		}
		if err != nil {
			log.Fatalf("directory: %v", err)
		}
		if *keys == "" {
			log.Fatal("-oidc-keys is required with -oidc-issuer")
		}
		authenticate = platformserver.OIDC(*issuer, *keys, func(subject string) (mes.Principal, bool) {
			p, ok := people[subject]
			return p, ok
		})
	}
	log.Printf("mes-server on http://%s (tenant %s)", *addr, tenant)
	log.Fatal(http.ListenAndServe(*addr, mes.NewServer(map[string]*mes.Plant{tenant: plant}, authenticate)))
}
