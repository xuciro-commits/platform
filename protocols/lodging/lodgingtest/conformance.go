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
	if b, ok := booking("LB-1"); !ok || b.Guest != "Ada" || b.RoomType != roomType || b.CheckIn != "2026-11-01" || b.Canceled {
		t.Fatalf("booking after reserve: %+v", b)
	}
	expect("change", call("change", "LB-1", "c3", map[string]string{"roomType": roomType, "checkIn": "2026-11-01", "checkOut": "2026-11-04"}), "ok")
	if b, _ := booking("LB-1"); b.CheckOut != "2026-11-04" {
		t.Fatalf("booking after change: %+v", b)
	}
	expect("cancel", call("cancel", "LB-1", "c4", struct{}{}), "ok")
	expect("cancel again", call("cancel", "LB-1", "c5", struct{}{}), "ERROR_CODE_NOT_FOUND")
	if b, _ := booking("LB-1"); !b.Canceled {
		t.Fatalf("booking after cancel: %+v", b)
	}
	timeline, _ := tenant.Read(member, "timeline")
	var told []string
	for _, n := range timeline.([]platformserver.Note) {
		if strings.HasSuffix(n.Entity, "/LB-1") {
			told = append(told, strings.SplitN(n.Text, " (", 2)[0])
		}
	}
	if !slices.Equal(told, []string{"Booking changed", "Booking canceled"}) {
		t.Fatalf("protocol events on the timeline: %v", told)
	}
}
