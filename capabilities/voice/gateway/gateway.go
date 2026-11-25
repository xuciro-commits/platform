package gateway

import (
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

const (
	defaultRingTimeout = 45 * time.Second
	defaultExpires     = 3600
	minExpires         = 60
)

type Config struct {
	Realm        string
	Host         string
	Trunks       map[string]string
	DialPrefixes []string
	RingTimeout  time.Duration
	Logger       *slog.Logger
}

type Registration struct {
	Trunk   string
	Contact string
	Source  string
	Expires time.Time
}

type Gateway struct {
	cfg      Config
	log      *slog.Logger
	now      func() time.Time
	auth     *authenticator
	ua       *sipgo.UserAgent
	server   *sipgo.Server
	dialogs  *sipgo.DialogServerCache
	outgoing *sipgo.DialogClientCache
	onCall   func(*Call)
	mu       sync.Mutex
	regs     map[string]Registration
	active   map[string]*Call
	media    net.IP
}

func New(cfg Config, onCall func(*Call)) (*Gateway, error) {
	if cfg.Realm == "" {
		return nil, errors.New("voice gateway: a realm is required")
	}
	if len(cfg.Trunks) == 0 {
		return nil, errors.New("voice gateway: at least one trunk account is required")
	}
	if onCall == nil {
		return nil, errors.New("voice gateway: a call handler is required")
	}
	if cfg.RingTimeout <= 0 {
		cfg.RingTimeout = defaultRingTimeout
	}
	log := cfg.Logger
	if log == nil {
		log = slog.Default()
	}
	ua, err := sipgo.NewUA(sipgo.WithUserAgent("platform-voice"))
	if err != nil {
		return nil, fmt.Errorf("voice gateway: user agent: %w", err)
	}
	server, err := sipgo.NewServer(ua)
	if err != nil {
		ua.Close()
		return nil, fmt.Errorf("voice gateway: server: %w", err)
	}
	g := &Gateway{cfg: cfg, log: log, now: time.Now, ua: ua, server: server, onCall: onCall, regs: map[string]Registration{}, active: map[string]*Call{}}
	g.auth = newAuthenticator(cfg.Realm, cfg.Trunks, func() time.Time { return g.now() })
	server.OnRegister(g.register)
	server.OnInvite(g.invite)
	server.OnAck(g.ack)
	server.OnBye(g.bye)
	server.OnOptions(g.options)
	return g, nil
}

func (g *Gateway) Serve(conn net.PacketConn) error {
	host, portText, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		return fmt.Errorf("voice gateway: listen address: %w", err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return fmt.Errorf("voice gateway: listen port: %w", err)
	}
	if g.cfg.Host != "" {
		host = g.cfg.Host
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsUnspecified() {
		return errors.New("voice gateway: listening on all interfaces needs the address trunks reach the gateway at")
	}
	client, err := sipgo.NewClient(g.ua, sipgo.WithClientConnectionAddr(conn.LocalAddr().String()), sipgo.WithClientHostname(host), sipgo.WithClientPort(port))
	if err != nil {
		return fmt.Errorf("voice gateway: client: %w", err)
	}
	contact := sip.ContactHeader{Address: sip.Uri{Scheme: "sip", User: gatewayUser, Host: host, Port: port}}
	g.mu.Lock()
	g.dialogs, g.outgoing = sipgo.NewDialogServerCache(client, contact), sipgo.NewDialogClientCache(client, contact)
	g.media = mediaAddress(host)
	g.mu.Unlock()
	g.log.Info("voice gateway listening", "address", net.JoinHostPort(host, portText), "realm", g.cfg.Realm, "trunks", len(g.cfg.Trunks))
	return g.server.ServeUDP(conn)
}

func (g *Gateway) Close() error { return g.ua.Close() }

func (g *Gateway) Registrations() []Registration {
	g.mu.Lock()
	defer g.mu.Unlock()
	now := g.now()
	out := make([]Registration, 0, len(g.regs))
	for trunk, reg := range g.regs {
		if !now.Before(reg.Expires) {
			delete(g.regs, trunk)
			continue
		}
		out = append(out, reg)
	}
	slices.SortFunc(out, func(a, b Registration) int { return strings.Compare(a.Trunk, b.Trunk) })
	return out
}

func (g *Gateway) registration(trunk string) (Registration, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	reg, ok := g.regs[trunk]
	return reg, ok && g.now().Before(reg.Expires)
}

func (g *Gateway) claim(c *Call) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active[c.Trunk] != nil {
		return false
	}
	g.active[c.Trunk] = c
	c.onEnd = func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.active[c.Trunk] == c {
			delete(g.active, c.Trunk)
		}
	}
	return true
}

