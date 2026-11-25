package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const (
	testRealm    = "test.local"
	testTrunk    = "phone1"
	testPassword = "secret"
	testCaller   = "+8613800138000"
)

var (
	offer  = []byte("v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 40000 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n")
	answer = []byte("v=0\r\no=- 2 2 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 40002 RTP/AVP 0\r\na=rtpmap:0 PCMU/8000\r\n")
)

func listen(t *testing.T) (net.PacketConn, int) {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(conn.LocalAddr().String())
	n, _ := strconv.Atoi(port)
	return conn, n
}

func ready(t *testing.T, ua *sipgo.UserAgent, conn net.PacketConn) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		if c, err := ua.TransportLayer().GetConnection("udp", conn.LocalAddr().String()); err == nil && c != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the SIP listener did not start")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func startGateway(t *testing.T, ring time.Duration) (*Gateway, sip.Uri, chan *Call) {
	t.Helper()
	calls := make(chan *Call, 4)
	g, err := New(Config{Realm: testRealm, Trunks: map[string]string{testTrunk: testPassword}, RingTimeout: ring, Logger: slog.New(slog.DiscardHandler)},
		func(c *Call) { calls <- c })
	if err != nil {
		t.Fatal(err)
	}
	conn, port := listen(t)
	go g.Serve(conn)
	t.Cleanup(func() {
		g.Close()
		conn.Close()
	})
	ready(t, g.ua, conn)
	return g, sip.Uri{Scheme: "sip", Host: "127.0.0.1", Port: port}, calls
}

type offered struct {
	session *sipgo.DialogServerSession
	req     *sip.Request
	done    chan struct{}
}

type trunk struct {
	client   *sipgo.Client
	dialogs  *sipgo.DialogClientCache
	incoming *sipgo.DialogServerCache
	offers   chan offered
	contact  sip.ContactHeader
	gateway  sip.Uri
}

func newTrunk(t *testing.T, gateway sip.Uri) *trunk {
	t.Helper()
	ua, err := sipgo.NewUA(sipgo.WithUserAgent(testTrunk))
	if err != nil {
		t.Fatal(err)
	}
	conn, port := listen(t)
	server, err := sipgo.NewServer(ua)
	if err != nil {
		t.Fatal(err)
	}
	client, err := sipgo.NewClient(ua, sipgo.WithClientConnectionAddr(conn.LocalAddr().String()), sipgo.WithClientHostname("127.0.0.1"), sipgo.WithClientPort(port))
	if err != nil {
		t.Fatal(err)
	}
	contact := sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: testTrunk, Host: "127.0.0.1", Port: port}}
	dialogs := sipgo.NewDialogClientCache(client, contact)
	incoming := sipgo.NewDialogServerCache(client, contact)
	offers := make(chan offered)
	server.OnInvite(func(req *sip.Request, tx sip.ServerTransaction) {
		session, err := incoming.ReadInvite(req, tx)
		if err != nil {
			tx.Respond(sip.NewResponseFromRequest(req, sip.StatusBadRequest, "Bad Request", nil))
			return
		}
		o := offered{session: session, req: req, done: make(chan struct{})}
		select {
		case offers <- o:
			<-o.done
		case <-time.After(5 * time.Second):
			session.Respond(sip.StatusTemporarilyUnavailable, "Temporarily Unavailable", nil)
		}
	})
	server.OnAck(func(req *sip.Request, tx sip.ServerTransaction) { incoming.ReadAck(req, tx) })
	server.OnBye(func(req *sip.Request, tx sip.ServerTransaction) {
		if incoming.ReadBye(req, tx) == nil || dialogs.ReadBye(req, tx) == nil {
			return
		}
		tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
	})
	go server.ServeUDP(conn)
	t.Cleanup(func() {
		ua.Close()
		conn.Close()
	})
	ready(t, ua, conn)
	return &trunk{client: client, dialogs: dialogs, incoming: incoming, offers: offers, contact: contact, gateway: gateway}
}

