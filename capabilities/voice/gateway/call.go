package gateway

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"sync"

	"github.com/emiago/sipgo/sip"
)

type Direction string

const (
	Inbound  Direction = "inbound"
	Outbound Direction = "outbound"
)

type State string

const (
	Dialing   State = "dialing"
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
	ErrNoSession    = errors.New("voice gateway: a session description is required")
)

type Call struct {
	ID        string
	Direction Direction
	Trunk     string
	Caller    string
	Name      string
	Dialed    string
	Remote    []byte

	media      net.IP
	log        *slog.Logger
	bye        func(context.Context) error
	decided    chan decision
	settled    chan struct{}
	done       chan struct{}
	onEnd      func()
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

func newCall(direction Direction, trunk string, state State) *Call {
	return &Call{Direction: direction, Trunk: trunk, decided: make(chan decision), settled: make(chan struct{}), done: make(chan struct{}), state: state}
}

func (c *Call) Answer(ctx context.Context, sdp []byte) error {
	if len(sdp) == 0 {
		return ErrNoSession
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
	return c.bye(ctx)
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

func (c *Call) ring() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state == Dialing {
		c.state = Ringing
	}
}

func (c *Call) connect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.state != Ended {
		c.state = Connected
	}
}

func (c *Call) end(cause Cause) {
	c.endOnce.Do(func() {
		c.mu.Lock()
		c.state, c.cause = Ended, cause
		c.mu.Unlock()
		if c.onEnd != nil {
			c.onEnd()
		}
		close(c.done)
	})
}
