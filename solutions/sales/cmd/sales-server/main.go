// Command sales-server runs the composed Hotel + CRM software on the platform host
// for one demo tenant; the deployment flags are platformserver's (ADR-0010).
package main

import (
	"flag"
	"log"

	"hotel"
	"platformserver"
	"sales"
)

func seat(token, id string, roles map[string]string) platformserver.Seat {
	return platformserver.Seat{Subjects: []string{token}, Member: platformserver.Member{ID: id, Roles: roles}}
}

// demo is the directory without -directory; a development token is the subject.
var demo = []platformserver.Seat{
	withUnits(seat("sales", "sales-1", map[string]string{"crm": "sales", "hotel": "front-desk"}),
		platformserver.Membership{Unit: "sales-team", Role: "account executive", Primary: true},
		platformserver.Membership{Unit: "offsite-2026", Role: "project lead"}),
	seat("sales-only", "sales-2", map[string]string{"crm": "sales"}),
	withUnits(seat("manager", "manager-1", map[string]string{"crm": "sales-manager", "hotel": "manager", "platform": "admin", "org": "admin"}),
		platformserver.Membership{Unit: "hotel-a", Role: "general manager", Primary: true},
		platformserver.Membership{Unit: "hotel-a-co", Role: "director"},
		platformserver.Membership{Unit: "guest-committee", Role: "chair"}),
	withUnits(seat("desk", "desk-1", map[string]string{"hotel": "front-desk"}), platformserver.Membership{Unit: "front-office", Role: "receptionist", Primary: true}),
}

func withUnits(s platformserver.Seat, units ...platformserver.Membership) platformserver.Seat {
	s.Units = units
	return s
}

func main() {
	deployment := platformserver.Flags("127.0.0.1:8495")
	flag.Parse()
	t, err := sales.NewTenant("hotel-a", map[string]hotel.RoomType{"standard": {Rooms: 3, Overbooking: 1}, "suite": {Rooms: 1}}, deployment.Seats(demo)...)
	if err == nil {
		err = deployment.Serve(t)
	}
	log.Fatal(err)
}
