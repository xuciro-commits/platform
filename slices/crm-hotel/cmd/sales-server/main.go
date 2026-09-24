// Command sales-server runs the composed Hotel + CRM software for one demo tenant.
package main

import (
	"flag"
	"log"
	"net/http"

	"crmhotel"
	"hotel"
	"platformserver"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8495", "listen address")
	flag.Parse()
	const tenant = "hotel-a"
	rooms := map[string]hotel.RoomType{"standard": {Rooms: 3, Overbooking: 1}, "suite": {Rooms: 1}}
	member := func(id string, roles map[string]string) crmhotel.Member {
		return crmhotel.Member{ID: id, Tenant: tenant, Roles: roles}
	}
	tokens := platformserver.Tokens(map[string]crmhotel.Member{
		"sales":      member("sales-1", map[string]string{"crm": "sales", "hotel": "front-desk"}),
		"sales-only": member("sales-2", map[string]string{"crm": "sales"}),
		"manager":    member("manager-1", map[string]string{"crm": "sales-manager", "hotel": "manager"}),
		"desk":       member("desk-1", map[string]string{"hotel": "front-desk"}),
	})
	log.Printf("sales-server on http://%s (tenant %s: hotel, crm, crm-hotel)", *addr, tenant)
	log.Fatal(http.ListenAndServe(*addr, crmhotel.NewServer(map[string]*crmhotel.Tenant{tenant: crmhotel.NewTenant(tenant, rooms)}, tokens)))
}
