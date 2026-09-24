package platformserver

import (
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

// CheckReplay is the platform's replay conformance (ADR-0007, ADR-0013,
// ADR-0014), for the tests of every composition: build makes a fresh tenant of
// the same apps, the journal is replayed into it with every outbound call
// failing the test, and what the host shows must equal the live tenant's —
// audit, deliveries, owned work, effects and endpoints, notifications, connector
// cursors and switches, settings, protocol bindings, and every read of every app
// as a member holding a role in each. Volatile state is left out, as it is not
// journaled by design: heartbeats, the last refused input, endpoint health (the
// secret store), and a job's run count and next run (runs that did nothing).
func CheckReplay(t testing.TB, live *Tenant, journal []Entry, build func() *Tenant) {
	t.Helper()
	stored := make([]Entry, len(journal)) // as the PostgreSQL journal keeps them
	for i, e := range journal {
		raw, _ := json.Marshal(e)
		json.Unmarshal(raw, &stored[i])
	}
	again := build()
	again.Outbound = func(*http.Request, bool) (*http.Response, error) {
		t.Fatal("replay made an outbound call")
		return nil, nil
	}
	if err := again.Replay(stored); err != nil {
		t.Fatalf("replay: %v", err)
	}
	if a, b := snapshot(live), snapshot(again); a != b {
		t.Fatalf("replayed tenant differs from the live one:\nlive:   %s\nreplay: %s", a, b)
	}
}

func snapshot(t *Tenant) string {
	at := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) // effect bodies are kept: nothing is 30 days old
	connectors := []any{}
	for _, c := range t.Connectors(at) {
		connectors = append(connectors, []any{c.ID, c.Cursor, c.Disabled})
	}
	everyone := Member{ID: "conformance", Tenant: t.ID, Roles: map[string]string{}}
	for _, a := range t.apps {
		if roles := a.Manifest().Actions.Roles(); len(roles) > 0 && !slices.Contains(roles, AnyMember) {
			everyone.Roles[a.Manifest().ID] = roles[0]
		}
	}
	reads := map[string]any{}
	for _, a := range t.apps {
		if a.Manifest().ID == PlatformApp {
			continue // its reads are the host views above
		}
		for _, name := range a.Manifest().Reads {
			out, err := a.Read(t.caller(everyone, a, false), name)
			reads[name] = []any{out, err}
		}
	}
	tasks := []any{}
	for _, x := range t.Tasks() {
		if x.Kind == "job" { // runs that did nothing are not journaled: a job's count and next run are volatile
			tasks = append(tasks, []string{x.ID, x.State})
			continue
		}
		tasks = append(tasks, x)
	}
	endpoints := []any{}
	for _, e := range t.Endpoints() { // health depends on the secret store, not the journal
		endpoints = append(endpoints, []any{e.Endpoint, e.Pending, e.Delivers})
	}
	t.opsMu.Lock()
	notices := slices.Clone(t.notices)
	t.opsMu.Unlock()
	bindings := []any{}
	for _, p := range t.Protocols() {
		bindings = append(bindings, []string{p.ID, p.Bound})
	}
	raw, _ := json.Marshal([]any{t.Audit(), t.Deliveries(), tasks, t.Effects(at), endpoints, notices, connectors, t.Settings(), bindings, reads})
	return string(raw)
}
