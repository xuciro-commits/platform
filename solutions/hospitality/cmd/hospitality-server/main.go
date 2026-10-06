// Command hospitality-server runs the hospitality solution (CRM, PMS, HCM, CSM) on the platform host
// for one demo tenant; the deployment flags are platformserver's (ADR-0010).
package main

import (
	"flag"
	"log"

	"hospitality"
	"lodging"
	"platformserver"
	"platformserver/apps/build"
	"platformserver/platform"
	"pms"
)

func seat(token, id string, roles map[string]string) platformserver.Seat {
	return platformserver.Seat{Subjects: []string{token}, Member: platform.Member{ID: id, Roles: roles}}
}

// demo is the directory without -directory; a development token is the subject.
var demo = []platformserver.Seat{
	withUnits(seat("sales", "sales-1", map[string]string{"crm": "sales", "pms": "front-desk", "memstay": lodging.Keeper, "ai": "user", "hcm": "employee"}),
		platform.Membership{Unit: "sales-team", Role: "account executive", Primary: true},
		platform.Membership{Unit: "offsite-2026", Role: "project lead"}),
	seat("sales-only", "sales-2", map[string]string{"crm": "sales"}),
	withUnits(seat("manager", "manager-1", map[string]string{"crm": "sales-manager", "pms": "manager", "memstay": lodging.Keeper, "platform": "admin", "enterprise": "admin", "ai": "admin", "hcm": "hr", "work": "admin", "flow": "admin", "agent": "admin", "csm": "lead", "knowledge": "editor", build.ID: build.Builder}),
		platform.Membership{Unit: "hotel-a", Role: "general manager", Primary: true},
		platform.Membership{Unit: "hotel-a", Role: "manager"}, platform.Membership{Unit: "hospitality", Role: "head"}, // HR's approvers (ADR-0017)
		platform.Membership{Unit: "hotel-a-co", Role: "director"},
		platform.Membership{Unit: "guest-committee", Role: "chair"}),
	withUnits(seat("desk", "desk-1", map[string]string{"pms": "front-desk", "hcm": "employee", "csm": "desk", build.ID: build.User}), platform.Membership{Unit: "front-office", Role: "receptionist", Primary: true}),
	// A separate business approver; a published object's role is granted by
	// the original Console, rather than giving this member builder/admin access.
	seat("business-supervisor", "business-supervisor-1", map[string]string{}),
}

func withUnits(s platformserver.Seat, units ...platform.Membership) platformserver.Seat {
	s.Units = units
	return s
}

func main() {
	deployment := platformserver.Flags("127.0.0.1:8496")
	flag.Parse()
	seats := deployment.Seats(demo)
	deployment.Rebuild = func(id string) (*platformserver.Tenant, error) {
		return hospitality.NewTenant(id, map[string]pms.RoomType{
			"standard": {Rooms: 3, Overbooking: 1}, "suite": {Rooms: 1}}, seats...)
	}
	t, err := deployment.Rebuild("hotel-a")
	if err == nil {
		err = deployment.Serve(t)
	}
	if err != nil {
		log.Fatal(err)
	}
}
