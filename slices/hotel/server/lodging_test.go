package hotel

import (
	"testing"

	"lodging"
	"platformserver"
)

// The hotel conforms to the lodging protocol it provides.
func TestHotelProvidesLodging(t *testing.T) {
	tn, err := platformserver.NewTenant("hotel-a", platformserver.NewConsole("hotel-a"), platformserver.NewRelations("hotel-a"),
		NewHotel("hotel-a", map[string]RoomType{"suite": {Rooms: 1}}))
	if err != nil {
		t.Fatal(err)
	}
	lodging.Conformance(t, tn, platformserver.Member{ID: "manager-1", Tenant: "hotel-a", Roles: map[string]string{"hotel": string(Manager)}}, "suite")
}
