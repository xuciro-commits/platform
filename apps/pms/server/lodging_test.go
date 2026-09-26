package pms

import (
	"testing"

	"lodging/lodgingtest"
	"platformserver"
	"platformserver/platform"
)

// The hotel conforms to the lodging protocol it provides.
func TestHotelProvidesLodging(t *testing.T) {
	tn, err := platformserver.NewTenant("hotel-a", platformserver.NewConsole("hotel-a"), platformserver.NewRelations("hotel-a"),
		New("hotel-a", map[string]RoomType{"suite": {Rooms: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	lodgingtest.Conformance(t, tn, platform.Member{ID: "manager-1", Tenant: "hotel-a", Roles: map[string]string{"pms": string(Manager)}}, "suite")
}
