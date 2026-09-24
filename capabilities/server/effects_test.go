package platformserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
)

// receiver is a webhook consumer as ADR-0014 expects one: it checks the
// signature and keeps one copy per webhook-id.
type receiver struct {
	mu      sync.Mutex
	fail    bool
	calls   int
	kept    map[string]string // webhook-id → body
	invalid int
}

func (r *receiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	body, _ := io.ReadAll(req.Body)
	id, stamp := req.Header.Get("webhook-id"), req.Header.Get("webhook-timestamp")
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write([]byte(id + "." + stamp + "." + string(body)))
	if req.Header.Get("webhook-signature") != "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)) || req.Header.Get("Idempotency-Key") != id {
		r.invalid++
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.fail {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	if _, seen := r.kept[id]; !seen {
		r.kept[id] = string(body)
	}
	w.WriteHeader(http.StatusNoContent)
}

func TestOutboundEffects(t *testing.T) {
	sink := &receiver{kept: map[string]string{}}
	server := httptest.NewServer(sink)
	defer server.Close()
	var journal []Entry
	build := func() *Tenant {
		dir := NewDirectory("t-1", Seat{Subjects: []string{"ana"}, Member: Member{ID: "ana", Roles: map[string]string{"a": "writer", PlatformApp: Admin}}},
			Seat{Subjects: []string{"bo"}, Member: Member{ID: "bo", Roles: map[string]string{"a": "writer"}}})
		tn, err := NewTenant("t-1", dir, newNotes("t-1", "a", ""))
		if err != nil {
			t.Fatal(err)
		}
		tn.Secrets = func(name string) ([]byte, bool) { return []byte("s3cret"), name == "hook" }
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) {
		raw, _ := json.Marshal(e)
		var stored Entry
		json.Unmarshal(raw, &stored)
		journal = append(journal, stored)
	}
	ana, _ := tn.app(PlatformApp).(*Directory).Member("ana")
	bo, _ := tn.app(PlatformApp).(*Directory).Member("bo")
	t0 := time.Date(2026, 9, 24, 9, 0, 0, 0, time.UTC)
	keys := 0
	decide := func(tn *Tenant, m Member, schema, typ, id string, payload any) string {
		keys++
		raw, _ := json.Marshal(payload)
		_, err := tn.Submit(m, &pb.Submission{TenantId: "t-1", PrincipalId: m.ID, Authority: PlatformApp, IdempotencyKey: fmt.Sprint("d", keys),
			Target: &pb.EntityRef{Type: typ, Id: id}, Schema: &pb.SchemaRef{Name: schema, Version: 1}, Payload: raw}, t0)
		if err != nil {
			return err.Error()
		}
		return "ok"
	}
	write := func(tn *Tenant, key, text string, at time.Time) {
		t.Helper()
		if _, err := tn.Submit(ana, note("t-1", "ana", "a", key, "x", text), at); err != nil {
			t.Fatal(err)
		}
	}
	endpoint := map[string]any{"url": server.URL + "/hook", "secret": "hook", "events": []string{"a.note"}, "allowPrivate": true}
	if got := decide(tn, bo, SchemaEndpointAdd, EndpointType, "crm-sync", endpoint); got != "ERROR_CODE_POLICY_DENIED" {
		t.Fatalf("a member without the platform role added an endpoint: %s", got)
	}
	if got := decide(tn, ana, SchemaEndpointAdd, EndpointType, "plain", map[string]any{"url": "http://example.com/x", "secret": "hook", "events": []string{"a.note"}}); got != "ERROR_CODE_INVALID_ARGUMENT" {
		t.Fatalf("plain http to a public address was accepted: %s", got)
	}
	if got := decide(tn, ana, SchemaEndpointAdd, EndpointType, "crm-sync", endpoint); got != "ok" {
		t.Fatal(got)
	}

	// A decision becomes an effect; the dispatcher delivers it signed, with its key.
	write(tn, "k1", "one", t0)
	tn.Dispatch(t0)
	e := tn.Effects(t0)
	if len(e) != 1 || e[0].State != "delivered" || len(sink.kept) != 1 || sink.invalid != 0 || sink.kept[e[0].ID] == "" {
		t.Fatalf("effects %+v, receiver kept %d (invalid %d)", e, len(sink.kept), sink.invalid)
	}
	var body struct {
		Type string
		Data struct{ Entity, Principal string }
	}
	json.Unmarshal([]byte(sink.kept[e[0].ID]), &body)
	if body.Type != "a.note" || body.Data.Entity != "a.topic/x" || body.Data.Principal != "ana" {
		t.Fatalf("body %s", sink.kept[e[0].ID])
	}

	// The receiver fails: the effect retries with backoff and shows its state;
	// later effects to the same endpoint wait behind it (ordered per endpoint).
	sink.fail = true
	write(tn, "k2", "two", t0.Add(time.Minute))
	write(tn, "k3", "three", t0.Add(time.Minute))
	tn.Dispatch(t0.Add(time.Minute))
	tn.Dispatch(t0.Add(time.Minute + time.Second)) // not due yet
	e = tn.Effects(t0)
	if e[1].State != "retrying" || e[1].Attempts != 1 || e[1].Error != "503 Service Unavailable" || e[0].State != "pending" || sink.calls != 2 {
		t.Fatalf("after a failure: %+v, %d calls", e[:2], sink.calls)
	}
	if v := tn.Endpoints(); v[0].Health != "failing" || v[0].Pending != 2 || v[0].Delivers != 1 {
		t.Fatalf("endpoint %+v", v[0])
	}
	sink.fail = false
	// The attempt reaches the receiver, then the process stops before journaling
	// its outcome: the journal so far is what a restart finds.
	crashed := len(journal)
	tn.Dispatch(t0.Add(time.Hour))
	if e := tn.Effects(t0); e[1].State != "delivered" || len(sink.kept) != 2 {
		t.Fatalf("after recovery: %+v", e)
	}
	restarted := build()
	calls := 0
	restarted.Outbound = func(req *http.Request, allow bool) (*http.Response, error) { calls++; return guarded(req, allow) }
	if err := restarted.Replay(journal[:crashed]); err != nil {
		t.Fatal(err)
	}
	if calls != 0 {
		t.Fatal("the replay called out")
	}
	restarted.Dispatch(t0.Add(2 * time.Hour)) // resends "two" with the same key, then sends "three"
	restarted.Dispatch(t0.Add(2 * time.Hour))
	if len(sink.kept) != 3 || calls != 2 {
		t.Fatalf("receiver kept %d copies after %d calls: a resend must be deduplicated by its key", len(sink.kept), calls)
	}

	// A full replay applies every recorded outcome and sends nothing.
	again := build()
	again.Outbound = func(*http.Request, bool) (*http.Response, error) { t.Fatal("replay called out"); return nil, nil }
	if err := again.Replay(journal); err != nil {
		t.Fatal(err)
	}
	states := func(tn *Tenant) string {
		var out []string
		for _, x := range tn.Effects(t0) {
			out = append(out, fmt.Sprint(x.State, x.Attempts))
		}
		return fmt.Sprint(out)
	}
	if states(again) != states(tn) {
		t.Fatalf("replayed effects %s, recorded %s", states(again), states(tn))
	}

	// An endpoint on a private address without permission is refused at connect time.
	if got := decide(tn, ana, SchemaEndpointAdd, EndpointType, "strict", map[string]any{"url": "https://127.0.0.1:1/x", "secret": "hook", "events": []string{"a.note"}}); got != "ok" {
		t.Fatal(got)
	}
	tn.Dispatch(t0.Add(3 * time.Hour))
	write(tn, "k4", "four", t0.Add(3*time.Hour))
	tn.Dispatch(t0.Add(3 * time.Hour))
	for _, x := range tn.Effects(t0) {
		if x.Endpoint == "strict" && (x.State != "rejected" || x.Error != errPrivate.Error()) {
			t.Fatalf("private address: %+v", x)
		}
	}
	// A rejected effect is retried as a decision, and discarded as one.
	var rejected string
	for _, x := range tn.Effects(t0) {
		if x.Endpoint == "strict" {
			rejected = x.ID
		}
	}
	if got := decide(tn, ana, SchemaEffectRetry, EffectType, rejected, struct{}{}); got != "ok" {
		t.Fatal(got)
	}
	if got := decide(tn, ana, SchemaEffectDiscard, EffectType, rejected, struct{}{}); got != "ok" {
		t.Fatal(got)
	}
}
