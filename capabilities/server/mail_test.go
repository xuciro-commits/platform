package platformserver

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"net/mail"
	"net/textproto"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	pb "platformkernel/gen/platform/kernel/v1alpha1"
	"platformserver/apps/org"
	"platformserver/platform"
)

// mailbox is an SMTP server as a mail provider is: it keeps one message per
// Message-ID, and answers DATA with reply (250 unless a test changes it).
type mailbox struct {
	mu    sync.Mutex
	reply int
	calls int
	kept  map[string]*mail.Message // Message-ID → message
	to    map[string]string        // Message-ID → recipient
}

func newMailbox(t *testing.T) (*mailbox, string) {
	box := &mailbox{reply: 250, kept: map[string]*mail.Message{}, to: map[string]string{}}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go box.serve(conn)
		}
	}()
	return box, l.Addr().String()
}

func (b *mailbox) serve(conn net.Conn) {
	defer conn.Close()
	tp := textproto.NewConn(conn)
	tp.PrintfLine("220 mailbox")
	var rcpt string
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return
		}
		switch verb := strings.ToUpper(strings.Fields(line + " x")[0]); verb {
		case "EHLO", "HELO", "MAIL":
			tp.PrintfLine("250 ok")
		case "RCPT":
			rcpt = strings.Trim(strings.TrimPrefix(line[len("RCPT TO:"):], " "), "<>")
			tp.PrintfLine("250 ok")
		case "DATA":
			tp.PrintfLine("354 go on")
			raw, _ := tp.ReadDotBytes()
			b.mu.Lock()
			b.calls++
			reply := b.reply
			if reply == 250 {
				if m, err := mail.ReadMessage(bufio.NewReader(strings.NewReader(string(raw)))); err == nil {
					id := m.Header.Get("Message-ID")
					if _, seen := b.kept[id]; !seen {
						b.kept[id], b.to[id] = m, rcpt
					}
				}
			}
			b.mu.Unlock()
			tp.PrintfLine("%d %s", reply, map[bool]string{true: "queued", false: "refused"}[reply == 250])
		case "QUIT":
			tp.PrintfLine("221 bye")
			return
		default:
			tp.PrintfLine("502 unknown")
		}
	}
}

