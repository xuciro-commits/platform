// Command hotel-server runs the Hotel reference slice with two demo tenants.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"hotel"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8480", "listen address")
	delay := flag.Duration("response-delay", 0, "delay every response (reproduces client timeouts)")
	delayCount := flag.Int("delay-count", 0, "delay only the first N responses (0: all)")
	flag.Parse()
	rooms := map[string]hotel.RoomType{"standard": {Rooms: 3, Overbooking: 1}, "suite": {Rooms: 1},
		"apartment": {Rooms: 2, MinUnits: 28}, "meeting-room": {Rooms: 1, Hourly: true}, "hot-desk": {Rooms: 6, Hourly: true}}
	server := &hotel.Server{
		Hotels: map[string]*hotel.Hotel{
			"hotel-a": hotel.NewHotel("hotel-a", rooms, hotel.DefaultPolicy),
			"hotel-b": hotel.NewHotel("hotel-b", rooms, hotel.DefaultPolicy),
		},
		Tokens: map[string]hotel.Principal{
			"desk-a":    {ID: "desk-1", Tenant: "hotel-a", Role: hotel.FrontDesk},
			"manager-a": {ID: "manager-1", Tenant: "hotel-a", Role: hotel.Manager},
			"channel-a": {ID: "channel-sim", Tenant: "hotel-a", Role: hotel.Channel},
			"desk-b":    {ID: "desk-7", Tenant: "hotel-b", Role: hotel.FrontDesk},
		},
		ResponseDelay: *delay,
		DelayCount:    *delayCount,
		Now:           time.Now,
	}
	log.Printf("hotel-server on http://%s (tenants hotel-a, hotel-b)", *addr)
	log.Fatal(http.ListenAndServe(*addr, server.Handler()))
}
