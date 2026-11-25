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
	mu            sync.Mutex
	recs          map[string]*Record
	keys          map[string]string
	actions       []string
	changes       chan struct{}
	refuseConnect bool
	failedWrites  int
	party         Party
}

func newFakeRecords() *fakeRecords {
	return &fakeRecords{recs: map[string]*Record{}, keys: map[string]string{}, changes: make(chan struct{}, 1)}
}

var allowedFrom = map[string][]string{
	"create": {""}, "receive": {"ringing"}, "connect": {"ringing"},
	"finish": {"connected"}, "miss": {"ringing"}, "fail": {"ringing", "connected"}, "widen": {"ringing"},
}

func (f *fakeRecords) Submit(_ context.Context, action, id string, payload map[string]any, key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if got, ok := f.keys[key]; ok {
		return got, nil
	}
	name := action[len("phone.call."):]
	if name == "connect" && f.refuseConnect {
		return "", &Refused{Status: 409, Code: "state"}
	}
	if name == "fail" && f.failedWrites > 0 {
		f.failedWrites--
		return "", context.DeadlineExceeded
	}
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
		r.Direction, r.Preferred = "inbound", text("preferred")
	case "widen":
		if r.Offer != "" || r.Widened {
			return "", &Refused{Status: 200, Code: "guard", Message: "already taken or widened"}
		}
		r.Widened = true
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

func (f *fakeRecords) Contact(context.Context, string) Party {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.party
}

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

func deskGateway(t *testing.T, setup ...func(*Desk, *fakeRecords)) (*fakeRecords, *trunk) {
	t.Helper()
	recs := newFakeRecords()
	d := NewDesk(recs, slog.New(slog.DiscardHandler))
	d.poll = 50 * time.Millisecond
	for _, s := range setup {
		s(d, recs)
	}
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

func TestDeskRingsTheContactsOwnerFirstThenEveryone(t *testing.T) {
	recs, tr := deskGateway(t, func(d *Desk, f *fakeRecords) {
		d.PreferFor(300 * time.Millisecond)
		f.party = Party{Contact: "C1", Organization: "O1", Owner: "member-7"}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ringCtx, hangUp := context.WithCancel(ctx)
	defer hangUp()
	session := tr.invite(t, ringCtx)
	waited := make(chan error, 1)
	go func() { waited <- session.WaitAnswer(ringCtx, answerOptions(testPassword, nil)) }()
	ringing := recs.wait(t, "ringing for the owner", func(r Record) bool { return r.Direction == "inbound" && r.Preferred == "member-7" })
	if ringing.Widened {
		t.Fatal("the call rang for everyone before the owner had a chance")
	}
	recs.wait(t, "ringing for everyone", func(r Record) bool { return r.ID == ringing.ID && r.Widened })
	hangUp()
	<-waited
	recs.wait(t, "missed", func(r Record) bool { return r.ID == ringing.ID && r.State == "missed:canceled" })
}

func TestDeskDoesNotWidenACallTheOwnerTook(t *testing.T) {
	recs, tr := deskGateway(t, func(d *Desk, f *fakeRecords) {
		d.PreferFor(400 * time.Millisecond)
		f.party = Party{Contact: "C1", Owner: "member-7"}
	})
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
	ringing := recs.wait(t, "ringing for the owner", func(r Record) bool { return r.Preferred == "member-7" })
	recs.change(ringing.ID, func(r *Record) { r.Agent, r.Offer = "member-7", string(a.offer) })
	if err := <-waited; err != nil {
		t.Fatal(err)
	}
	session.Ack(ctx)
	connected := recs.wait(t, "connected", func(r Record) bool { return r.State == "connected" && r.Answer != "" })
	time.Sleep(600 * time.Millisecond)
	recs.mu.Lock()
	widened := recs.recs[connected.ID].Widened
	recs.mu.Unlock()
	if widened || connected.Agent != "member-7" {
		t.Fatalf("the owner's call was widened (%v) or went to %q", widened, connected.Agent)
	}
	session.Bye(ctx)
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

func TestDeskEndsTheLineWhenThePlatformRefusesToRecordTheConnection(t *testing.T) {
	recs, tr := deskGateway(t)
	recs.mu.Lock()
	recs.refuseConnect = true
	recs.failedWrites = 3
	recs.mu.Unlock()
	l, a := newLine(t), newAgent(t)
	recs.put(Record{ID: "CALL-9", State: "ringing", Direction: "outbound", Number: testNumber, Agent: "member-1", Offer: string(a.offer), CalledAt: time.Now()})
	o := tr.offer(t)
	if err := o.session.RespondSDP(l.sdp()); err != nil {
		t.Fatal(err)
	}
	recs.wait(t, "failed", func(r Record) bool { return r.ID == "CALL-9" && r.State == "failed" })
	select {
	case <-o.session.Context().Done():
	case <-time.After(2 * time.Second):
		t.Fatal("the trunk call is still open after the connection was refused")
	}
	select {
	case next := <-tr.offers:
		close(next.done)
		t.Fatal("the call was dialed again while its failure was being written")
	default:
	}
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
