package hospitality

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"crm"
	"lodging"
	"pms"

	"platformserver"
)

// ADR-0026 D6: the group block. Planned rooms are held at the hotel until a
// cutoff; won, they are confirmed; lost, or past the cutoff, released. Each
// answer of the hotel lands on the opportunity; a block the hotel cannot hold
// whole fails and gives back what it held; with no provider, people answer.
func TestGroupBlock(t *testing.T) {
	w := newWorld(t, hotelProvider) // one suite
	w.setup()
	opportunity := func(w *world, id string) crm.Opportunity {
		v, err := w.tenant.RecordOf(w.members["manager"], crm.OpportunityType, id, t0)
		if err != nil {
			t.Fatalf("%s: %v", id, err)
		}
		return v.Record.(crm.Opportunity)
	}
	block := func(w *world, id string) string {
		o := opportunity(w, id)
		var rooms []string
		for _, st := range o.Stays {
			rooms = append(rooms, st.Booking+":"+st.Status)
		}
		return o.Block + " " + strings.Join(rooms, " ")
	}
	reservation := func(id string) string {
		for _, r := range w.reservations("manager") {
			if res := r.(pms.Reservation); res.ID == id {
				return res.Status
			}
		}
		return "none"
	}
	open := func(w *world, id string) {
		if id != "OPP-1" {
			w.expect(w.submit("sales", crm.ID, crm.SchemaOpen, crm.OpportunityType, id, "open-"+id, map[string]string{"account": "ACME", "title": "Group " + id}), "ok")
		}
	}
	plan := func(w *world, id string, rooms int, arrive, cutoff string) string {
		return w.submit("sales", crm.ID, crm.SchemaPlan, crm.OpportunityType, id, "plan-"+id+arrive,
			map[string]any{"rooms": rooms, "roomType": "suite", "arrive": arrive, "depart": arrive[:8] + "28", "cutoff": cutoff})
	}
	closeAs := func(w *world, id, outcome string) {
		w.expect(w.submit("sales", crm.ID, crm.SchemaClose, crm.OpportunityType, id, "close-"+id, map[string]string{"outcome": outcome}), "ok")
	}

	// Held, then won: confirmed, and the hotel's reservation is booked.
	open(w, "OPP-1")
	w.expect(plan(w, "OPP-1", 1, "2026-10-20", "2026-10-10"), "ok")
	w.expect(block(w, "OPP-1"), "held OPP-1-H1:held")
	w.expect(reservation("OPP-1-H1"), lodging.Held)
	closeAs(w, "OPP-1", "won")
	w.expect(block(w, "OPP-1")+" "+reservation("OPP-1-H1"), "confirmed OPP-1-H1:booked booked")

	// What the hotel cannot hold at all is refused at once, and nothing is recorded.
	open(w, "OPP-2")
	w.expect(plan(w, "OPP-2", 1, "2026-10-20", "2026-10-10"), "ERROR_CODE_CONFLICT")
	w.expect(block(w, "OPP-2"), " ")

	// Two suites where one exists: the second is refused, so the block fails
	// and the first is given back.
	w.expect(plan(w, "OPP-2", 2, "2026-11-20", "2026-11-10"), "ok")
	w.expect(block(w, "OPP-2")+" "+reservation("OPP-2-H1"), "failed OPP-2-H1:released OPP-2-H2:refused released")

	// Lost: released.
	open(w, "OPP-3")
	w.expect(plan(w, "OPP-3", 1, "2026-12-20", "2026-12-10"), "ok")
	closeAs(w, "OPP-3", "lost")
	w.expect(block(w, "OPP-3")+" "+reservation("OPP-3-H1"), "released OPP-3-H1:released released")

	// Past the cutoff, the hotel releases the room on its own and the
	// opportunity hears it.
	open(w, "OPP-4")
	w.expect(plan(w, "OPP-4", 1, "2026-12-20", "2026-09-30"), "ok")
	for at := t0; at.Before(t0.Add(8 * 24 * time.Hour)); at = at.Add(time.Hour) {
		w.tenant.Work(at)
	}
	w.expect(block(w, "OPP-4")+" "+reservation("OPP-4-H1"), "released OPP-4-H1:released released")
	told := w.timeline("sales", "crm.opportunity/OPP-4")
	w.expect(fmt.Sprint(len(told), " ", strings.Contains(fmt.Sprint(told), "Hold released")), "1 true")

	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })

	// No provider: the rooms wait for the hotel's answer, which a person records.
	alone := newWorld(t)
	alone.setup()
	w.expect(plan(alone, "OPP-1", 1, "2026-10-20", "2026-10-10"), "ok")
	w.expect(block(alone, "OPP-1"), "holding OPP-1-H1:asked")
	w.expect(alone.submit("sales", crm.ID, crm.SchemaAnswer, crm.OpportunityType, "OPP-1", "phone",
		map[string]string{"call": "OPP-1-H1", "action": "hold", "outcome": "accepted"}), "ok")
	closeAs(alone, "OPP-1", "won")
	w.expect(block(alone, "OPP-1"), "confirming OPP-1-H1:held")
	w.expect(alone.submit("sales", crm.ID, crm.SchemaAnswer, crm.OpportunityType, "OPP-1", "phone-2",
		map[string]string{"call": "OPP-1-H1", "action": "confirm", "outcome": "accepted"}), "ok")
	w.expect(block(alone, "OPP-1"), "confirmed OPP-1-H1:booked")
	platformserver.CheckReplay(t, alone.tenant, alone.journal, func() *platformserver.Tenant { return newWorld(t).tenant })
}
