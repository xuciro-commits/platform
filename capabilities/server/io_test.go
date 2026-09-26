package platformserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/platform"
)

// ADR-0027 10b: work on the outside runs side by side, a failing destination
// opens its breaker, and what gives up is told to the administrators.
func TestIOLane(t *testing.T) {
	t0 := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)

	// Breakers: five failures in a row open one; after the pause one probe
	// goes through; a failed probe doubles the pause; a success closes it.
	var b breakers
	for range breakerAfter {
		if !b.allow("x", t0) {
			t.Fatal("closed breaker refused")
		}
		b.report("x", false, t0)
	}
	if b.allow("x", t0.Add(breakerPause-time.Second)) {
		t.Fatal("an open breaker let work through")
	}
	if !b.allow("x", t0.Add(breakerPause)) || b.allow("x", t0.Add(breakerPause)) {
		t.Fatal("after the pause exactly one probe goes through")
	}
	b.report("x", false, t0.Add(breakerPause))
	if b.allow("x", t0.Add(2*breakerPause)) || !b.allow("x", t0.Add(3*breakerPause)) {
		t.Fatal("a failed probe doubles the pause")
	}
	b.report("x", true, t0.Add(3*breakerPause))
	if !b.allow("x", t0.Add(3*breakerPause)) || !b.allow("x", t0.Add(3*breakerPause)) {
		t.Fatal("a success closes the breaker")
	}

	// A slow endpoint does not hold back another: they are sent side by side.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer slow.Close()
	fast := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer fast.Close()
	dir := NewConsole("t-1", Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{"a": "writer", PlatformApp: Admin}}})
	tn, err := NewTenant("t-1", dir, newNotes("t-1", "a"))
	if err != nil {
		t.Fatal(err)
	}
	tn.Secrets = func(string) ([]byte, bool) { return []byte("s3cret"), true }
	ana, _ := dir.Member("ana")
	keys := 0
	submit := func(s *pb.Submission) {
		t.Helper()
		if _, err := tn.Submit(ana, s, t0); err != nil {
			t.Fatal(err)
		}
	}
	for id, url := range map[string]string{"slow": slow.URL, "fast": fast.URL} {
		keys++
		raw, _ := json.Marshal(map[string]any{"url": url, "secret": "hook", "events": []string{"a.note"}, "allowPrivate": true})
		submit(&pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: PlatformApp, IdempotencyKey: fmt.Sprint("e", keys),
			Target: &pb.EntityRef{Type: EndpointType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaEndpointAdd, Version: 1}, Payload: raw})
	}
	submit(note("t-1", "ana", "a", "k1", "x", "one"))
	started := time.Now()
	tn.Dispatch(t0)
	if took := time.Since(started); took > 280*time.Millisecond {
		t.Fatalf("dispatch took %v: the endpoints were not sent side by side", took)
	}
	states := func() string {
		var out []string
		for _, e := range tn.Effects(t0) {
			out = append(out, e.Endpoint+" "+e.State)
		}
		slices.Sort(out)
		return strings.Join(out, ", ")
	}
	if got := states(); got != "fast delivered, slow retrying" {
		t.Fatalf("effects: %s", got)
	}

	// The slow endpoint keeps failing: its breaker opens and Settings shows it;
	// once the effect gives up, the administrators are told.
	at := t0
	for range effectRetry.Attempts + breakerAfter {
		at = at.Add(2 * time.Hour)
		tn.Dispatch(at)
	}
	if got := states(); got != "fast delivered, slow failed" {
		t.Fatalf("effects after hours of failures: %s", got)
	}
	if bs := tn.Breakers(at); len(bs) != 1 || bs[0].Destination != "endpoint:slow" || bs[0].Failures < breakerAfter {
		t.Fatalf("breakers %+v", bs)
	}
	if h := tn.Health(at); h.Status != "degraded" || h.OpenBreakers != 1 || len(h.Breakers) != 1 {
		t.Fatalf("health %+v", h)
	}
	told := tn.notificationsFor("ana")
	if !slices.ContainsFunc(told, func(n platform.Notification) bool { return strings.HasPrefix(n.Title, "Effect to slow failed") }) {
		t.Fatalf("the administrator was not told: %+v", told)
	}
}
