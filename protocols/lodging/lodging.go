// Package lodging is the lodging booking protocol (ADR-0011): what any app that
// sells stays provides — hotels, serviced apartments, coworking spaces — and what
// any app that needs stays consumes, without either knowing the other. It follows
// the core of OpenTravel/HTNG reservations: reserve, change and cancel a stay of
// a room type, and read the bookings. A consumer that needs rooms before it is
// sure holds them until a date, then confirms or releases them (ADR-0026 D3);
// the provider releases a hold whose date has passed.
package lodging

import "platformserver/platform"

const ID = "lodging.booking/1"

// Booking is the protocol's read shape.
type Booking struct {
	ID       string `json:"id"`
	RoomType string `json:"roomType"`
	CheckIn  string `json:"checkIn"`
	CheckOut string `json:"checkOut"`
	Guest    string `json:"guest"`
	Status   string `json:"status"`          // held, booked, canceled or released
	Until    string `json:"until,omitempty"` // a hold's last day
}

// A booking's statuses: a hold takes a room until its date; a booking keeps it.
const (
	Held     = "held"
	Booked   = "booked"
	Canceled = "canceled"
	Released = "released"
)

// Open reports whether the booking takes a room.
func (b Booking) Open() bool { return b.Status == Held || b.Status == Booked }

func Protocol() platform.Protocol {
	stay := []platform.Field{{Name: "roomType", Type: "string", Required: true, Description: "Room type the provider sells"},
		{Name: "checkIn", Type: "date", Required: true, Description: "First night (YYYY-MM-DD; hourly types YYYY-MM-DDTHH:MM)"},
		{Name: "checkOut", Type: "date", Required: true, Description: "Departure, exclusive"}}
	return platform.Protocol{Name: "lodging.booking", Version: 1,
		Actions: []platform.Action{
			{Schema: "reserve", Title: "Reserve stay", Description: "Reserve a stay; refused when the provider cannot sell it.",
				Payload: append(stay, platform.Field{Name: "guest", Type: "string", Required: true, Description: "Guest name"})},
			{Schema: "change", Title: "Change stay", Description: "Change the room type or dates of a booking.", Payload: stay},
			{Schema: "cancel", Title: "Cancel booking", Description: "Cancel a booking; it stays in the history.", Payload: []platform.Field{}},
			{Schema: "hold", Title: "Hold stay", Description: "Hold a room for a stay until a date, without booking it; refused when the provider cannot sell it.",
				Payload: append(append([]platform.Field{}, stay...), platform.Field{Name: "guest", Type: "string", Required: true, Description: "Guest or group name"},
					platform.Field{Name: "until", Type: "date", Required: true, Description: "The hold's last day; the provider releases it after"})},
			{Schema: "confirm", Title: "Confirm hold", Description: "Book a held stay.", Payload: []platform.Field{}},
			{Schema: "release", Title: "Release hold", Description: "Give a held room back.", Payload: []platform.Field{}},
		},
		Reads: []string{"bookings"},
		Events: []platform.ProtocolEvent{{Name: "changed", Title: "Booking changed"}, {Name: "canceled", Title: "Booking canceled"},
			{Name: "confirmed", Title: "Hold confirmed"}, {Name: "released", Title: "Hold released"}},
	}
}
