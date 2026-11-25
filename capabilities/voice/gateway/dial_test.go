package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const testNumber = "+8613900139000"

type dialed struct {
	call *Call
	err  error
}

func registered(t *testing.T, ring time.Duration) (*Gateway, *trunk, chan *Call) {
	t.Helper()
	g, gw, calls := startGateway(t, ring)
	tr := newTrunk(t, gw)
	if res := tr.register(t, testPassword, 300); res.StatusCode != sip.StatusOK {
		t.Fatalf("the trunk does not register: %d", res.StatusCode)
	}
	return g, tr, calls
}

func dial(g *Gateway, ctx context.Context, number string, ringing func()) chan dialed {
	out := make(chan dialed, 1)
	go func() {
		call, err := g.Dial(ctx, DialRequest{Trunk: testTrunk, Number: number, Offer: offer, Ringing: ringing})
		out <- dialed{call, err}
	}()
	return out
}

func (tr *trunk) offer(t *testing.T) offered {
	t.Helper()
	select {
	case o := <-tr.offers:
		t.Cleanup(func() { close(o.done) })
		return o
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway did not call the trunk")
		return offered{}
	}
}

func outcome(t *testing.T, pending chan dialed) dialed {
	t.Helper()
	select {
	case got := <-pending:
		return got
	case <-time.After(10 * time.Second):
		t.Fatal("dialing did not finish")
		return dialed{}
	}
}

func answered(t *testing.T, ctx context.Context, g *Gateway, tr *trunk) (*Call, offered) {
	t.Helper()
	pending := dial(g, ctx, testNumber, nil)
	o := tr.offer(t)
	if err := o.session.RespondSDP(answer); err != nil {
		t.Fatalf("the trunk could not answer: %v", err)
	}
	got := outcome(t, pending)
	if got.err != nil {
		t.Fatalf("dialing failed: %v", got.err)
	}
	return got.call, o
}