func (g *Gateway) register(req *sip.Request, tx sip.ServerTransaction) {
	trunk, v := g.auth.check(req)
	if v != allowed {
		g.auth.refuse(req, tx, v)
		return
	}
	contact := req.Contact()
	expires := requestedExpiry(req, contact)
	res := sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
	switch {
	case contact == nil:
	case contact.Address.Wildcard || expires == 0:
		g.mu.Lock()
		delete(g.regs, trunk)
		g.mu.Unlock()
		g.log.Info("trunk unregistered", "trunk", trunk)
	case expires < minExpires:
		res = sip.NewResponseFromRequest(req, sip.StatusIntervalToBrief, "Interval Too Brief", nil)
		res.AppendHeader(sip.NewHeader("Min-Expires", strconv.Itoa(minExpires)))
	default:
		expires = min(expires, defaultExpires)
		address := contact.Address.String()
		g.mu.Lock()
		g.regs[trunk] = Registration{Trunk: trunk, Contact: address, Source: req.Source(), Expires: g.now().Add(time.Duration(expires) * time.Second)}
		g.mu.Unlock()
		res.AppendHeader(sip.NewHeader("Contact", fmt.Sprintf("<%s>;expires=%d", address, expires)))
		res.AppendHeader(sip.NewHeader("Expires", strconv.Itoa(expires)))
		g.log.Info("trunk registered", "trunk", trunk, "source", req.Source(), "expires", expires)
	}
	tx.Respond(res)
}

func requestedExpiry(req *sip.Request, contact *sip.ContactHeader) int {
	if contact != nil {
		if value, ok := contact.Params.Get("expires"); ok {
			if n, err := strconv.Atoi(value); err == nil && n >= 0 {
				return n
			}
		}
	}
	if h := req.GetHeader("Expires"); h != nil {
		if n, err := strconv.Atoi(strings.TrimSpace(h.Value())); err == nil && n >= 0 {
			return n
		}
	}
	return defaultExpires
}

func (g *Gateway) invite(req *sip.Request, tx sip.ServerTransaction) {
	trunk, v := g.auth.check(req)
	if v != allowed {
		g.auth.refuse(req, tx, v)
		return
	}
	session, err := g.dialogs.ReadInvite(req, tx)
	if err != nil {
		tx.Respond(sip.NewResponseFromRequest(req, sip.StatusBadRequest, "Bad Request", nil))
		return
	}
	number, name := identity(req)
	call := newCall(Inbound, trunk, Ringing)
	call.ID, call.Caller, call.Name, call.Dialed, call.Remote = req.CallID().Value(), number, name, req.Recipient.User, req.Body()
	call.bye = session.Bye
	g.mu.Lock()
	call.media = g.media
	g.mu.Unlock()
	log := g.log.With("call", call.ID, "trunk", trunk)
	if !g.claim(call) {
		session.Respond(sip.StatusBusyHere, "Busy Here", nil)
		session.Close()
		log.Info("incoming call refused: the trunk is on another call")
		return
	}
	if err := session.Respond(sip.StatusTrying, "Trying", nil); err == nil {
		err = session.Respond(sip.StatusRinging, "Ringing", nil)
	}
	if err != nil {
		call.settle()
		call.end(Failed)
		session.Close()
		log.Warn("incoming call failed before ringing", "error", err)
		return
	}
	log.Info("incoming call ringing", "caller", redact(number), "dialed", redact(call.Dialed))
	go g.onCall(call)
	timer := time.NewTimer(g.cfg.RingTimeout)
	defer timer.Stop()
	select {
	case d := <-call.decided:
		call.settle()
		if d.answer == nil {
			err := session.Respond(d.code, d.reason, nil)
			call.end(d.cause)
			session.Close()
			d.result <- err
			log.Info("incoming call refused", "status", d.code)
			return
		}
		if err := session.RespondSDP(d.answer); err != nil {
			cause := Failed
			if session.Context().Err() != nil {
				cause = Canceled
			}
			call.end(cause)
			session.Close()
			d.result <- err
			log.Warn("incoming call could not be answered", "error", err)
			return
		}
		call.connect()
		d.result <- nil
		log.Info("incoming call connected")
		go func() {
			<-session.Context().Done()
			session.Close()
			call.end(Completed)
			log.Info("call ended")
		}()
	case <-timer.C:
		call.settle()
		session.Respond(sip.StatusTemporarilyUnavailable, "Temporarily Unavailable", nil)
		call.end(Unanswered)
		session.Close()
		log.Info("incoming call was not answered in time")
	case <-session.Context().Done():
		call.settle()
		call.end(Canceled)
		session.Close()
		log.Info("caller hung up while ringing")
	}
}

func (g *Gateway) ack(req *sip.Request, tx sip.ServerTransaction) {
	g.dialogs.ReadAck(req, tx)
}

func (g *Gateway) bye(req *sip.Request, tx sip.ServerTransaction) {
	if g.dialogs.ReadBye(req, tx) == nil || g.outgoing.ReadBye(req, tx) == nil {
		return
	}
	tx.Respond(sip.NewResponseFromRequest(req, sip.StatusCallTransactionDoesNotExists, "Call/Transaction Does Not Exist", nil))
}

func (g *Gateway) options(req *sip.Request, tx sip.ServerTransaction) {
	res := sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil)
	res.AppendHeader(sip.NewHeader("Allow", "INVITE, ACK, CANCEL, BYE, OPTIONS, REGISTER"))
	tx.Respond(res)
}
