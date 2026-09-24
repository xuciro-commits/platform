package mes

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

// NewServer serves plants through the platform server capability, plus the plant's
// reads and its two connectors (gateway push, ERP poll).
func NewServer(plants map[string]*Plant, authenticate platformserver.Authenticate[Principal]) http.Handler {
	s := platformserver.New(plants, authenticate)
	s.Kernel(func(who Principal, p *Plant, sub *pb.Submission, now time.Time) (*pb.ChangeRecord, *kernel.Error) {
		return p.Submit(who, sub, now)
	}, (*Plant).Declarations)
	s.Handle("POST /v1/connectors/states", func(w http.ResponseWriter, r *http.Request, who Principal, p *Plant) {
		var b StateBatch
		if json.NewDecoder(r.Body).Decode(&b) != nil {
			platformserver.Reply(w, nil, invalid)
			return
		}
		_, err := p.DeliverStates(who, b, s.Now())
		platformserver.Reply(w, nil, err)
	})
	s.Handle("POST /v1/connectors/planned-orders", func(w http.ResponseWriter, r *http.Request, who Principal, p *Plant) {
		var page PlannedPage
		if json.NewDecoder(r.Body).Decode(&page) != nil {
			platformserver.Reply(w, nil, invalid)
			return
		}
		platformserver.Reply(w, nil, p.DeliverPlanned(who, page, s.Now()))
	})
	s.Handle("POST /v1/connectors/heartbeat", func(w http.ResponseWriter, _ *http.Request, who Principal, p *Plant) {
		platformserver.Reply(w, nil, p.Heartbeat(who, s.Now()))
	})
	s.Read("/v1/master", func(_ Principal, p *Plant) any { return p.Master() })
	s.Read("/v1/orders", func(_ Principal, p *Plant) any { return p.Orders() })
	s.Read("/v1/sfcs", func(_ Principal, p *Plant) any { return p.SFCs() })
	s.Read("/v1/planned-orders", func(_ Principal, p *Plant) any { return p.Planned() })
	s.Read("/v1/downtime", func(_ Principal, p *Plant) any { return p.Downtime() })
	s.Read("/v1/connectors", func(_ Principal, p *Plant) any { return p.Connectors(s.Now()) })
	return s.Handler()
}