func TestOutgoingCallRingsAndConnects(t *testing.T) {
	g, tr, _ := registered(t, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rang := make(chan struct{}, 4)
	pending := dial(g, ctx, testNumber, func() { rang <- struct{}{} })
	o := tr.offer(t)
	if o.req.Recipient.User != testNumber || string(o.req.Body()) != string(offer) {
		t.Fatalf("the trunk was asked to dial %q with %q", o.req.Recipient.User, o.req.Body())
	}
	if from := o.req.From(); from == nil || from.Address.User != gatewayUser || from.Address.Host != testRealm {
		t.Fatalf("the call does not come from the gateway: %v", o.req.From())
	}
	if err := o.session.Respond(sip.StatusRinging, "Ringing", nil); err != nil {
		t.Fatal(err)
	}
	select {
	case <-rang:
	case <-time.After(5 * time.Second):
		t.Fatal("ringing was not reported")
	}
	if err := o.session.RespondSDP(answer); err != nil {
		t.Fatal(err)
	}
	got := outcome(t, pending)
	if got.err != nil {
		t.Fatalf("dialing failed: %v", got.err)
	}
	call := got.call
	if call.Direction != Outbound || call.State() != Connected || call.Trunk != testTrunk || call.Dialed != testNumber || call.ID == "" || string(call.Remote) != string(answer) {
		t.Fatalf("the call is described as %+v in state %s", call, call.State())
	}
	if len(rang) != 0 {
		t.Fatal("ringing was reported more than once")
	}
	if err := call.Answer(ctx, answer); !errors.Is(err, ErrDecided) {
		t.Fatalf("an outgoing call can be answered: %v", err)
	}
	if err := o.session.Bye(ctx); err != nil {
		t.Fatal(err)
	}
	waitEnded(t, call, Completed)
}

func TestGatewayHangsUpOutgoingCall(t *testing.T) {
	g, tr, _ := registered(t, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	call, o := answered(t, ctx, g, tr)
	if err := call.Hangup(ctx); err != nil {
		t.Fatalf("the gateway could not hang up: %v", err)
	}
	waitEnded(t, call, Completed)
	select {
	case <-o.session.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the trunk did not see the hangup")
	}
}

func TestDialCanceledWhileRinging(t *testing.T) {
	g, tr, _ := registered(t, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	dialing, stop := context.WithCancel(ctx)
	pending := dial(g, dialing, testNumber, nil)
	o := tr.offer(t)
	if err := o.session.Respond(sip.StatusRinging, "Ringing", nil); err != nil {
		t.Fatal(err)
	}
	stop()
	got := outcome(t, pending)
	if !errors.Is(got.err, context.Canceled) || got.call == nil {
		t.Fatalf("a canceled dial returned %v", got.err)
	}
	waitEnded(t, got.call, Canceled)
	select {
	case <-o.session.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the trunk did not see the cancel")
	}
}

func TestRefusedDialReportsBusy(t *testing.T) {
	g, tr, _ := registered(t, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pending := dial(g, ctx, testNumber, nil)
	o := tr.offer(t)
	if err := o.session.Respond(sip.StatusBusyHere, "Busy Here", nil); err != nil {
		t.Fatal(err)
	}
	got := outcome(t, pending)
	var refused *DialError
	if !errors.As(got.err, &refused) || refused.Status != sip.StatusBusyHere {
		t.Fatalf("a busy far end returned %v", got.err)
	}
	waitEnded(t, got.call, Rejected)
}

func TestUnansweredDialTimesOut(t *testing.T) {
	g, tr, _ := registered(t, 300*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pending := dial(g, ctx, testNumber, nil)
	o := tr.offer(t)
	if err := o.session.Respond(sip.StatusRinging, "Ringing", nil); err != nil {
		t.Fatal(err)
	}
	got := outcome(t, pending)
	if !errors.Is(got.err, ErrUnanswered) {
		t.Fatalf("an unanswered dial returned %v", got.err)
	}
	waitEnded(t, got.call, Unanswered)
	select {
	case <-o.session.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the trunk was not told to stop ringing")
	}
}

func TestDialNeedsARegisteredTrunkAndAnAllowedNumber(t *testing.T) {
	g, _, _ := startGateway(t, time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if call, err := g.Dial(ctx, DialRequest{Trunk: testTrunk, Number: testNumber, Offer: offer}); !errors.Is(err, ErrNotRegistered) || call != nil {
		t.Fatalf("an unregistered trunk was dialed: %v", err)
	}
	if _, err := g.Dial(ctx, DialRequest{Trunk: testTrunk, Number: testNumber}); !errors.Is(err, ErrNoSession) {
		t.Fatalf("a dial without a session description was accepted: %v", err)
	}
	for _, number := range []string{"110", "+112", "120", "911", "", "13900-139000", "1+3"} {
		if _, err := g.Dial(ctx, DialRequest{Trunk: testTrunk, Number: number, Offer: offer}); !errors.Is(err, ErrNumber) {
			t.Errorf("%q was not refused: %v", number, err)
		}
	}
	for number, want := range map[string]bool{"13900139000": true, "+8613900139000": true, "*99#": true, "02112345678": false} {
		if got := dialable(number, []string{"139", "+86", "*"}); got != want {
			t.Errorf("%q with prefixes: got %v, want %v", number, got, want)
		}
	}
}

func TestTrunkCarriesOneCallAtATime(t *testing.T) {
	g, tr, calls := registered(t, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	inbound, session := connect(t, ctx, tr, calls, nil)
	if _, err := g.Dial(ctx, DialRequest{Trunk: testTrunk, Number: testNumber, Offer: offer}); !errors.Is(err, ErrBusy) {
		t.Fatalf("a busy trunk was dialed: %v", err)
	}
	if err := session.Bye(ctx); err != nil {
		t.Fatal(err)
	}
	waitEnded(t, inbound, Completed)
	outbound, o := answered(t, ctx, g, tr)
	second := tr.invite(t, ctx)
	err := second.WaitAnswer(ctx, answerOptions(testPassword, nil))
	var busy *sipgo.ErrDialogResponse
	if !errors.As(err, &busy) || busy.Res.StatusCode != sip.StatusBusyHere {
		t.Fatalf("a second call on a busy trunk got %v, want 486", err)
	}
	if err := o.session.Bye(ctx); err != nil {
		t.Fatal(err)
	}
	waitEnded(t, outbound, Completed)
}
