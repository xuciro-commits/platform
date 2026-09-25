package crm

import (
	"fmt"
	"time"

	"lodging"
	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// groupStay is the group-stay flow's data: the bookings made so far.
type groupStay struct {
	Booked []string `json:"booked"`
}

// GroupStay books a won opportunity's planned rooms through the tenant's
// lodging provider, one by one, then asks the owner to confirm them with the
// customer within two days. Released, or unanswered, the bookings are canceled
// again, newest first (ADR-0020). The CRM knows no provider: it books through
// the lodging protocol, and the provider's own rules decide each room.
func GroupStay() platform.Flow {
	opp := func(c platform.Caller, r *platform.Run) Opportunity {
		o, _ := platform.Get[Opportunity](c, r.Key)
		return o
	}
	id := func(_ platform.Caller, r *platform.Run) string { return r.Key }
	return platform.Flow{Name: "group-stay", Title: "Group stay", Version: 1, Owners: []string{string(Manager)},
		Start: platform.Start{On: []string{SchemaClose}, Begin: func(c platform.Caller, e platform.Event) (string, any, bool) {
			o, _ := platform.Get[Opportunity](c, e.Record.GetSubmission().GetTarget().GetId())
			return o.ID, groupStay{Booked: []string{}}, o.Stage == "won" && o.Rooms > 0
		}},
		Steps: []platform.Step{
			{Name: "book", Title: "Book a room", Act: &platform.Act{Action: SchemaBook, Target: id,
				Payload: func(c platform.Caller, r *platform.Run) any {
					o := opp(c, r)
					return map[string]string{"roomType": o.RoomType, "checkIn": o.Arrive, "checkOut": o.Depart,
						"guest": fmt.Sprintf("%s, room %d", o.Title, len(platform.DataOf[groupStay](r).Booked)+1)}
				},
				Done: func(c platform.Caller, r *platform.Run, _ *pb.EntityRef) {
					g := platform.DataOf[groupStay](r)
					g.Booked = append(g.Booked, fmt.Sprintf("%s-B%d", r.Key, opp(c, r).Booked))
					r.Set(g)
				}},
				Undo: &platform.Act{Protocol: lodging.ID, Action: "cancel", Target: func(_ platform.Caller, r *platform.Run) string {
					g := platform.DataOf[groupStay](r)
					return g.Booked[len(g.Booked)-1]
				}},
				Choose: func(c platform.Caller, r *platform.Run) (string, string) {
					booked, rooms := len(platform.DataOf[groupStay](r).Booked), opp(c, r).Rooms
					if booked < rooms {
						return "book", fmt.Sprintf("%d of %d rooms booked", booked, rooms)
					}
					return "confirm", fmt.Sprintf("all %d rooms booked", rooms)
				}},
			{Name: "confirm", Title: "Confirm with the customer", Timeout: 48 * time.Hour, OnTimeout: platform.Compensate,
				Ask: &platform.Ask{Answers: []string{"confirmed", "release"},
					Title: func(c platform.Caller, r *platform.Run) string {
						o := opp(c, r)
						return fmt.Sprintf("Confirm %d rooms for %s with the customer", o.Rooms, o.Title)
					},
					Body: func(c platform.Caller, r *platform.Run) string {
						return "Released or unanswered in two days, the rooms are canceled again."
					},
					Ref: func(_ platform.Caller, r *platform.Run) string { return OpportunityType + "/" + r.Key },
					To: func(c platform.Caller, r *platform.Run) []platform.Recipient {
						return []platform.Recipient{{Member: opp(c, r).Owner}}
					}},
				Choose: func(_ platform.Caller, r *platform.Run) (string, string) {
					if r.Answer == "release" {
						return platform.Compensate, "the customer released the rooms"
					}
					return "", "the customer confirmed"
				}},
		}}
}
