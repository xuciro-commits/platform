package gateway

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

// fakeRecords keeps call records the way the phone app does: each action moves a
// record only from the states the action allows, and a repeated key is answered
// with the record it wrote the first time.
type fakeRecords struct {
	mu      sync.Mutex
	recs    map[string]*Record
	keys    map[string]string
	actions []string
	changes chan struct{}
}

func newFakeRecords() *fakeRecords {
	return &fakeRecords{recs: map[string]*Record{}, keys: map[string]string{}, changes: make(chan struct{}, 1)}
}

var allowedFrom = map[string][]string{
	"create": {""}, "receive": {"ringing"}, "connect": {"ringing"},
	"finish": {"connected"}, "miss": {"ringing"}, "fail": {"ringing", "connected"},
}

func (f *fakeRecords) Submit(_ context.Context, action, id string, payload map[string]any, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got, ok := f.keys[key]; ok {
		return got, nil
	}
	name := action[len("phone.call."):]
	from, ok := allowedFrom[name]
	if !ok {
		return "", &Refused{Status: 200, Code: "unknown", Message: action}
	}
	r := f.recs[id]
	state := ""
	if r != nil {
		state = r.State
	}
	if !slices.Contains(from, state) {
		return "", &Refused{Status: 200, Code: "state", Message: fmt.Sprintf("%s from %q", action, state)}
	}
	text := func(k string) string { s, _ := payload[k].(string); return s }
	switch name {
	case "create":
		id = fmt.Sprintf("CALL-%d", len(f.recs)+1)
		r = &Record{ID: id, State: "ringing", Direction: "outbound", Number: text("number"), CalledAt: time.Now()}
		f.recs[id] = r
	case "receive":
		r.Direction = "inbound"
	case "connect":
		r.State, r.Agent, r.Answer = "connected", text("agent"), text("answer")
	case "finish":
		r.State = "completed"
	case "miss":
		r.State = "missed:" + text("reason")
	case "fail":
		r.State = "failed"
	}
	f.keys[key] = id
	f.actions = append(f.actions, name)
	f.notify()
	return id, nil
}

func (f *fakeRecords) Read(_ context.Context, id string) (Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r := f.recs[id]; r != nil {
		return *r, nil
	}
	return Record{}, &Refused{Status: 404, Code: "not_found"}
}