// Email as a notification channel (ADR-0014 D7): an administrator adds an SMTP
// endpoint for an app's notifications; each notification to a member who signs
// in with an email address is mailed at least once, with its effect ID as the
// Message-ID, and replay rebuilds the mails without sending them.
func TestNotificationsByEmail(t *testing.T) {
	box, addr := newMailbox(t)
	var journal []Entry
	build := func() *Tenant {
		dir := NewConsole("t-1",
			Seat{Subjects: []string{"ana"}, Member: platform.Member{ID: "ana", Roles: map[string]string{PlatformApp: Admin}}},
			Seat{Subjects: []string{"sup", "user:sup@example.com"}, Member: platform.Member{ID: "sup", Roles: map[string]string{}}},
			Seat{Subjects: []string{"client:bot"}, Member: platform.Member{ID: "bot", Roles: map[string]string{}}},
			Seat{Subjects: []string{"gw"}, Member: platform.Member{ID: "gw", Roles: map[string]string{}}})
		org := org.New("t-1", platform.OrgSeed{Structures: []platform.Structure{{ID: "site", Name: "Sites", Kind: "site"}},
			Units:       []platform.Unit{{ID: "L1", Kind: "line"}},
			Memberships: []platform.Membership{{Party: "member:sup", Unit: "L1", Role: "supervisor"}, {Party: "member:bot", Unit: "L1", Role: "supervisor"}}})
		tn, err := NewTenant("t-1", dir, org, probe{newNotes("t-1", "p")})
		if err == nil {
			err = tn.Connect(&pb.ConnectorDescriptor{ConnectorId: "gw", Direction: pb.ConnectorDirection_CONNECTOR_DIRECTION_PUSH,
				DataClasses: []string{"p.topic"}, Heartbeat: durationpb.New(time.Minute)})
		}
		if err != nil {
			t.Fatal(err)
		}
		return tn
	}
	tn := build()
	tn.Record = func(e Entry) { journal = append(journal, e) }
	member := func(id string) platform.Member { m, _ := tn.app(PlatformApp).(*Console).Member(id); return m }
	ana, gw := member("ana"), member("gw")
	now := time.Date(2026, 9, 25, 9, 0, 0, 0, time.UTC)
	keys := 0
	add := func(id string, endpoint map[string]any) string {
		keys++
		raw, _ := json.Marshal(endpoint)
		if _, err := tn.Submit(ana, &pb.Submission{TenantId: "t-1", PrincipalId: "ana", Authority: PlatformApp, IdempotencyKey: fmt.Sprint("e", keys),
			Target: &pb.EntityRef{Type: EndpointType, Id: id}, Schema: &pb.SchemaRef{Name: SchemaEndpointAdd, Version: 1}, Payload: raw}, now); err != nil {
			return err.Error()
		}
		return "ok"
	}
	email := func(change map[string]any) map[string]any {
		out := map[string]any{"kind": "email", "url": "smtp://" + addr, "from": "plant@example.com", "notifications": []string{"p"}, "allowPrivate": true}
		for k, v := range change {
			out[k] = v
		}
		return out
	}
	for _, c := range []map[string]any{{"from": "plant"}, {"notifications": []string{"nope"}}, {"notifications": nil}, {"url": "https://mail.example.com"},
		{"events": []string{"p.note"}}, {"url": "smtp://user@" + addr}} { // a user needs a secret to authenticate with
		if got := add("bad", email(c)); got != "ERROR_CODE_INVALID_ARGUMENT" {
			t.Fatalf("%v accepted: %s", c, got)
		}
	}
	if got := add("mail", email(nil)); got != "ok" {
		t.Fatal(got)
	}
	feed := func(text string, at time.Time) {
		t.Helper()
		if _, err := tn.Input(gw, "p-feed", []byte(`"`+text+`"`), at); err != nil { // journaled as JSON
			t.Fatal(err)
		}
	}
	// A notification to the line's supervisors: the person is mailed, the bot has no address.
	feed("one", now)
	tn.Dispatch(now)
	if len(box.kept) != 1 {
		t.Fatalf("mailed %d", len(box.kept))
	}
	for id, m := range box.kept {
		if box.to[id] != "sup@example.com" || m.Header.Get("Subject") != `Feed "one"` || m.Header.Get("From") != "plant@example.com" ||
			m.Header.Get("X-Platform-Effect") != "t-1:notice:n-1:mail" {
			t.Fatalf("mail %s to %s: %v", id, box.to[id], m.Header)
		}
	}
	// A temporary refusal is retried with the same Message-ID; a permanent one is rejected.
	box.reply = 451
	feed("two", now.Add(time.Minute))
	tn.Dispatch(now.Add(time.Minute))
	if e := tn.Effects(now)[0]; e.State != "retrying" || !strings.Contains(e.Error, "451") {
		t.Fatalf("after 451: %+v", e)
	}
	box.reply = 250
	tn.Dispatch(now.Add(time.Hour))
	if e := tn.Effects(now)[0]; e.State != "delivered" || len(box.kept) != 2 || box.calls != 3 {
		t.Fatalf("after recovery: %+v, %d kept after %d calls", e, len(box.kept), box.calls)
	}
	box.reply = 550
	feed("three", now.Add(2*time.Hour))
	tn.Dispatch(now.Add(2 * time.Hour))
	if e := tn.Effects(now)[0]; e.State != "rejected" {
		t.Fatalf("after 550: %+v", e)
	}
	CheckReplay(t, tn, journal, build)
	if v := tn.Endpoints()[0]; v.Kind != "email" || v.Health != "ok" || v.Delivers != 2 {
		t.Fatalf("endpoint %+v", v)
	}
}
