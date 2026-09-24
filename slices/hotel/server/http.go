package hotel

import (
	"encoding/json"
	"net/http"
	"time"

	"platformserver"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformkernel/kernel"
)

func (p Principal) TenantID() string    { return p.Tenant }
func (p Principal) PrincipalID() string { return p.ID }

// NewServer serves the hotels through the platform server capability. responseDelay
// holds every submission answer back after processing (reproduces client timeouts);
// delayCount limits it to the first N answers (0: all).
func NewServer(hotels map[string]*Hotel, authenticate platformserver.Authenticate[Principal], responseDelay time.Duration, delayCount int) http.Handler {
	s := platformserver.New(hotels, authenticate)
	delayed := 0
	s.Kernel(func(p Principal, h *Hotel, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
		record, err := h.Submit(p, sub, now)
		if delayCount == 0 || delayed < delayCount {
			delayed++
			time.Sleep(responseDelay)
		}
		return record, err
	}, (*Hotel).Declarations)
	s.Handle("POST /v1/channel/bookings", func(w http.ResponseWriter, r *http.Request, p Principal, h *Hotel) {
		var b ChannelBooking
		if p.Role != Channel || json.NewDecoder(r.Body).Decode(&b) != nil {
			platformserver.Reply(w, nil, denied())
			return
		}
		record, err := h.IngestChannelBooking(p, b, s.Now())
		platformserver.Reply(w, record, err)
	})
	s.Read("/v1/reservations", func(_ Principal, h *Hotel) any { return h.Reservations() })
	return s.Handler()
}
