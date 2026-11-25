package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/emiago/sipgo/sip"
)

const (
	defaultPoll = 2 * time.Second
	dialWindow  = 2 * time.Minute
	recordWait  = 10 * time.Second
	preferFirst = 15 * time.Second
)

// Record is the part of a call record the desk reads: the record the platform keeps
// for every call, which agents take and hang up from the browser.
type Record struct {
	ID        string
	State     string
	Direction string
	Number    string
	Agent     string
	Offer     string
	Answer    string
	Ended     bool
	Preferred string
	Widened   bool
	CalledAt  time.Time
}

// Party is who a number belongs to: the business partner contact, their organization,
// and the member who owns that contact, whom an incoming call rings for first.
type Party struct {
	Contact      string
	Organization string
	Owner        string
}

// Records is the platform seen from the gateway: it writes call records through the
// platform's actions and reads them back.
type Records interface {
	Submit(ctx context.Context, action, id string, payload map[string]any, key string) (string, error)
	Read(ctx context.Context, id string) (Record, error)
	Active(ctx context.Context) ([]Record, error)
	Contact(ctx context.Context, number string) Party
	Changes() <-chan struct{}
}

// Refused is the platform declining a write, as opposed to not being reachable.
type Refused struct {
	Status  int
	Code    string
	Message string
}

func (r *Refused) Error() string {
	return fmt.Sprintf("the platform refused (%d %s): %s", r.Status, r.Code, r.Message)
}

// Desk connects calls to call records: an incoming call becomes a ringing record that
// the first agent takes in the browser, and an outgoing record an agent places is
// dialed on a free line. The record carries the browser's session description and the
// gateway's answer, so the browser and the gateway never talk directly.
type Desk struct {
	records Records
	gw      *Gateway
	log     *slog.Logger
	now     func() time.Time
	poll    time.Duration
	prefer  time.Duration
	mu      sync.Mutex
	held    map[string]bool
	signal  chan struct{}
	done    chan struct{}
}

func NewDesk(records Records, log *slog.Logger) *Desk {
	if log == nil {
		log = slog.Default()
	}
	return &Desk{records: records, log: log, now: time.Now, poll: defaultPoll, prefer: preferFirst, held: map[string]bool{}, signal: make(chan struct{}), done: make(chan struct{})}
}

// PreferFor sets how long an incoming call rings only for the contact's owner before
// it rings for every agent; zero keeps the default.
func (d *Desk) PreferFor(window time.Duration) {
	if window > 0 {
		d.prefer = window
	}
}

// Attach gives the desk the gateway that carries its calls.
func (d *Desk) Attach(g *Gateway) { d.gw = g }

// Run follows the call records until ctx ends: it closes what an earlier run left
// open, then dials the outgoing calls agents place.
func (d *Desk) Run(ctx context.Context) {
	defer close(d.done)
	d.reconcile(ctx)
	tick := time.NewTicker(d.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.records.Changes():
		case <-tick.C:
		}
		d.wake()
		d.scan(ctx)
	}
}

