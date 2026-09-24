// Command hotel-server runs the Hotel app on the platform host with two demo tenants.
package main

import (
	"flag"
	"log"
	"net/http"
	"strings"
	"time"

	"hotel"
	"platformserver"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8480", "listen address")
	delay := flag.Duration("response-delay", 0, "delay submission answers (reproduces client timeouts)")
	delayCount := flag.Int("delay-count", 0, "delay only the first N answers (0: all)")
	flag.Parse()
	rooms := map[string]hotel.RoomType{"standard": {Rooms: 3, Overbooking: 1}, "suite": {Rooms: 1},
		"apartment": {Rooms: 2, MinUnits: 28}, "meeting-room": {Rooms: 1, Hourly: true}, "hot-desk": {Rooms: 6, Hourly: true}}
	seat := func(token, id string, role hotel.Role) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{token}, Member: platformserver.Member{ID: id, Roles: map[string]string{"hotel": string(role)}}}
	}
	tenant := func(id string, seats ...platformserver.Seat) *platformserver.Tenant {
		t, err := platformserver.NewTenant(id, platformserver.NewConsole(id, seats...), hotel.NewHotel(id, rooms))
		if err != nil {
			log.Fatal(err)
		}
		return t
	}
	a, b := tenant("hotel-a", seat("desk-a", "desk-1", hotel.FrontDesk), seat("manager-a", "manager-1", hotel.Manager), seat("channel-a", "channel-sim", hotel.Channel)),
		tenant("hotel-b", seat("desk-b", "desk-7", hotel.FrontDesk))
	if err := a.Connect(hotel.ChannelConnector("channel-sim")); err != nil {
		log.Fatal(err)
	}
	platformserver.RunWork(a, b)
	host := platformserver.NewHost(platformserver.Tokens(map[string]string{"desk-a": "desk-a", "manager-a": "manager-a", "channel-a": "channel-a", "desk-b": "desk-b"}), a, b)
	handler, delayed := host.Handler(), 0
	log.Printf("hotel-server on http://%s (tenants hotel-a, hotel-b)", *addr)
	log.Fatal(http.ListenAndServe(*addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r) // the answer is buffered until this handler returns
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/submissions") && (*delayCount == 0 || delayed < *delayCount) {
			delayed++
			time.Sleep(*delay)
		}
	})))
}