func (f *fakeRecords) Active(context.Context) ([]Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []Record
	for _, r := range f.recs {
		if r.State == "ringing" || r.State == "connected" {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (f *fakeRecords) Contact(context.Context, string) (string, string) { return "", "" }

func (f *fakeRecords) Changes() <-chan struct{} { return f.changes }

func (f *fakeRecords) notify() {
	select {
	case f.changes <- struct{}{}:
	default:
	}
}

// change edits a record as the browser would through take or hangup, then reports it.
func (f *fakeRecords) change(id string, edit func(*Record)) {
	f.mu.Lock()
	edit(f.recs[id])
	f.mu.Unlock()
	f.notify()
}

func (f *fakeRecords) put(r Record) {
	f.mu.Lock()
	f.recs[r.ID] = &r
	f.mu.Unlock()
	f.notify()
}

func (f *fakeRecords) wait(t *testing.T, what string, match func(Record) bool) Record {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		for _, r := range f.recs {
			if match(*r) {
				got := *r
				f.mu.Unlock()
				return got
			}
		}
		f.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no call record became %s; actions: %v", what, f.actions)
	return Record{}
}

func deskGateway(t *testing.T) (*fakeRecords, *trunk) {
	t.Helper()
	recs := newFakeRecords()
	d := NewDesk(recs, slog.New(slog.DiscardHandler))
	d.poll = 50 * time.Millisecond
	g, err := New(Config{Realm: testRealm, Trunks: map[string]string{testTrunk: testPassword}, RingTimeout: 5 * time.Second, Logger: slog.New(slog.DiscardHandler)}, d.Incoming)
	if err != nil {
		t.Fatal(err)
	}
	conn, port := listen(t)
	go g.Serve(conn)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		g.Close()
		conn.Close()
	})
	ready(t, g.ua, conn)
	d.Attach(g)
	go d.Run(ctx)
	tr := newTrunk(t, sip.Uri{Scheme: "sip", Host: "127.0.0.1", Port: port})
	if res := tr.register(t, testPassword, 300); res.StatusCode != sip.StatusOK {
		t.Fatalf("the trunk does not register: %d", res.StatusCode)
	}
	return recs, tr
}

func TestDeskConnectsIncomingCallToTheAgentWhoTakesIt(t *testing.T) {
	recs, tr := deskGateway(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	l, a := newLine(t), newAgent(t)
	recipient := sip.Uri{Scheme: "sip", User: "100", Host: tr.gateway.Host, Port: tr.gateway.Port}
	session, err := tr.dialogs.Invite(ctx, recipient, l.sdp(),
		sip.NewHeader("Content-Type", "application/sdp"),
		sip.NewHeader("P-Asserted-Identity", "<tel:"+testCaller+">"))
	if err != nil {
		t.Fatal(err)
	}
	waited := make(chan error, 1)
	go func() { waited <- session.WaitAnswer(ctx, answerOptions(testPassword, nil)) }()
	ringing := recs.wait(t, "an incoming ringing call", func(r Record) bool { return r.Direction == "inbound" && r.State == "ringing" })
	if ringing.Number != testCaller {
		t.Fatalf("the record has number %q, want %q", ringing.Number, testCaller)
	}
	recs.change(ringing.ID, func(r *Record) { r.Agent, r.Offer = "member-1", string(a.offer) })
	if err := <-waited; err != nil {
		t.Fatal(err)
	}
	if err := session.Ack(ctx); err != nil {
		t.Fatal(err)
	}
	connected := recs.wait(t, "connected", func(r Record) bool { return r.State == "connected" && r.Answer != "" })
	if connected.Agent != "member-1" {
		t.Fatalf("the call was connected to %q", connected.Agent)
	}
	a.accept(t, []byte(connected.Answer))
	talk(t, a, l, mediaOf(t, session.InviteResponse.Body()))
	if err := session.Bye(ctx); err != nil {
		t.Fatal(err)
	}
	recs.wait(t, "completed", func(r Record) bool { return r.ID == ringing.ID && r.State == "completed" })
}

func TestDeskRecordsCallerHangingUpWhileRinging(t *testing.T) {
	recs, tr := deskGateway(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ringCtx, hangUp := context.WithCancel(ctx)
	session := tr.invite(t, ringCtx)
	// The trunk answers the gateway's challenge while it waits for an answer.
	waited := make(chan error, 1)
	go func() { waited <- session.WaitAnswer(ringCtx, answerOptions(testPassword, nil)) }()
	ringing := recs.wait(t, "an incoming ringing call", func(r Record) bool { return r.Direction == "inbound" && r.State == "ringing" })
	hangUp()
	<-waited
	recs.wait(t, "missed", func(r Record) bool { return r.ID == ringing.ID && r.State == "missed:canceled" })
}

func TestDeskDialsTheCallAnAgentPlacesAndHangsUpFromTheBrowser(t *testing.T) {
	recs, tr := deskGateway(t)
	l, a := newLine(t), newAgent(t)
	recs.put(Record{ID: "CALL-9", State: "ringing", Direction: "outbound", Number: testNumber, Agent: "member-1", Offer: string(a.offer), CalledAt: time.Now()})
	o := tr.offer(t)
	invite := o.req.Body()
	if err := o.session.RespondSDP(l.sdp()); err != nil {
		t.Fatal(err)
	}
	connected := recs.wait(t, "connected", func(r Record) bool { return r.ID == "CALL-9" && r.State == "connected" && r.Answer != "" })
	a.accept(t, []byte(connected.Answer))
	talk(t, a, l, mediaOf(t, invite))
	recs.change("CALL-9", func(r *Record) { r.Ended = true })
	recs.wait(t, "completed", func(r Record) bool { return r.ID == "CALL-9" && r.State == "completed" })
}

func TestDeskGivesUpOnCallsNotPlacedInTime(t *testing.T) {
	recs, _ := deskGateway(t)
	recs.put(Record{ID: "CALL-7", State: "ringing", Direction: "outbound", Number: testNumber, CalledAt: time.Now().Add(-3 * time.Minute)})
	recs.put(Record{ID: "CALL-8", State: "ringing", Direction: "outbound", Number: testNumber, Ended: true, CalledAt: time.Now()})
	recs.wait(t, "failed", func(r Record) bool { return r.ID == "CALL-7" && r.State == "failed" })
	recs.wait(t, "missed", func(r Record) bool { return r.ID == "CALL-8" && r.State == "missed:canceled" })
}

func TestDeskClosesCallsAnEarlierRunLeftOpen(t *testing.T) {
	recs := newFakeRecords()
	recs.put(Record{ID: "CALL-1", State: "ringing", Direction: "inbound", Number: testCaller})
	recs.put(Record{ID: "CALL-2", State: "connected", Direction: "outbound", Number: testNumber, Answer: "v=0"})
	recs.put(Record{ID: "CALL-3", State: "ringing", Direction: "outbound", Number: testNumber, CalledAt: time.Now()})
	NewDesk(recs, slog.New(slog.DiscardHandler)).reconcile(context.Background())
	for id, want := range map[string]string{"CALL-1": "failed", "CALL-2": "failed", "CALL-3": "ringing"} {
		if got, _ := recs.Read(context.Background(), id); got.State != want {
			t.Errorf("%s is %s, want %s", id, got.State, want)
		}
	}
}
