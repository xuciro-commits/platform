package platformserver

import (
	"encoding/json"
	"net/http"
	"platformserver/platform"
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
	want := snapshot(live)
	if got := snapshot(again); got != want {
		t.Fatalf("replayed tenant differs from the live one:\nlive:   %s\nreplay: %s", want, got)
	}
	// A snapshot is a shortcut, never a different outcome (ADR-0019 D6): taken
	// after any prefix of the journal, restored into a new tenant, and given
	// the rest, it reaches the live state; saved again, it saves the same.
	for _, k := range slices.Compact([]int{0, len(stored) / 3, len(stored) / 2, len(stored)}) {
		before := build()
		if err := before.Replay(stored[:k]); err != nil {
			t.Fatalf("replay of %d entries: %v", k, err)
		}
		saved, _, err := before.Snapshot(func() int64 { return int64(k) })
		if err != nil {
			t.Fatalf("snapshot after %d entries: %v", k, err)
		}
		restored := build()
		restored.Outbound = again.Outbound
		if err := restored.Restore(saved); err != nil {
			t.Fatalf("restore of the snapshot after %d entries: %v", k, err)
		}
		if resaved, _, _ := restored.Snapshot(func() int64 { return 0 }); string(resaved) != string(saved) {
			t.Fatalf("a restored snapshot saves differently after %d entries:\nsaved:   %s\nresaved: %s", k, saved, resaved)
		}
		if err := restored.Replay(stored[k:]); err != nil {
			t.Fatalf("replay after the snapshot at %d: %v", k, err)
		}
		if got := snapshot(restored); got != want {
			t.Fatalf("a snapshot after %d of %d entries, then the rest, differs from the live tenant:\nlive:     %s\nrestored: %s", k, len(stored), want, got)
		}
	}
}

func snapshot(t *Tenant) string {
	at := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC) // effect bodies are kept: nothing is 30 days old
	connectors := []any{}
	for _, c := range t.Connectors(at) {
		connectors = append(connectors, []any{c.ID, c.Cursor, c.Disabled})
	}
	everyone := platform.Member{ID: "conformance", Tenant: t.ID, Roles: map[string]string{}}
	for _, a := range t.apps {
		if roles := a.Manifest().Actions.Roles(); len(roles) > 0 && !slices.Contains(roles, platform.AnyMember) {
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
	t.records.mu.Lock()
	records := map[string]any{}
	for typ, et := range t.records.types {
		rows := map[string]any{}
		for id, r := range et.rows {
			rows[id] = []any{r.value.Interface(), r.history}
		}
		records[typ] = rows
	}
	t.records.mu.Unlock()
	raw, _ := json.Marshal([]any{records, t.Audit(), t.Deliveries(), tasks, t.Effects(at), endpoints, notices, connectors, t.Settings(), bindings, reads})
	return string(raw)
}
