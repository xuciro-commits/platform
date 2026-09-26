package hospitality

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"crm"
	"pms"

	"platformserver"
	"platformserver/platform"
)

// ADR-0020 D9 (2): a flow across apps. A won opportunity's planned rooms are
// booked through the lodging protocol, one by one; the owner confirms them with
// the customer, or they are canceled again — released, unanswered for two
// days, or when the provider cannot sell them all.
func TestGroupStayFlow(t *testing.T) {
	w := newWorld(t, hotelProvider) // one suite
	w.setup()
	now := t0
	work := func(d time.Duration) {
		for end := now.Add(d); now.Before(end); now = now.Add(time.Second) {
			w.tenant.Work(now)
		}
	}
	admin := platform.Member{ID: "flows", Tenant: "hotel-a", Roles: map[string]string{platformserver.FlowApp: platformserver.FlowAdmin}}
	instance := func(opp string) platformserver.FlowInstance {
		page, _ := w.tenant.Records(admin, platformserver.InstanceType, platform.Query{Domain: json.RawMessage(`[["key","=","` + opp + `"]]`)}, now)
		if len(page.Records) == 0 {
			return platformserver.FlowInstance{}
		}
		return page.Records[0].(platformserver.FlowInstance)
	}
	trace := func(opp string) string {
		var out []string
		for _, l := range instance(opp).Trace {
			out = append(out, strings.TrimSpace(l.Step+" "+l.What))
		}
		return strings.Join(slices.Compact(out), ", ")
	}
	canceled := func() string {
		var out []string
		for _, r := range w.reservations("manager") {
			res := r.(pms.Reservation)
			out = append(out, fmt.Sprintf("%s:%v", res.ID, res.Canceled))
		}
		return strings.Join(out, " ")
	}
	win := func(opp string, rooms int, arrive, depart string) {
		t.Helper()
		if opp != "OPP-1" {
			w.expect(w.submit("sales", crm.ID, crm.SchemaOpen, crm.OpportunityType, opp, "open-"+opp, map[string]string{"account": "ACME", "title": "Group " + opp}), "ok")
		}
		w.expect(w.submit("sales", crm.ID, crm.SchemaPlan, crm.OpportunityType, opp, "plan-"+opp,
			map[string]any{"rooms": rooms, "roomType": "suite", "arrive": arrive, "depart": depart}), "ok")
		w.expect(w.submit("sales", crm.ID, crm.SchemaClose, crm.OpportunityType, opp, "won-"+opp, map[string]string{"outcome": "won"}), "ok")
	}
	answer := func(opp, choice string) {
		t.Helper()
		out, _ := w.tenant.Read(w.members["sales"], "inbox")
		tasks := out.([]platformserver.WorkTask)
		i := slices.IndexFunc(tasks, func(x platformserver.WorkTask) bool { return strings.Contains(x.Title, "Group "+opp) })
		if i < 0 {
			t.Fatalf("no task for %s in %v", opp, tasks)
		}
		w.expect(w.submit("sales", platformserver.WorkApp, "work.task.complete", platformserver.TaskType, tasks[i].ID, "answer-"+opp, map[string]string{"answer": choice}), "ok")
	}

	// Two suites wanted, one exists: the second booking is refused and retried,
	// then the first is canceled again.
	win("OPP-1", 2, "2026-10-01", "2026-10-03")
	work(time.Minute)
	w.expect(instance("OPP-1").State+" / "+canceled(), "compensated / OPP-1-B1:true")
	w.expect(trace("OPP-1"), "started, book acted, book chose, book retry, compensating, book undone, ended")

	// One suite: booked, then the owner confirms it with the customer.
	win("OPP-2", 1, "2026-10-01", "2026-10-03")
	work(2 * time.Second)
	w.expect(instance("OPP-2").State+" / "+canceled(), "waiting / OPP-1-B1:true OPP-2-B1:false")
	answer("OPP-2", "confirmed")
	work(2 * time.Second)
	w.expect(instance("OPP-2").State+" "+instance("OPP-2").Trace[len(instance("OPP-2").Trace)-2].Detail, "done the end: the customer confirmed")

	// Released by the customer: canceled again.
	win("OPP-3", 1, "2026-11-01", "2026-11-03")
	work(2 * time.Second)
	answer("OPP-3", "release")
	work(2 * time.Second)
	w.expect(instance("OPP-3").State+" / "+canceled(), "compensated / OPP-1-B1:true OPP-2-B1:false OPP-3-B1:true")

	// Unanswered for two days: canceled again, and the task closes.
	win("OPP-4", 1, "2026-12-01", "2026-12-03")
	work(2 * time.Second)
	now = now.Add(48 * time.Hour)
	work(2 * time.Second)
	w.expect(instance("OPP-4").State+" / "+canceled(), "compensated / OPP-1-B1:true OPP-2-B1:false OPP-3-B1:true OPP-4-B1:true")
	out, _ := w.tenant.Read(w.members["sales"], "inbox")
	w.expect(fmt.Sprint(len(out.([]platformserver.WorkTask))), "0")

	platformserver.CheckReplay(t, w.tenant, w.journal, func() *platformserver.Tenant { return newWorld(t, hotelProvider).tenant })
}