// Incoming is the gateway's call handler: it records the call and waits for an agent.
func (d *Desk) Incoming(c *Call) {
	key := "voice:" + c.ID
	id, err := d.submit("create", "", map[string]any{"number": c.Caller}, key+":create")
	if err != nil {
		c.Unavailable(context.Background())
		return
	}
	d.tryHold(id)
	defer d.release(id)
	receive := map[string]any{"line": c.Trunk}
	party := d.contact(c.Caller)
	party.into(receive)
	if party.Owner != "" {
		receive["preferred"] = party.Owner
	}
	if _, err := d.submit("receive", id, receive, key+":receive"); err != nil {
		c.Unavailable(context.Background())
		<-c.Done()
		d.submit("fail", id, map[string]any{"failure": "The call could not be recorded."}, key+":fail")
		return
	}
	var widen <-chan time.Time
	if party.Owner != "" {
		timer := time.NewTimer(d.prefer)
		defer timer.Stop()
		widen = timer.C
	}
	for {
		wake := d.changed()
		select {
		case <-c.Done():
			d.unanswered(id, key, c.Cause())
			return
		case <-widen:
			widen = nil
			if rec, err := d.read(id); err == nil && rec.Offer == "" && !rec.Ended {
				d.submit("widen", id, map[string]any{}, key+":widen")
			}
			continue
		case <-wake:
		}
		rec, err := d.read(id)
		if err != nil {
			continue
		}
		if rec.Ended {
			c.Reject(context.Background())
			continue
		}
		if rec.Offer == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), recordWait)
		answer, err := c.Connect(ctx, []byte(rec.Offer))
		cancel()
		if errors.Is(err, ErrDecided) {
			continue
		}
		if err != nil {
			d.log.Warn("the browser's session cannot carry the call", "record", id, "error", err)
			c.Unavailable(context.Background())
			<-c.Done()
			d.submit("fail", id, map[string]any{"failure": "The browser's audio could not be connected."}, key+":fail")
			return
		}
		d.connect(c, id, key, map[string]any{"agent": rec.Agent, "answer": string(answer)})
		return
	}
}

func (d *Desk) outgoing(r Record) {
	defer d.release(r.ID)
	key := "voice:" + r.ID
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dialing := make(chan struct{})
	go func() {
		for {
			wake := d.changed()
			select {
			case <-dialing:
				return
			case <-wake:
			}
			if rec, err := d.read(r.ID); err == nil && rec.Ended {
				cancel()
				return
			}
		}
	}()
	call, answer, err := d.dial(ctx, r)
	close(dialing)
	if err != nil {
		d.dialFailed(r.ID, key, err)
		return
	}
	connect := map[string]any{"agent": r.Agent, "line": call.Trunk, "answer": string(answer)}
	d.contact(r.Number).into(connect)
	d.connect(call, r.ID, key, connect)
}

func (d *Desk) dial(ctx context.Context, r Record) (*Call, []byte, error) {
	err := ErrNotRegistered
	for _, reg := range d.gw.Registrations() {
		call, answer, dialErr := d.gw.DialBrowser(ctx, DialRequest{Trunk: reg.Trunk, Number: r.Number}, []byte(r.Offer))
		if !errors.Is(dialErr, ErrBusy) {
			return call, answer, dialErr
		}
		err = dialErr
	}
	return nil, nil, err
}

func (d *Desk) connect(c *Call, id, key string, payload map[string]any) {
	if _, err := d.submit("connect", id, payload, key+":connect"); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), recordWait)
		c.Hangup(ctx)
		cancel()
		c.end(Failed)
		failure := map[string]any{"failure": "The platform could not record the connection."}
		for {
			_, err := d.submit("fail", id, failure, key+":fail")
			var refused *Refused
			if err == nil || errors.As(err, &refused) {
				return
			}
			select {
			case <-d.done:
				return
			case <-time.After(d.poll):
			}
		}
	}
	d.talk(c, id, key)
}

func (d *Desk) talk(c *Call, id, key string) {
	start := d.now()
	for {
		wake := d.changed()
		select {
		case <-c.Done():
			seconds := int(d.now().Sub(start).Round(time.Second) / time.Second)
			d.submit("finish", id, map[string]any{"seconds": seconds}, key+":finish")
			return
		case <-wake:
		}
		if rec, err := d.read(id); err == nil && rec.Ended && c.State() == Connected {
			ctx, cancel := context.WithTimeout(context.Background(), recordWait)
			c.Hangup(ctx)
			cancel()
		}
	}
}

func (d *Desk) unanswered(id, key string, cause Cause) {
	reason := map[Cause]string{Canceled: "canceled", Unanswered: "unanswered", Rejected: "rejected"}[cause]
	if reason == "" {
		d.submit("fail", id, map[string]any{"failure": "The phone line dropped the call."}, key+":fail")
		return
	}
	d.submit("miss", id, map[string]any{"reason": reason}, key+":miss")
}

