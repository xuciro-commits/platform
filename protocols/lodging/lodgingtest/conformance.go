// Package lodgingtest checks a tenant's provider of the lodging protocol. It
// drives a tenant, so it is apart from the protocol, which apps import.
package lodgingtest

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"lodging"
	"platformserver"
	"platformserver/apps/relations"
	"platformserver/platform"
)

// Conformance checks that a tenant's provider of the lodging protocol behaves as the
// protocol says. The tenant must run the platform's Relations; member must be
// allowed to reserve, change and cancel with the provider (ADR-0011).
func Conformance(t *testing.T, tenant *platformserver.Tenant, member platform.Member, roomType string) {
	t.Helper()
	now := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	call := func(action, id, key string, payload any) string {
		raw, _ := json.Marshal(payload)
		_, _, err := tenant.Invoke(member, lodging.ID, action, id, raw, key, now)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	booking := func(id string) (lodging.Booking, bool) {
		out, err := tenant.Query(member, lodging.ID, "bookings")
		if err != nil {
			t.Fatalf("bookings: %v", err)
		}
		all := out.([]lodging.Booking)
		i := slices.IndexFunc(all, func(b lodging.Booking) bool { return b.ID == id })
		if i < 0 {
			return lodging.Booking{}, false
		}
		return all[i], true
	}
	expect := func(step, got, want string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %s, want %s", step, got, want)
		}
	}
	stay := map[string]string{"roomType": roomType, "checkIn": "2026-11-01", "checkOut": "2026-11-03", "guest": "Ada"}
	expect("reserve", call("reserve", "LB-1", "c1", stay), "ok")
	expect("resend", call("reserve", "LB-1", "c1", stay), "ok")
	expect("reserve an existing booking", call("reserve", "LB-1", "c2", stay), "ERROR_CODE_CONFLICT")
	if b, ok := booking("LB-1"); !ok || b.Guest != "Ada" || b.RoomType != roomType || b.CheckIn != "2026-11-01" || b.Status != lodging.Booked {
		t.Fatalf("booking after reserve: %+v", b)
	}
	expect("change", call("change", "LB-1", "c3", map[string]string{"roomType": roomType, "checkIn": "2026-11-01", "checkOut": "2026-11-04"}), "ok")
	if b, _ := booking("LB-1"); b.CheckOut != "2026-11-04" {
		t.Fatalf("booking after change: %+v", b)
	}
	expect("cancel", call("cancel", "LB-1", "c4", struct{}{}), "ok")
	expect("cancel again", call("cancel", "LB-1", "c5", struct{}{}), "ERROR_CODE_NOT_FOUND")
	if b, _ := booking("LB-1"); b.Status != lodging.Canceled {
		t.Fatalf("booking after cancel: %+v", b)
	}
	// Holds (ADR-0026 D3): held until a date, then confirmed or released; the
	// provider releases a hold whose last day has passed.
	hold := func(until string) map[string]string {
		return map[string]string{"roomType": roomType, "checkIn": "2026-12-01", "checkOut": "2026-12-02", "guest": "Group", "until": until}
	}
	expect("hold without a date", call("hold", "LB-2", "h0", map[string]string{"roomType": roomType, "checkIn": "2026-12-01", "checkOut": "2026-12-02", "guest": "Group"}), "ERROR_CODE_INVALID_ARGUMENT")
	expect("hold", call("hold", "LB-2", "h1", hold("2026-11-15")), "ok")
	if b, _ := booking("LB-2"); b.Status != lodging.Held || b.Until != "2026-11-15" {
		t.Fatalf("booking after hold: %+v", b)
	}
	expect("confirm", call("confirm", "LB-2", "h3", struct{}{}), "ok")
	expect("confirm again", call("confirm", "LB-2", "h4", struct{}{}), "ERROR_CODE_NOT_FOUND")
	expect("release a booking", call("release", "LB-2", "h5", struct{}{}), "ERROR_CODE_NOT_FOUND")
	if b, _ := booking("LB-2"); b.Status != lodging.Booked {
		t.Fatalf("booking after confirm: %+v", b)
	}
	expect("cancel the booking", call("cancel", "LB-2", "h6", struct{}{}), "ok")
	expect("hold again", call("hold", "LB-3", "h7", hold("2026-11-15")), "ok")
	expect("release", call("release", "LB-3", "h8", struct{}{}), "ok")
	if b, _ := booking("LB-3"); b.Status != lodging.Released || b.Open() {
		t.Fatalf("booking after release: %+v", b)
	}
	expect("hold until tomorrow", call("hold", "LB-4", "h9", hold("2026-09-25")), "ok")
	tenant.Work(now.AddDate(0, 0, 1))
	if b, _ := booking("LB-4"); b.Status != lodging.Held {
		t.Fatalf("a hold released on its last day: %+v", b)
	}
	tenant.Work(now.AddDate(0, 0, 2))
	if b, _ := booking("LB-4"); b.Status != lodging.Released {
		t.Fatalf("a hold past its last day: %+v", b)
	}
	timeline, _ := tenant.Read(member, "timeline")
	var told []string
	for _, n := range timeline.([]relations.Note) {
		if strings.HasSuffix(n.Entity, "/LB-1") {
			told = append(told, strings.SplitN(n.Text, " (", 2)[0])
		}
	}
	if !slices.Equal(told, []string{"Booking changed", "Booking canceled"}) {
		t.Fatalf("protocol events on the timeline: %v", told)
	}
}