func (tr *trunk) register(t *testing.T, password string, expires int) *sip.Response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	aor := sip.Uri{Scheme: "sip", User: testTrunk, Host: tr.gateway.Host, Port: tr.gateway.Port}
	req := sip.NewRequest(sip.REGISTER, sip.Uri{Scheme: "sip", Host: tr.gateway.Host, Port: tr.gateway.Port})
	from := &sip.FromHeader{Address: aor, Params: sip.NewParams()}
	from.Params.Add("tag", sip.GenerateTagN(16))
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: aor, Params: sip.NewParams()})
	req.AppendHeader(tr.contact.Clone())
	req.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
	res, err := tr.client.Do(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == sip.StatusUnauthorized {
		if res, err = tr.client.DoDigestAuth(ctx, req, res, sipgo.DigestAuth{Username: testTrunk, Password: password}); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func (tr *trunk) invite(t *testing.T, ctx context.Context) *sipgo.DialogClientSession {
	t.Helper()
	recipient := sip.Uri{Scheme: "sip", User: "100", Host: tr.gateway.Host, Port: tr.gateway.Port}
	session, err := tr.dialogs.Invite(ctx, recipient, offer,
		sip.NewHeader("Content-Type", "application/sdp"),
		sip.NewHeader("P-Asserted-Identity", `"Li Lei" <sip:`+testCaller+`@127.0.0.1>, <tel:`+testCaller+`>`))
	if err != nil {
		t.Fatal(err)
	}
	return session
}

func answerOptions(password string, codes chan<- int) sipgo.AnswerOptions {
	return sipgo.AnswerOptions{Username: testTrunk, Password: password, OnResponse: func(res *sip.Response) error {
		if codes != nil {
			select {
			case codes <- res.StatusCode:
			default:
			}
		}
		return nil
	}}
}

func nextCall(t *testing.T, calls chan *Call) *Call {
	t.Helper()
	select {
	case call := <-calls:
		return call
	case <-time.After(5 * time.Second):
		t.Fatal("the gateway offered no call")
		return nil
	}
}

func waitEnded(t *testing.T, call *Call, want Cause) {
	t.Helper()
	select {
	case <-call.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not end")
	}
	if call.State() != Ended || call.Cause() != want {
		t.Fatalf("the call ended as %s/%s, want %s", call.State(), call.Cause(), want)
	}
}

func connect(t *testing.T, ctx context.Context, tr *trunk, calls chan *Call, codes chan<- int) (*Call, *sipgo.DialogClientSession) {
	t.Helper()
	session := tr.invite(t, ctx)
	got := make(chan *Call, 1)
	answered := make(chan error, 1)
	go func() {
		select {
		case call := <-calls:
			got <- call
			answered <- call.Answer(ctx, answer)
		case <-ctx.Done():
			answered <- ctx.Err()
		}
	}()
	if err := session.WaitAnswer(ctx, answerOptions(testPassword, codes)); err != nil {
		t.Fatalf("the trunk saw no answer: %v", err)
	}
	if err := session.Ack(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-answered; err != nil {
		t.Fatalf("answering failed: %v", err)
	}
	return <-got, session
}

func TestTrunkRegistersWithDigest(t *testing.T) {
	g, gw, _ := startGateway(t, time.Second)
	tr := newTrunk(t, gw)
	if res := tr.register(t, "wrong", 300); res.StatusCode != sip.StatusForbidden || len(g.Registrations()) != 0 {
		t.Fatalf("a wrong password registers: %d", res.StatusCode)
	}
	if res := tr.register(t, testPassword, 300); res.StatusCode != sip.StatusOK {
		t.Fatalf("the trunk does not register: %d", res.StatusCode)
	}
	regs := g.Registrations()
	if len(regs) != 1 || regs[0].Trunk != testTrunk || regs[0].Contact != tr.contact.Address.String() || !regs[0].Expires.After(time.Now().Add(250*time.Second)) {
		t.Fatalf("the registration is not kept: %+v", regs)
	}
	if res := tr.register(t, testPassword, 30); res.StatusCode != sip.StatusIntervalToBrief || res.GetHeader("Min-Expires") == nil {
		t.Fatalf("a too short registration is accepted: %d", res.StatusCode)
	}
	if res := tr.register(t, testPassword, 0); res.StatusCode != sip.StatusOK || len(g.Registrations()) != 0 {
		t.Fatalf("unregistering keeps the binding: %d %+v", res.StatusCode, g.Registrations())
	}
}

func TestIncomingCallRingsAndConnects(t *testing.T) {
	_, gw, calls := startGateway(t, 5*time.Second)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	codes := make(chan int, 16)
	call, session := connect(t, ctx, tr, calls, codes)
	close(codes)
	var seen []int
	for code := range codes {
		seen = append(seen, code)
	}
	if !slices.Contains(seen, sip.StatusUnauthorized) || !slices.Contains(seen, sip.StatusRinging) || seen[len(seen)-1] != sip.StatusOK {
		t.Fatalf("the trunk saw %v, want a challenge, ringing and an answer", seen)
	}
	if call.Trunk != testTrunk || call.Caller != testCaller || call.Name != "Li Lei" || call.Dialed != "100" || call.Direction != Inbound || string(call.Remote) != string(offer) {
		t.Fatalf("the call is described as %+v", call)
	}
	if string(session.InviteResponse.Body()) != string(answer) || call.State() != Connected {
		t.Fatalf("the trunk did not get the answer: %q %s", session.InviteResponse.Body(), call.State())
	}
	if err := call.Answer(ctx, answer); !errors.Is(err, ErrDecided) {
		t.Fatalf("a second answer is taken: %v", err)
	}
	if err := session.Bye(ctx); err != nil {
		t.Fatal(err)
	}
	waitEnded(t, call, Completed)
}

func TestGatewayHangsUpConnectedCall(t *testing.T) {
	_, gw, calls := startGateway(t, 5*time.Second)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	call, session := connect(t, ctx, tr, calls, nil)
	if err := call.Hangup(ctx); err != nil {
		t.Fatalf("the gateway could not hang up: %v", err)
	}
	waitEnded(t, call, Completed)
	select {
	case <-session.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the trunk did not see the hangup")
	}
	if err := call.Hangup(ctx); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("an ended call hangs up again: %v", err)
	}
}

func TestCallerHangsUpWhileRinging(t *testing.T) {
	_, gw, calls := startGateway(t, 5*time.Second)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := tr.invite(t, ctx)
	ringing := make(chan int, 16)
	waiting, stop := context.WithCancel(ctx)
	result := make(chan error, 1)
	go func() { result <- session.WaitAnswer(waiting, answerOptions(testPassword, ringing)) }()
	call := nextCall(t, calls)
	for code := range ringing {
		if code == sip.StatusRinging {
			break
		}
	}
	stop()
	if err := <-result; err == nil {
		t.Fatal("a canceled call was answered")
	}
	waitEnded(t, call, Canceled)
	if err := call.Answer(ctx, answer); !errors.Is(err, ErrDecided) {
		t.Fatalf("a canceled call can be answered: %v", err)
	}
}

func TestUnansweredCallTimesOut(t *testing.T) {
	_, gw, calls := startGateway(t, 300*time.Millisecond)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := tr.invite(t, ctx)
	err := session.WaitAnswer(ctx, answerOptions(testPassword, nil))
	var refused *sipgo.ErrDialogResponse
	if !errors.As(err, &refused) || refused.Res.StatusCode != sip.StatusTemporarilyUnavailable {
		t.Fatalf("the trunk got %v, want 480", err)
	}
	waitEnded(t, nextCall(t, calls), Unanswered)
}

func TestRefusedCallReportsBusy(t *testing.T) {
	_, gw, calls := startGateway(t, 5*time.Second)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := tr.invite(t, ctx)
	result := make(chan error, 1)
	go func() { result <- session.WaitAnswer(ctx, answerOptions(testPassword, nil)) }()
	call := nextCall(t, calls)
	if err := call.Reject(ctx); err != nil {
		t.Fatal(err)
	}
	var refused *sipgo.ErrDialogResponse
	if err := <-result; !errors.As(err, &refused) || refused.Res.StatusCode != sip.StatusBusyHere {
		t.Fatalf("the trunk got %v, want 486", err)
	}
	waitEnded(t, call, Rejected)
}

func TestWrongPasswordCannotCall(t *testing.T) {
	_, gw, calls := startGateway(t, 5*time.Second)
	tr := newTrunk(t, gw)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	session := tr.invite(t, ctx)
	var refused *sipgo.ErrDialogResponse
	if err := session.WaitAnswer(ctx, answerOptions("wrong", nil)); !errors.As(err, &refused) || refused.Res.StatusCode != sip.StatusForbidden {
		t.Fatalf("the trunk got %v, want 403", err)
	}
	select {
	case call := <-calls:
		t.Fatalf("a call with a wrong password was offered: %+v", call)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestCallerIdentity(t *testing.T) {
	for value, want := range map[string][2]string{
		`"Li Lei" <sip:+8613800138000@pbx>, <tel:+8613800138000>`: {"+8613800138000", "Li Lei"},
		`<sip:1001@pbx;user=phone>`:                               {"1001", ""},
		`<sips:alice;x=1@example.com>`:                            {"alice", ""},
		`<tel:+15550100;phone-context=example.com>`:               {"+15550100", ""},
		`<sip:example.com>`:                                       {"", ""},
	} {
		if number, name := parseIdentity(value); number != want[0] || name != want[1] {
			t.Errorf("%s: got %q %q, want %q %q", value, number, name, want[0], want[1])
		}
	}
	if redact("+8613800138000") != "************00" || redact("7") != "**" {
		t.Error("numbers are not redacted in logs")
	}
}