func (d *Desk) dialFailed(id, key string, err error) {
	var refused *DialError
	switch {
	case errors.As(err, &refused) && refused.Status == sip.StatusBusyHere, errors.Is(err, ErrBusy):
		d.submit("miss", id, map[string]any{"reason": "busy"}, key+":miss")
	case errors.As(err, &refused) && refusalCause(refused.Status) == Rejected:
		d.submit("miss", id, map[string]any{"reason": "rejected"}, key+":miss")
	case errors.As(err, &refused) && refusalCause(refused.Status) == Unanswered, errors.Is(err, ErrUnanswered):
		d.submit("miss", id, map[string]any{"reason": "unanswered"}, key+":miss")
	case errors.Is(err, context.Canceled):
		d.submit("miss", id, map[string]any{"reason": "canceled"}, key+":miss")
	case errors.Is(err, ErrNotRegistered):
		d.submit("fail", id, map[string]any{"failure": "No phone line is registered with the voice gateway."}, key+":fail")
	case errors.Is(err, ErrNumber):
		d.submit("fail", id, map[string]any{"failure": "This number cannot be dialed."}, key+":fail")
	default:
		d.submit("fail", id, map[string]any{"failure": err.Error()}, key+":fail")
	}
}

// scan dials the outgoing calls agents placed, and gives up on ones nobody placed in time.
func (d *Desk) scan(ctx context.Context) {
	if d.gw == nil {
		return
	}
	records, err := d.records.Active(ctx)
	if err != nil {
		d.log.Warn("could not read the open calls", "error", err)
		return
	}
	for _, r := range records {
		if r.Direction != "outbound" || r.State != "ringing" || r.Answer != "" || d.isHeld(r.ID) {
			continue
		}
		key := "voice:" + r.ID
		late := d.now().Sub(r.CalledAt) > dialWindow
		switch {
		case r.Ended:
			d.submit("miss", r.ID, map[string]any{"reason": "canceled"}, key+":miss")
		case late:
			d.submit("fail", r.ID, map[string]any{"failure": "The call was not placed in time."}, key+":late")
		case r.Offer != "" && d.tryHold(r.ID):
			go d.outgoing(r)
		}
	}
}

// reconcile closes the calls an earlier run of the gateway left open; their lines are gone.
func (d *Desk) reconcile(ctx context.Context) {
	records, err := d.records.Active(ctx)
	if err != nil {
		d.log.Warn("could not read the open calls", "error", err)
		return
	}
	for _, r := range records {
		if r.Direction == "inbound" || r.State == "connected" || r.Answer != "" {
			d.submit("fail", r.ID, map[string]any{"failure": "The voice gateway restarted during the call."}, "voice:"+r.ID+":restart")
		}
	}
}

func (d *Desk) submit(action, id string, payload map[string]any, key string) (string, error) {
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), recordWait)
		var got string
		got, err = d.records.Submit(ctx, "phone.call."+action, id, payload, key)
		cancel()
		if err == nil {
			return got, nil
		}
		var refused *Refused
		if errors.As(err, &refused) {
			break
		}
		time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
	}
	d.log.Warn("call record not written", "action", action, "record", id, "error", err)
	return "", err
}

func (d *Desk) read(id string) (Record, error) {
	ctx, cancel := context.WithTimeout(context.Background(), recordWait)
	defer cancel()
	return d.records.Read(ctx, id)
}

func (p Party) into(payload map[string]any) {
	if p.Contact == "" {
		return
	}
	payload["contact"] = p.Contact
	if p.Organization != "" {
		payload["organization"] = p.Organization
	}
}

func (d *Desk) contact(number string) Party {
	ctx, cancel := context.WithTimeout(context.Background(), recordWait)
	defer cancel()
	return d.records.Contact(ctx, number)
}

func (d *Desk) changed() <-chan struct{} {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.signal
}

func (d *Desk) wake() {
	d.mu.Lock()
	defer d.mu.Unlock()
	close(d.signal)
	d.signal = make(chan struct{})
}

func (d *Desk) tryHold(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.held[id] {
		return false
	}
	d.held[id] = true
	return true
}

func (d *Desk) isHeld(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.held[id]
}

func (d *Desk) release(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.held, id)
}
