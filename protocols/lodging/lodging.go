// Package lodging is the lodging booking protocol (ADR-0011): what any app that
// sells stays provides — hotels, serviced apartments, coworking spaces — and what
// any app that needs stays consumes, without either knowing the other. It follows
// the core of OpenTravel/HTNG reservations: reserve, change and cancel a stay of
// a room type, and read the bookings.
package lodging

import "platformserver"

const ID = "lodging.booking/1"

// Booking is the protocol's read shape.
type Booking struct {
	ID       string `json:"id"`
	RoomType string `json:"roomType"`
	CheckIn  string `json:"checkIn"`
	CheckOut string `json:"checkOut"`
	Guest    string `json:"guest"`
	Canceled bool   `json:"canceled"`
}

func Protocol() platformserver.Protocol {
	stay := []platformserver.Field{{Name: "roomType", Type: "string", Required: true, Description: "Room type the provider sells"},
		{Name: "checkIn", Type: "date", Required: true, Description: "First night (YYYY-MM-DD; hourly types YYYY-MM-DDTHH:MM)"},
		{Name: "checkOut", Type: "date", Required: true, Description: "Departure, exclusive"}}
	return platformserver.Protocol{Name: "lodging.booking", Version: 1,
		Actions: []platformserver.Action{
			{Schema: "reserve", Title: "Reserve stay", Description: "Reserve a stay; refused when the provider cannot sell it.",
				Payload: append(stay, platformserver.Field{Name: "guest", Type: "string", Required: true, Description: "Guest name"})},
			{Schema: "change", Title: "Change stay", Description: "Change the room type or dates of a booking.", Payload: stay},
			{Schema: "cancel", Title: "Cancel booking", Description: "Cancel a booking; it stays in the history.", Payload: []platformserver.Field{}},
		},
		Reads:  []string{"bookings"},
		Events: []platformserver.ProtocolEvent{{Name: "changed", Title: "Booking changed"}, {Name: "canceled", Title: "Booking canceled"}},
	}
}
