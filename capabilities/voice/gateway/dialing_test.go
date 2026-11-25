package gateway

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
)

func TestTrunkDialingSettingsAddHeaders(t *testing.T) {
	dialing := map[string]TrunkDialing{testTrunk: {NumberHeader: "X-Dial-Number", Headers: map[string]string{"X-Sim-Slot": "1"}}}
	g, err := New(Config{Realm: testRealm, Trunks: map[string]string{testTrunk: testPassword}, Dialing: dialing, RingTimeout: 5 * time.Second, Logger: slog.New(slog.DiscardHandler)},
		func(*Call) {})
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
	tr := newTrunk(t, sip.Uri{Scheme: "sip", Host: "127.0.0.1", Port: port})
	if res := tr.register(t, testPassword, 300); res.StatusCode != sip.StatusOK {
		t.Fatalf("the trunk does not register: %d", res.StatusCode)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pending := dial(g, ctx, testNumber, nil)
	o := tr.offer(t)
	if o.req.Recipient.User != testNumber {
		t.Errorf("the Request-URI dials %q, want %q", o.req.Recipient.User, testNumber)
	}
	for name, want := range map[string]string{"X-Dial-Number": testNumber, "X-Sim-Slot": "1"} {
		if h := o.req.GetHeader(name); h == nil || strings.TrimSpace(h.Value()) != want {
			t.Errorf("header %s is %v, want %q", name, h, want)
		}
	}
	if err := o.session.RespondSDP(answer); err != nil {
		t.Fatal(err)
	}
	if got := outcome(t, pending); got.err != nil {
		t.Fatalf("dialing failed: %v", got.err)
	}
}

func TestTrunkDialingSettingsAreChecked(t *testing.T) {
	trunks := map[string]string{testTrunk: testPassword}
	for name, dialing := range map[string]map[string]TrunkDialing{
		"unknown trunk":   {"phone9": {NumberHeader: "X-Dial"}},
		"reserved header": {testTrunk: {Headers: map[string]string{"Contact": "<sip:x@y>"}}},
		"bad name":        {testTrunk: {NumberHeader: "X Dial"}},
		"line break":      {testTrunk: {Headers: map[string]string{"X-Slot": "1\r\nVia: x"}}},
	} {
		if _, err := New(Config{Realm: testRealm, Trunks: trunks, Dialing: dialing}, func(*Call) {}); err == nil {
			t.Errorf("%s: the gateway accepted %v", name, dialing)
		}
	}
}

func TestTrunkAccountsChangeWhileTheGatewayRuns(t *testing.T) {
	g, err := New(Config{Realm: testRealm, Trunks: map[string]string{testTrunk: testPassword}, RingTimeout: 5 * time.Second, Logger: slog.New(slog.DiscardHandler)}, func(*Call) {})
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
	tr := newTrunk(t, sip.Uri{Scheme: "sip", Host: "127.0.0.1", Port: port})
	if res := tr.register(t, testPassword, 300); res.StatusCode != sip.StatusOK {
		t.Fatalf("the trunk does not register: %d", res.StatusCode)
	}
	if err := g.SetTrunks(map[string]string{testTrunk: "rotated-secret"}, map[string]TrunkDialing{testTrunk: {NumberHeader: "X-Dial"}}); err != nil {
		t.Fatal(err)
	}
	if regs := g.Registrations(); len(regs) != 0 {
		t.Fatalf("a trunk whose password changed kept its registration: %v", regs)
	}
	if res := tr.register(t, testPassword, 300); res.StatusCode != sip.StatusForbidden {
		t.Fatalf("the old password still registers: %d", res.StatusCode)
	}
	if res := tr.register(t, "rotated-secret", 300); res.StatusCode != sip.StatusOK {
		t.Fatalf("the new password does not register: %d", res.StatusCode)
	}
	if got := g.dialingFor(testTrunk).NumberHeader; got != "X-Dial" {
		t.Fatalf("the dialing settings were not replaced: %q", got)
	}
	if err := g.SetTrunks(map[string]string{}, nil); err != nil {
		t.Fatal(err)
	}
	if res := tr.register(t, "rotated-secret", 300); res.StatusCode != sip.StatusForbidden {
		t.Fatalf("a removed trunk still registers: %d", res.StatusCode)
	}
	if err := g.SetTrunks(map[string]string{"bad name": "x"}, nil); err == nil {
		t.Fatal("the gateway accepted a trunk account it cannot authenticate")
	}
}
