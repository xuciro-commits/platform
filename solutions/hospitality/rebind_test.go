package hospitality

import (
	"testing"

	"crm"
	"lodging"
	"pms"

	"platformserver"
)

// F-39: a hold made at the hotel is confirmed at the hotel after the
// administrator binds the lodging protocol to another provider; new holds go
// to the new one.
func TestHoldsStayWithTheirProvider(t *testing.T) {
	w := newWorld(t, hotelProvider, memoryProvider)
	w.setup()
	plan := func(id, key string) string {
		return w.submit("sales", crm.ID, crm.SchemaPlan, crm.OpportunityType, id, key,
			map[string]any{"rooms": 1, "roomType": "suite", "arrive": "2026-11-20", "depart": "2026-11-28", "cutoff": "2026-11-10"})
	}
	opportunity := func(id string) crm.Opportunity {
		v, err := w.tenant.RecordOf(w.members["manager"], crm.OpportunityType, id, t0)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return v.Record.(crm.Opportunity)
	}
	w.expect(plan("OPP-1", "plan-1"), "ok")
	w.expect(opportunity("OPP-1").Block, "held")
	w.expect(w.submit("admin", platformserver.PlatformApp, platformserver.SchemaProtocolBind, platformserver.ProtocolType, lodging.ID, "bind",
		map[string]string{"provider": "memstay"}), "ok")
	w.expect(w.submit("sales", crm.ID, crm.SchemaClose, crm.OpportunityType, "OPP-1", "close-1", map[string]string{"outcome": "won"}), "ok")
	o := opportunity("OPP-1")
	w.expect(o.Stage+" "+o.Block, "won confirmed")
	booked := ""
	for _, r := range w.reservations("manager") {
		if res := r.(pms.Reservation); res.ID == o.Stays[0].Booking {
			booked = res.Status
		}
	}
	w.expect(booked, "booked")

	// A new block goes to the provider now bound.
	w.expect(w.submit("sales", crm.ID, crm.SchemaOpen, crm.OpportunityType, "OPP-2", "open-2", map[string]string{"account": "ACME", "title": "Group 2"}), "ok")
	w.expect(plan("OPP-2", "plan-2"), "ok")
	w.expect(opportunity("OPP-2").Block, "held")
	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider, memoryProvider).tenant })
}
