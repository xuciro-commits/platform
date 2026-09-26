package hospitality

import (
	"encoding/json"
	"testing"
	"time"

	"hcm"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver"
	"platformserver/apps/work"
	"platformserver/platform"
)

// ADR-0028 D7: an approval due in two working days of the requester's
// calendar. The office closes for National Day week and weekends; the front
// office works every day.
func TestWorkingDays(t *testing.T) {
	seat := func(id string, roles map[string]string, units ...platform.Membership) platformserver.Seat {
		return platformserver.Seat{Subjects: []string{id}, Member: platform.Member{ID: id, Roles: roles}, Units: units}
	}
	seats := []platformserver.Seat{
		seat("ana", map[string]string{"hcm": hcm.Employee}, platform.Membership{Unit: "sales-team", Role: "account executive", Primary: true}),
		seat("dan", map[string]string{"hcm": hcm.Employee}, platform.Membership{Unit: "front-office", Role: "receptionist", Primary: true}),
		seat("max", map[string]string{"hcm": hcm.HR}, platform.Membership{Unit: "hotel-a", Role: "manager"}),
	}
	tn, err := Compose("hotel-a", nil, seats...)
	if err != nil {
		t.Fatal(err)
	}
	wednesday := time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)
	due := func(who, leave string) string {
		m := platform.Member{ID: who, Tenant: "hotel-a", Roles: map[string]string{"hcm": hcm.Employee}}
		submit := func(schema string, payload any) {
			raw, _ := json.Marshal(payload)
			if _, err := tn.Submit(m, &pb.Submission{TenantId: "hotel-a", PrincipalId: who, Authority: hcm.ID, IdempotencyKey: schema + leave,
				Target: &pb.EntityRef{Type: hcm.LeaveType, Id: leave}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, wednesday); err != nil {
				t.Fatalf("%s %s: %v", who, schema, err)
			}
		}
		submit(hcm.SchemaCreate, map[string]string{"kind": "vacation", "from": "2026-11-02", "until": "2026-11-03"})
		submit(hcm.SchemaSubmit, struct{}{})
		out, _ := tn.Read(m, "requests")
		for _, r := range out.([]work.ApprovalRequest) {
			if r.Target == hcm.LeaveType+"/"+leave && len(r.Levels) > 0 {
				return r.Levels[0].Due.Format(time.DateOnly)
			}
		}
		t.Fatalf("no approval for %s", leave)
		return ""
	}
	// Wednesday before National Day: Thursday to Wednesday are closed, then Thursday and Friday count.
	if got := due("ana", "L-1"); got != "2026-10-09" {
		t.Fatalf("the office's due day %s", got)
	}
	if got := due("dan", "L-2"); got != "2026-10-02" {
		t.Fatalf("the front office's due day %s", got)
	}
}
