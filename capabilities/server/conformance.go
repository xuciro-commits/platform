package platformserver

import (
	"encoding/json"
	"net/http"
	controlowner "platformserver/kernel/control"
	"platformserver/platform"
	"slices"
	"testing"
	"time"
)

func CheckReplay(t testing.TB, live *Tenant, journal []Entry, build func() *Tenant) {
	t.Helper()
	observe := func(tenant *Tenant) string {
		t.Helper()
		image, err := checkedSnapshot(tenant)
		if err != nil {
			t.Fatalf("observable tenant metadata: %v", err)
		}
		return image
	}
	stored := make([]Entry, len(journal))
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
	want := observe(live)
	if got := observe(again); got != want {
		t.Fatalf("replayed tenant differs from the live one:\nlive:   %s\nreplay: %s", want, got)
	}
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
		if got := observe(restored); got != want {
			t.Fatalf("a snapshot after %d of %d entries, then the rest, differs from the live tenant:\nlive:     %s\nrestored: %s", k, len(stored), want, got)
		}
	}
}

func snapshot(t *Tenant) string {
	image, err := checkedSnapshot(t)
	if err != nil {
		panic(err)
	}
	return image
}

func checkedSnapshot(t *Tenant) (string, error) {
	if t.currentReadScopes {
		t.mu.Lock()
		defer t.mu.Unlock()
		if err := t.recordReadRefusal(); err != nil {
			return "", err
		}
	}
	at := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
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
		if a.Manifest().ID == controlowner.PlatformApp {
			continue
		}
		for _, name := range a.Manifest().Reads {
			out, err := platform.CallRead(a, t.caller(everyone, a, false), name)
			reads[name] = []any{out, err}
		}
	}
	tasks := []any{}
	for _, x := range t.Tasks() {
		if x.Kind == "job" {
			tasks = append(tasks, []string{x.ID, x.State})
			continue
		}
		tasks = append(tasks, x)
	}
	endpoints := []any{}
	for _, e := range t.Endpoints() {
		endpoints = append(endpoints, []any{e.Endpoint, e.Pending, e.Delivers})
	}
	notices, _ := t.notices.State()
	bindings := []any{}
	for _, p := range t.Protocols() {
		bindings = append(bindings, []string{p.ID, p.Bound})
	}
	recordView := map[string]any{}
	func() {
		t.records.Lock()
		defer t.records.Unlock()
		for typ, et := range t.records.Types() {
			rows := map[string]any{}
			for id, r := range et.Rows().All() {
				rows[id] = []any{r.Value.Interface(), r.RecordHistory()}
			}
			recordView[typ] = rows
		}
	}()
	definitions, err := t.ownedAssetDefinitions(t.definitions)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal([]any{recordView, definitions, t.Audit(), t.committed.AllRefusals(), t.committed.AllAnswers(), t.releases.Candidates(), t.releases.Active(), t.releases.Applied(), t.Deliveries(), tasks, t.Effects(at), endpoints, notices, connectors, t.Settings(), bindings, reads})
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
