package gateway

import (
	"context"
	"errors"
	"sync"

	"github.com/emiago/sipgo"
	"github.com/emiago/sipgo/sip"
)

type State string

const (
	Ringing   State = "ringing"
	Connected State = "connected"
	Ended     State = "ended"
)

type Cause string

const (
	Completed  Cause = "completed"
	Canceled   Cause = "canceled"
	Unanswered Cause = "unanswered"
	Rejected   Cause = "rejected"
	Failed     Cause = "failed"
)

var (
	ErrDecided      = errors.New("voice gateway: the call was already answered or ended")
	ErrNotConnected = errors.New("voice gateway: the call is not connected")
	ErrNoAnswer     = errors.New("voice gateway: answering needs a session description")
)

type Call struct {
	ID     string
	Trunk  string
	Caller string
	Name   string
	Dialed string
	Offer  []byte

	session    *sipgo.DialogServerSession
	decided    chan decision
	settled    chan struct{}
	done       chan struct{}
	settleOnce sync.Once
	endOnce    sync.Once
	mu         sync.Mutex
	state      State
	cause      Cause
}

type decision struct {
	answer []byte
	code   int
	reason string
	cause  Cause
	result chan error
}

func (c *Call) Answer(ctx context.Context, sdp []byte) error {
	if len(sdp) == 0 {
		return ErrNoAnswer
	}
	return c.decide(ctx, decision{answer: sdp})
}

func (c *Call) Reject(ctx context.Context) error {
	return c.decide(ctx, decision{code: sip.StatusBusyHere, reason: "Busy Here", cause: Rejected})
}

func (c *Call) Unavailable(ctx context.Context) error {
	return c.decide(ctx, decision{code: sip.StatusTemporarilyUnavailable, reason: "Temporarily Unavailable", cause: Unanswered})
}

func (c *Call) Hangup(ctx context.Context) error {
	if c.State() != Connected {
		return ErrNotConnected
	}
	return c.session.Bye(ctx)
}

func (c *Call) Done() <-chan struct{} { return c.done }

func (c *Call) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state
}

func (c *Call) Cause() Cause {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cause
}

func (c *Call) decide(ctx context.Context, d decision) error {
	d.result = make(chan error, 1)
	select {
	case c.decided <- d:
	case <-c.settled:
		return ErrDecided
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-d.result:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *Call) settle() { c.settleOnce.Do(func() { close(c.settled) }) }

func (c *Call) connect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Ringing {
		c.state = Connected
	}
}

func (c *Call) end(cause Cause) {
	c.endOnce.Do(func() {
		c.mu.Lock()
		c.state, c.cause = Ended, cause
		c.mu.Unlock()
		close(c.done)
	})
}
